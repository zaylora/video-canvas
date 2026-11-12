package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	. "video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/utils"
	"video-canvas/internal/router"
	"video-canvas/internal/service"
	"video-canvas/internal/service/stepfake"
	"video-canvas/internal/stepkit"
)

// 本文件测试环节后台接口 /admin/steps、/admin/step-trials：鉴权、参数校验、业务错误码、成功响应。
// 路由用真实的 router.New（含 JWTAuth、RequireAdmin），服务用真实实现，仓储 / 技能库 / 模型用内存 fake。

const (
	stJWTSecret = "step-handler-secret"
	stAdmin     = 2
	stNormal    = 3
	stBase      = "/api/v1/admin"
	stKey       = "test.step"
)

type stEnv struct {
	engine *gin.Engine
	repo   *stepfake.Repo
	lib    *stepfake.Library
	caller *stepfake.Caller
	svc    *service.StepAdminService
	tokens map[int]string
	now    time.Time
}

func stTestStep() stepkit.Step {
	return stepkit.Step{
		Key: stKey, Title: "测试环节", Studio: "test", Duty: "写。", Media: stepkit.MediaNone, Builtin: "内置",
		Schema:   json.RawMessage(`{"type":"object","required":["title"],"properties":{"title":{"type":"string"}}}`),
		Rules:    stepkit.Rules{Hard: []string{"必须有标题"}},
		Samples:  []stepkit.Sample{{Key: "s1", Name: "样例", Build: func(context.Context) (stepkit.Input, error) { return stepkit.Input{}, nil }}},
		Validate: func(json.RawMessage, stepkit.Input) (stepkit.Flags, error) { return nil, nil },
	}
}

func newStEnv(t *testing.T) *stEnv {
	t.Helper()
	repo := stepfake.NewRepo()
	repo.Credits[stAdmin] = &model.UserCredit{UserID: stAdmin, Balance: 100}
	lib := stepfake.NewLibrary()
	lib.Add("tvc", &stepfake.Skill{ID: 7, Title: "TVC", Enabled: true, Active: 1, Versions: map[int]string{1: "方法"}})
	repo.SkillNames[7] = "tvc"
	caller := &stepfake.Caller{}
	reg := stepkit.NewRegistry()
	reg.MustRegister(stTestStep())
	env := &stEnv{repo: repo, lib: lib, caller: caller, tokens: map[int]string{}, now: time.Now()}
	env.svc = service.NewStepAdminService(service.StepAdminDeps{Registry: reg, Repo: repo, Skills: lib, Models: stepfake.NewModels(),
		Credits: stepfake.Policy{Initial: 50}, Audit: &stepfake.Audit{}, Caller: caller, Now: func() time.Time { return env.now }})
	roles := map[uint64]string{stAdmin: model.RoleAdmin, stNormal: model.RoleUser}
	stateLookup := func(_ context.Context, id uint64) (*middleware.UserState, error) {
		role, ok := roles[id]
		if !ok {
			return nil, nil
		}
		return &middleware.UserState{Role: role, Status: model.UserStatusActive}, nil
	}
	roleLookup := func(_ context.Context, id uint64) (string, error) { return roles[id], nil }
	for id := range roles {
		tok, _, err := utils.GenerateToken(uint(id), "u"+strconv.Itoa(int(id)), 0, stJWTSecret, "test", 1)
		if err != nil {
			t.Fatal(err)
		}
		env.tokens[int(id)] = tok
	}
	env.engine = router.New(gin.TestMode, stJWTSecret, router.Handlers{AdminStep: NewAdminStepHandler(env.svc), AdminRole: roleLookup, UserState: stateLookup})
	return env
}

func (e *stEnv) do(method, path string, body any, user int) shResp {
	var buf *bytes.Reader
	switch b := body.(type) {
	case nil:
		buf = bytes.NewReader(nil)
	case string:
		buf = bytes.NewReader([]byte(b))
	default:
		raw, _ := json.Marshal(b)
		buf = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, buf)
	req.Header.Set("Content-Type", "application/json")
	if user > 0 {
		req.Header.Set("Authorization", "Bearer "+e.tokens[user])
	}
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	var parsed struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &parsed)
	return shResp{Status: w.Code, Code: parsed.Code, Msg: parsed.Msg, Data: parsed.Data, Raw: w.Body.String(), Header: w.Header()}
}

