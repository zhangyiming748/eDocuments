// Package cache 负责 OCR 结果在本地磁盘的 JSON 持久化（每份文档一个文件）。
//
// 缓存有两个作用：
//  1. 增量索引：根据每页的 size/mtime 判断是否需要重新 OCR；
//  2. 备份重建：ES 数据丢失时仅凭缓存目录即可完整重建索引。
package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"edocs/internal/model"
)

// Cache 是 OCR JSON 缓存目录。
type Cache struct {
	dir string
}

// New 创建/打开缓存目录（不存在则创建）。
func New(dir string) (*Cache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建缓存目录失败: %w", err)
	}
	return &Cache{dir: dir}, nil
}

func (c *Cache) path(id string) string {
	return filepath.Join(c.dir, id+".json")
}

// Load 读取一份文档的缓存；文件不存在时返回 (nil, nil)。
func (c *Cache) Load(id string) (*model.Document, error) {
	data, err := os.ReadFile(c.path(id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var doc model.Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("解析缓存 %s 失败: %w", id, err)
	}
	return &doc, nil
}

// Save 原子写入一份文档缓存（先写临时文件再 rename，防止中途崩溃产生半截 JSON）。
func (c *Cache) Save(doc *model.Document) error {
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	final := c.path(doc.ID)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

// Delete 删除一份文档缓存（文档从扫描目录消失时调用）；文件不存在不视为错误。
func (c *Cache) Delete(id string) error {
	err := os.Remove(c.path(id))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// LoadAll 读取缓存目录中的全部文档（用于 --reindex 重建 ES）。
func (c *Cache) LoadAll() ([]*model.Document, error) {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return nil, err
	}
	var docs []*model.Document
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		id := e.Name()[:len(e.Name())-len(".json")]
		doc, err := c.Load(id)
		if err != nil {
			return nil, err
		}
		if doc != nil {
			docs = append(docs, doc)
		}
	}
	return docs, nil
}
