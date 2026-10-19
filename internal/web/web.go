// Package web 提供搜索页面与查询接口（net/http + html/template，无前端框架）。
package web

import (
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"edocs/internal/scanner"
	"edocs/internal/store"
)

const pageSize = 20

// Handler 持有搜索所需依赖与解析后的模板。
type Handler struct {
	store    store.Store
	tmpl     *template.Template
	scanRoot string // 扫描件根目录，用于展示原始扫描件
}

// NewHandler 解析内嵌模板并构造 Handler；scanRoot 为扫描件根目录。
func NewHandler(s store.Store, scanRoot string) (*Handler, error) {
	tmpl, err := template.New("pages").Parse(pageTemplate)
	if err != nil {
		return nil, err
	}
	return &Handler{store: s, tmpl: tmpl, scanRoot: filepath.Clean(scanRoot)}, nil
}

// Routes 注册路由。
func (h *Handler) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", h.index)
	mux.HandleFunc("/search", h.search)
	mux.HandleFunc("/doc", h.doc)
	mux.HandleFunc("/doc/image", h.docImage)
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

// doc 渲染档案详情页：展示第 n 页的原始扫描件。
func (h *Handler) doc(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("p")
	dirPath, ok := h.docDir(rel)
	if !ok {
		h.notFound(w, "找不到这份档案，它可能已被移出扫描目录。")
		return
	}
	pages, err := scanner.PageFiles(dirPath)
	if err != nil || len(pages) == 0 {
		h.notFound(w, "这份档案下没有可显示的扫描件。")
		return
	}

	page := 1
	if n, err := strconv.Atoi(r.URL.Query().Get("n")); err == nil && n > 0 {
		page = n
	}
	// 越界一律夹取到有效范围，手改 URL 不会报错。
	if page > len(pages) {
		page = len(pages)
	}

	d := &docView{
		Title:   filepath.Base(dirPath),
		RelPath: rel,
		Page:    page,
		Total:   len(pages),
		File:    pages[page-1],
	}
	if d.HasPrev = page > 1; d.HasPrev {
		d.PrevPage = page - 1
	}
	if d.HasNext = page < len(pages); d.HasNext {
		d.NextPage = page + 1
	}
	h.render(w, http.StatusOK, &pageData{Doc: d})
}

// docImage 输出某一份档案中某一页的原始图片字节。
func (h *Handler) docImage(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("p")
	name := r.URL.Query().Get("f")
	dirPath, ok := h.docDir(rel)
	if !ok {
		h.notFound(w, "找不到这个文件。")
		return
	}
	// 文件名必须是该目录实际列举出的图片之一，不直接信任 URL 入参。
	pages, err := scanner.PageFiles(dirPath)
	if err != nil {
		h.notFound(w, "找不到这个文件。")
		return
	}
	valid := false
	for _, p := range pages {
		if p == name {
			valid = true
			break
		}
	}
	if !valid {
		h.notFound(w, "找不到这个文件。")
		return
	}

	// 用 os.OpenRoot 打开：即使扫描目录内存在指向根目录之外的符号链接也不会被跟随。
	root, err := os.OpenRoot(h.scanRoot)
	if err != nil {
		h.notFound(w, "找不到这个文件。")
		return
	}
	defer root.Close()
	f, err := root.Open(filepath.Join(filepath.Clean(filepath.FromSlash(rel)), name))
	if err != nil {
		h.notFound(w, "找不到这个文件。")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		h.notFound(w, "找不到这个文件。")
		return
	}
	// ServeContent 自动按扩展名给出 Content-Type，并支持 Range 与 If-Modified-Since。
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// docDir 把 URL 中的相对路径解析为扫描根目录下的绝对路径，
// 并确保结果没有逃出根目录（防止手改 URL 用 ../ 读取根目录之外的文件）。
func (h *Handler) docDir(rel string) (string, bool) {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || clean == ".." || filepath.IsAbs(clean) ||
		strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false
	}
	full := filepath.Join(h.scanRoot, clean)
	if within, err := filepath.Rel(h.scanRoot, full); err != nil ||
		within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return "", false
	}
	return full, true
}

// notFound 以 404 渲染友好提示，不向使用者暴露内部细节。
func (h *Handler) notFound(w http.ResponseWriter, msg string) {
	h.render(w, http.StatusNotFound, &pageData{ErrMsg: msg})
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
	Doc      *docView // 非空时渲染档案详情页（原始扫描件浏览）
}

// docView 是档案详情页的渲染数据：某一份档案的第 Page 页原始扫描件。
type docView struct {
	Title    string // 档案名（叶子目录名）
	RelPath  string // 相对扫描根目录的路径，用于拼图片与翻页链接
	Page     int    // 当前页码，从 1 起
	Total    int    // 总页数
	File     string // 当前页图片文件名
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
