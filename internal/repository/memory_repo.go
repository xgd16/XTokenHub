package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/textseg"
)

// FTS 索引列截断上限（runes）：索引只服务检索，正文全量存 memories.content。
const (
	ftsTitleRunes   = 128
	ftsContentRunes = 4000
	ftsTagsRunes    = 256
)

// MemoryRepo 记忆数据访问实现（memories 表 + memories_fts 虚表手动同步）。
type MemoryRepo struct {
	db *gorm.DB
}

// NewMemoryRepository 构造记忆仓储。
func NewMemoryRepository(db *gorm.DB) MemoryRepository {
	return &MemoryRepo{db: db}
}

// syncFTS 重建单行 FTS 索引：textseg 预切分后写入 *_seg 列（先删后插，幂等）。
func syncFTS(tx *gorm.DB, m *model.Memory) error {
	if err := tx.Exec("DELETE FROM memories_fts WHERE rowid = ?", m.ID).Error; err != nil {
		return err
	}
	return tx.Exec(
		"INSERT INTO memories_fts(rowid, title_seg, content_seg, tags_seg) VALUES (?, ?, ?, ?)",
		m.ID,
		textseg.Segmented(textseg.Clip(m.Title, ftsTitleRunes)),
		textseg.Segmented(textseg.Clip(m.Content, ftsContentRunes)),
		textseg.Segmented(textseg.Clip(m.Tags, ftsTagsRunes)),
	).Error
}

// Create 写入记忆并同步 FTS 索引。
func (r *MemoryRepo) Create(ctx context.Context, m *model.Memory) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(m).Error; err != nil {
			return err
		}
		return syncFTS(tx, m)
	})
}

// Update 全字段更新并同步 FTS 索引。
func (r *MemoryRepo) Update(ctx context.Context, m *model.Memory) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(m).Error; err != nil {
			return err
		}
		return syncFTS(tx, m)
	})
}

