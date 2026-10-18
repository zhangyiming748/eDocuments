// Package model 定义扫描件、OCR 结果与搜索相关的共享数据结构。
package model

import "time"

// SourcePage 是磁盘上一张待识别的扫描图片（尚未 OCR）。
type SourcePage struct {
	Path  string    // 图片绝对路径
	File  string    // 文件名（页序键），如 scan_0001.jpg
	Size  int64     // 文件字节数
	Mtime time.Time // 修改时间
}

// SourceDocument 是扫描根目录下发现的一份文档（一个叶子目录）。
type SourceDocument struct {
	ID           string       // 相对路径的 SHA-256 十六进制
	Title        string       // 叶子目录名
	RelativePath string       // 相对根目录的路径（/ 分隔）
	AbsDir       string       // 叶子目录绝对路径
	Pages        []SourcePage // 已按文件名升序排好
	Warnings     []string     // 该文档发现过程中的告警（如混入非图片、编号异常）
}

// Page 是单页图片的 OCR 结果，也是 JSON 缓存的逐页结构。
type Page struct {
	File          string    `json:"file"`
	Size          int64     `json:"size"`
	Mtime         time.Time `json:"mtime"`
	Text          string    `json:"text"`
	AvgConfidence float64   `json:"avg_confidence"`
	Err           string    `json:"err,omitempty"` // 该页 OCR 失败时的错误信息
}

// Document 是一份档案的完整 OCR 结果，缓存中每份文档对应一个 JSON 文件。
type Document struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	RelativePath  string    `json:"relative_path"`
	PageCount     int       `json:"page_count"`
	OCRFinishedAt time.Time `json:"ocr_finished_at"`
	Pages         []Page    `json:"pages"`
}

// Content 返回按页序拼接的全文，页间以换行分隔，用于写入全文索引。
func (d *Document) Content() string {
	var b []byte
	for i := range d.Pages {
		if i > 0 {
			b = append(b, '\n')
		}
		b = append(b, d.Pages[i].Text...)
	}
	return string(b)
}
