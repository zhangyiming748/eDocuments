// Command edocs 是纸质档案数字化服务：
//
//	edocs index  扫描图片目录 → 本地 Tesseract OCR → JSON 缓存 → Elasticsearch
//	edocs serve  启动局域网搜索网页
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"edocs/internal/cache"
	"edocs/internal/model"
	"edocs/internal/ocr"
	"edocs/internal/scanner"
	"edocs/internal/store"
	"edocs/internal/web"
)

const lowConfidenceThreshold = 70.0

func main() {
	log.SetFlags(log.LstdFlags)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "index":
		err = runIndex(os.Args[2:])
	case "serve":
		err = runServe(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		log.Fatalf("错误: %v", err)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `edocs — 纸质档案数字化

用法:
  edocs index [参数]   扫描/OCR/入库（支持增量）
  edocs serve [参数]   启动搜索网页

通用参数（也可用环境变量 EDOCS_ES / EDOCS_CACHE / EDOCS_SCAN_ROOT）:
  --es       Elasticsearch 地址 (默认 http://elasticsearch:9200)
  --cache    OCR JSON 缓存目录 (默认 ./data/cache)
`)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// ------------------------------ index ------------------------------

func runIndex(args []string) error {
	fs := flag.NewFlagSet("index", flag.ExitOnError)
	root := fs.String("root", env("EDOCS_SCAN_ROOT", "./data/scan"), "扫描件根目录（每个叶子目录为一份文档）")
	cacheDir := fs.String("cache", env("EDOCS_CACHE", "./data/cache"), "OCR JSON 缓存目录")
	esAddr := fs.String("es", env("EDOCS_ES", "http://elasticsearch:9200"), "Elasticsearch 地址")
	workers := fs.Int("workers", 2, "OCR 并发数")
	langs := fs.String("langs", "chi_sim+eng", "tesseract 语言")
	forceOCR := fs.Bool("force-ocr", false, "忽略 JSON 缓存，全部重新 OCR")
	reindex := fs.Bool("reindex", false, "不扫描不 OCR，只从 JSON 缓存重建 ES 索引")
	recreate := fs.Bool("recreate", false, "重建 ES 索引（先删除）")
	_ = fs.Parse(args)

	ch, err := cache.New(filepath.Clean(*cacheDir))
	if err != nil {
		return err
	}
	st, err := store.NewElastic(*esAddr, 30*time.Second)
	if err != nil {
		return err
	}
	ctx := context.Background()
	if *recreate {
		if err := st.RecreateIndex(ctx); err != nil {
			return err
		}
		log.Println("已重建 Elasticsearch 索引")
	} else if err := st.EnsureIndex(ctx); err != nil {
		return err
	}

	if *reindex {
		return rebuildFromCache(ctx, ch, st)
	}
	return scanAndIndex(ctx, *root, ch, st, *workers, *langs, *forceOCR)
}

func scanAndIndex(ctx context.Context, root string, ch *cache.Cache, st store.Store, workers int, langs string, force bool) error {
	engine, err := ocr.New(ocr.WithWorkers(workers), ocr.WithLangs(langs))
	if err != nil {
		return err
	}

	docs, scanWarnings, err := scanner.Scan(root)
	if err != nil {
		return err
	}
	for _, w := range scanWarnings {
		log.Printf("[扫描] %s", w)
	}
	log.Printf("[扫描] 共发现 %d 份文档", len(docs))

	var created, updated, unchanged int
	var failedPages, lowConfPages []string

	current := make(map[string]bool, len(docs))
	for _, src := range docs {
		current[src.ID] = true
		for _, w := range src.Warnings {
			log.Printf("[扫描] %s", w)
		}

		cached, err := ch.Load(src.ID)
		if err != nil {
			return fmt.Errorf("读取缓存 %s 失败: %w", src.ID, err)
		}
		isNew := cached == nil

		// 计算需要 OCR 的页：新文档全部页；否则仅变化页或上次失败页。
		var toOCR []model.SourcePage
		resultPages := make([]model.Page, len(src.Pages))
		oldByFile := map[string]model.Page{}
		if cached != nil {
			for _, p := range cached.Pages {
				oldByFile[p.File] = p
			}
		}
		for i, sp := range src.Pages {
			if force || cached == nil {
				toOCR = append(toOCR, sp)
				continue
			}
			old, ok := oldByFile[sp.File]
			if !ok || old.Size != sp.Size || !old.Mtime.Equal(sp.Mtime) || old.Err != "" {
				toOCR = append(toOCR, sp)
			} else {
				resultPages[i] = old
			}
		}

		if len(toOCR) > 0 {
			log.Printf("[OCR] %s（%d/%d 页需识别）", src.RelativePath, len(toOCR), len(src.Pages))
			newPages := engine.Batch(ctx, toOCR)
			// 按文件名把新结果合并回完整页序列。
			np := map[string]model.Page{}
			for _, p := range newPages {
				np[p.File] = p
			}
			for i, sp := range src.Pages {
				if p, ok := np[sp.File]; ok {
					resultPages[i] = p
				}
			}
			if isNew {
				created++
			} else {
				updated++
			}
		} else {
			unchanged++
		}

		for i := range resultPages {
			if resultPages[i].Err != "" {
				failedPages = append(failedPages, fmt.Sprintf("%s/%s: %s", src.RelativePath, resultPages[i].File, resultPages[i].Err))
			} else if resultPages[i].AvgConfidence < lowConfidenceThreshold && resultPages[i].Text != "" {
				lowConfPages = append(lowConfPages, fmt.Sprintf("%s/%s（置信度 %.1f）",
					src.RelativePath, resultPages[i].File, resultPages[i].AvgConfidence))
			}
		}

		doc := &model.Document{
			ID:            src.ID,
			Title:         src.Title,
			RelativePath:  src.RelativePath,
			PageCount:     len(resultPages),
			OCRFinishedAt: time.Now(),
			Pages:         resultPages,
		}
		if err := ch.Save(doc); err != nil {
			return fmt.Errorf("写入缓存失败 %s: %w", src.ID, err)
		}
		if err := st.UpsertDocument(ctx, doc); err != nil {
			return err
		}
	}

	// 同步删除：扫描目录中已不存在、但缓存仍残留的文档。
	deleted, err := deleteMissing(ctx, ch, st, current)
	if err != nil {
		return err
	}

	log.Printf("[完成] 新增 %d，更新 %d，未变化 %d，删除 %d", created, updated, unchanged, deleted)
	if len(failedPages) > 0 {
		log.Printf("[警告] %d 页 OCR 失败（下次 index 会自动重试）:", len(failedPages))
		for _, f := range failedPages {
			log.Printf("  失败页 %s", f)
		}
	}
	if len(lowConfPages) > 0 {
		log.Printf("[提示] %d 页平均置信度低于 %.0f，建议抽查:", len(lowConfPages), lowConfidenceThreshold)
		for _, f := range lowConfPages {
			log.Printf("  低置信度 %s", f)
		}
	}
	return nil
}

// deleteMissing 删除磁盘上已消失文档的 ES 文档与 JSON 缓存。
func deleteMissing(ctx context.Context, ch *cache.Cache, st store.Store, current map[string]bool) (int, error) {
	all, err := ch.LoadAll()
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, d := range all {
		if current[d.ID] {
			continue
		}
		if err := st.DeleteDocument(ctx, d.ID); err != nil {
			return deleted, err
		}
		if err := ch.Delete(d.ID); err != nil {
			return deleted, err
		}
		log.Printf("[删除] 文档已从扫描目录移除: %s", d.RelativePath)
		deleted++
	}
	return deleted, nil
}

// rebuildFromCache 对应 --reindex：仅凭 JSON 缓存重建 ES 数据。
func rebuildFromCache(ctx context.Context, ch *cache.Cache, st store.Store) error {
	docs, err := ch.LoadAll()
	if err != nil {
		return err
	}
	log.Printf("[重建] 从缓存读取到 %d 份文档", len(docs))
	for _, d := range docs {
		if err := st.UpsertDocument(ctx, d); err != nil {
			return err
		}
	}
	log.Printf("[重建] 完成，共写入 %d 份文档", len(docs))
	return nil
}

// ------------------------------ serve ------------------------------

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	cacheDir := fs.String("cache", env("EDOCS_CACHE", "./data/cache"), "OCR JSON 缓存目录（serve 仅用于提示，不读写）")
	esAddr := fs.String("es", env("EDOCS_ES", "http://elasticsearch:9200"), "Elasticsearch 地址")
	addr := fs.String("addr", env("EDOCS_ADDR", ":8080"), "HTTP 监听地址")
	_ = fs.Parse(args)
	_ = cacheDir

	st, err := store.NewElastic(*esAddr, 30*time.Second)
	if err != nil {
		return err
	}
	if err := st.EnsureIndex(context.Background()); err != nil {
		return err
	}
	h, err := web.NewHandler(st)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           h.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		log.Printf("搜索服务启动，监听 %s（仅供局域网访问）", *addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP 服务失败: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Println("正在关闭服务…")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}
