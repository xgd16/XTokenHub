package provider

import (
	"unicode"
	"unicode/utf8"
)

// EstimateTokens 本地启发式 token 估算（兜底路径：上游未返回 usage 时使用）。
// 规则：ASCII/拉丁字符约 4 字符/token；CJK 字符约 1.5 字符/token；附加少量常量开销。
// 结果用于统计参考，不保证与厂商 tokenizer 一致。
func EstimateTokens(text string) int64 {
	if text == "" {
		return 0
	}
	var cjk, other int
	for _, r := range text {
		if isCJK(r) {
			cjk++
		} else {
			other++
		}
	}
	tokens := float64(cjk)/1.5 + float64(other)/4
	if tokens < 1 {
		tokens = 1
	}
	return int64(tokens + 2) // 消息结构开销
}

func isCJK(r rune) bool {
	switch {
	case unicode.Is(unicode.Han, r), // 中文
		unicode.Is(unicode.Hiragana, r),
		unicode.Is(unicode.Katakana, r),
		unicode.Is(unicode.Hangul, r):
		return true
	}
	return false
}

// EstimateBytes 对非 UTF-8 安全的字节流估算（按 rune 容错处理）。
func EstimateBytes(b []byte) int64 {
	if !utf8.Valid(b) {
		return int64(len(b)) / 4
	}
	return EstimateTokens(string(b))
}
