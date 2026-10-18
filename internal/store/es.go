package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/elastic/elastic-transport-go/v8/elastictransport"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"

	"edocs/internal/model"
)

// ElasticStore 是 Store 接口的 Elasticsearch 实现。
type ElasticStore struct {
	client *elasticsearch.Client
	index  string
}

// NewElastic 构造 ES 客户端并等待集群可用（单节点首次启动较慢）。
func NewElastic(address string, timeout time.Duration) (*ElasticStore, error) {
	cfg := elasticsearch.Config{
		Addresses: []string{address},
		// 纯内网单节点：关闭安全认证。
		Transport: &http.Transport{
			MaxIdleConnsPerHost:   10,
			ResponseHeaderTimeout: timeout,
		},
		Logger: &elastictransport.TextLogger{Output: io.Discard}, // 出错时另取响应体打印
	}
	client, err := elasticsearch.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("创建 ES 客户端失败: %w", err)
	}
	s := &ElasticStore{client: client, index: "documents"}
	if err := s.waitReady(60 * time.Second); err != nil {
		return nil, err
	}
	return s, nil
}

// waitReady 轮询集群健康接口直到 yellow/green（单节点只需 yellow）。
func (s *ElasticStore) waitReady(max time.Duration) error {
	deadline := time.Now().Add(max)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := s.client.Cluster.Health(s.client.Cluster.Health.WithContext(context.Background()),
			s.client.Cluster.Health.WithWaitForStatus("yellow"),
			s.client.Cluster.Health.WithTimeout(5*time.Second))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("ES 健康检查状态码 %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("等待 Elasticsearch 就绪超时: %w", lastErr)
}

// indexMapping 定义 documents 索引：IK 中文分词，标题加权。
const indexMapping = `{
  "settings": {
    "number_of_shards": 1,
    "number_of_replicas": 0
  },
  "mappings": {
    "properties": {
      "title": {
        "type": "text",
        "analyzer": "ik_max_word",
        "search_analyzer": "ik_smart",
        "fields": { "raw": { "type": "keyword" } }
      },
      "content": {
        "type": "text",
        "analyzer": "ik_max_word",
        "search_analyzer": "ik_smart"
      },
      "relative_path": { "type": "keyword" },
      "page_count": { "type": "integer" },
      "indexed_at": { "type": "date" }
    }
  }
}`

// EnsureIndex 在索引不存在时按既定 mapping 创建。
func (s *ElasticStore) EnsureIndex(ctx context.Context) error {
	resp, err := s.client.Indices.Exists([]string{s.index}, s.client.Indices.Exists.WithContext(ctx))
	if err != nil {
		return err
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return nil // 已存在，保留现有 mapping
	case http.StatusNotFound:
		return s.createIndex(ctx)
	default:
		return fmt.Errorf("检查索引存在性失败: %s", resp.Status())
	}
}

// RecreateIndex 删除并重建索引。
func (s *ElasticStore) RecreateIndex(ctx context.Context) error {
	resp, err := s.client.Indices.Delete([]string{s.index}, s.client.Indices.Delete.WithContext(ctx),
		s.client.Indices.Delete.WithIgnoreUnavailable(true))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return s.createIndex(ctx)
}

func (s *ElasticStore) createIndex(ctx context.Context) error {
	resp, err := s.client.Indices.Create(s.index,
		s.client.Indices.Create.WithContext(ctx),
		s.client.Indices.Create.WithBody(strings.NewReader(indexMapping)),
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.IsError() {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("创建索引失败: %s %s", resp.Status(), strings.TrimSpace(string(body)))
	}
	return nil
}

// indexedDoc 是写入 ES 的扁平化文档。
type indexedDoc struct {
	Title        string    `json:"title"`
	Content      string    `json:"content"`
	RelativePath string    `json:"relative_path"`
	PageCount    int       `json:"page_count"`
	IndexedAt    time.Time `json:"indexed_at"`
}

// UpsertDocument 按文档 _id 写入（覆盖），重复索引不产生重复文档。
func (s *ElasticStore) UpsertDocument(ctx context.Context, doc *model.Document) error {
	body := indexedDoc{
		Title:        doc.Title,
		Content:      doc.Content(),
		RelativePath: doc.RelativePath,
		PageCount:    doc.PageCount,
		IndexedAt:    time.Now(),
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req := esapi.IndexRequest{
		Index:      s.index,
		DocumentID: doc.ID,
		Body:       bytes.NewReader(raw),
	}
	resp, err := req.Do(ctx, s.client)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.IsError() {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("写入文档 %s 失败: %s %s", doc.ID, resp.Status(), strings.TrimSpace(string(b)))
	}
	return nil
}

// DeleteDocument 删除文档，不存在时静默成功。
func (s *ElasticStore) DeleteDocument(ctx context.Context, id string) error {
	req := esapi.DeleteRequest{
		Index:      s.index,
		DocumentID: id,
	}
	resp, err := req.Do(ctx, s.client)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.IsError() && resp.StatusCode != http.StatusNotFound {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("删除文档 %s 失败: %s %s", id, resp.Status(), strings.TrimSpace(string(b)))
	}
	return nil
}

// buildSearchBody 组装查询体。两阶段检索：
//   - 精确模式：simple_query_string 词/短语匹配；
//   - 容错模式：multi_match + fuzziness，召回 OCR 错字或用户输入差一字的结果。
//
// 仅当精确模式命中为 0 时才退到容错模式，避免精确命中时混入模糊噪声。
// fuzziness 用 AUTO:2,5：单字词不容错，2～4 字词容忍 1 字之差，5 字以上容忍 2 字。
func buildSearchBody(query string, fuzzy bool) []byte {
	var q map[string]any
	if fuzzy {
		q = map[string]any{
			"multi_match": map[string]any{
				"query":          query,
				"fields":         []string{"title^3", "content"},
				"fuzziness":      "AUTO:2,5",
				"prefix_length":  0,
				"max_expansions": 50,
			},
		}
	} else {
		q = map[string]any{
			"simple_query_string": map[string]any{
				"query":  query,
				"fields": []string{"title^3", "content"},
			},
		}
	}
	body := map[string]any{
		"query": q,
		"highlight": map[string]any{
			"fields": map[string]any{
				"title": map[string]any{
					"fragment_size":       80,
					"number_of_fragments": 1,
				},
				"content": map[string]any{
					"fragment_size":       120,
					"number_of_fragments": 3,
				},
			},
		},
	}
	raw, _ := json.Marshal(body)
	return raw
}

// Search 执行全文检索：先精确，零结果时自动容错兜底。
func (s *ElasticStore) Search(ctx context.Context, query string, from, size int) (*Result, error) {
	if size <= 0 || size > 100 {
		size = 20
	}
	if from < 0 {
		from = 0
	}

	result, err := s.executeSearch(ctx, query, from, size, buildSearchBody(query, false))
	if err != nil {
		return nil, err
	}
	if result.Total == 0 {
		// 精确无结果：用容错模式再试一次（翻页时行为一致，因为精确总数恒为 0）。
		return s.executeSearch(ctx, query, from, size, buildSearchBody(query, true))
	}
	return result, nil
}

func (s *ElasticStore) executeSearch(ctx context.Context, query string, from, size int, raw []byte) (*Result, error) {
	req := esapi.SearchRequest{
		Index:          []string{s.index},
		Body:           bytes.NewReader(raw),
		From:           &from,
		Size:           &size,
		TrackTotalHits: true,
	}
	resp, err := req.Do(ctx, s.client)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.IsError() {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("搜索失败: %s %s", resp.Status(), strings.TrimSpace(string(b)))
	}

	var parsed struct {
		Hits struct {
			Total struct {
				Value int64 `json:"value"`
			} `json:"total"`
			Hits []struct {
				ID        string              `json:"_id"`
				Source    json.RawMessage     `json:"_source"`
				Highlight map[string][]string `json:"highlight"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}

	result := &Result{Query: query, Total: parsed.Hits.Total.Value, From: from, Size: size}
	for _, h := range parsed.Hits.Hits {
		var src indexedDoc
		if err := json.Unmarshal(h.Source, &src); err != nil {
			log.Printf("解析搜索结果 %s 失败: %v", h.ID, err)
			continue
		}
		hit := Hit{
			ID:           h.ID,
			Title:        src.Title,
			RelativePath: src.RelativePath,
			PageCount:    src.PageCount,
		}
		if hl := h.Highlight["title"]; len(hl) > 0 {
			hit.HighlightTitle = template.HTML(strings.Join(hl, " "))
		}
		if hl := h.Highlight["content"]; len(hl) > 0 {
			for _, snip := range hl {
				hit.Snippets = append(hit.Snippets, template.HTML(snip))
			}
		}
		result.Hits = append(result.Hits, hit)
	}
	return result, nil
}

// Close 当前 ES 客户端无需显式关闭，保留以满足接口。
func (s *ElasticStore) Close() error { return nil }
