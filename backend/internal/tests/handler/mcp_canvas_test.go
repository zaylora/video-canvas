package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/handler/mcp"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	agentsvc "video-canvas/internal/service/agent"
)

// mcpCall 记录一次工具调用的入参，用来验证网关有没有把画布翻译对。
type mcpCall struct {
	name string
	rc   agentsvc.RunContext
	args map[string]any
}

type mcpStubTools struct{ last *mcpCall }

func (s *mcpStubTools) ExecuteExternalTool(_ context.Context, rc agentsvc.RunContext, _, name string, args json.RawMessage) (*agentsvc.ToolResult, error) {
	s.last = &mcpCall{name: name, rc: rc, args: map[string]any{}}
	_ = json.Unmarshal(args, &s.last.args)
	return &agentsvc.ToolResult{Content: "ok"}, nil
}

// mcpStubCanvases 是内存版画布目录：只有登记过的 (user, canvas) 组合才算存在。
type mcpStubCanvases struct{ owned map[uint64][]uint64 }

func (s *mcpStubCanvases) GetByID(_ context.Context, userID, id uint64) (*model.CanvasProject, error) {
	for _, c := range s.owned[userID] {
		if c == id {
			return &model.CanvasProject{ID: id, Title: "画布"}, nil
		}
	}
	return nil, errors.New("not found")
}

func (s *mcpStubCanvases) List(_ context.Context, userID uint64, _ string, _, limit int) ([]model.CanvasProjectItem, int64, error) {
	ids := s.owned[userID]
	out := make([]model.CanvasProjectItem, 0, len(ids))
	for i, id := range ids {
		if i >= limit {
			break
		}
		out = append(out, model.CanvasProjectItem{ID: id, Title: "画布"})
	}
	return out, int64(len(ids)), nil
}

// scopeSet 模拟认证后拿到的权限集合：后端在 ScopeSet() 里已经补全了隐含依赖，
// 所以测试也要按展开后的形态塞进上下文，否则测的就不是线上真实行为。
func scopeSet(scopes ...string) map[string]bool {
	set := map[string]bool{}
	for _, s := range model.ExpandMCPScopes(scopes) {
		set[s] = true
	}
	return set
}

// newMCPRouter 起一个只挂了 /mcp 的路由，身份由测试直接塞进上下文（省掉令牌认证）。
func newMCPRouter(tools mcp.ToolExecutor, canvases mcp.CanvasDirectory, userID, defaultCanvas uint64, scopes map[string]bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	srv := mcp.NewServer(tools, canvases)
	r := gin.New()
	r.POST("/mcp", func(c *gin.Context) {
		c.Set(middleware.CtxMCPProjectIDKey, uint64(1))
		c.Set(middleware.CtxMCPDefaultCanvasKey, defaultCanvas)
		c.Set(middleware.CtxMCPScopesKey, scopes)
		c.Set(middleware.CtxUserIDKey, uint(userID))
		c.Next()
	}, srv.Handle)
	return r
}

func postMCP(t *testing.T, r *gin.Engine, body string) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是 JSON：%s", w.Body.String())
	}
	return out
}

// resultText 取出 result 里的第一段文字与 isError 标记。
func resultText(t *testing.T, resp map[string]any) (string, bool) {
	t.Helper()
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("响应里没有 result：%v", resp)
	}
	flag, _ := res["isError"].(bool)
	content, _ := res["content"].([]any)
	if len(content) == 0 {
		return "", flag
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	return text, flag
}

func callToolBody(name, args string) string {
	return `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + name + `","arguments":` + args + `}}`
}

// slowMCPTools 模拟一个跑不完的工具：一直等到 ctx 被取消。
type slowMCPTools struct{}

