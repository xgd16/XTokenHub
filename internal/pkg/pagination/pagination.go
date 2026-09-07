// Package pagination 提供列表接口的分页参数解析与 GORM 应用。
package pagination

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	DefaultPage    = 1
	DefaultPerPage = 20
	MaxPerPage     = 200
)

// Params 归一化后的分页参数。
type Params struct {
	Page    int   `json:"page"`
	PerPage int   `json:"per_page"`
	Offset  int   `json:"-"`
	Limit   int   `json:"-"`
	Total   int64 `json:"-"`
}

// FromGin 从查询参数解析分页参数并做边界修正。
func FromGin(c *gin.Context) Params {
	page, _ := strconv.Atoi(c.DefaultQuery("page", strconv.Itoa(DefaultPage)))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", strconv.Itoa(DefaultPerPage)))
	return Normalize(page, perPage)
}

// Normalize 修正非法值：page<1 -> 1；perPage 越界 -> 默认/上限。
func Normalize(page, perPage int) Params {
	if page < 1 {
		page = DefaultPage
	}
	switch {
	case perPage < 1:
		perPage = DefaultPerPage
	case perPage > MaxPerPage:
		perPage = MaxPerPage
	}
	return Params{Page: page, PerPage: perPage, Offset: (page - 1) * perPage, Limit: perPage}
}

// Apply 在已含 Count 结果的查询上应用分页，返回 total 与查询对象。
// 用法：
//
//	q := db.Model(&X{}).Where(...)
//	paged, total, err := pagination.Apply(q, p, &items)
func Apply(q *gorm.DB, p Params, dest any) (*gorm.DB, int64, error) {
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return q, 0, err
	}
	p.Total = total
	err := q.Offset(p.Offset).Limit(p.Limit).Find(dest).Error
	return q, total, err
}
