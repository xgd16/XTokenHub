package model

import (
	"strings"
	"time"
)

// MemoryKind 记忆类型（受控枚举，便于筛选与展示）。
type MemoryKind string

const (
	MemoryKindNote       MemoryKind = "note"       // 一般记录
	MemoryKindFact       MemoryKind = "fact"       // 事实
	MemoryKindDecision   MemoryKind = "decision"   // 决策
	MemoryKindConvention MemoryKind = "convention" // 约定/规范
	MemoryKindPreference MemoryKind = "preference" // 偏好
	MemoryKindTask       MemoryKind = "task"       // 任务进展
	MemoryKindLesson     MemoryKind = "lesson"     // 经验教训
)

// AllMemoryKinds 全部合法类型（稳定顺序，前端下拉与图表例复用）。
var AllMemoryKinds = []MemoryKind{
	MemoryKindNote, MemoryKindFact, MemoryKindDecision, MemoryKindConvention,
	MemoryKindPreference, MemoryKindTask, MemoryKindLesson,
}

// Valid 类型是否合法。
func (k MemoryKind) Valid() bool {
	for _, v := range AllMemoryKinds {
		if k == v {
			return true
		}
	}
	return false
}

// MemoryStatus 记忆状态（失效不删除：superseded/archived 仅退出默认检索）。
type MemoryStatus string

const (
	MemoryStatusActive     MemoryStatus = "active"     // 生效
	MemoryStatusSuperseded MemoryStatus = "superseded" // 被新记忆替代
	MemoryStatusArchived   MemoryStatus = "archived"   // 归档
	MemoryStatusDeleted    MemoryStatus = "deleted"    // 软删除（回收站语义）
)

// Valid 状态是否合法。
func (s MemoryStatus) Valid() bool {
	switch s {
	case MemoryStatusActive, MemoryStatusSuperseded, MemoryStatusArchived, MemoryStatusDeleted:
		return true
	}
	return false
}

// MemorySource 写入来源。
type MemorySource string

const (
	MemorySourceAPI     MemorySource = "api"     // /memory/v1 接入端
	MemorySourceMCP     MemorySource = "mcp"     // MCP 工具
	MemorySourceConsole MemorySource = "console" // 控制台页面
	MemorySourceSystem  MemorySource = "system"  // 系统任务（如 LLM 分类）
)

// MemoryNamespaceSource 命名空间归属方式。
type MemoryNamespaceSource string

const (
	NamespaceSourceNone     MemoryNamespaceSource = ""         // 未分类
	NamespaceSourceExplicit MemoryNamespaceSource = "explicit" // 接入端显式指定
	NamespaceSourceLLM      MemoryNamespaceSource = "llm"      // 一键 LLM 分类
	NamespaceSourceManual   MemoryNamespaceSource = "manual"   // 控制台人工指定
)

// MemoryLinkRelation 记忆间关系类型（关系图的边）。
type MemoryLinkRelation string

const (
	MemoryLinkRelated     MemoryLinkRelation = "related"      // 相关
	MemoryLinkSupersedes  MemoryLinkRelation = "supersedes"   // 替代
	MemoryLinkConflicts   MemoryLinkRelation = "conflicts"    // 冲突
	MemoryLinkDerivedFrom MemoryLinkRelation = "derived_from" // 派生自
)

// AllMemoryLinkRelations 全部合法关系（稳定顺序）。
var AllMemoryLinkRelations = []MemoryLinkRelation{
	MemoryLinkRelated, MemoryLinkSupersedes, MemoryLinkConflicts, MemoryLinkDerivedFrom,
}

// Valid 关系是否合法。
func (r MemoryLinkRelation) Valid() bool {
	for _, v := range AllMemoryLinkRelations {
		if r == v {
			return true
		}
	}
	return false
}

// 记忆字段边界（服务层校验 + FTS 索引截断共用）。
const (
	// MemoryTitleMaxRunes 标题长度上限（超出截断）。
	MemoryTitleMaxRunes = 128
	// MemoryTagsMax 标签数量上限。
	MemoryTagsMax = 16
	// MemoryTagMaxRunes 单标签长度上限。
	MemoryTagMaxRunes = 32
	// MemoryNamespaceMaxRunes 命名空间名长度上限。
	MemoryNamespaceMaxRunes = 64
)

