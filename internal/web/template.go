package web

// pageTemplate 是搜索页唯一模板。用户输入经 html/template 自动转义；
// 高亮片段来自 ES 的受控 <em> 标签，以 template.HTML 渲染。
const pageTemplate = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>档案全文检索</title>
<style>
  :root { --primary:#2b5a88; --bg:#f5f6f8; --em:#d9480f; }
  * { box-sizing:border-box; }
  body { margin:0; font-family:"PingFang SC","Microsoft YaHei",sans-serif; background:var(--bg); color:#222; }
  header { background:var(--primary); color:#fff; padding:20px 0; }
  .wrap { max-width:860px; margin:0 auto; padding:0 16px; }
  h1 { font-size:20px; margin:0 0 14px; font-weight:600; }
  form { display:flex; gap:8px; }
  input[type=text] { flex:1; padding:10px 12px; border:0; border-radius:4px; font-size:15px; }
  button { padding:10px 22px; border:0; border-radius:4px; background:#ffd43b; color:#333; font-size:15px; cursor:pointer; }
  button:hover { background:#fcc419; }
  main { padding:20px 0 40px; }
  .meta { color:#666; font-size:13px; margin:12px 0; }
  .err { background:#fff4e6; border:1px solid #ffa94d; color:#d9480f; padding:10px 12px; border-radius:4px; margin:12px 0; }
  .card { background:#fff; border:1px solid #e6e8ec; border-radius:6px; padding:16px 18px; margin-bottom:12px; }
  .card h2 { margin:0 0 6px; font-size:16px; }
  .card h2 a { color:var(--primary); text-decoration:none; }
  .path { color:#888; font-size:12px; margin-bottom:8px; }
  .snippet { font-size:14px; line-height:1.7; color:#333; margin:6px 0 0; }
  em { color:var(--em); font-style:normal; font-weight:600; }
  .empty { color:#777; text-align:center; padding:50px 0; font-size:15px; }
  nav.pager { margin-top:20px; text-align:center; }
  nav.pager a, nav.pager span.cur {
    display:inline-block; min-width:34px; padding:7px 0; margin:0 3px;
    border:1px solid #d0d4da; border-radius:4px; text-decoration:none; color:var(--primary); background:#fff; font-size:14px;
  }
  nav.pager span.cur { background:var(--primary); color:#fff; border-color:var(--primary); }
  .viewer-head { background:#fff; border:1px solid #e6e8ec; border-radius:6px; padding:12px 16px; margin-bottom:12px; }
  .viewer-head h2 { margin:0 0 4px; font-size:16px; }
  .viewer-head .path { margin:0; }
  .viewer-head .back { float:right; font-size:13px; color:var(--primary); text-decoration:none; }
  .viewer { background:#3a3a3a; border-radius:6px; padding:16px; text-align:center; }
  .viewer img.page { max-width:100%; max-height:76vh; background:#fff; border-radius:2px; box-shadow:0 2px 10px rgba(0,0,0,.35); }
  .viewer .plain { color:#ddd; font-size:13px; padding:40px 0; }
  nav.viewer-nav { margin-top:14px; text-align:center; }
  nav.viewer-nav a, nav.viewer-nav span.disabled {
    display:inline-block; min-width:88px; padding:8px 16px; margin:0 6px;
    border:1px solid #d0d4da; border-radius:4px; text-decoration:none; font-size:14px; background:#fff;
  }
  nav.viewer-nav a { color:var(--primary); }
  nav.viewer-nav span.disabled { color:#aaa; border-color:#e6e8ec; background:#f2f3f5; }
  nav.viewer-nav .pos { display:inline-block; min-width:110px; color:#555; font-size:14px; }
  footer { color:#999; font-size:12px; text-align:center; padding:16px 0; }
</style>
</head>
<body>
<header>
  <div class="wrap">
    <h1>纸质档案全文检索</h1>
    <form action="/search" method="get">
      <input type="text" name="q" value="{{.Query}}" placeholder="输入关键词，多词用空格分隔，可用引号匹配短语" autofocus>
      <button type="submit">搜索</button>
    </form>
  </div>
</header>
<main>
  <div class="wrap">
    {{if .ErrMsg}}<div class="err">{{.ErrMsg}}</div>{{end}}
    {{if .Doc}}
      <div class="viewer-head">
        <a class="back" href="/">返回检索</a>
        <h2>{{.Doc.Title}}</h2>
        <div class="path">{{.Doc.RelPath}}</div>
      </div>
      <div class="viewer">
        <img class="page" src="/doc/image?p={{.Doc.RelPath | urlquery}}&f={{.Doc.File | urlquery}}" alt="{{.Doc.Title}} 第 {{.Doc.Page}} 页">
      </div>
      <nav class="viewer-nav">
        {{if .Doc.HasPrev}}<a href="/doc?p={{.Doc.RelPath | urlquery}}&n={{.Doc.PrevPage}}">上一页</a>{{else}}<span class="disabled">上一页</span>{{end}}
        <span class="pos">第 {{.Doc.Page}} / {{.Doc.Total}} 页</span>
        {{if .Doc.HasNext}}<a href="/doc?p={{.Doc.RelPath | urlquery}}&n={{.Doc.NextPage}}">下一页</a>{{else}}<span class="disabled">下一页</span>{{end}}
      </nav>
    {{else if .Result}}
      <div class="meta">共找到 {{.Result.Total}} 条与“{{.Query}}”相关的档案{{if gt .Result.Total 99}}（前 100 条可分页查看）{{end}}</div>
      {{if .Result.Hits}}
        {{range .Result.Hits}}
        <div class="card">
          <h2><a href="/doc?p={{.RelativePath | urlquery}}">{{if .HighlightTitle}}{{.HighlightTitle}}{{else}}{{.Title}}{{end}}</a></h2>
          <div class="path">{{.RelativePath}} ｜ 共 {{.PageCount}} 页</div>
          {{range .Snippets}}<p class="snippet">{{.}}</p>{{end}}
        </div>
        {{end}}
        {{if or .HasPrev .HasNext}}
        <nav class="pager">
          {{if .HasPrev}}<a href="/search?q={{$.Query | urlquery}}&page={{.PrevPage}}">上一页</a>{{end}}
          {{range .Pages}}{{if eq . $.Page}}<span class="cur">{{.}}</span>{{else}}<a href="/search?q={{$.Query | urlquery}}&page={{.}}">{{.}}</a>{{end}}{{end}}
          {{if .HasNext}}<a href="/search?q={{$.Query | urlquery}}&page={{.NextPage}}">下一页</a>{{end}}
        </nav>
        {{end}}
      {{else}}
        <div class="empty">没有找到包含“{{.Query}}”的档案，可尝试更换或减少关键词。</div>
      {{end}}
    {{else if not .ErrMsg}}
      <div class="empty">输入关键词开始检索档案全文。</div>
    {{end}}
  </div>
</main>
<footer>eDocuments · 仅供单位局域网内部使用</footer>
</body>
</html>`