func (slowMCPTools) ExecuteExternalTool(ctx context.Context, _ agentsvc.RunContext, _, _ string, _ json.RawMessage) (*agentsvc.ToolResult, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// TestMCPCallTimeoutReturnsActionableError 单次调用超过上限要中止，并回一条模型能照着做的 isError 结果，
// 而不是让客户端一直挂着（也检查那条 ctx 真的传到了工具层）。
func TestMCPCallTimeoutReturnsActionableError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := mcp.NewServer(slowMCPTools{}, &mcpStubCanvases{owned: map[uint64][]uint64{7: {18}}})
	srv.SetCallTimeout(func(context.Context) time.Duration { return 30 * time.Millisecond })
	r := gin.New()
	r.POST("/mcp", func(c *gin.Context) {
		c.Set(middleware.CtxMCPProjectIDKey, uint64(1))
		c.Set(middleware.CtxMCPDefaultCanvasKey, uint64(0))
		c.Set(middleware.CtxMCPScopesKey, scopeSet(model.MCPScopeCanvasRead))
		c.Set(middleware.CtxUserIDKey, uint(7))
		c.Next()
	}, srv.Handle)

	resp := postMCP(t, r, callToolBody("canvas_get_state", `{"canvas":"`+idcodec.Encode(18)+`"}`))
	text, isErr := resultText(t, resp)
	if !isErr || !strings.Contains(text, "超过") || !strings.Contains(text, "拆小") {
		t.Fatalf("超时要回可执行的 isError 结果：%q %v", text, isErr)
	}
}

// TestMCPCanvasDefaultsWhenNotSpecified 不指定画布时用项目配的默认画布，并把它翻成 RunContext.CanvasID。
func TestMCPCanvasDefaultsWhenNotSpecified(t *testing.T) {
	tools := &mcpStubTools{}
	canvases := &mcpStubCanvases{owned: map[uint64][]uint64{7: {4}}}
	r := newMCPRouter(tools, canvases, 7, 4, scopeSet(model.MCPScopeCanvasRead))

	resp := postMCP(t, r, callToolBody("canvas_get_state", `{}`))
	if text, flag := resultText(t, resp); flag {
		t.Fatalf("有默认画布时不该失败：%s", text)
	}
	if tools.last == nil || tools.last.rc.CanvasID != 4 {
		t.Fatalf("画布没落到默认值：%+v", tools.last)
	}
	if _, ok := tools.last.args["canvas"]; ok {
		t.Fatal("canvas 是网关字段，不该传给工具")
	}
}

// TestMCPCanvasLessToolsNeedNoCanvas 模型 / 技能 / 任务类工具不落在画布上：
// 项目没配默认画布、这次也没指定 canvas 时也必须能跑。它们此前被「非后台模式必须有画布」
// 挡住过（线上 task_get 直接报「运行上下文缺少 canvas_id」），这条用例守住这个口子。
func TestMCPCanvasLessToolsNeedNoCanvas(t *testing.T) {
	for _, name := range []string{"task_get", "model_list", "skill_search", "skill_read"} {
		tools := &mcpStubTools{}
		canvases := &mcpStubCanvases{owned: map[uint64][]uint64{7: {4}}}
		r := newMCPRouter(tools, canvases, 7, 0, scopeSet(model.MCPScopeCanvasBrowse))

		resp := postMCP(t, r, callToolBody(name, `{}`))
		text, flag := resultText(t, resp)
		if flag {
			t.Fatalf("%s 不该要求画布：%s", name, text)
		}
		if tools.last == nil {
			t.Fatalf("%s 没落到工具层", name)
		}
		if !tools.last.rc.NoCanvas || tools.last.rc.CanvasID != 0 {
			t.Fatalf("%s 的上下文不对：%+v", name, tools.last.rc)
		}
		if err := agentsvc.ValidateRunContextForTest(tools.last.rc); err != nil {
			t.Fatalf("%s 的上下文应该被判为合法：%v", name, err)
		}
	}

	// 画布类工具仍然必须带画布：别把上面的放行顺手漏成「都能不要画布」。
	tools := &mcpStubTools{}
	r := newMCPRouter(tools, &mcpStubCanvases{owned: map[uint64][]uint64{7: {4}}}, 7, 0, scopeSet(model.MCPScopeCanvasRead))
	if text, flag := resultText(t, postMCP(t, r, callToolBody("canvas_get_state", `{}`))); !flag {
		t.Fatalf("画布工具没画布时必须拦住，得到 %q", text)
	}
}