func trialBody() map[string]any {
	return map[string]any{"candidate": map[string]any{"source": "builtin"}, "sample_key": "s1", "text_model": "text-a", "compare": false}
}

func TestAdminStepHandler_Auth(t *testing.T) {
	e := newStEnv(t)
	paths := []struct{ method, path string }{
		{http.MethodGet, stBase + "/steps"},
		{http.MethodGet, stBase + "/steps/" + stKey},
		{http.MethodPost, stBase + "/steps/" + stKey + "/trials"},
		{http.MethodPut, stBase + "/steps/" + stKey + "/binding"},
		{http.MethodGet, stBase + "/step-trials/" + idcodec.Encode(1)},
	}
	for _, p := range paths {
		if r := e.do(p.method, p.path, nil, 0); r.Status != http.StatusUnauthorized {
			t.Fatalf("%s %s 未登录应 401：%d", p.method, p.path, r.Status)
		}
		if r := e.do(p.method, p.path, nil, stNormal); r.Status != http.StatusForbidden {
			t.Fatalf("%s %s 普通用户应 403：%d %s", p.method, p.path, r.Status, r.Raw)
		}
	}
}

func TestAdminStepHandler_ListAndGet(t *testing.T) {
	e := newStEnv(t)
	r := e.do(http.MethodGet, stBase+"/steps", nil, stAdmin).want(t, 200, 0)
	var list []map[string]any
	if err := json.Unmarshal(r.Data, &list); err != nil || len(list) != 1 {
		t.Fatalf("列表不对：%s", r.Raw)
	}
	row := list[0]
	for _, k := range []string{"key", "title", "studio", "media", "binding", "effective", "newer_version", "last_trial", "fail_rate_7d"} {
		if _, ok := row[k]; !ok {
			t.Fatalf("列表行缺少契约字段 %s：%s", k, r.Raw)
		}
	}
	d := e.do(http.MethodGet, stBase+"/steps/"+stKey, nil, stAdmin).want(t, 200, 0).obj(t)
	for _, k := range []string{"duty", "inputs", "schema", "rules", "samples", "history", "candidates"} {
		if _, ok := d[k]; !ok {
			t.Fatalf("详情缺少契约字段 %s", k)
		}
	}
	if _, ok := d["schema"].(string); !ok {
		t.Fatal("schema 应是 JSON 字符串")
	}
	e.do(http.MethodGet, stBase+"/steps/nope.step", nil, stAdmin).want(t, http.StatusNotFound, 68012)
}

func TestAdminStepHandler_CreateTrialsValidation(t *testing.T) {
	e := newStEnv(t)
	cases := map[string]any{
		"空体":        `{}`,
		"不是 JSON":   `not json`,
		"缺候选来源":     map[string]any{"candidate": map[string]any{}, "sample_key": "s1", "text_model": "text-a"},
		"来源不合法":     map[string]any{"candidate": map[string]any{"source": "magic"}, "sample_key": "s1", "text_model": "text-a"},
		"技能来源缺技能名":  map[string]any{"candidate": map[string]any{"source": "skill"}, "sample_key": "s1", "text_model": "text-a"},
		"版本为负":      map[string]any{"candidate": map[string]any{"source": "skill", "skill_name": "tvc", "version": -1}, "sample_key": "s1", "text_model": "text-a"},
		"缺样例":       map[string]any{"candidate": map[string]any{"source": "builtin"}, "text_model": "text-a"},
		"缺模型":       map[string]any{"candidate": map[string]any{"source": "builtin"}, "sample_key": "s1"},
		"环节 key 超长": nil,
	}
	for name, body := range cases {
		path := stBase + "/steps/" + stKey + "/trials"
		if body == nil {
			path = stBase + "/steps/" + string(bytes.Repeat([]byte("k"), 65)) + "/trials"
			body = trialBody()
		}
		e.do(http.MethodPost, path, body, stAdmin).want(t, http.StatusBadRequest, 10001)
		_ = name
	}
	if len(e.repo.Trials) != 0 {
		t.Fatal("参数校验失败不应写试跑")
	}
}

