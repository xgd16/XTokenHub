package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/pagination"
	"xtokenhub/internal/repository"
)

// KeyService 网关密钥业务：生成、管理、鉴权校验。
type KeyService struct {
	repo repository.APIKeyRepository
}

// NewKeyService 构造密钥服务。
func NewKeyService(repo repository.APIKeyRepository) *KeyService {
	return &KeyService{repo: repo}
}

// KeyInput 创建/更新密钥入参（key 由服务端生成，不可通过入参指定）。
type KeyInput struct {
	Name   string           `json:"name" binding:"required,max=128"`
	Status *model.KeyStatus `json:"status"`
	Remark string           `json:"remark" binding:"max=512"`
}

// GenerateKey 生成随机网关密钥：sk-xt- + 32 位十六进制。
func GenerateKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return model.KeyPrefix + hex.EncodeToString(b), nil
}

// Create 创建密钥（自动生成随机 key；key 原文撞唯一索引时重新生成重试）。
func (s *KeyService) Create(ctx context.Context, in *KeyInput) (*model.APIKey, error) {
	if _, err := s.repo.GetByName(ctx, in.Name); err == nil {
		return nil, errs.New(errs.CodeInvalidParams, "密钥名已存在")
	}
	const attempts = 3
	for i := 0; i < attempts; i++ {
		raw, err := GenerateKey()
		if err != nil {
			return nil, err
		}
		k := &model.APIKey{
			Name:   in.Name,
			Key:    raw,
			Status: model.KeyEnabled,
			Remark: in.Remark,
		}
		if in.Status != nil {
			k.Status = *in.Status
		}
		if err := s.repo.Create(ctx, k); err != nil {
			// 仅 key 原文重复时重试（16 字节随机，概率可忽略）；其余错误原样返回
			if _, gerr := s.repo.GetByKey(ctx, raw); gerr == nil {
				continue
			}
			return nil, err
		}
		return k, nil
	}
	return nil, errs.New(errs.CodeInternal, "生成密钥失败")
}

// Update 更新密钥（名称/备注/状态；key 本体不可改）。
func (s *KeyService) Update(ctx context.Context, id int64, in *KeyInput) (*model.APIKey, error) {
	k, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if dup, err := s.repo.GetByName(ctx, in.Name); err == nil && dup.ID != id {
		return nil, errs.New(errs.CodeInvalidParams, "密钥名已存在")
	}
	k.Name = in.Name
	k.Remark = in.Remark
	if in.Status != nil {
		k.Status = *in.Status
	}
	if err := s.repo.Update(ctx, k); err != nil {
		return nil, err
	}
	return k, nil
}

// Delete 删除密钥（历史日志保留 key_id/key_name 快照）。
func (s *KeyService) Delete(ctx context.Context, id int64) error {
	return s.repo.Delete(ctx, id)
}

// Get 查询密钥。
func (s *KeyService) Get(ctx context.Context, id int64) (*model.APIKey, error) {
	return s.repo.Get(ctx, id)
}

// List 密钥列表。
func (s *KeyService) List(ctx context.Context, p pagination.Params) ([]model.APIKey, int64, error) {
	return s.repo.List(ctx, p)
}

// Check 网关鉴权：按原文查 key，且必须处于启用状态。
func (s *KeyService) Check(ctx context.Context, raw string) (*model.APIKey, error) {
	k, err := s.repo.GetByKey(ctx, raw)
	if err != nil {
		return nil, err
	}
	if k.Status != model.KeyEnabled {
		return nil, errs.New(errs.CodeInvalidParams, "密钥已停用")
	}
	return k, nil
}
