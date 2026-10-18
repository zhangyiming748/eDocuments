// Package ocr 封装本地 Tesseract 识别：单页识别、EXIF 自动旋转、
// TSV 结果解析与置信度统计，以及固定数量 worker 的并发批处理。
package ocr

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"edocs/internal/model"
)

// Engine 通过系统 tesseract 命令执行 OCR（不使用 CGO 绑定）。
type Engine struct {
	binary  string
	langs   string
	workers int
	timeout time.Duration
}

// Option 配置 Engine。
type Option func(*Engine)

// WithLangs 设置 tesseract 语言，如 "chi_sim+eng"。
func WithLangs(l string) Option { return func(e *Engine) { e.langs = l } }

// WithWorkers 设置 OCR 并发数（Tesseract 为 CPU 密集任务，建议 1～4）。
func WithWorkers(n int) Option {
	return func(e *Engine) {
		if n > 0 {
			e.workers = n
		}
	}
}

// WithTimeout 设置单页识别超时。
func WithTimeout(d time.Duration) Option {
	return func(e *Engine) { e.timeout = d }
}

// New 检查本机 tesseract 可执行文件并构造引擎。
func New(opts ...Option) (*Engine, error) {
	bin, err := exec.LookPath("tesseract")
	if err != nil {
		return nil, errors.New("未找到 tesseract 可执行文件，请先在系统/镜像中安装 tesseract-ocr")
	}
	e := &Engine{
		binary:  bin,
		langs:   "chi_sim+eng",
		workers: 2,
		timeout: 5 * time.Minute,
	}
	for _, o := range opts {
		o(e)
	}
	return e, nil
}

// ProcessPage 识别单页图片，返回带文本与平均置信度的 Page。
// 单页失败不中断批次：错误被写入返回值的 Err 字段。
func (e *Engine) ProcessPage(ctx context.Context, src model.SourcePage) model.Page {
	p := model.Page{File: src.File, Size: src.Size, Mtime: src.Mtime}

	imgPath, cleanup, err := autoOrient(src.Path)
	if err != nil {
		p.Err = fmt.Sprintf("图像旋转预处理失败: %v", err)
		return p
	}
	defer cleanup()

	cctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	// tesseract <image> stdout -l <langs> tsv
	// tsv 配置让结果以制表符表格输出到 stdout，含每词置信度。
	cmd := exec.CommandContext(cctx, e.binary, imgPath, "stdout", "-l", e.langs, "tsv")
	out, err := cmd.Output()
	if err != nil {
		p.Err = fmt.Sprintf("tesseract 执行失败: %v", err)
		return p
	}

	text, conf := parseTSV(out)
	p.Text = text
	p.AvgConfidence = conf
	return p
}

// Batch 并发识别多页，返回顺序与输入一致的结果。
func (e *Engine) Batch(ctx context.Context, pages []model.SourcePage) []model.Page {
	results := make([]model.Page, len(pages))
	sem := make(chan struct{}, e.workers)
	var wg sync.WaitGroup

	for i, p := range pages {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, p model.SourcePage) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = e.ProcessPage(ctx, p)
		}(i, p)
	}
	wg.Wait()
	return results
}
