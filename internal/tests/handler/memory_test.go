package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// doJSONAuth 携带 Authorization 头发送 JSON 请求（记忆 API 鉴权矩阵用）。
func doJSONAuth(t *testing.T, srv *httptest.Server, method, path, bearer string, body any) (int, map[string]any, []byte) {
	t.Helper()
	var reader *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = strings.NewReader(string(b))
	} else {
		reader = strings.NewReader("")
	}
	req, _ := http.NewRequest(method, srv.URL+path, reader)
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io_readAll(resp)
	var env map[string]any
	_ = json.Unmarshal(raw, &env)
	return resp.StatusCode, env, raw
}

// createTestKey 创建一个 API Key，返回原始 key 串。
func createTestKey(t *testing.T, srv *httptest.Server, name string) string {
	t.Helper()
	code, env, _ := doJSON(t, srv, "POST", "/api/v1/keys", map[string]any{"name": name})
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("创建 key: %d %v", code, env)
	}
	return env["data"].(map[string]any)["key"].(string)
}

// TestMemoryAuthMatrix require_key 开/关下记忆 API 与控制台面的鉴权行为。
func TestMemoryAuthMatrix(t *testing.T) {
	// 强制鉴权：Agent 面 401，控制台面照常
	_, srv, _ := newTestEnvOpts(t, true)
	if code, env, _ := doJSON(t, srv, "GET", "/memory/v1/stats", nil); code != 401 {
		t.Fatalf("缺 key 应 401: %d %v", code, env)
	}
	if code, _, _ := doJSON(t, srv, "GET", "/memory/v1/stats", nil); code != 401 {
		t.Fatal("缺 key 应 401")
	}
	raw := createTestKey(t, srv, "agent-mem")
	if code, env, _ := doJSONAuth(t, srv, "GET", "/memory/v1/stats", "Bearer "+raw, nil); code != 200 || env["code"] != float64(0) {
		t.Fatalf("带 key 应 200: %d %v", code, env)
	}
	if code, env, _ := doJSONAuth(t, srv, "GET", "/memory/v1/stats", "Bearer sk-xt-bad", nil); code != 401 {
		t.Fatalf("坏 key 应 401: %d %v", code, env)
	}
	// 控制台面不受 key 影响
	if code, env, _ := doJSON(t, srv, "GET", "/api/v1/memories", nil); code != 200 || env["code"] != float64(0) {
		t.Fatalf("控制台面应放行: %d %v", code, env)
	}
	// MCP 同样受保护
	if code, _, _ := doJSON(t, srv, "POST", "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}); code != 401 {
		t.Fatalf("MCP 缺 key 应 401: %d", code)
	}

	// 不强制鉴权：匿名可用
	_, openSrv := newTestEnv(t)
	if code, env, _ := doJSON(t, openSrv, "GET", "/memory/v1/stats", nil); code != 200 || env["code"] != float64(0) {
		t.Fatalf("匿名应放行: %d %v", code, env)
	}
}