// Get 按 id 读取。
func (r *MemoryRepo) Get(ctx context.Context, id int64) (*model.Memory, error) {
	var m model.Memory
	err := r.db.WithContext(ctx).First(&m, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.New(errs.CodeNotFound, "记忆不存在")
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// GetByHash 同命名空间内按内容哈希查活跃记忆（写入去重）。
func (r *MemoryRepo) GetByHash(ctx context.Context, hash, namespace string) (*model.Memory, error) {
	var m model.Memory
	err := r.db.WithContext(ctx).
		Where("content_hash = ? AND namespace = ? AND status = ?", hash, namespace, model.MemoryStatusActive).
		First(&m).Error
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// ClearNamespace 把某命名空间的记忆移回未分类（清空归属来源，便于重新分类）。
func (r *MemoryRepo) ClearNamespace(ctx context.Context, namespace string) (int64, error) {
	var moved int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		moved, err = clearNamespaceTx(tx, namespace)
		return err
	})
	return moved, err
}

// clearNamespaceTx 事务内把命名空间的记忆移回未分类。
func clearNamespaceTx(tx *gorm.DB, namespace string) (int64, error) {
	if namespace == "" {
		return 0, nil
	}
	res := tx.Model(&model.Memory{}).
		Where("namespace = ?", namespace).
		Updates(map[string]any{"namespace": "", "namespace_source": model.NamespaceSourceNone})
	return res.RowsAffected, res.Error
}

// applyMemoryFilter 组装过滤条件；prefix 为列前缀（join 查询传 "m."）。
func applyMemoryFilter(q *gorm.DB, f MemoryFilter, prefix string) *gorm.DB {
	if f.Namespace != "" {
		q = q.Where(prefix+"namespace = ?", f.Namespace)
	}
	if f.Uncategorized {
		q = q.Where(prefix + "namespace = ''")
	}
	if f.Kind != "" {
		q = q.Where(prefix+"kind = ?", f.Kind)
	}
	if f.Status != "" {
		q = q.Where(prefix+"status = ?", f.Status)
	}
	if f.PinnedOnly {
		q = q.Where(prefix + "pinned = 1")
	}
	for _, tag := range f.Tags {
		// 标签以逗号分隔存储，两侧补逗号做精确成员匹配（避免子串误命中）。
		q = q.Where(`(',' || `+prefix+`tags || ',') LIKE ? ESCAPE '\'`, "%,"+escapeLike(tag)+",%")
	}
	if f.ExcludeExpired && !f.Now.IsZero() {
		q = q.Where("("+prefix+"expires_at IS NULL OR "+prefix+"expires_at > ?)", f.Now)
	}
	return q
}

// escapeLike 转义 LIKE 通配符（与 likePattern 同一替换表）。
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// Search FTS5 全文检索，按 bm25 升序返回候选。
func (r *MemoryRepo) Search(ctx context.Context, match string, f MemoryFilter, limit int) ([]MemoryHit, error) {
	if strings.TrimSpace(match) == "" {
		return []MemoryHit{}, nil
	}
	if limit <= 0 {
		limit = 50
	}
	q := r.db.WithContext(ctx).
		Table("memories_fts").
		Select("m.*, bm25(memories_fts) AS score").
		Joins("JOIN memories m ON m.id = memories_fts.rowid").
		Where("memories_fts MATCH ?", match)
	q = applyMemoryFilter(q, f, "m.")
	hits := make([]MemoryHit, 0, limit)
	if err := q.Order("score ASC").Limit(limit).Scan(&hits).Error; err != nil {
		return nil, err
	}
	return hits, nil
}

// LikeSearch 兜底子串检索（单字中文等无法进 FTS 的查询），按更新时间降序。
func (r *MemoryRepo) LikeSearch(ctx context.Context, needle string, f MemoryFilter, limit int) ([]MemoryHit, error) {
	if strings.TrimSpace(needle) == "" {
		return []MemoryHit{}, nil
	}
	if limit <= 0 {
		limit = 50
	}
	q := r.db.WithContext(ctx).Model(&model.Memory{})
	q = applyMemoryFilter(q, f, "")
	items := make([]model.Memory, 0, limit)
	err := q.Where(`(content LIKE ? ESCAPE '\' OR title LIKE ? ESCAPE '\')`,
		likePattern(needle), likePattern(needle)).
		Order("updated_at DESC").Limit(limit).Find(&items).Error
	if err != nil {
		return nil, err
	}
	hits := make([]MemoryHit, 0, len(items))
	for _, m := range items {
		hits = append(hits, MemoryHit{Memory: m})
	}
	return hits, nil
}

// List 分页列表。
func (r *MemoryRepo) List(ctx context.Context, f MemoryFilter, sort string, offset, limit int) ([]model.Memory, int64, error) {
	if limit <= 0 {
		limit = 20
	}
	q := r.db.WithContext(ctx).Model(&model.Memory{})
	q = applyMemoryFilter(q, f, "")
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := make([]model.Memory, 0, limit)
	err := q.Order(memoryOrder(sort)).Offset(offset).Limit(limit).Find(&items).Error
	return items, total, err
}

// memoryOrder 排序白名单（避免拼接注入）。
func memoryOrder(sort string) string {
	switch sort {
	case "created":
		return "pinned DESC, created_at DESC"
	case "importance":
		return "pinned DESC, importance DESC, updated_at DESC"
	case "access":
		return "pinned DESC, access_count DESC, updated_at DESC"
	default:
		return "pinned DESC, updated_at DESC"
	}
}

// GraphNodes 关系图节点：置顶/重要度/访问热度优先。
func (r *MemoryRepo) GraphNodes(ctx context.Context, f MemoryFilter, limit int) ([]model.Memory, error) {
	if limit <= 0 {
		limit = 150
	}
	q := r.db.WithContext(ctx).Model(&model.Memory{})
	q = applyMemoryFilter(q, f, "")
	items := make([]model.Memory, 0, limit)
	err := q.Order("pinned DESC, importance DESC, access_count DESC, updated_at DESC").
		Limit(limit).Find(&items).Error
	return items, err
}

// TouchAccess 批量刷新访问统计。
func (r *MemoryRepo) TouchAccess(ctx context.Context, ids []int64, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.Memory{}).
		Where("id IN ?", ids).
		Updates(map[string]any{
			"access_count":     gorm.Expr("access_count + 1"),
			"last_accessed_at": at,
		}).Error
}

