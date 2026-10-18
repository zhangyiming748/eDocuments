package ocr

import (
	"bytes"
	"strconv"
	"strings"
	"unicode/utf8"
)

// parseTSV 解析 `tesseract ... tsv` 的输出，还原按行组织的文本，
// 并计算全部已识别词的平均置信度（0～100，无词时为 0）。
//
// TSV 列：level page block_num par_num line_num word_num
// left top width height conf text，其中 level 5 为 WORD。
func parseTSV(data []byte) (string, float64) {
	lines := bytes.Split(data, []byte("\n"))
	if len(lines) < 2 {
		return "", 0
	}

	header := strings.Split(string(lines[0]), "\t")
	iLevel := colIndex(header, "level")
	iConf := colIndex(header, "conf")
	iText := colIndex(header, "text")
	iBlock := colIndex(header, "block_num")
	iPar := colIndex(header, "par_num")
	iLine := colIndex(header, "line_num")
	if iLevel < 0 || iText < 0 {
		return "", 0
	}

	var (
		builder    strings.Builder
		curLineKey string
		prevInLine string // 当前行已拼接的最后一个词，用于判断词间分隔
		confSum    float64
		confN      int
	)

	for _, raw := range lines[1:] {
		if len(raw) == 0 {
			continue
		}
		cols := strings.Split(string(raw), "\t")
		if len(cols) <= iLevel {
			continue
		}
		level, _ := strconv.Atoi(cols[iLevel])
		if level != 5 || len(cols) <= iText { // 只处理 WORD 行
			continue
		}
		text := cols[iText]
		if strings.TrimSpace(text) == "" {
			continue
		}

		key := ""
		if iBlock >= 0 && iPar >= 0 && iLine >= 0 && len(cols) > iLine {
			key = cols[iBlock] + ":" + cols[iPar] + ":" + cols[iLine]
		}
		if key != "" && key != curLineKey {
			builder.WriteByte('\n')
			curLineKey = key
			prevInLine = ""
		}
		// 中文（汉字/全角标点）之间不加空格，英文词之间保留空格。
		if prevInLine != "" && needSpace(prevInLine, text) {
			builder.WriteByte(' ')
		}
		builder.WriteString(text)
		prevInLine = text

		if iConf >= 0 && len(cols) > iConf {
			if conf, err := strconv.ParseFloat(cols[iConf], 64); err == nil && conf >= 0 {
				confSum += conf
				confN++
			}
		}
	}

	avg := 0.0
	if confN > 0 {
		avg = confSum / float64(confN)
	}
	return strings.TrimSpace(builder.String()), avg
}

func colIndex(header []string, name string) int {
	for i, h := range header {
		if h == name {
			return i
		}
	}
	return -1
}

// needSpace 判断相邻两个 Tesseract 词之间是否需要空格：
// 仅当两侧都是 ASCII 字母/数字（英文单词、数字）时加空格；
// 汉字之间、汉字与标点之间直接拼接。
func needSpace(prev, cur string) bool {
	pr, _ := utf8.DecodeLastRuneInString(prev)
	cr, _ := utf8.DecodeRuneInString(cur)
	return isASCIIWordRune(pr) && isASCIIWordRune(cr)
}

func isASCIIWordRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}
