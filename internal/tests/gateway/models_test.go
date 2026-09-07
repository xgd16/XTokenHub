package gateway_test

import (
	"context"
	"testing"
	"time"

	"xtokenhub/internal/gateway"
	"xtokenhub/internal/model"
)

// AvailableModels 聚合启用渠道显式配置的模型清单：去重、排序；
// models 为空的渠道（支持全部）无法枚举，不产生条目。
func TestAvailableModels(t *testing.T) {
	src := &memChannels{items: []model.Channel{
		{Models: "gpt-4o,claude-sonnet-4-5", Status: model.ChannelEnabled},
		{Models: "deepseek-chat,gpt-4o", Status: model.ChannelEnabled},
		{Models: "", Status: model.ChannelEnabled},
		{Models: "disabled-model", Status: model.ChannelDisabled},
	}}
	exec := gateway.NewExecutor(src, &memLogs{}, nil, time.Second)

	ms, err := exec.AvailableModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"claude-sonnet-4-5", "deepseek-chat", "gpt-4o"}
	if len(ms) != len(want) {
		t.Fatalf("models = %v, want %v", ms, want)
	}
	for i := range want {
		if ms[i] != want[i] {
			t.Errorf("models[%d] = %q, want %q", i, ms[i], want[i])
		}
	}
}

func TestAvailableModelsEmpty(t *testing.T) {
	exec := gateway.NewExecutor(&memChannels{}, &memLogs{}, nil, time.Second)
	ms, err := exec.AvailableModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 0 {
		t.Errorf("无渠道应返回空列表, got %v", ms)
	}
}

// 自定义模型 ID 应出现在 /v1/models 能力清单中；停用分组不出现。
func TestAvailableModelsIncludesCustomGroups(t *testing.T) {
	exec := gateway.NewExecutor(&memChannels{items: []model.Channel{
		{Models: "gpt-4o", Status: model.ChannelEnabled},
	}}, &memLogs{}, nil, time.Second)
	enabled := model.CustomModel{Name: "free-1M", Status: model.ChannelEnabled}
	disabled := model.CustomModel{Name: "hidden-group", Status: model.ChannelDisabled}
	exec.SetCustomModels(&memCustoms{items: []model.CustomModel{enabled, disabled}})

	ms, err := exec.AvailableModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"free-1M", "gpt-4o"}
	if len(ms) != len(want) {
		t.Fatalf("models = %v, want %v", ms, want)
	}
	for i := range want {
		if ms[i] != want[i] {
			t.Errorf("models[%d] = %q, want %q", i, ms[i], want[i])
		}
	}
}
