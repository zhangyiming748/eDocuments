// Package store 定义全文存储的抽象接口及数据传输结构。
// 当前实现为 Elasticsearch（见 es.go）；接口化是为了将来可替换为
// SQLite FTS5 等其他后端，而 OCR、缓存与 Web 层无需改动。
package store

import (
	"context"
	"html/template"

	"edocs/internal/model"
)

// Hit 是单条搜索结果。高亮字段含 <em> 标签，已视为安全 HTML。
type Hit struct {
	ID             string
	Title          string
	RelativePath   string
	PageCount      int
	HighlightTitle template.HTML
	Snippets       []template.HTML
}

// Result 是一次搜索的完整响应。
type Result struct {
	Query string
	Total int64
	From  int
	Size  int
	Hits  []Hit
}

// Store 是全文存储后端需要实现的能力。
type Store interface {
	// EnsureIndex 确保索引存在且 mapping 符合预期。
	EnsureIndex(ctx context.Context) error
	// RecreateIndex 删除并重建索引（--reindex 使用）。
	RecreateIndex(ctx context.Context) error
	// UpsertDocument 按文档 ID 写入（存在则覆盖）。
	UpsertDocument(ctx context.Context, doc *model.Document) error
	// DeleteDocument 删除一份文档；不存在不视为错误。
	DeleteDocument(ctx context.Context, id string) error
	// Search 执行全文检索，from 为偏移量、size 为每页条数。
	Search(ctx context.Context, query string, from, size int) (*Result, error)
	// Close 释放底层连接。
	Close() error
}
