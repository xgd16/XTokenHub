package textseg_test

import (
	"testing"

	"xtokenhub/internal/pkg/textseg"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"小写", "Hello WORLD", "hello world"},
		{"全角转半角", "ＡＢＣ１２３", "abc123"},
		{"全角空格折叠", "记忆\u3000库", "记忆 库"},
		{"多空白折叠", "  a \t\n b  ", "a b"},
		{"全角标点转半角", "记忆，库", "记忆,库"},
		{"空串", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := textseg.Normalize(tt.in); got != tt.want {
				t.Errorf("Normalize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSegmented(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"三字中文", "记忆库", "记忆 忆库"},
		{"两字中文", "记忆", "记忆"},
		{"单字中文无 token", "库", ""},
		{"中英混合", "Memory 记忆库 Cache", "memory 记忆 忆库 cache"},
		{"标点分隔不成段", "记忆，库", "记忆"},
		{"标签逗号分隔", "支付,回调", "支付 回调"},
		{"标识符整体保留", "memory_add", "memory_add"},
		{"数字与下划线", "v1.2_3", "v1 2_3"},
		{"单独下划线是分隔", "a _ b", "a b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := textseg.Segmented(tt.in); got != tt.want {
				t.Errorf("Segmented(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestBuildMatch(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		wantStrict string
		wantLoose  string
	}{
		{"两字短语", "记忆", `"记忆"`, `"记忆"`},
		{"多段 AND", "记忆库 设计", `"记忆 忆库" AND "设计"`, `"记忆 忆库" OR "设计"`},
		{"中英混合", "hello 记忆库", `"hello" AND "记忆 忆库"`, `"hello" OR "记忆 忆库"`},
		{"单汉字无表达式", "库", "", ""},
		{"纯标点无表达式", "。！？", "", ""},
		{"引号断句不跨标点匹配", `记忆"库`, `"记忆"`, `"记忆"`},
		{"FTS 语法符被转义", "NEAR(a b) OR *", `"near" AND "a" AND "b" AND "or"`, `"near" OR "a" OR "b" OR "or"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := textseg.BuildMatch(tt.in)
			if got.Strict != tt.wantStrict {
				t.Errorf("BuildMatch(%q).Strict = %q, want %q", tt.in, got.Strict, tt.wantStrict)
			}
			if got.Loose != tt.wantLoose {
				t.Errorf("BuildMatch(%q).Loose = %q, want %q", tt.in, got.Loose, tt.wantLoose)
			}
		})
	}
}

func TestLikeNeedle(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"单汉字", "库", "库"},
		{"空格分隔的汉字", "记 忆", "记忆"},
		{"有 token 时不兜底", "记忆", ""},
		{"英文单词不兜底", "memory", ""},
		{"纯标点不兜底", "。！", ""},
		{"全角单字归一", "Ａ", ""}, // 全角字母归一后是可用 token，不走 LIKE
		{"空串", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := textseg.LikeNeedle(tt.in); got != tt.want {
				t.Errorf("LikeNeedle(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestClipAndFirstLine(t *testing.T) {
	if got := textseg.Clip("记忆库设计", 3); got != "记忆库" {
		t.Errorf("Clip = %q, want 记忆库", got)
	}
	if got := textseg.Clip("abc", 10); got != "abc" {
		t.Errorf("Clip 不应截断短串, got %q", got)
	}
	if got := textseg.FirstLine("\n\n  第一行标题\n正文", 20); got != "第一行标题" {
		t.Errorf("FirstLine = %q, want 第一行标题", got)
	}
	if got := textseg.FirstLine("正文很长很长", 2); got != "正文" {
		t.Errorf("FirstLine 截断 = %q, want 正文", got)
	}
	if got := textseg.FirstLine("   ", 10); got != "" {
		t.Errorf("FirstLine 空内容 = %q, want 空串", got)
	}
}

func TestMarkdownTitle(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"普通首行", "记一条普通结论\n第二行", 40, "记一条普通结论"},
		{"跳过空行", "\n\n  真正的首行", 40, "真正的首行"},
		{"二级标题", "## 部署流程\n正文", 40, "部署流程"},
		{"标题闭合井号", "# 标题 #", 40, "标题"},
		{"引用", "> 引用里的结论", 40, "引用里的结论"},
		{"嵌套引用标题", "> ## 引用标题", 40, "引用标题"},
		{"无序列表", "- 第一项\n- 第二项", 40, "第一项"},
		{"有序列表", "3) 第三项", 40, "第三项"},
		{"任务框", "- [x] 已完成事项", 40, "已完成事项"},
		{"行内标记", "**结论**与`代码`", 40, "结论与代码"},
		{"链接", "[设计文档](https://example.com/a)", 40, "设计文档"},
		{"跳过代码围栏", "```go\nfunc main() {}\n```\n真正的标题", 40, "真正的标题"},
		{"截断", "一二三四五六", 3, "一二三"},
		{"纯符号行回退为空", "###", 40, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := textseg.MarkdownTitle(tt.in, tt.max); got != tt.want {
				t.Errorf("MarkdownTitle(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
