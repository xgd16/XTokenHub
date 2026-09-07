package pagination_test

import (
	"xtokenhub/internal/pkg/pagination"

	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		name                string
		page, perPage       int
		wantPage, wantPer   int
		wantOffset, wantLim int
	}{
		{"默认值", 0, 0, 1, 20, 0, 20},
		{"正常", 3, 50, 3, 50, 100, 50},
		{"page负数", -2, 10, 1, 10, 0, 10},
		{"perPage超上限", 1, 9999, 1, 200, 0, 200},
		{"perPage负数", 2, -1, 2, 20, 20, 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := pagination.Normalize(tt.page, tt.perPage)
			if p.Page != tt.wantPage || p.PerPage != tt.wantPer || p.Offset != tt.wantOffset || p.Limit != tt.wantLim {
				t.Errorf("pagination.Normalize(%d,%d) = %+v", tt.page, tt.perPage, p)
			}
		})
	}
}

func TestFromGin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Request = httptest.NewRequest("GET", "/?page=2&per_page=15", nil)
	p := pagination.FromGin(c)
	if p.Page != 2 || p.PerPage != 15 || p.Offset != 15 {
		t.Errorf("FromGin = %+v", p)
	}
}