// NamespaceCounts 各命名空间的 active 记忆数（键 "" 为未分类）。
func (r *MemoryRepo) NamespaceCounts(ctx context.Context) (map[string]int64, error) {
	type row struct {
		Namespace string
		N         int64
	}
	var rows []row
	err := r.db.WithContext(ctx).Model(&model.Memory{}).
		Select("namespace, COUNT(*) AS n").
		Where("status = ?", model.MemoryStatusActive).
		Group("namespace").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.Namespace] = r.N
	}
	return out, nil
}

// Stats 记忆库总览。
func (r *MemoryRepo) Stats(ctx context.Context) (*MemoryStats, error) {
	db := r.db.WithContext(ctx)
	st := &MemoryStats{ByKind: map[string]int64{}, ByNamespace: map[string]int64{}}

	type cntRow struct {
		Key string
		N   int64
	}
	var statusRows []cntRow
	if err := db.Model(&model.Memory{}).Select("status AS key, COUNT(*) AS n").Group("status").Scan(&statusRows).Error; err != nil {
		return nil, err
	}
	for _, row := range statusRows {
		// 总量口径 = 未被软删除的记忆（生效 + 被替代 + 归档）；
		// 软删除只计入 Deleted，否则「全部记忆」会数出用户看不见的回收站记录。
		switch model.MemoryStatus(row.Key) {
		case model.MemoryStatusActive:
			st.Active = row.N
		case model.MemoryStatusSuperseded:
			st.Superseded = row.N
		case model.MemoryStatusArchived:
			st.Archived = row.N
		case model.MemoryStatusDeleted:
			st.Deleted = row.N
			continue // 软删除不计入总量
		default:
			continue
		}
		st.Total += row.N
	}

	var kindRows []cntRow
	if err := db.Model(&model.Memory{}).Select("kind AS key, COUNT(*) AS n").
		Where("status = ?", model.MemoryStatusActive).Group("kind").Scan(&kindRows).Error; err != nil {
		return nil, err
	}
	for _, row := range kindRows {
		st.ByKind[row.Key] = row.N
	}

	counts, err := r.NamespaceCounts(ctx)
	if err != nil {
		return nil, err
	}
	st.Uncategorized = counts[""]
	for ns, n := range counts {
		if ns == "" {
			continue
		}
		st.ByNamespace[ns] = n
	}

	if err := db.Model(&model.MemoryLink{}).Count(&st.Links).Error; err != nil {
		return nil, err
	}
	return st, nil
}

// RebuildFTS 重建全文索引（修复用）：清空后按现存记忆（不含已删除）重灌。
func (r *MemoryRepo) RebuildFTS(ctx context.Context) (int, error) {
	n := 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM memories_fts").Error; err != nil {
			return err
		}
		var items []model.Memory
		if err := tx.Where("status <> ?", model.MemoryStatusDeleted).Find(&items).Error; err != nil {
			return err
		}
		for i := range items {
			if err := syncFTS(tx, &items[i]); err != nil {
				return err
			}
		}
		n = len(items)
		return nil
	})
	return n, err
}

// MemoryNamespaceRepo 命名空间注册表数据访问实现。
type MemoryNamespaceRepo struct {
	db *gorm.DB
}

// NewMemoryNamespaceRepository 构造命名空间仓储。
func NewMemoryNamespaceRepository(db *gorm.DB) MemoryNamespaceRepository {
	return &MemoryNamespaceRepo{db: db}
}

// List 全部命名空间，按名称升序。
func (r *MemoryNamespaceRepo) List(ctx context.Context) ([]model.MemoryNamespace, error) {
	items := make([]model.MemoryNamespace, 0)
	err := r.db.WithContext(ctx).Order("name ASC").Find(&items).Error
	return items, err
}

// GetByName 按名称读取。
func (r *MemoryNamespaceRepo) GetByName(ctx context.Context, name string) (*model.MemoryNamespace, error) {
	var ns model.MemoryNamespace
	err := r.db.WithContext(ctx).Where("name = ?", name).First(&ns).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.New(errs.CodeNotFound, "命名空间不存在: "+name)
	}
	if err != nil {
		return nil, err
	}
	return &ns, nil
}