// TestMCPCanvasMustBelongToUser 指定别人的画布必须拒绝：这是外部接入唯一的资源边界。
func TestMCPCanvasMustBelongToUser(t *testing.T) {
	tools := &mcpStubTools{}
	canvases := &mcpStubCanvases{owned: map[uint64][]uint64{7: {4}, 9: {99}}}
	r := newMCPRouter(tools, canvases, 7, 0, scopeSet(model.MCPScopeCanvasRead))

	resp := postMCP(t, r, callToolBody("canvas_get_state", `{"canvas":"`+idcodec.Encode(99)+`"}`))
	text, flag := resultText(t, resp)
	if !flag {
		t.Fatalf("别人的画布应该被拒：%v", resp)
	}
	if !strings.Contains(text, "canvas_list") {
		t.Fatalf("拒绝时要告诉模型下一步怎么做，得到 %q", text)
	}
	if tools.last != nil {
		t.Fatal("被拒的调用不该落到工具层")
	}
}

// TestMCPCanvasRequiredWithoutDefault 既没指定画布、项目也没配默认画布时要拦住，并引导先列画布。
func TestMCPCanvasRequiredWithoutDefault(t *testing.T) {
	tools := &mcpStubTools{}
	canvases := &mcpStubCanvases{owned: map[uint64][]uint64{7: {4}}}
	r := newMCPRouter(tools, canvases, 7, 0, scopeSet(model.MCPScopeCanvasRead))

	resp := postMCP(t, r, callToolBody("canvas_get_state", `{}`))
	text, flag := resultText(t, resp)
	if !flag || !strings.Contains(text, "canvas_list") {
		t.Fatalf("缺画布时应该给出可纠正的提示，得到 %q", text)
	}
	if tools.last != nil {
		t.Fatal("没有画布时不该调工具")
	}
}

// TestMCPCanvasExplicitOverridesDefault 显式指定的画布优先于默认画布。
func TestMCPCanvasExplicitOverridesDefault(t *testing.T) {
	tools := &mcpStubTools{}
	canvases := &mcpStubCanvases{owned: map[uint64][]uint64{7: {4, 5}}}
	r := newMCPRouter(tools, canvases, 7, 4, scopeSet(model.MCPScopeCanvasRead))

	postMCP(t, r, callToolBody("canvas_get_state", `{"canvas":"`+idcodec.Encode(5)+`"}`))
	if tools.last == nil || tools.last.rc.CanvasID != 5 {
		t.Fatalf("显式画布没有覆盖默认值：%+v", tools.last)
	}
}