// TestMemoryAPILifecycle Agent API 全流程：写入（显式命名空间/去重）→ 检索 → 更新 → 替代 → 软删。
func TestMemoryAPILifecycle(t *testing.T) {
	_, srv := newTestEnv(t)

	// 写入（显式命名空间自动创建）
	code, env, _ := doJSON(t, srv, "POST", "/memory/v1/memories", map[string]any{
		"content": "支付回调必须做幂等校验，重复通知直接返回成功。", "namespace": "支付回调",
		"kind": "convention", "tags": []string{"支付", "幂等"}, "importance": 4,
	})
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("add: %d %v", code, env)
	}
	data := env["data"].(map[string]any)
	first := data["memory"].(map[string]any)
	id := int64(first["id"].(float64))
	if data["duplicated"] != false || first["namespace"] != "支付回调" {
		t.Fatalf("add data: %v", data)
	}

	// 同内容去重
	code, env, _ = doJSON(t, srv, "POST", "/memory/v1/memories", map[string]any{
		"content": "支付回调必须做幂等校验，重复通知直接返回成功。", "namespace": "支付回调",
	})
	if code != 200 || env["data"].(map[string]any)["duplicated"] != true {
		t.Fatalf("去重失败: %v", env)
	}

	// 检索（中文两字词走 FTS）
	code, env, _ = doJSON(t, srv, "GET", "/memory/v1/memories/search?q=幂等", nil)
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("search: %d %v", code, env)
	}
	res := env["data"].(map[string]any)
	items := res["items"].([]any)
	if res["mode"] != "fts" || len(items) != 1 {
		t.Fatalf("search data: %v", res)
	}
	nsFacets := res["namespaces"].([]any)
	if len(nsFacets) != 1 || nsFacets[0].(map[string]any)["namespace"] != "支付回调" {
		t.Fatalf("facets: %v", nsFacets)
	}

	// 单字检索走 LIKE 兜底
	code, env, _ = doJSON(t, srv, "GET", "/memory/v1/memories/search?q=幂", nil)
	if res = env["data"].(map[string]any); res["mode"] != "like" || len(res["items"].([]any)) != 1 {
		t.Fatalf("LIKE 兜底: %v", res)
	}

	// 更新
	code, env, _ = doJSON(t, srv, "PATCH", "/memory/v1/memories/"+itoa(id), map[string]any{
		"importance": 5, "pinned": true,
	})
	if code != 200 || env["data"].(map[string]any)["importance"] != float64(5) {
		t.Fatalf("update: %d %v", code, env)
	}
	// 非法 status 被拒（业务码非 0）
	if code, env, _ = doJSON(t, srv, "PATCH", "/memory/v1/memories/"+itoa(id), map[string]any{
		"status": "deleted",
	}); env["code"] == float64(0) {
		t.Fatalf("PATCH 应拒绝 deleted: %d %v", code, env)
	}

	// 新记忆替代旧记忆
	code, env, _ = doJSON(t, srv, "POST", "/memory/v1/memories", map[string]any{
		"content": "支付回调幂等校验改为按业务单号去重。", "namespace": "支付回调",
		"supersedes": []int64{id},
	})
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("supersede 写入: %d %v", code, env)
	}
	newID := int64(env["data"].(map[string]any)["memory"].(map[string]any)["id"].(float64))
	// 旧记忆默认检索不再命中
	code, env, _ = doJSON(t, srv, "GET", "/memory/v1/memories/search?q=幂等", nil)
	items = env["data"].(map[string]any)["items"].([]any)
	if len(items) != 1 || int64(items[0].(map[string]any)["id"].(float64)) != newID {
		t.Fatalf("被替代记忆不应命中: %v", items)
	}

	// 软删除
	code, env, _ = doJSON(t, srv, "DELETE", "/memory/v1/memories/"+itoa(newID), nil)
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("forget: %d %v", code, env)
	}
	if code, env, _ = doJSON(t, srv, "GET", "/memory/v1/memories/search?q=幂等", nil); len(env["data"].(map[string]any)["items"].([]any)) != 0 {
		t.Fatalf("软删除后不应命中: %v", env)
	}
}