// GetByID 按 id 读取。
func (r *MemoryNamespaceRepo) GetByID(ctx context.Context, id int64) (*model.MemoryNamespace, error) {
	var ns model.MemoryNamespace
	err := r.db.WithContext(ctx).First(&ns, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.New(errs.CodeNotFound, "命名空间不存在")
	}
	if err != nil {
		return nil, err
	}
	return &ns, nil
}

// Ensure 不存在则创建（并发冲突吞掉），返回是否新建。
func (r *MemoryNamespaceRepo) Ensure(ctx context.Context, name, description, createdBy string) (bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return false, errs.New(errs.CodeInvalidParams, "命名空间名不能为空")
	}
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&model.MemoryNamespace{
		Name: name, Description: description, CreatedBy: createdBy,
	})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// Update 更新描述。
func (r *MemoryNamespaceRepo) Update(ctx context.Context, ns *model.MemoryNamespace) error {
	return r.db.WithContext(ctx).Model(&model.MemoryNamespace{}).
		Where("id = ?", ns.ID).
		Updates(map[string]any{"description": ns.Description}).Error
}

// Rename 事务内改名并同步记忆行。
func (r *MemoryNamespaceRepo) Rename(ctx context.Context, from, to string) (int64, error) {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if from == "" || to == "" {
		return 0, errs.New(errs.CodeInvalidParams, "命名空间名不能为空")
	}
	if from == to {
		return 0, nil
	}
	var moved int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&model.MemoryNamespace{}).Where("name = ?", to).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return errs.New(errs.CodeInvalidParams, "命名空间「"+to+"」已存在，可改用合并")
		}
		res := tx.Model(&model.MemoryNamespace{}).Where("name = ?", from).Update("name", to)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errs.New(errs.CodeNotFound, "命名空间不存在: "+from)
		}
		mres := tx.Model(&model.Memory{}).Where("namespace = ?", from).Update("namespace", to)
		if mres.Error != nil {
			return mres.Error
		}
		moved = mres.RowsAffected
		return nil
	})
	return moved, err
}

// Merge 事务内把 from 的记忆并入 to 并删除 from 注册行。
func (r *MemoryNamespaceRepo) Merge(ctx context.Context, from, to string) (int64, error) {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if from == "" || to == "" {
		return 0, errs.New(errs.CodeInvalidParams, "命名空间名不能为空")
	}
	if from == to {
		return 0, errs.New(errs.CodeInvalidParams, "不能合并到自身")
	}
	var moved int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&model.MemoryNamespace{}).Where("name = ?", to).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return errs.New(errs.CodeNotFound, "目标命名空间不存在: "+to)
		}
		mres := tx.Model(&model.Memory{}).Where("namespace = ?", from).Update("namespace", to)
		if mres.Error != nil {
			return mres.Error
		}
		moved = mres.RowsAffected
		if err := tx.Where("name = ?", from).Delete(&model.MemoryNamespace{}).Error; err != nil {
			return err
		}
		return nil
	})
	return moved, err
}

// Delete 删除注册行并把其记忆移回未分类。
func (r *MemoryNamespaceRepo) Delete(ctx context.Context, id int64) (int64, error) {
	var moved int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ns model.MemoryNamespace
		if err := tx.First(&ns, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errs.New(errs.CodeNotFound, "命名空间不存在")
			}
			return err
		}
		var err error
		moved, err = clearNamespaceTx(tx, ns.Name)
		if err != nil {
			return err
		}
		return tx.Delete(&model.MemoryNamespace{}, id).Error
	})
	return moved, err
}

// MemoryLinkRepo 记忆关系数据访问实现。
type MemoryLinkRepo struct {
	db *gorm.DB
}

// NewMemoryLinkRepository 构造记忆关系仓储。
func NewMemoryLinkRepository(db *gorm.DB) MemoryLinkRepository {
	return &MemoryLinkRepo{db: db}
}

