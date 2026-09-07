package resp_test

import (
	"xtokenhub/internal/pkg/resp"

	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"xtokenhub/internal/pkg/errs"
)

func init() { gin.SetMode(gin.TestMode) }

func newRecorder(handler func(c *gin.Context)) (*httptest.ResponseRecorder, resp.Envelope) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/x", handler)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	r.ServeHTTP(w, req)
	var env resp.Envelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	return w, env
}

func TestOK(t *testing.T) {
	w, env := newRecorder(func(c *gin.Context) { resp.OK(c, gin.H{"id": 1}) })
	if w.Code != 200 || env.Code != errs.CodeOK || env.Message != "ok" {
		t.Fatalf("code=%d body=%v", w.Code, env)
	}
	data, _ := env.Data.(map[string]any)
	if data["id"] != float64(1) {
		t.Errorf("data = %v", env.Data)
	}
}

func TestOKPage(t *testing.T) {
	_, env := newRecorder(func(c *gin.Context) { resp.OKPage(c, []int{1, 2}, 42, 1, 20) })
	page, _ := env.Data.(map[string]any)
	if page["total"] != float64(42) || page["per_page"] != float64(20) {
		t.Errorf("page = %v", page)
	}
}

func TestFailHTTPStatus(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   int
	}{
		{"参数错误->400", errs.New(errs.CodeInvalidParams, "bad"), 400, errs.CodeInvalidParams},
		{"不存在->404", errs.New(errs.CodeNotFound, "no"), 404, errs.CodeNotFound},
		{"内部->500", errs.New(errs.CodeInternal, "oops"), 500, errs.CodeInternal},
		{"业务码保持200", errs.New(errs.CodeNoChannel, "no channel"), 200, errs.CodeNoChannel},
		{"普通错误归一化", errors.New("boom"), 500, errs.CodeUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, env := newRecorder(func(c *gin.Context) { resp.Fail(c, tt.err) })
			if w.Code != tt.wantStatus || env.Code != tt.wantCode {
				t.Errorf("status=%d code=%d, want %d/%d", w.Code, env.Code, tt.wantStatus, tt.wantCode)
			}
		})
	}
}
