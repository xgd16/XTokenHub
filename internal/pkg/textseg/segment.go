// Package textseg 中文友好的检索文本切分：ASCII 词整段保留，CJK 连续段切重叠 bigram。
//
// SQLite FTS5 的 unicode61 分词器把整段连续中文当作一个 token（「记忆」查不到
// 「长期记忆系统」），trigram 分词器又对 1~2 字查询必然失配。因此索引与查询两侧
// 共用本包管线：中文按重叠 bigram 展开后再进 FTS5，任意长度（≥2 字）的中文子串
// 查询都能用短语匹配命中；单个汉字由调用方走 LIKE 兜底（LikeNeedle）。
package textseg

import (
	"regexp"
	"strings"
	"unicode"
)

const (
	// maxGroups 查询保留的关键词组数上限，防止超长 prompt 变成巨型 MATCH 表达式。
	maxGroups = 16
	// maxTokensPerGroup 单个短语组的 token 上限。
	maxTokensPerGroup = 24
	// maxNeedleRunes LIKE 兜底串长度上限（runes）。
	maxNeedleRunes = 32
)

// Normalize 归一化：全角转半角、小写、空白折叠（首尾去空白）。
func Normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		if r == '\u3000' { // 全角空格
			r = ' '
		}
		if r >= '\uFF01' && r <= '\uFF5E' { // 全角 ASCII 可见字符
			r -= 0xFEE0
		}
		r = unicode.ToLower(r)
		if unicode.IsSpace(r) {
			if b.Len() > 0 {
				space = true
			}
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Tokens 切分文本：ASCII 词（字母/数字/下划线）整段保留；CJK 连续段（≥2 字）
// 输出重叠 bigram。索引侧与查询侧共用同一结果。
func Tokens(s string) []string {
	var out []string
	for _, g := range groups(Normalize(s)) {
		out = append(out, g...)
	}
	return out
}

// Segmented 索引侧切分：以空格连接的 token 串，写入 FTS5 的 *_seg 列。
// 单字 CJK 段不产生 token（无法 bigram 化），由 LIKE 兜底查询覆盖。
func Segmented(s string) string {
	return strings.Join(Tokens(s), " ")
}

// Match FTS5 检索表达式：Strict 组间 AND，Loose 组间 OR（严格版 0 结果时降级用）。
// 无可用 token（如仅一个汉字）时两字段均为空串，调用方改用 LikeNeedle。
type Match struct {
	Strict string
	Loose  string
}

// BuildMatch 把查询切分为 FTS5 MATCH 表达式；token 一律引号转义，
// 避免模型传入的标点/引号触发 FTS5 语法错误或注入查询语法。
func BuildMatch(q string) Match {
	gs := groups(Normalize(q))
	if len(gs) == 0 {
		return Match{}
	}
	if len(gs) > maxGroups {
		gs = gs[:maxGroups]
	}
	strict := make([]string, 0, len(gs))
	loose := make([]string, 0, len(gs))
	for _, g := range gs {
		if len(g) > maxTokensPerGroup {
			g = g[:maxTokensPerGroup]
		}
		p := quotePhrase(g)
		strict = append(strict, p)
		loose = append(loose, p)
	}
	return Match{
		Strict: strings.Join(strict, " AND "),
		Loose:  strings.Join(loose, " OR "),
	}
}

// LikeNeedle 兜底 LIKE 片段：仅当切分不出任何 token 且查询含中文时返回
// 去空白后的归一化串（「记 忆」→「记忆」），否则返回空串。
func LikeNeedle(q string) string {
	n := Normalize(q)
	if len(groups(n)) > 0 {
		return ""
	}
	dense := strings.Join(strings.Fields(n), "")
	if dense == "" || !hasCJK(dense) {
		return ""
	}
	runes := []rune(dense)
	if len(runes) > maxNeedleRunes {
		runes = runes[:maxNeedleRunes]
	}
	return string(runes)
}

// Clip 按 runes 截断（不劈开多字节字符），用于标题与 FTS 索引的长度控制。
func Clip(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}

// FirstLine 取首行非空文本并截断，供标题缺省时从正文推导。
func FirstLine(s string, maxRunes int) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		return Clip(line, maxRunes)
	}
	return ""
}

var (
	mdLinkRe = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	// 行内标记：加粗/斜体星号、下划线强调、行内代码反引号。
	mdInlineRe = strings.NewReplacer("**", "", "__", "", "*", "", "`", "")
)

