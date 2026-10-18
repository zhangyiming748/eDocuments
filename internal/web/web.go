// Package web 提供搜索页面与查询接口（net/http + html/template，无前端框架）。
package web

import (
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"

	"edocs/internal/store"
)

const pageSize = 20

// Handler 持有搜索所需依赖与解析后的模板。
type Handler struct {
	store store.Store
	tmpl  *template.Template
}

// NewHandler 解析内嵌模板并构造 Handler。
func NewHandler(s store.Store) (*Handler, error) {
	tmpl, err := template.New("pages").Parse(pageTemplate)
	if err != nil {
		return nil, err
	}
	return &Handler{store: s, tmpl: tmpl}, nil
}

// Routes 注册路由。
func (h *Handler) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", h.index)
	mux.HandleFunc("/search", h.search)
	return mux
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	h.render(w, http.StatusOK, &pageData{})
}

func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	data := &pageData{Query: q}

	if q != "" {
		page := 1
		if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
			page = p
		}
		from := (page - 1) * pageSize

		res, err := h.store.Search(r.Context(), q, from, pageSize)
		if err != nil {
			log.Printf("搜索失败 q=%q: %v", q, err)
			h.render(w, http.StatusInternalServerError, &pageData{Query: q, ErrMsg: "搜索服务暂时不可用，请稍后重试"})
			return
		}
		data.Result = res
		fillPaging(data, page, res.Total)
	}
	h.render(w, http.StatusOK, data)
}

func (h *Handler) render(w http.ResponseWriter, status int, data *pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := h.tmpl.Execute(w, data); err != nil {
		log.Printf("模板渲染失败: %v", err)
	}
}

// pageData 是模板渲染数据。
type pageData struct {
	Query    string
	Result   *store.Result
	ErrMsg   string
	Page     int
	Pages    []int
	PrevPage int
	NextPage int
	HasPrev  bool
	HasNext  bool
}

func fillPaging(d *pageData, cur int, total int64) {
	d.Page = cur
	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
	if totalPages <= 1 {
		return
	}
	start := cur - 4
	if start < 1 {
		start = 1
	}
	end := start + 9
	if end > totalPages {
		end = totalPages
	}
	if end-start < 9 {
		start = end - 9
		if start < 1 {
			start = 1
		}
	}
	for p := start; p <= end; p++ {
		d.Pages = append(d.Pages, p)
	}
	d.HasPrev = cur > 1
	if d.HasPrev {
		d.PrevPage = cur - 1
	}
	d.HasNext = cur < totalPages
	if d.HasNext {
		d.NextPage = cur + 1
	}
}