// TestMCPCanvasListOnlyOwned 列画布只返回本人名下，且 id 是对外的 code。
func TestMCPCanvasListOnlyOwned(t *testing.T) {
	tools := &mcpStubTools{}
	canvases := &mcpStubCanvases{owned: map[uint64][]uint64{7: {4, 5}, 9: {99}}}
	r := newMCPRouter(tools, canvases, 7, 0, scopeSet(model.MCPScopeCanvasRead))

	resp := postMCP(t, r, callToolBody("canvas_list", `{}`))
	text, flag := resultText(t, resp)
	if flag {
		t.Fatalf("列画布不该失败：%s", text)
	}
	var body struct {
		Canvases []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"canvases"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal([]byte(text), &body); err != nil {
		t.Fatalf("画布列表不是 JSON：%s", text)
	}
	if body.Total != 2 || len(body.Canvases) != 2 {
		t.Fatalf("只该看到自己的 2 块画布：%+v", body)
	}
	if body.Canvases[0].ID != idcodec.Encode(4) {
		t.Fatalf("画布 id 应该是对外 code，得到 %q", body.Canvases[0].ID)
	}
	if tools.last != nil {
		t.Fatal("canvas_list 由网关自己实现，不该走工具层")
	}
}

// TestMCPCanvasListNeedsBrowse 给任何画布档位都隐含 browse，所以都能列画布；一个都不给才看不到。
func TestMCPCanvasListNeedsBrowse(t *testing.T) {
	tools := &mcpStubTools{}
	canvases := &mcpStubCanvases{owned: map[uint64][]uint64{7: {4}}}
	r := newMCPRouter(tools, canvases, 7, 0, scopeSet(model.MCPScopeCanvasWrite))

	resp := postMCP(t, r, callToolBody("canvas_list", `{}`))
	if _, flag := resultText(t, resp); flag {
		t.Fatalf("写画布隐含浏览目录，应该能列画布：%v", resp)
	}

	// 一个画布权限都没有就看不到，也不能调
	empty := newMCPRouter(tools, canvases, 7, 0, scopeSet())
	resp = postMCP(t, empty, callToolBody("canvas_list", `{}`))
	if _, ok := resp["error"]; !ok {
		t.Fatalf("一个画布档位都不给时调用应该被拒：%v", resp)
	}
}

// TestMCPWriteImpliesRead 权限之间不互斥：只勾「改画布」的令牌，也能读画布。
// 少了这一步，工具改画布前先读会被拒，等于给了把用不了的钥匙。
func TestMCPWriteImpliesRead(t *testing.T) {
	tools := &mcpStubTools{}
	canvases := &mcpStubCanvases{owned: map[uint64][]uint64{7: {4}}}
	r := newMCPRouter(tools, canvases, 7, 4, scopeSet(model.MCPScopeCanvasWrite))

	resp := postMCP(t, r, callToolBody("canvas_get_state", `{}`))
	if text, flag := resultText(t, resp); flag {
		t.Fatalf("写画布隐含读画布，读操作不该被拒：%s", text)
	}

	// 反过来不成立：只勾读画布不能改
	readOnly := newMCPRouter(tools, canvases, 7, 4, scopeSet(model.MCPScopeCanvasRead))
	resp = postMCP(t, readOnly, callToolBody("canvas_apply_ops", `{"ops":[]}`))
	if _, ok := resp["error"]; !ok {
		t.Fatalf("只读档位不能改画布：%v", resp)
	}
}

// TestMCPGenerateToolDescribesExternalSemantics 外部接入的生成工具，说明与 schema 都要按这条通道的真实行为写：
// 提交即返回、用 task_get 查进度；wait 在内置流程里才有意义，外部接入留着只会让模型白等。
func TestMCPGenerateToolDescribesExternalSemantics(t *testing.T) {
	r := newMCPRouter(&mcpStubTools{}, &mcpStubCanvases{}, 7, 0, scopeSet(model.MCPScopeCanvasGenerate))
	resp := postMCP(t, r, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/list 没回 result：%v", resp)
	}
	list, _ := res["tools"].([]any)
	var found map[string]any
	for _, item := range list {
		tool, _ := item.(map[string]any)
		if tool["name"] == "generate_media" {
			found = tool
		}
	}
	if found == nil {
		t.Fatal("勾了生成素材，清单里就该有 generate_media")
	}
	if desc, _ := found["description"].(string); !strings.Contains(desc, "task_get") || strings.Contains(desc, "自动把结果发给你") {
		t.Fatalf("说明要写外部接入的真实语义，得到 %q", desc)
	}
	schema, _ := found["inputSchema"].(map[string]any)
	props, _ := schema["properties"].(map[string]any)
	if _, has := props["wait"]; has {
		t.Fatal("wait 在外部接入这条通道上无效，不该出现在 schema 里")
	}
	if _, has := props["canvas"]; !has {
		t.Fatal("生成落在画布节点上，schema 里应该有 canvas")
	}
}

// TestMCPDramaToolsAreIndependentScope 短剧是独立能力域：drama:* 决定能看到哪些短剧工具，
// 而且不需要同时开画布权限——短剧画布本来就不在 canvas_list 里。
func TestMCPDramaToolsAreIndependentScope(t *testing.T) {
	toolsFor := func(scopes ...string) []string {
		r := newMCPRouter(&mcpStubTools{}, &mcpStubCanvases{}, 7, 0, scopeSet(scopes...))
		resp := postMCP(t, r, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
		res, ok := resp["result"].(map[string]any)
		if !ok {
			t.Fatalf("tools/list 没回 result：%v", resp)
		}
		list, _ := res["tools"].([]any)
		out := make([]string, 0, len(list))
		for _, item := range list {
			tool, _ := item.(map[string]any)
			name, _ := tool["name"].(string)
			out = append(out, name)
		}
		return out
	}
	has := func(names []string, want string) bool { return slices.Contains(names, want) }

	read := toolsFor(model.MCPScopeDramaRead)
	if !has(read, "drama_list") || !has(read, "drama_read") {
		t.Fatalf("勾读短剧就该能发现短剧并读内容，得到 %v", read)
	}
	if has(read, "drama_update_outline") || has(read, "drama_run_job") {
		t.Fatalf("只勾读短剧不该看到改剧本与跑任务，得到 %v", read)
	}
	if has(read, "canvas_get_state") {
		t.Fatalf("读短剧不该顺带给出画布权限，得到 %v", read)
	}

	write := toolsFor(model.MCPScopeDramaWrite)
	for _, want := range []string{"drama_list", "drama_read", "drama_update_outline", "drama_update_episode", "drama_update_segment"} {
		if !has(write, want) {
			t.Fatalf("改剧本应该能看到 %s，得到 %v", want, write)
		}
	}
	if has(write, "drama_run_job") {
		t.Fatalf("改剧本不该连带跑任务（花钱的一档要单独勾），得到 %v", write)
	}

	run := toolsFor(model.MCPScopeDramaRun)
	if !has(run, "drama_run_job") || !has(run, "drama_read") {
		t.Fatalf("跑任务应该带读短剧，得到 %v", run)
	}

	// 没勾短剧就一个短剧工具都不该出现——包括 drama_list。
	for _, name := range toolsFor(model.MCPScopeCanvasRead) {
		if strings.HasPrefix(name, "drama") {
			t.Fatalf("没勾短剧却看到了 %s", name)
		}
	}
}

// TestMCPDramaToolsRequireCanvas 短剧工具必须落在具体短剧画布上：没指定也没默认画布时给可执行提示。
func TestMCPDramaToolsRequireCanvas(t *testing.T) {
	r := newMCPRouter(&mcpStubTools{}, &mcpStubCanvases{}, 7, 0, scopeSet(model.MCPScopeDramaRead))
	resp := postMCP(t, r, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"drama_read","arguments":{"scope":"overview"}}}`)
	text, isErr := resultText(t, resp)
	if !isErr || !strings.Contains(text, "canvas") {
		t.Fatalf("没指定画布应回可执行提示，得到 %q isError=%v", text, isErr)
	}
}

// stubDramas 给 drama_list 用：只证明「列出来的是短剧画布 code」，不涉及短剧业务。
type stubDramas struct{}

func (stubDramas) List(_ context.Context, _ uint64, _ string, _, _ int) ([]model.DramaProject, int64, error) {
	return []model.DramaProject{{CanvasID: 7, Title: "测试短剧", Status: "ready", Phase: "done", TargetEpisodes: 3}}, 1, nil
}

// TestMCPDramaListReturnsCanvasCode 短剧不在 canvas_list 里，drama_list 必须给出能直接填进
// drama_* 的 canvas 参数——也就是短剧画布的 code，不是短剧项目 id。
func TestMCPDramaListReturnsCanvasCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := mcp.NewServer(&mcpStubTools{}, &mcpStubCanvases{})
	srv.SetDramaDirectory(stubDramas{})
	r := gin.New()
	r.POST("/mcp", func(c *gin.Context) {
		c.Set(middleware.CtxMCPProjectIDKey, uint64(1))
		c.Set(middleware.CtxMCPScopesKey, scopeSet(model.MCPScopeDramaRead))
		c.Set(middleware.CtxUserIDKey, uint(7))
		c.Next()
	}, srv.Handle)

	resp := postMCP(t, r, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"drama_list","arguments":{}}}`)
	text, isErr := resultText(t, resp)
	if isErr {
		t.Fatalf("drama_list 失败：%s", text)
	}
	var out struct {
		Dramas []struct {
			Canvas string `json:"canvas"`
			Title  string `json:"title"`
		} `json:"dramas"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("drama_list 结果不是 JSON：%s", text)
	}
	if out.Total != 1 || len(out.Dramas) != 1 {
		t.Fatalf("应该列出一条短剧，得到 %+v", out)
	}
	if out.Dramas[0].Canvas != idcodec.Encode(7) {
		t.Fatalf("canvas 必须是可以直接用的画布 code，得到 %q", out.Dramas[0].Canvas)
	}
	if out.Dramas[0].Title != "测试短剧" {
		t.Fatalf("标题丢了：%+v", out.Dramas[0])
	}
}