// stripMDMarker 去掉行首一个 Markdown 结构符号（无匹配原样返回）：
// 引用 >、标题 #、无序列表 -/*/+、有序列表 1./1)、任务复选框 [ ]/[x]。
func stripMDMarker(line string) string {
	if strings.HasPrefix(line, ">") {
		return strings.TrimPrefix(line, ">")
	}
	if strings.HasPrefix(line, "#") {
		trimmed := strings.TrimLeft(line, "#")
		if trimmed == "" || trimmed[0] == ' ' || trimmed[0] == '\t' {
			return trimmed
		}
		return line
	}
	if len(line) >= 2 && (line[0] == '-' || line[0] == '*' || line[0] == '+') && (line[1] == ' ' || line[1] == '\t') {
		return line[2:]
	}
	if len(line) >= 3 && line[0] == '[' && line[2] == ']' && (line[1] == ' ' || line[1] == 'x' || line[1] == 'X') {
		return line[3:]
	}
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i > 0 && i < len(line) && (line[i] == '.' || line[i] == ')') {
		rest := line[i+1:]
		if rest == "" || rest[0] == ' ' || rest[0] == '\t' {
			return rest
		}
	}
	return line
}

// MarkdownTitle 从 Markdown 正文推导标题：取首个非空行，逐层去掉行首结构符号
// （标题 #、引用 >、列表 -/1./任务框）与行内标记（加粗/行内代码/链接目标），
// 让缺省标题保持可读；代码围栏行不作为标题。
func MarkdownTitle(s string, maxRunes int) string {
	inFence := false
	for _, raw := range strings.Split(s, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || line == "" {
			continue
		}
		heading := false
		for i := 0; i < 6; i++ {
			if strings.HasPrefix(line, "#") {
				heading = true
			}
			trimmed := strings.TrimSpace(stripMDMarker(line))
			if trimmed == line {
				break
			}
			line = trimmed
			if line == "" {
				break
			}
		}
		if heading {
			// Markdown 标题允许闭合井号（# 标题 #）
			line = strings.TrimRight(strings.TrimSpace(line), "# \t")
		}
		line = mdLinkRe.ReplaceAllString(line, "$1")
		line = mdInlineRe.Replace(line)
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		return Clip(line, maxRunes)
	}
	return ""
}

// groups 把归一化文本切成词组：ASCII 词每个一组；CJK 连续段（≥2 字）按位置
// 生成重叠 bigram 短语组（组内 token 须相邻，即原文子串）。
func groups(s string) [][]string {
	var out [][]string
	var word []rune
	var cjk []rune

	flushWord := func() {
		// 纯下划线的「词」不产生 token（FTS5 也会把它当分隔符丢弃）。
		if !hasWordRune(word) {
			word = word[:0]
			return
		}
		out = append(out, []string{string(word)})
		word = word[:0]
	}
	flushCJK := func() {
		if len(cjk) >= 2 {
			g := make([]string, 0, len(cjk)-1)
			for i := 0; i+1 < len(cjk); i++ {
				g = append(g, string(cjk[i:i+2]))
			}
			out = append(out, g)
		}
		cjk = cjk[:0]
	}

	for _, r := range s {
		switch {
		case isCJKRune(r):
			flushWord()
			cjk = append(cjk, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
			// 下划线并入词内：memory_add 这类标识符整体成组，查询时按短语邻接匹配。
			flushCJK()
			word = append(word, r)
		default: // 空白与标点均为分隔符
			flushWord()
			flushCJK()
		}
	}
	flushWord()
	flushCJK()
	return out
}

// quotePhrase 生成 FTS5 短语字面量：组内 token 以空格连接并整体加引号，
// 双引号按 FTS5 规则双写转义。
func quotePhrase(toks []string) string {
	parts := make([]string, 0, len(toks))
	for _, t := range toks {
		parts = append(parts, strings.ReplaceAll(t, `"`, `""`))
	}
	return `"` + strings.Join(parts, " ") + `"`
}

func hasCJK(s string) bool {
	for _, r := range s {
		if isCJKRune(r) {
			return true
		}
	}
	return false
}

func hasWordRune(rs []rune) bool {
	for _, r := range rs {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// isCJKRune 无需空格分词的表意文字（汉字/假名/谚文），按同样规则做 bigram。
func isCJKRune(r rune) bool {
	switch {
	case r >= 0x3040 && r <= 0x30FF: // 平假名 / 片假名
		return true
	case r >= 0x3400 && r <= 0x4DBF: // CJK 扩展 A
		return true
	case r >= 0x4E00 && r <= 0x9FFF: // CJK 基本区
		return true
	case r >= 0xF900 && r <= 0xFAFF: // CJK 兼容表意
		return true
	case r >= 0xAC00 && r <= 0xD7AF: // 谚文音节
		return true
	case r >= 0x1100 && r <= 0x11FF: // 谚文字母
		return true
	case r >= 0x20000 && r <= 0x2FA1F: // CJK 扩展 B 及以后
		return true
	}
	return false
}