// TestMemoryLinksAndGraph 关系边与关系图接口。
func TestMemoryLinksAndGraph(t *testing.T) {
	_, srv := newTestEnv(t)
	add := func(content string) int64 {
		code, env, _ := doJSON(t, srv, "POST", "/memory/v1/memories", map[string]any{
			"content": content, "namespace": "design",
		})
		if code != 200 || env["code"] != float64(0) {
			t.Fatalf("add: %d %v", code, env)
		}
		return int64(env["data"].(map[string]any)["memory"].(map[string]any)["id"].(float64))
	}
	a, b := add("图节点 A"), add("图节点 B")

	code, env, _ := doJSON(t, srv, "POST", "/memory/v1/memories/"+itoa(a)+"/links", map[string]any{
		"to_id": b, "relation": "related", "note": "同主题",
	})
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("link: %d %v", code, env)
	}
	linkID := int64(env["data"].(map[string]any)["id"].(float64))

	code, env, _ = doJSON(t, srv, "GET", "/memory/v1/graph?namespace=design", nil)
	g := env["data"].(map[string]any)
	if len(g["nodes"].([]any)) != 2 || len(g["edges"].([]any)) != 1 {
		t.Fatalf("graph: %v", g)
	}
	// 控制台面同一实现的图接口挂在 /api/v1/memories/graph（前端实际调用路径）
	code, env, _ = doJSON(t, srv, "GET", "/api/v1/memories/graph?namespace=design", nil)
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("console graph: %d %v", code, env)
	}
	gc := env["data"].(map[string]any)
	if len(gc["nodes"].([]any)) != 2 || len(gc["edges"].([]any)) != 1 {
		t.Fatalf("console graph data: %v", gc)
	}

	code, env, _ = doJSON(t, srv, "DELETE", "/memory/v1/memories/"+itoa(a)+"/links/"+itoa(linkID), nil)
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("unlink: %d %v", code, env)
	}
	if code, env, _ = doJSON(t, srv, "GET", "/memory/v1/memories/"+itoa(a), nil); code != 200 {
		t.Fatalf("get: %d %v", code, env)
	}
}

// TestMemoryCreateWithLinks 写入时随请求带 links / supersedes 建边。
// 回归点：service.MemoryLinkInput 曾缺 json tag，而 MCP memory_add 的 links[] 与
// 控制台 POST /memories 的 links 都直接反序列化它 → `to_id` 匹配不上 `ToID`，
// 所有显式 links 被判「含非法 to_id」，写入时建边功能形同虚设。
func TestMemoryCreateWithLinks(t *testing.T) {
	_, srv := newTestEnv(t)
	addID := func(path string, body map[string]any) int64 {
		code, env, _ := doJSON(t, srv, "POST", path, body)
		if code != 200 || env["code"] != float64(0) {
			t.Fatalf("add %s: %d %v", path, code, env)
		}
		return int64(env["data"].(map[string]any)["memory"].(map[string]any)["id"].(float64))
	}
	linksOf := func(id int64) []any {
		code, env, _ := doJSON(t, srv, "GET", "/memory/v1/memories/"+itoa(id), nil)
		if code != 200 || env["code"] != float64(0) {
			t.Fatalf("get %d: %d %v", id, code, env)
		}
		return env["data"].(map[string]any)["links"].([]any)
	}

	// 目标记忆
	target := addID("/memory/v1/memories", map[string]any{
		"content": "建边目标：限流阈值统一为 100 QPS。", "namespace": "design",
	})

	// 控制台面 + agent 面：create 时带 links 必须真的建出边
	for _, path := range []string{"/api/v1/memories", "/memory/v1/memories"} {
		created := addID(path, map[string]any{
			"content": "由 " + path + " 写入并建边：限流改为按接口维度配置。", "namespace": "design",
			"links": []map[string]any{
				{"to_id": target, "relation": "derived_from", "note": "在旧阈值上细化"},
			},
		})
		ls := linksOf(created)
		// 自动关联还会补 related 边，所以按「存在 derived_from 且字段没丢」来断言，
		// 不能数总条数
		var found map[string]any
		for _, l := range ls {
			if l.(map[string]any)["relation"] == "derived_from" {
				found = l.(map[string]any)
			}
		}
		if found == nil {
			t.Fatalf("%s create 带 links 未建出 derived_from 边: %v", path, ls)
		}
		if found["note"] != "在旧阈值上细化" {
			t.Fatalf("%s links.note 丢失: %v", path, found)
		}
		if int64(found["to_id"].(float64)) != target {
			t.Fatalf("%s links.to_id 丢失: %v", path, found)
		}
		// 反向确认目标记忆也看得到这条边
		if len(linksOf(target)) == 0 {
			t.Fatalf("%s 建边未双向可见", path)
		}
	}

	// supersedes：必须既建边又翻转旧条状态（只建边是半截结果）
	oldID := addID("/memory/v1/memories", map[string]any{
		"content": "将被推翻：限流固定 100 QPS。", "namespace": "design",
	})
	newID := addID("/api/v1/memories", map[string]any{
		"content": "推翻旧结论：限流改为动态计算。", "namespace": "design",
		"supersedes": []int64{oldID},
	})
	if newID == oldID {
		t.Fatalf("两条内容不同，不应被判重复")
	}
	var supFound bool
	for _, l := range linksOf(newID) {
		if l.(map[string]any)["relation"] == "supersedes" {
			supFound = true
		}
	}
	if !supFound {
		t.Fatalf("supersedes 未建边: %v", linksOf(newID))
	}
	code, env, _ := doJSON(t, srv, "GET", "/memory/v1/memories/"+itoa(oldID), nil)
	if code != 200 {
		t.Fatalf("get old: %d", code)
	}
	oldMem := env["data"].(map[string]any)["memory"].(map[string]any)
	if oldMem["status"] != "superseded" {
		t.Fatalf("supersedes 未翻转旧记忆状态: %v", oldMem["status"])
	}
	if int64(oldMem["superseded_by"].(float64)) != newID {
		t.Fatalf("superseded_by = %v, 期望 %d", oldMem["superseded_by"], newID)
	}
}

