package ocr

import "testing"

// 与 tesseract 5 tsv 输出列顺序一致的样本（精简列）。
const sampleTSV = "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n" +
	"1\t1\t0\t0\t0\t0\t0\t0\t100\t200\t-1\t\n" +
	"2\t1\t1\t0\t0\t0\t10\t10\t80\t30\t-1\t\n" +
	"3\t1\t1\t1\t0\t0\t10\t10\t80\t30\t-1\t\n" +
	"4\t1\t1\t1\t1\t0\t10\t10\t80\t15\t-1\t\n" +
	"5\t1\t1\t1\t1\t1\t10\t10\t30\t15\t96.5\t财务\n" +
	"5\t1\t1\t1\t1\t2\t45\t10\t40\t15\t92.0\t管理\n" +
	"4\t1\t1\t1\t2\t0\t10\t30\t80\t15\t-1\t\n" +
	"5\t1\t1\t1\t2\t1\t10\t30\t30\t15\t80\t规定\n"

func TestParseTSV(t *testing.T) {
	text, conf := parseTSV([]byte(sampleTSV))
	// 中文词之间不插空格，行间换行
	if text != "财务管理\n规定" {
		t.Fatalf("文本还原错误: %q", text)
	}
	// (96.5 + 92.0 + 80) / 3 = 89.5
	if conf < 89.4 || conf > 89.6 {
		t.Fatalf("平均置信度错误: %v", conf)
	}
}

func TestParseTSVEnglishKeepsSpaces(t *testing.T) {
	tsv := "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n" +
		"5\t1\t1\t1\t1\t1\t0\t0\t10\t10\t90\tAnnual\n" +
		"5\t1\t1\t1\t1\t2\t0\t0\t10\t10\t90\tReport\n" +
		"5\t1\t1\t1\t1\t3\t0\t0\t10\t10\t90\t2024\n"
	text, _ := parseTSV([]byte(tsv))
	if text != "Annual Report 2024" {
		t.Fatalf("英文词间应保留空格: %q", text)
	}
}

func TestParseTSVEmpty(t *testing.T) {
	text, conf := parseTSV([]byte(""))
	if text != "" || conf != 0 {
		t.Fatalf("空输入应返回零值，得到 %q %v", text, conf)
	}
}
