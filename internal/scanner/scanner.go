// Package scanner 递归遍历扫描根目录，发现叶子目录（每份文档）及其图片页。
package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"edocs/internal/model"
)

// imageExts 为受支持的扫描图片扩展名（一律小写比较）。
var imageExts = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".tif":  true,
	".tiff": true,
}

var digitSeq = regexp.MustCompile(`\d+`)

// Scan 递归扫描 root，返回全部文档（叶子目录）。
//
// 叶子目录：目录树下不含任何子目录的目录。空目录、只含非图片文件的目录
// 会被跳过并以告警形式返回；非图片文件忽略并告警。
func Scan(root string) ([]model.SourceDocument, []string, error) {
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("扫描根目录不是目录: %s", root)
	}

	// hasSubDir 记录每个目录下是否存在子目录。
	hasSubDir := map[string]bool{}
	var allDirs []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		allDirs = append(allDirs, path)
		if path != root {
			hasSubDir[filepath.Dir(path)] = true
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// 保证遍历顺序稳定，输出可预期。
	sort.Strings(allDirs)

	var docs []model.SourceDocument
	var warnings []string

	for _, dir := range allDirs {
		if hasSubDir[dir] {
			continue // 非叶子目录
		}
		doc, warn, skip, err := buildDocument(root, dir)
		if err != nil {
			return nil, nil, err
		}
		warnings = append(warnings, warn...)
		if skip {
			continue
		}
		docs = append(docs, doc)
	}
	return docs, warnings, nil
}

// buildDocument 解析单个叶子目录，skip=true 表示该目录应跳过。
func buildDocument(root, dir string) (doc model.SourceDocument, warnings []string, skip bool, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return doc, nil, false, err
	}

	var images []string
	var nonImages []string
	for _, e := range entries {
		if e.IsDir() {
			// 叶子目录判定之外的竞态保护：新出现的子目录直接跳过本目录。
			return doc, nil, true, nil
		}
		name := e.Name()
		if imageExts[strings.ToLower(filepath.Ext(name))] {
			images = append(images, name)
		} else {
			nonImages = append(nonImages, name)
		}
	}

	if len(images) == 0 {
		warnings = append(warnings, fmt.Sprintf("跳过空/无图片目录: %s", relSlash(root, dir)))
		return doc, warnings, true, nil
	}

	// 定宽零填充命名下，字典序即数字序。
	sort.Strings(images)
	checkPageSequence(images, &warnings, relSlash(root, dir))
	if len(nonImages) > 0 {
		sort.Strings(nonImages)
		shown := nonImages
		if len(shown) > 5 {
			shown = shown[:5]
		}
		warnings = append(warnings, fmt.Sprintf("目录 %s 忽略 %d 个非图片文件: %s",
			relSlash(root, dir), len(nonImages), strings.Join(shown, ", ")))
	}

	rel := relSlash(root, dir)
	doc = model.SourceDocument{
		ID:           docID(rel),
		Title:        filepath.Base(dir),
		RelativePath: rel,
		AbsDir:       dir,
		Warnings:     warnings,
	}
	for _, name := range images {
		full := filepath.Join(dir, name)
		in, err := os.Stat(full)
		if err != nil {
			return doc, nil, false, fmt.Errorf("读取图片信息失败 %s: %w", full, err)
		}
		doc.Pages = append(doc.Pages, model.SourcePage{
			Path:  full,
			File:  name,
			Size:  in.Size(),
			Mtime: in.ModTime(),
		})
	}
	return doc, warnings, false, nil
}

// checkPageSequence 解析文件名中的数字，若出现重复或非递增则告警，
// 防止两套命名混排导致页序静默错乱。页序本身仍按文件名字典序。
func checkPageSequence(names []string, warnings *[]string, rel string) {
	var prev int64 = -1
	seen := map[int64]bool{}
	for _, name := range names {
		m := digitSeq.FindString(name)
		if m == "" {
			*warnings = append(*warnings, fmt.Sprintf("目录 %s 中文件名无数字序号: %s", rel, name))
			continue
		}
		n, err := strconv.ParseInt(m, 10, 64)
		if err != nil {
			continue
		}
		if seen[n] {
			*warnings = append(*warnings, fmt.Sprintf("目录 %s 中页序号重复: %s", rel, name))
		}
		seen[n] = true
		if prev >= 0 && n <= prev {
			*warnings = append(*warnings, fmt.Sprintf("目录 %s 中页序号非递增，请人工核对: %s", rel, name))
		}
		prev = n
	}
}

// docID 以相对路径的 SHA-256 作为文档全局唯一 ID。
func docID(relPath string) string {
	sum := sha256.Sum256([]byte(relPath))
	return hex.EncodeToString(sum[:])
}

func relSlash(root, dir string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return filepath.ToSlash(dir)
	}
	return filepath.ToSlash(rel)
}