// Memory 一条记忆。
type Memory struct {
	ID int64 `gorm:"primaryKey;autoIncrement" json:"id"`
	// Title 标题；空时由服务层取正文首行。
	Title string `gorm:"size:256;not null;default:''" json:"title"`
	// Content 记忆正文（Markdown 文本）。
	Content string `gorm:"not null" json:"content"`
	// Namespace 命名空间（唯一归类单位）；空串 = 未分类。
	Namespace string `gorm:"size:64;not null;default:'';index:idx_memories_ns_status,priority:1" json:"namespace"`
	// NamespaceSource 归属方式：explicit | llm | manual | 空（未分类）。
	NamespaceSource MemoryNamespaceSource `gorm:"size:16;not null;default:''" json:"namespace_source"`
	// Kind 记忆类型。
	Kind MemoryKind `gorm:"size:16;not null;default:note;index" json:"kind"`
	// Tags 标签（逗号分隔，经 SetTags/TagList 读写）。
	Tags string `gorm:"size:512;not null;default:''" json:"tags"`
	// Importance 重要度 1~5（排序加权）。
	Importance int `gorm:"not null;default:3" json:"importance"`
	// Pinned 置顶（免疫衰减并被排序加权）。
	Pinned bool `gorm:"not null;default:false" json:"pinned"`
	// Status 状态，默认 active。
	Status MemoryStatus `gorm:"size:16;not null;default:active;index:idx_memories_ns_status,priority:2" json:"status"`
	// SupersededBy 替代本条的记忆 id（0 = 无）。
	SupersededBy int64 `gorm:"not null;default:0" json:"superseded_by"`
	// ExpiresAt 过期时间（可空；过期记忆退出默认检索，不删除）。
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	// Source 写入来源。
	Source MemorySource `gorm:"size:16;not null;default:api" json:"source"`
	// CreatedBy 写入者（API Key 名 / console / anonymous）。
	CreatedBy string `gorm:"size:128;not null;default:''" json:"created_by"`
	// SourceSession 来源会话（X-Session-Id 等，可空）。
	SourceSession string `gorm:"size:128;not null;default:''" json:"source_session,omitempty"`
	// SourceRef 来源引用（仓库/文件/任务号等自由串，可空）。
	SourceRef string `gorm:"size:256;not null;default:''" json:"source_ref,omitempty"`
	// ContentHash 归一化正文哈希（同命名空间活跃记忆去重用）。
	ContentHash string `gorm:"size:64;not null;default:'';index" json:"-"`
	// AccessCount 被检索命中次数（访问强化）。
	AccessCount int64 `gorm:"not null;default:0" json:"access_count"`
	// LastAccessedAt 最近一次被检索命中的时间。
	LastAccessedAt *time.Time `json:"last_accessed_at,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 表名。
func (Memory) TableName() string { return "memories" }

// TagList 解析标签列表（逗号分隔）。
func (m *Memory) TagList() []string { return splitCSV(m.Tags) }

// SetTags 规范化写入标签：去空白、去重、保序、逗号连接。
func (m *Memory) SetTags(tags []string) {
	seen := make(map[string]bool, len(tags))
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	m.Tags = strings.Join(out, ",")
}

// MemoryNamespace 命名空间（唯一归类单位；记忆行只存名字，本表补描述与管理信息）。
type MemoryNamespace struct {
	ID int64 `gorm:"primaryKey;autoIncrement" json:"id"`
	// Name 命名空间名（唯一）。
	Name string `gorm:"size:64;not null;uniqueIndex" json:"name"`
	// Description 一句话描述（LLM 分类与展示用）。
	Description string `gorm:"size:512;not null;default:''" json:"description"`
	// CreatedBy 创建来源：api | console | llm。
	CreatedBy string `gorm:"size:16;not null;default:''" json:"created_by"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 表名。
func (MemoryNamespace) TableName() string { return "memory_namespaces" }

// MemoryLink 记忆间关系（关系图的边）。
type MemoryLink struct {
	ID int64 `gorm:"primaryKey;autoIncrement" json:"id"`
	// FromID 起点记忆 id。
	FromID int64 `gorm:"not null;uniqueIndex:idx_memory_links_triple,priority:1" json:"from_id"`
	// ToID 终点记忆 id。
	ToID int64 `gorm:"not null;uniqueIndex:idx_memory_links_triple,priority:2;index" json:"to_id"`
	// Relation 关系类型。
	Relation MemoryLinkRelation `gorm:"size:16;not null;uniqueIndex:idx_memory_links_triple,priority:3" json:"relation"`
	// Note 备注（可空）。
	Note string `gorm:"size:512;not null;default:''" json:"note"`
	// CreatedBy 建边者（API Key 名 / console / system）。
	CreatedBy string `gorm:"size:128;not null;default:''" json:"created_by"`

	CreatedAt time.Time `json:"created_at"`
}

// TableName 表名。
func (MemoryLink) TableName() string { return "memory_links" }

// 记忆分类任务状态（后台任务写回 memory_settings）。
const (
	MemoryJobIdle    = "" // 未运行过 / 空闲
	MemoryJobRunning = "running"
	MemoryJobDone    = "done"
	MemoryJobFailed  = "failed"
)

// MemorySettings 记忆库设置与分类任务状态（单行表，ID 固定为 1）。
// 配置列由控制台维护；任务状态列由分类任务写回，二者分开更新，禁止整体覆盖
// （与 data_settings 同款双写者模式）。
type MemorySettings struct {
	ID int64 `gorm:"primaryKey" json:"id"`

	// ---- 分类配置（控制台可改） ----

	// ClassifierChannelID 分类调用的渠道 id（0 = 未配置）。
	ClassifierChannelID int64 `json:"classifier_channel_id"`
	// ClassifierModel 分类调用的上游模型名（空 = 未配置）。
	ClassifierModel string `gorm:"size:128;not null;default:''" json:"classifier_model"`

	// ---- 分类任务状态（分类任务维护） ----

	// JobStatus 最近一次任务状态：空 | running | done | failed。
	JobStatus string `gorm:"size:16;not null;default:''" json:"job_status"`
	// JobTotal 本次待分类记忆总数。
	JobTotal int `json:"job_total"`
	// JobDone 已处理数量。
	JobDone int `json:"job_done"`
	// JobAssigned 已归类到命名空间的数量。
	JobAssigned int `json:"job_assigned"`
	// JobNewNamespaces 新建命名空间数。
	JobNewNamespaces int `json:"job_new_namespaces"`
	// JobMessage 结果摘要或错误信息。
	JobMessage string `gorm:"size:512;not null;default:''" json:"job_message"`
	// JobStartedAt 本次任务开始时间。
	JobStartedAt *time.Time `json:"job_started_at,omitempty"`
	// JobFinishedAt 本次任务结束时间。
	JobFinishedAt *time.Time `json:"job_finished_at,omitempty"`

	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 表名。
func (MemorySettings) TableName() string { return "memory_settings" }

// ClampImportance 重要度收敛到 1~5。
func ClampImportance(v int) int {
	switch {
	case v < 1:
		return 1
	case v > 5:
		return 5
	}
	return v
}