// Add 建边；已存在时幂等返回既有行。
func (r *MemoryLinkRepo) Add(ctx context.Context, l *model.MemoryLink) (*model.MemoryLink, error) {
	db := r.db.WithContext(ctx)
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(l).Error; err != nil {
		return nil, err
	}
	if l.ID != 0 {
		return l, nil
	}
	var existing model.MemoryLink
	if err := db.Where("from_id = ? AND to_id = ? AND relation = ?", l.FromID, l.ToID, l.Relation).
		First(&existing).Error; err != nil {
		return nil, err
	}
	return &existing, nil
}

// Delete 按 id 删边。
func (r *MemoryLinkRepo) Delete(ctx context.Context, id int64) error {
	db := r.db.WithContext(ctx)
	var l model.MemoryLink
	if err := db.First(&l, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errs.New(errs.CodeNotFound, "关系不存在")
		}
		return err
	}
	return db.Delete(&model.MemoryLink{}, id).Error
}

// ListByMemory 与某条记忆相关的全部边。
func (r *MemoryLinkRepo) ListByMemory(ctx context.Context, id int64) ([]model.MemoryLink, error) {
	items := make([]model.MemoryLink, 0)
	err := r.db.WithContext(ctx).
		Where("from_id = ? OR to_id = ?", id, id).
		Order("id ASC").Find(&items).Error
	return items, err
}

// DeleteAutoByMemory 回收某条记忆发出的自动边（只删指定建边者，避免误伤显式关系）。
func (r *MemoryLinkRepo) DeleteAutoByMemory(ctx context.Context, memoryID int64, createdBy string) (int64, error) {
	tx := r.db.WithContext(ctx).
		Where("from_id = ? AND created_by = ?", memoryID, createdBy).
		Delete(&model.MemoryLink{})
	return tx.RowsAffected, tx.Error
}

// ListForNodes 两端都在 ids 内的边。
func (r *MemoryLinkRepo) ListForNodes(ctx context.Context, ids []int64) ([]model.MemoryLink, error) {
	if len(ids) == 0 {
		return []model.MemoryLink{}, nil
	}
	items := make([]model.MemoryLink, 0)
	err := r.db.WithContext(ctx).
		Where("from_id IN ? AND to_id IN ?", ids, ids).
		Order("id ASC").Find(&items).Error
	return items, err
}

// Count 边总数。
func (r *MemoryLinkRepo) Count(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.MemoryLink{}).Count(&n).Error
	return n, err
}

// MemorySettingsRepo 记忆库设置数据访问实现（单行表，ID 固定为 1）。
type MemorySettingsRepo struct {
	db *gorm.DB
}

// NewMemorySettingsRepository 构建设置仓储。
func NewMemorySettingsRepository(db *gorm.DB) MemorySettingsRepository {
	return &MemorySettingsRepo{db: db}
}

// Get 读取单行设置。
func (r *MemorySettingsRepo) Get(ctx context.Context) (*model.MemorySettings, error) {
	var s model.MemorySettings
	if err := r.db.WithContext(ctx).First(&s, 1).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

// Save 全量保存（首次播种用）。
func (r *MemorySettingsRepo) Save(ctx context.Context, s *model.MemorySettings) error {
	return r.db.WithContext(ctx).Save(s).Error
}

// UpdateConfig 只更新分类配置列（双写者：不碰任务状态列）。
func (r *MemorySettingsRepo) UpdateConfig(ctx context.Context, s *model.MemorySettings) error {
	return r.db.WithContext(ctx).Model(&model.MemorySettings{}).
		Where("id = ?", 1).
		Updates(map[string]any{
			"classifier_channel_id": s.ClassifierChannelID,
			"classifier_model":      s.ClassifierModel,
		}).Error
}

// UpdateJobStatus 只更新任务状态列（双写者：不碰分类配置列）。
func (r *MemorySettingsRepo) UpdateJobStatus(ctx context.Context, st MemoryJobStatus) error {
	return r.db.WithContext(ctx).Model(&model.MemorySettings{}).
		Where("id = ?", 1).
		Updates(map[string]any{
			"job_status":         st.Status,
			"job_total":          st.Total,
			"job_done":           st.Done,
			"job_assigned":       st.Assigned,
			"job_new_namespaces": st.NewNamespaces,
			"job_message":        textseg.Clip(st.Message, 300),
			"job_started_at":     st.StartedAt,
			"job_finished_at":    st.FinishedAt,
		}).Error
}