// TestMemoryNamespacesAndStats 命名空间清单与统计接口。
func TestMemoryNamespacesAndStats(t *testing.T) {
	_, srv := newTestEnv(t)
	for _, content := range []string{"设计记忆一", "设计记忆二"} {
		doJSON(t, srv, "POST", "/memory/v1/memories", map[string]any{"content": content, "namespace": "design"})
	}
	doJSON(t, srv, "POST", "/memory/v1/memories", map[string]any{"content": "未分类记忆"})

	code, env, _ := doJSON(t, srv, "GET", "/memory/v1/namespaces", nil)
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("namespaces: %d %v", code, env)
	}
	items := env["data"].(map[string]any)["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["name"] != "design" || items[0].(map[string]any)["count"] != float64(2) {
		t.Fatalf("namespaces data: %v", items)
	}

	code, env, _ = doJSON(t, srv, "GET", "/memory/v1/stats", nil)
	st := env["data"].(map[string]any)
	if st["active"] != float64(3) || st["uncategorized"] != float64(1) {
		t.Fatalf("stats: %v", st)
	}
	// 控制台面统计挂在 /api/v1/memories/stats（前端实际调用路径）
	code, env, _ = doJSON(t, srv, "GET", "/api/v1/memories/stats", nil)
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("console stats: %d %v", code, env)
	}
	if sc := env["data"].(map[string]any); sc["active"] != float64(3) || sc["uncategorized"] != float64(1) {
		t.Fatalf("console stats data: %v", sc)
	}
}

// TestMemoryConsoleNamespaceAdmin 控制台管理面：命名空间改名/合并/删除 + 批量移动。
func TestMemoryConsoleNamespaceAdmin(t *testing.T) {
	_, srv := newTestEnv(t)
	add := func(content, ns string) int64 {
		_, env, _ := doJSON(t, srv, "POST", "/api/v1/memories", map[string]any{"content": content, "namespace": ns})
		return int64(env["data"].(map[string]any)["memory"].(map[string]any)["id"].(float64))
	}
	a := add("第一条", "old")
	b := add("第二条", "keep")

	// 改名
	code, env, _ := doJSON(t, srv, "POST", "/api/v1/memory-namespaces/rename", map[string]any{"from": "old", "to": "new"})
	if code != 200 || env["data"].(map[string]any)["moved"] != float64(1) {
		t.Fatalf("rename: %d %v", code, env)
	}
	// 合并
	code, env, _ = doJSON(t, srv, "POST", "/api/v1/memory-namespaces/merge", map[string]any{"from": "new", "to": "keep"})
	if code != 200 || env["data"].(map[string]any)["moved"] != float64(1) {
		t.Fatalf("merge: %d %v", code, env)
	}
	// 批量移动回未分类
	code, env, _ = doJSON(t, srv, "POST", "/api/v1/memories/batch-move", map[string]any{
		"ids": []int64{a, b}, "namespace": "",
	})
	if code != 200 || env["data"].(map[string]any)["moved"] != float64(2) {
		t.Fatalf("batch-move: %d %v", code, env)
	}
	// 删除命名空间（其记忆已移走）
	_, env, _ = doJSON(t, srv, "GET", "/api/v1/memory-namespaces", nil)
	nsItems := env["data"].(map[string]any)["items"].([]any)
	if len(nsItems) != 1 {
		t.Fatalf("namespaces: %v", nsItems)
	}
	nsID := int64(nsItems[0].(map[string]any)["id"].(float64))
	if code, env, _ = doJSON(t, srv, "DELETE", "/api/v1/memory-namespaces/"+itoa(nsID), nil); env["code"] != float64(0) {
		t.Fatalf("delete namespace: %d %v", code, env)
	}
}

