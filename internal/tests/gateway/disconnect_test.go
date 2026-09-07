package gateway_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"xtokenhub/internal/gateway"
	"xtokenhub/internal/model"
	"xtokenhub/internal/provider"
)

// 客户端断开（inbound ctx 取消）：
//  1. 不应再向后续候选空转 failover（ctx 取消会连带取消上游请求）；
//  2. 请求日志仍须落库（finish 使用与请求解耦的 context），
//     且错误语义为「客户端连接中断」而非渠道失败。
func TestHandleClientDisconnect(t *testing.T) {
	var hits atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		time.Sleep(200 * time.Millisecond)
	}))
	defer up.Close()

	chs := &memChannels{items: []model.Channel{
		{ID: 1, Name: "a", BaseURL: up.URL, Provider: model.ProviderOpenAICompatible,
			Status: model.ChannelEnabled, Protocols: "chat_completions"},
		{ID: 2, Name: "b", BaseURL: up.URL, Provider: model.ProviderOpenAICompatible,
			Status: model.ChannelEnabled, Protocols: "chat_completions"},
	}}
	logs := &memLogs{}
	exec := gateway.NewExecutor(chs, logs, nil, time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 模拟客户端已断开

	_, w := ginCtx(t)
	exec.Handle(ctx, w, &gateway.Request{
		Protocol: model.ProtocolChatCompletions,
		Body:     []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}],"stream":true}`),
		Params:   provider.ReqParams{Model: "m", Stream: true},
	})

	if n := hits.Load(); n != 0 {
		t.Errorf("ctx 已取消不应发起上游请求, hits=%d", n)
	}
	snap := logs.snapshot()
	if len(snap) != 1 {
		t.Fatalf("断开后日志仍应落库, logs=%d", len(snap))
	}
	if snap[0].Error != "客户端连接中断" {
		t.Errorf("错误语义: %q", snap[0].Error)
	}
}
