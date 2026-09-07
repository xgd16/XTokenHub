package service

import (
	"context"
	"strings"
	"time"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/logger"
	"xtokenhub/internal/pkg/pagination"
	"xtokenhub/internal/repository"
)

// CustomModelService 自定义模型组管理：把相似的真实模型聚合到同一个对外模型 ID 下，
// 由网关按成员优先级 + 渠道优先级自动路由。
type CustomModelService struct {
	repo repository.CustomModelRepository
}

// NewCustomModelService 构造。
func NewCustomModelService(repo repository.CustomModelRepository) *CustomModelService {
	return &CustomModelService{repo: repo}
}

// CustomModelInput 创建/更新入参。
type CustomModelInput struct {
	Name    string               `json:"name" binding:"required,max=128"`
	Members []model.ModelMember  `json:"members"`
	Status  *model.ChannelStatus `json:"status"`
	Remark  string               `json:"remark" binding:"max=512"`
}

// CustomModelView 对外视图：members 解析为数组（原始 JSON 串不下发）。
type CustomModelView struct {
	ID        int64               `json:"id"`
	Name      string              `json:"name"`
	Members   []model.ModelMember `json:"members"`
	Status    model.ChannelStatus `json:"status"`
	Remark    string              `json:"remark"`
	CreatedAt time.Time           `json:"created_at"`
	UpdatedAt time.Time           `json:"updated_at"`
}

func customModelView(cm *model.CustomModel) CustomModelView {
	members := cm.MemberList()
	if members == nil {
		members = []model.ModelMember{}
	}
	return CustomModelView{
		ID: cm.ID, Name: cm.Name, Members: members,
		Status: cm.Status, Remark: cm.Remark,
		CreatedAt: cm.CreatedAt, UpdatedAt: cm.UpdatedAt,
	}
}

// build 由入参构造规范化实体。
func (s *CustomModelService) build(in *CustomModelInput) (*model.CustomModel, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return nil, errs.New(errs.CodeInvalidParams, "模型 ID 不能为空")
	}
	cm := &model.CustomModel{Name: in.Name, Remark: in.Remark}
	cm.SetMembers(in.Members)
	if len(cm.MemberList()) == 0 {
		return nil, errs.New(errs.CodeInvalidParams, "至少需要配置一个成员模型")
	}
	if in.Status != nil {
		cm.Status = *in.Status
	} else {
		cm.Status = model.ChannelEnabled
	}
	return cm, nil
}

// Create 创建自定义模型组（模型 ID 唯一）。
func (s *CustomModelService) Create(ctx context.Context, in *CustomModelInput) (*CustomModelView, error) {
	cm, err := s.build(in)
	if err != nil {
		return nil, err
	}
	if _, err := s.repo.GetByName(ctx, cm.Name); err == nil {
		return nil, errs.New(errs.CodeInvalidParams, "模型 ID 已存在: "+cm.Name)
	}
	if err := s.repo.Create(ctx, cm); err != nil {
		return nil, err
	}
	logger.L("custom-model").Info("created", "name", cm.Name, "members", len(cm.MemberList()))
	v := customModelView(cm)
	return &v, nil
}

// Update 更新自定义模型组。
func (s *CustomModelService) Update(ctx context.Context, id int64, in *CustomModelInput) (*CustomModelView, error) {
	cm, err := s.build(in)
	if err != nil {
		return nil, err
	}
	if dup, err := s.repo.GetByName(ctx, cm.Name); err == nil && dup.ID != id {
		return nil, errs.New(errs.CodeInvalidParams, "模型 ID 已存在: "+cm.Name)
	}
	cm.ID = id
	if err := s.repo.Update(ctx, cm); err != nil {
		return nil, err
	}
	updated, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	v := customModelView(updated)
	return &v, nil
}

// Delete 删除自定义模型组。
func (s *CustomModelService) Delete(ctx context.Context, id int64) error {
	return s.repo.Delete(ctx, id)
}

// Get 查询。
func (s *CustomModelService) Get(ctx context.Context, id int64) (*CustomModelView, error) {
	cm, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	v := customModelView(cm)
	return &v, nil
}

// List 列表。
func (s *CustomModelService) List(ctx context.Context, p pagination.Params) ([]CustomModelView, int64, error) {
	items, total, err := s.repo.List(ctx, p)
	if err != nil {
		return nil, 0, err
	}
	views := make([]CustomModelView, 0, len(items))
	for i := range items {
		views = append(views, customModelView(&items[i]))
	}
	return views, total, nil
}