// TestMemoryConsoleClassify 分类配置读写与一键分类触发/进度。
func TestMemoryConsoleClassify(t *testing.T) {
	_, srv := newTestEnv(t)
	// 未配置模型：触发也应正常结束并提示
	doJSON(t, srv, "POST", "/api/v1/memories", map[string]any{"content": "未分类记忆内容"})
	code, env, _ := doJSON(t, srv, "GET", "/api/v1/memories/classify-config", nil)
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("classify-config: %d %v", code, env)
	}
	// 配置校验：渠道与模型必须成对
	if code, env, _ = doJSON(t, srv, "PUT", "/api/v1/memories/classify-config", map[string]any{
		"channel_id": 1, "model": "",
	}); env["code"] == float64(0) {
		t.Fatalf("缺模型应报错: %d %v", code, env)
	}

	// 触发 + 进度（分类桩返回空归类，任务应 done）
	code, env, _ = doJSON(t, srv, "POST", "/api/v1/memories/classify", nil)
	if code != 200 || env["code"] != float64(0) {
		t.Fatalf("trigger: %d %v", code, env)
	}
	// 异步任务极快，轮询到终态即可（每次间隔 10ms 给后台协程让出调度）
	for i := 0; i < 50; i++ {
		code, env, _ = doJSON(t, srv, "GET", "/api/v1/memories/classify", nil)
		st := env["data"].(map[string]any)
		if st["status"] == "done" || st["status"] == "failed" {
			if st["status"] != "done" {
				t.Fatalf("分类任务状态: %v", st)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("分类任务未在预期时间内结束")
}

// TestMCPHandshakeAndTools MCP 端点：握手、工具列表、工具调用与协议错误。
func TestMCPHandshakeAndTools(t *testing.T) {
	_, srv := newTestEnv(t)
	mcp := func(body any) (int, map[string]any) {
		code, env, _ := doJSON(t, srv, "POST", "/mcp", body)
		return code, env
	}

	// initialize：回显协议版本
	code, env := mcp(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2025-06-18", "clientInfo": map[string]any{"name": "test"}},
	})
	if code != 200 {
		t.Fatalf("initialize: %d", code)
	}
	res := env["result"].(map[string]any)
	if res["protocolVersion"] != "2025-06-18" || res["serverInfo"].(map[string]any)["name"] != "xtokenhub-memory" {
		t.Fatalf("initialize result: %v", res)
	}
	// instructions 是「接入即引导」通道：必须写清何时检索、何时写入（agent 一接入就知道用法）。
	ins, _ := res["instructions"].(string)
	if !strings.Contains(ins, "memory_search") || !strings.Contains(ins, "memory_add") || !strings.Contains(ins, "Markdown") {
		t.Fatalf("instructions 缺少使用时机引导: %q", ins)
	}
	// 同样地，关系绑定必须在 initialize 指令里就讲清楚；旧文案「无需手动补」会让 agent 永远不建边。
	for _, kw := range []string{"links", "supersedes", "同一命名空间"} {
		if !strings.Contains(ins, kw) {
			t.Fatalf("instructions 缺少关系绑定指引 %q: %q", kw, ins)
		}
	}
	if strings.Contains(ins, "无需手动补") {
		t.Fatalf("instructions 仍在劝 agent 不要手动建边: %q", ins)
	}

	// tools/list
	code, env = mcp(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
	tools := env["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 7 {
		t.Fatalf("tools 数量 = %d", len(tools))
	}
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.(map[string]any)["name"].(string)] = true
		if tool.(map[string]any)["inputSchema"] == nil {
			t.Fatalf("工具缺少 inputSchema: %v", tool)
		}
	}
	for _, want := range []string{"memory_add", "memory_search", "memory_get", "memory_update", "memory_forget", "memory_link", "memory_namespaces"} {
		if !names[want] {
			t.Fatalf("缺少工具 %s", want)
		}
	}

	// memory_add 的关系参数是 agent 唯一能拿到「有向语义边」的入口：
	// 自动关联只产 related 且限同命名空间，conflicts/derived_from/supersedes 全靠这里显式声明。
	// 参数没写进 schema / 工具描述还劝 agent「无需手动补」，关系图就会长期只有自动弱边。
	addSchema := map[string]any{}
	for _, tool := range tools {
		tm := tool.(map[string]any)
		if tm["name"] != "memory_add" {
			continue
		}
		addSchema = tm
	}
	if addSchema["description"] == nil {
		t.Fatal("memory_add 缺少 description")
	}
	addDesc, _ := addSchema["description"].(string)
	for _, kw := range []string{"links", "supersedes", "不会产出", "孤点"} {
		if !strings.Contains(addDesc, kw) {
			t.Fatalf("memory_add description 缺少关系指引 %q: %s", kw, addDesc)
		}
	}
	// 旧文案「无需手动补」会把 agent 引导到不建边，必须已移除。
	if strings.Contains(addDesc, "无需手动补") {
		t.Fatalf("memory_add description 仍在劝 agent 不要手动建边: %s", addDesc)
	}
	props, _ := addSchema["inputSchema"].(map[string]any)["properties"].(map[string]any)
	for _, key := range []string{"links", "supersedes"} {
		prop, ok := props[key].(map[string]any)
		if !ok {
			t.Fatalf("memory_add 缺少参数 %s", key)
		}
		if prop["description"] == nil || prop["description"] == "" {
			t.Fatalf("memory_add.%s 缺少 description（agent 无从判断何时使用）", key)
		}
	}
	// links[].relation 必须是带说明的枚举，否则 agent 只能猜合法值
	linkItems := props["links"].(map[string]any)["items"].(map[string]any)
	rel, _ := linkItems["properties"].(map[string]any)["relation"].(map[string]any)
	if rel == nil || rel["description"] == nil {
		t.Fatalf("memory_add.links[].relation 缺少说明: %v", rel)
	}
	if got := rel["enum"].([]any); len(got) != 4 {
		t.Fatalf("relation 枚举 = %v", got)
	}

	// tools/call memory_add
	code, env = mcp(map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "tools/call",
		"params": map[string]any{
			"name": "memory_add",
			"arguments": map[string]any{
				"content": "MCP 写入的记忆：数据库连接池按 4 配置。", "namespace": "配置约定",
			},
		},
	})
	res = env["result"].(map[string]any)
	if res["isError"] != false {
		t.Fatalf("memory_add 失败: %v", res)
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "配置约定") || !strings.Contains(text, "duplicated") {
		t.Fatalf("memory_add 返回: %s", text)
	}
	// 记下这条的 id：下面的建边用例要用真实 id（agent 也必须先 memory_search 查到）
	var added struct {
		Memory struct {
			ID int64 `json:"id"`
		} `json:"memory"`
	}
	if err := json.Unmarshal([]byte(text), &added); err != nil || added.Memory.ID == 0 {
		t.Fatalf("解析 memory_add 返回拿 id 失败: %v %s", err, text)
	}
	baseID := added.Memory.ID

	// tools/call memory_search（中文检索）
	code, env = mcp(map[string]any{
		"jsonrpc": "2.0", "id": 4, "method": "tools/call",
		"params": map[string]any{"name": "memory_search", "arguments": map[string]any{"query": "连接池"}},
	})
	text = env["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "配置约定") {
		t.Fatalf("memory_search 未命中: %s", text)
	}

	// 端到端：memory_add 显式传 links / supersedes 必须真的建边。
	// 只断言 schema 描述是不够的——真正保证 agent 建边的是这条链路可用。
	code, env = mcp(map[string]any{
		"jsonrpc": "2.0", "id": 7, "method": "tools/call",
		"params": map[string]any{
			"name": "memory_add",
			"arguments": map[string]any{
				"content": "MCP 显式建边：连接池配置改为按 CPU 核数计算。", "namespace": "配置约定",
				"links": []map[string]any{
					{"to_id": baseID, "relation": "derived_from", "note": "在旧结论上细化"},
				},
			},
		},
	})
	if env["result"].(map[string]any)["isError"] != false {
		t.Fatalf("带 links 的 memory_add 失败: %v", env)
	}
	linkText := env["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(linkText, "links") || !strings.Contains(linkText, "derived_from") {
		t.Fatalf("memory_add 未回显 links: %s", linkText)
	}
	// 原记忆应多出一条 derived_from 边
	code, env = mcp(map[string]any{
		"jsonrpc": "2.0", "id": 8, "method": "tools/call",
		"params": map[string]any{"name": "memory_get", "arguments": map[string]any{"id": baseID}},
	})
	if code != 200 {
		t.Fatalf("memory_get: %d", code)
	}
	if !strings.Contains(env["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string), "derived_from") {
		t.Fatalf("显式 links 未建边: %v", env)
	}

	// supersedes 必须同时「建边 + 翻转旧条状态」，不能只建边
	code, env = mcp(map[string]any{
		"jsonrpc": "2.0", "id": 9, "method": "tools/call",
		"params": map[string]any{
			"name":      "memory_add",
			"arguments": map[string]any{"content": "MCP 推翻旧结论：连接池一律固定为 4。", "namespace": "配置约定", "supersedes": []int64{baseID}},
		},
	})
	if env["result"].(map[string]any)["isError"] != false {
		t.Fatalf("带 supersedes 的 memory_add 失败: %v", env)
	}
	code, env = mcp(map[string]any{
		"jsonrpc": "2.0", "id": 10, "method": "tools/call",
		"params": map[string]any{"name": "memory_get", "arguments": map[string]any{"id": baseID}},
	})
	oldText := env["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(oldText, "superseded") {
		t.Fatalf("supersedes 未翻转旧记忆状态: %s", oldText)
	}
	if !strings.Contains(oldText, "supersedes") {
		t.Fatalf("supersedes 未建边: %s", oldText)
	}

	// 工具执行错误 → result.isError（不是协议错误）
	code, env = mcp(map[string]any{
		"jsonrpc": "2.0", "id": 5, "method": "tools/call",
		"params": map[string]any{"name": "memory_get", "arguments": map[string]any{"id": 99999}},
	})
	if env["result"].(map[string]any)["isError"] != true {
		t.Fatalf("坏 id 应 isError: %v", env)
	}

	// 未知方法 → JSON-RPC error
	code, env = mcp(map[string]any{"jsonrpc": "2.0", "id": 6, "method": "resources/list"})
	if env["error"] == nil {
		t.Fatalf("未知方法应报错: %v", env)
	}

	// 通知 → 202 无响应体
	req, _ := http.NewRequest("POST", srv.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 202 {
		t.Fatalf("通知应 202: %v %v", err, resp)
	}
	resp.Body.Close()

	// 批量请求 → 拒绝
	code, env = mcp([]any{map[string]any{"jsonrpc": "2.0", "id": 7, "method": "ping"}})
	if env["error"] == nil {
		t.Fatalf("批量应拒绝: %v", env)
	}

	// GET /mcp → 405
	req, _ = http.NewRequest("GET", srv.URL+"/mcp", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 405 {
		t.Fatalf("GET 应 405: %v %v", err, resp)
	}
	resp.Body.Close()
}
