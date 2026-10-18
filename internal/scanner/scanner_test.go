package scanner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"edocs/internal/model"
)

// 构造目录树：
//
//	root/
//	  2024/档案甲/scan_0001.jpg scan_0002.jpg notes.txt
//	  2024/档案乙/scan_0003.png
//	  空目录/
//	  杂项/readme.md            （无图片，跳过）
func buildTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "2024", "档案甲"))
	mustMkdir(t, filepath.Join(root, "2024", "档案乙"))
	mustMkdir(t, filepath.Join(root, "空目录"))
	mustMkdir(t, filepath.Join(root, "杂项"))

	mustWrite(t, filepath.Join(root, "2024", "档案甲", "scan_0001.jpg"), "x")
	mustWrite(t, filepath.Join(root, "2024", "档案甲", "scan_0002.jpg"), "x")
	mustWrite(t, filepath.Join(root, "2024", "档案甲", "notes.txt"), "x")
	mustWrite(t, filepath.Join(root, "2024", "档案乙", "scan_0003.png"), "x")
	mustWrite(t, filepath.Join(root, "杂项", "readme.md"), "x")
	return root
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanLeafDocuments(t *testing.T) {
	root := buildTree(t)
	docs, warnings, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 {
		t.Fatalf("应发现 2 份文档，实际 %d", len(docs))
	}

	// 文档顺序不依赖中文码点，按标题查找
	byTitle := map[string]int{}
	for i, d := range docs {
		byTitle[d.Title] = i
	}
	jiaIdx, ok := byTitle["档案甲"]
	if !ok {
		t.Fatalf("缺少文档 档案甲，实际: %v", docTitles(docs))
	}
	jia := docs[jiaIdx]
	if jia.RelativePath != filepath.ToSlash(filepath.Join("2024", "档案甲")) {
		t.Fatalf("相对路径异常: %s", jia.RelativePath)
	}
	if len(jia.Pages) != 2 {
		t.Fatalf("档案甲应有 2 页，实际 %d", len(jia.Pages))
	}
	if jia.Pages[0].File != "scan_0001.jpg" || jia.Pages[1].File != "scan_0002.jpg" {
		t.Fatalf("页序错误: %s, %s", jia.Pages[0].File, jia.Pages[1].File)
	}
	yiIdx, ok := byTitle["档案乙"]
	if !ok {
		t.Fatalf("缺少文档 档案乙，实际: %v", docTitles(docs))
	}
	if jia.ID == "" || jia.ID == docs[yiIdx].ID {
		t.Fatal("文档 ID 应非空且唯一")
	}

	// notes.txt 与空目录、无图片目录都应产生告警
	joined := ""
	for _, w := range warnings {
		joined += w + "\n"
	}
	for _, want := range []string{"notes.txt", "空目录", "杂项"} {
		if !strings.Contains(joined, want) {
			t.Errorf("告警中缺少 %q，实际告警:\n%s", want, joined)
		}
	}
}

func TestScanRootNotExist(t *testing.T) {
	if _, _, err := Scan(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("根目录不存在应返回错误")
	}
}

func docTitles(docs []model.SourceDocument) []string {
	titles := make([]string, 0, len(docs))
	for _, d := range docs {
		titles = append(titles, d.Title)
	}
	return titles
}