func TestAdminStepHandler_CreateTrialsFlow(t *testing.T) {
	e := newStEnv(t)
	path := stBase + "/steps/" + stKey + "/trials"
	// 成功：202，一条 queued
	r := e.do(http.MethodPost, path, trialBody(), stAdmin).want(t, http.StatusAccepted, 0)
	trials := r.obj(t)["trials"].([]any)
	if len(trials) != 1 {
		t.Fatalf("应返回 1 条：%s", r.Raw)
	}
	first := trials[0].(map[string]any)
	if first["status"] != "queued" || first["step_key"] != stKey || first["sample_key"] != "s1" || first["text_model"] != "text-a" {
		t.Fatalf("试跑视图不对：%s", r.Raw)
	}
	// 进行中再发：68017
	e.do(http.MethodPost, path, trialBody(), stAdmin).want(t, http.StatusConflict, 68017)
	// 查询：可以拿到刚才的试跑
	e.do(http.MethodGet, stBase+"/step-trials/"+first["id"].(string), nil, stAdmin).want(t, 200, 0)
}

func TestAdminStepHandler_CreateTrialsBusinessErrors(t *testing.T) {
	e := newStEnv(t)
	path := stBase + "/steps/" + stKey + "/trials"
	missing := trialBody()
	missing["candidate"] = map[string]any{"source": "skill", "skill_name": "ghost"}
	e.do(http.MethodPost, path, missing, stAdmin).want(t, http.StatusBadRequest, 68014)
	e.do(http.MethodPost, stBase+"/steps/nope.step/trials", trialBody(), stAdmin).want(t, http.StatusNotFound, 68012)
	poor := trialBody()
	e.repo.Credits[stAdmin].Balance = 1
	e.do(http.MethodPost, path, poor, stAdmin).want(t, http.StatusPaymentRequired, 40001)
}

func TestAdminStepHandler_GetTrial(t *testing.T) {
	e := newStEnv(t)
	e.do(http.MethodGet, stBase+"/step-trials/short", nil, stAdmin).want(t, http.StatusBadRequest, 10001)
	e.do(http.MethodGet, stBase+"/step-trials/"+idcodec.Encode(404), nil, stAdmin).want(t, http.StatusNotFound, 10002)
}

func TestAdminStepHandler_SetBinding(t *testing.T) {
	e := newStEnv(t)
	path := stBase + "/steps/" + stKey + "/binding"
	// 参数校验
	e.do(http.MethodPut, path, `{}`, stAdmin).want(t, http.StatusBadRequest, 10001)
	e.do(http.MethodPut, path, map[string]any{"source": "skill", "trial_id": idcodec.Encode(1)}, stAdmin).want(t, http.StatusBadRequest, 10001) // 缺技能名
	e.do(http.MethodPut, path, map[string]any{"source": "builtin"}, stAdmin).want(t, http.StatusBadRequest, 10001)                              // 缺 trial_id
	// 业务错误
	e.do(http.MethodPut, path, map[string]any{"source": "builtin", "trial_id": idcodec.Encode(404)}, stAdmin).want(t, http.StatusConflict, 68013)
	e.do(http.MethodPut, path, map[string]any{"source": "skill", "skill_name": "ghost", "version": 1, "trial_id": idcodec.Encode(1)}, stAdmin).want(t, http.StatusBadRequest, 68014)
	e.do(http.MethodPut, stBase+"/steps/nope.step/binding", map[string]any{"source": "builtin", "trial_id": idcodec.Encode(1)}, stAdmin).want(t, http.StatusNotFound, 68012)

	// 成功：先跑一条通过的技能试跑，再设为生效，返回最新详情
	e.caller.Replies = []stepfake.Reply{{Text: `{"title":"x"}`}}
	body := trialBody()
	body["candidate"] = map[string]any{"source": "skill", "skill_name": "tvc", "version": 1}
	r := e.do(http.MethodPost, stBase+"/steps/"+stKey+"/trials", body, stAdmin).want(t, http.StatusAccepted, 0)
	id := r.obj(t)["trials"].([]any)[0].(map[string]any)["id"].(string)
	for _, tid := range e.repo.Enqueued {
		if err := e.svc.HandleStepTrial(context.Background(), tid); err != nil {
			t.Fatal(err)
		}
	}
	d := e.do(http.MethodPut, path, map[string]any{"source": "skill", "skill_name": "tvc", "version": 1, "trial_id": id}, stAdmin).want(t, 200, 0).obj(t)
	binding := d["binding"].(map[string]any)
	if binding["source"] != "skill" || binding["skill_name"] != "tvc" || binding["version"] != float64(1) {
		t.Fatalf("返回的详情应带新绑定：%+v", d)
	}
	if hist := d["history"].([]any); len(hist) != 1 {
		t.Fatalf("应有一条历史：%+v", d["history"])
	}
}
