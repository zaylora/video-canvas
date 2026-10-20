package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	. "video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/utils"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/repository"
	"video-canvas/internal/router"
	"video-canvas/internal/service"
)

const (
	qaSuper  = 1
	qaAdmin  = 2
	qaNormal = 3
	qaOff    = 4 // 已停用的 super_admin
	qaSecret = "queue-test-secret"
	riverPfx = "/api/v1/admin/river"
)

// ---------- 假依赖（后台队列服务） ----------

type qaRepo struct{}

func (qaRepo) QueueCounts(context.Context, time.Time) (map[string]model.QueueCounts, error) {
	return map[string]model.QueueCounts{"submit": {Available: 3, Running: 2}}, nil
}
func (qaRepo) Throughput(context.Context, time.Time) ([]repository.ThroughputPoint, error) {
	return nil, nil
}
func (qaRepo) ChannelOccupancies(context.Context) ([]repository.ChannelOccupancy, error) {
	return nil, nil
}
func (qaRepo) ListInstances(context.Context) ([]model.WorkerInstance, error) { return nil, nil }

type qaChannels struct{}

func (qaChannels) ListChannels(context.Context) ([]model.AIChannel, error) {
	return []model.AIChannel{{Key: "k", Name: "可灵", RateLimitJSON: model.JSONText(`{"max_running":4}`)}}, nil
}

type qaSettings struct{ saved map[string]int }

func (s *qaSettings) SetQueueConcurrency(_ context.Context, _ uint64, stage string, n int) error {
	s.saved[stage] = n
	return nil
}

type qaCtrl struct {
	mu     sync.Mutex
	paused map[string]bool
}

func (c *qaCtrl) States(context.Context) ([]model.QueueStageState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []model.QueueStageState
	for _, st := range model.QueueStages {
		out = append(out, model.QueueStageState{Stage: st, Paused: c.paused[st], Concurrency: 8, HardCap: 64})
	}
	return out, nil
}
func (c *qaCtrl) Pause(_ context.Context, s string) error {
	c.mu.Lock()
	c.paused[s] = true
	c.mu.Unlock()
	return nil
}
func (c *qaCtrl) Resume(_ context.Context, s string) error {
	c.mu.Lock()
	c.paused[s] = false
	c.mu.Unlock()
	return nil
}
func (*qaCtrl) RefreshSettings(context.Context) {}
func (*qaCtrl) Heartbeat(context.Context)       {}
func (*qaCtrl) HardCap(string) int              { return 64 }

// qaEnv 用真实的路由、鉴权和 River UI 鉴权，后面接假的队列依赖与一个只回显路径的“River UI”。
type qaEnv struct {
	engine  *gin.Engine
	river   *RiverUIHandler
	roles   map[uint64]string
	status  map[uint64]string
	tokens  map[uint64]string
	ctrl    *qaCtrl
	setting *qaSettings
	now     time.Time
	nowMu   sync.Mutex
}

func newQAEnv(t *testing.T) *qaEnv {
	t.Helper()
	e := &qaEnv{
		roles:   map[uint64]string{qaSuper: model.RoleSuperAdmin, qaAdmin: model.RoleAdmin, qaNormal: model.RoleUser, qaOff: model.RoleSuperAdmin},
		status:  map[uint64]string{qaSuper: model.UserStatusActive, qaAdmin: model.UserStatusActive, qaNormal: model.UserStatusActive, qaOff: model.UserStatusDisabled},
		tokens:  map[uint64]string{},
		ctrl:    &qaCtrl{paused: map[string]bool{}},
		setting: &qaSettings{saved: map[string]int{}},
		now:     time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC),
	}
	for id := range e.roles {
		tok, _, err := utils.GenerateToken(uint(id), "u", 0, qaSecret, "test", 1)
		if err != nil {
			t.Fatal(err)
		}
		e.tokens[id] = tok
	}
	state := func(_ context.Context, id uint64) (*middleware.UserState, error) {
		role, ok := e.roles[id]
		if !ok {
			return nil, nil
		}
		return &middleware.UserState{Role: role, Status: e.status[id]}, nil
	}
	role := func(ctx context.Context, id uint64) (string, error) {
		st, err := state(ctx, id)
		if err != nil || st == nil {
			return "", err
		}
		return st.Role, nil
	}
	// 与生产装配一致：停用的账号不算 super_admin
	riverRole := func(ctx context.Context, id uint64) (string, error) {
		st, err := state(ctx, id)
		if err != nil || st == nil || st.Status != model.UserStatusActive {
			return "", err
		}
		return st.Role, nil
	}
	ui := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("UI:" + r.URL.Path)) })
	e.river = NewRiverUIHandler(ui, ws.NewMemoryTicketStore(), riverRole, qaSecret, riverPfx)
	e.river.SetClock(func() time.Time {
		e.nowMu.Lock()
		defer e.nowMu.Unlock()
		return e.now
	})
	svc := service.NewQueueAdminService(qaRepo{}, qaChannels{}, e.setting, e.ctrl, nil)
	e.engine = router.New(gin.TestMode, qaSecret, router.Handlers{
		AdminQueue: NewAdminQueueHandler(svc), RiverUI: e.river, AdminRole: role, UserState: state,
	})
	return e
}

func (e *qaEnv) advance(d time.Duration) {
	e.nowMu.Lock()
	e.now = e.now.Add(d)
	e.nowMu.Unlock()
}

type qaResp struct {
	Code int
	Raw  string
	HTTP *httptest.ResponseRecorder
}

func (e *qaEnv) do(method, path string, body any, user uint64) qaResp {
	var buf bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = *bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if user != 0 {
		req.Header.Set("Authorization", "Bearer "+e.tokens[user])
	}
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	var env struct {
		Code int `json:"code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	return qaResp{Code: env.Code, Raw: w.Body.String(), HTTP: w}
}

func (r qaResp) want(t *testing.T, status, code int) {
	t.Helper()
	if r.HTTP.Code != status || r.Code != code {
		t.Fatalf("期望 HTTP %d / code %d，实际 %d / %d：%s", status, code, r.HTTP.Code, r.Code, r.Raw)
	}
}

// ---------- 队列接口 ----------

func TestAdminQueue_Overview(t *testing.T) {
	e := newQAEnv(t)
	r := e.do(http.MethodGet, "/api/v1/admin/queue/overview", nil, qaAdmin)
	r.want(t, 200, 0)
	for _, want := range []string{`"label":"提交上游"`, `"label":"查询进度"`, `"label":"转存产物"`, `"available":3`, `"max_running":4`, `"hard_cap":64`} {
		if !strings.Contains(r.Raw, want) {
			t.Fatalf("响应缺少 %s：%s", want, r.Raw)
		}
	}
	if strings.Contains(r.Raw, "gen_submit") {
		t.Fatalf("不应暴露 River 的队列名：%s", r.Raw)
	}
	e.do(http.MethodGet, "/api/v1/admin/queue/overview", nil, qaSuper).want(t, 200, 0)
	e.do(http.MethodGet, "/api/v1/admin/queue/overview", nil, qaNormal).want(t, 403, errcode.ErrForbidden.Code)
	e.do(http.MethodGet, "/api/v1/admin/queue/overview", nil, 0).want(t, 401, errcode.ErrUnauthorized.Code)
}

func TestAdminQueue_Throughput(t *testing.T) {
	e := newQAEnv(t)
	r := e.do(http.MethodGet, "/api/v1/admin/queue/throughput?minutes=30", nil, qaAdmin)
	r.want(t, 200, 0)
	var body struct {
		Data []model.QueueThroughputPoint `json:"data"`
	}
	_ = json.Unmarshal([]byte(r.Raw), &body)
	if len(body.Data) != 30 {
		t.Fatalf("应返回 30 个点：%d", len(body.Data))
	}
	e.do(http.MethodGet, "/api/v1/admin/queue/throughput", nil, qaAdmin).want(t, 200, 0) // 缺省 60
	for _, bad := range []string{"minutes=14", "minutes=1441", "minutes=abc"} {
		e.do(http.MethodGet, "/api/v1/admin/queue/throughput?"+bad, nil, qaAdmin).want(t, 400, errcode.ErrInvalidParams.Code)
	}
}

func TestAdminQueue_SetConcurrency(t *testing.T) {
	e := newQAEnv(t)
	path := "/api/v1/admin/queue/queues/submit"
	e.do(http.MethodPut, path, map[string]any{"concurrency": 20}, qaAdmin).want(t, 403, errcode.ErrForbidden.Code) // 写仅 super_admin
	r := e.do(http.MethodPut, path, map[string]any{"concurrency": 20}, qaSuper)
	r.want(t, 200, 0)
	if !strings.Contains(r.Raw, `"name":"submit"`) || !strings.Contains(r.Raw, `"concurrency":20`) || e.setting.saved["submit"] != 20 {
		t.Fatalf("保存结果不对：%s %v", r.Raw, e.setting.saved)
	}
	for _, bad := range []any{map[string]any{}, map[string]any{"concurrency": 0}, map[string]any{"concurrency": -3}, map[string]any{"concurrency": 65}, map[string]any{"concurrency": "x"}} {
		e.do(http.MethodPut, path, bad, qaSuper).want(t, 400, errcode.ErrInvalidParams.Code)
	}
	e.do(http.MethodPut, "/api/v1/admin/queue/queues/nope", map[string]any{"concurrency": 5}, qaSuper).want(t, 404, errcode.ErrNotFound.Code)
	if e.setting.saved["submit"] != 20 || len(e.setting.saved) != 1 {
		t.Fatalf("失败的请求不应写设置：%v", e.setting.saved)
	}
}

func TestAdminQueue_PauseResume(t *testing.T) {
	e := newQAEnv(t)
	e.do(http.MethodPost, "/api/v1/admin/queue/queues/poll/pause", nil, qaAdmin).want(t, 403, errcode.ErrForbidden.Code)
	r := e.do(http.MethodPost, "/api/v1/admin/queue/queues/poll/pause", nil, qaSuper)
	r.want(t, 200, 0)
	if !strings.Contains(r.Raw, `"paused":true`) || !e.ctrl.paused["poll"] {
		t.Fatalf("应已暂停：%s", r.Raw)
	}
	// 幂等：重复暂停仍然成功
	e.do(http.MethodPost, "/api/v1/admin/queue/queues/poll/pause", nil, qaSuper).want(t, 200, 0)
	if ov := e.do(http.MethodGet, "/api/v1/admin/queue/overview", nil, qaAdmin); !strings.Contains(ov.Raw, `"paused":true`) {
		t.Fatalf("总览应反映暂停：%s", ov.Raw)
	}
	r = e.do(http.MethodPost, "/api/v1/admin/queue/queues/poll/resume", nil, qaSuper)
	r.want(t, 200, 0)
	if !strings.Contains(r.Raw, `"paused":false`) || e.ctrl.paused["poll"] {
		t.Fatalf("应已恢复：%s", r.Raw)
	}
	e.do(http.MethodPost, "/api/v1/admin/queue/queues/nope/pause", nil, qaSuper).want(t, 404, errcode.ErrNotFound.Code)
}

// ---------- River UI：一次性 ticket 换会话 cookie ----------

func (e *qaEnv) ticketURL(t *testing.T, user uint64) string {
	t.Helper()
	r := e.do(http.MethodPost, "/api/v1/admin/queue/river-ticket", nil, user)
	r.want(t, 200, 0)
	var body struct {
		Data model.RiverTicketResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(r.Raw), &body); err != nil || body.Data.URL == "" {
		t.Fatalf("响应不对：%s", r.Raw)
	}
	return body.Data.URL
}

// browse 模拟浏览器：不带 Authorization，只带 cookie。
func (e *qaEnv) browse(path, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != "" {
		req.Header.Set("Cookie", "vc_river="+cookie)
	}
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return w
}

func sessionCookie(w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == "vc_river" {
			return c
		}
	}
	return nil
}

func TestRiverUI_TicketToSession(t *testing.T) {
	t.Run("只有 super_admin 能拿 ticket", func(t *testing.T) {
		e := newQAEnv(t)
		e.do(http.MethodPost, "/api/v1/admin/queue/river-ticket", nil, qaAdmin).want(t, 403, errcode.ErrForbidden.Code)
		e.do(http.MethodPost, "/api/v1/admin/queue/river-ticket", nil, qaNormal).want(t, 403, errcode.ErrForbidden.Code)
		e.do(http.MethodPost, "/api/v1/admin/queue/river-ticket", nil, 0).want(t, 401, errcode.ErrUnauthorized.Code)
		if u := e.ticketURL(t, qaSuper); !strings.HasPrefix(u, riverPfx+"/?ticket=") {
			t.Fatalf("地址应是同源相对路径并带 ticket：%s", u)
		}
	})

	t.Run("ticket 换 cookie：重定向到干净地址，cookie 是 HttpOnly、SameSite=Strict、路径限定", func(t *testing.T) {
		e := newQAEnv(t)
		w := e.browse(e.ticketURL(t, qaSuper), "")
		if w.Code != http.StatusFound || w.Header().Get("Location") != riverPfx+"/" {
			t.Fatalf("应 302 到 %s：%d %s", riverPfx+"/", w.Code, w.Header().Get("Location"))
		}
		c := sessionCookie(w)
		if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != riverPfx || c.MaxAge != 30*60 {
			t.Fatalf("cookie 不对：%+v", c)
		}
		ok := e.browse(riverPfx+"/", c.Value)
		if ok.Code != 200 || ok.Body.String() != "UI:"+riverPfx+"/" {
			t.Fatalf("带 cookie 应进入 River UI：%d %s", ok.Code, ok.Body.String())
		}
		// River UI 内部的 API 与静态资源路径同样放行
		if api := e.browse(riverPfx+"/api/queues", c.Value); api.Code != 200 || api.Body.String() != "UI:"+riverPfx+"/api/queues" {
			t.Fatalf("子路径应放行：%d %s", api.Code, api.Body.String())
		}
	})

	t.Run("ticket 只能用一次", func(t *testing.T) {
		e := newQAEnv(t)
		url := e.ticketURL(t, qaSuper)
		if w := e.browse(url, ""); w.Code != http.StatusFound {
			t.Fatalf("第一次应成功：%d", w.Code)
		}
		w := e.browse(url, "")
		if w.Code != 401 || sessionCookie(w) != nil {
			t.Fatalf("第二次应 401 且不下发 cookie：%d", w.Code)
		}
	})

	t.Run("没有 cookie、cookie 被篡改、格式不对都是 401", func(t *testing.T) {
		e := newQAEnv(t)
		good := sessionCookie(e.browse(e.ticketURL(t, qaSuper), "")).Value
		parts := strings.Split(good, ".")
		forged := strings.Join([]string{"2", parts[1], parts[2]}, ".") // 冒充另一个用户，签名对不上
		tamperedExp := strings.Join([]string{parts[0], "9999999999", parts[2]}, ".")
		for name, cookie := range map[string]string{"没有": "", "冒充用户": forged, "篡改有效期": tamperedExp, "格式不对": "a.b", "乱码": "xxx"} {
			if w := e.browse(riverPfx+"/", cookie); w.Code != 401 {
				t.Fatalf("%s 应 401，实际 %d", name, w.Code)
			}
		}
	})

	t.Run("会话 30 分钟后过期", func(t *testing.T) {
		e := newQAEnv(t)
		c := sessionCookie(e.browse(e.ticketURL(t, qaSuper), "")).Value
		e.advance(29 * time.Minute)
		if w := e.browse(riverPfx+"/", c); w.Code != 200 {
			t.Fatalf("29 分钟内应有效：%d", w.Code)
		}
		e.advance(2 * time.Minute)
		if w := e.browse(riverPfx+"/", c); w.Code != 401 {
			t.Fatalf("超过 30 分钟应过期：%d", w.Code)
		}
	})

	t.Run("滑动续期：剩余不足一半时顺带换新 cookie，刚签发时不换", func(t *testing.T) {
		e := newQAEnv(t)
		c := sessionCookie(e.browse(e.ticketURL(t, qaSuper), "")).Value
		if w := e.browse(riverPfx+"/", c); sessionCookie(w) != nil {
			t.Fatal("刚签发的会话不应续期")
		}
		e.advance(20 * time.Minute) // 剩 10 分钟
		w := e.browse(riverPfx+"/", c)
		renewed := sessionCookie(w)
		if w.Code != 200 || renewed == nil || renewed.Value == c {
			t.Fatalf("应续期并换新 cookie：%d %+v", w.Code, renewed)
		}
		e.advance(20 * time.Minute) // 旧 cookie 已过期，新 cookie（续期后 30 分钟）仍有效
		if w := e.browse(riverPfx+"/", c); w.Code != 401 {
			t.Fatalf("旧 cookie 应已过期：%d", w.Code)
		}
		if w := e.browse(riverPfx+"/", renewed.Value); w.Code != 200 {
			t.Fatalf("新 cookie 应有效：%d", w.Code)
		}
	})

	t.Run("每次请求都重新查角色：被降级或停用后立即失效，换 cookie 时也不放行", func(t *testing.T) {
		e := newQAEnv(t)
		c := sessionCookie(e.browse(e.ticketURL(t, qaSuper), "")).Value
		e.roles[qaSuper] = model.RoleAdmin
		if w := e.browse(riverPfx+"/", c); w.Code != 403 {
			t.Fatalf("降级后应 403：%d", w.Code)
		}
		e.roles[qaSuper] = model.RoleSuperAdmin
		if w := e.browse(riverPfx+"/", c); w.Code != 200 {
			t.Fatalf("恢复后应放行：%d", w.Code)
		}
		e.status[qaSuper] = model.UserStatusDisabled
		if w := e.browse(riverPfx+"/", c); w.Code != 403 {
			t.Fatalf("停用后应 403：%d", w.Code)
		}

		// ticket 签发后、兑换前被降级：兑换时也要拒绝，并且不下发 cookie
		e2 := newQAEnv(t)
		url := e2.ticketURL(t, qaSuper)
		e2.roles[qaSuper] = model.RoleUser
		w := e2.browse(url, "")
		if w.Code != 403 || sessionCookie(w) != nil {
			t.Fatalf("兑换时已降级应 403 且无 cookie：%d", w.Code)
		}
	})

	t.Run("没有结尾斜杠的入口补上斜杠", func(t *testing.T) {
		e := newQAEnv(t)
		w := e.browse(riverPfx, "")
		if w.Code != http.StatusFound || w.Header().Get("Location") != riverPfx+"/" {
			t.Fatalf("%d %s", w.Code, w.Header().Get("Location"))
		}
	})
}

// 查角色失败是服务端问题，返回 500 而不是误报没有权限。
func TestRiverUI_RoleLookupFailure(t *testing.T) {
	boom := errors.New("db down")
	ui := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("UI")) })
	store := ws.NewMemoryTicketStore()
	h := NewRiverUIHandler(ui, store, func(context.Context, uint64) (string, error) { return "", boom }, qaSecret, riverPfx)
	r := gin.New()
	r.Any(riverPfx+"/*path", h.Serve)
	ticket, _ := store.Issue(context.Background(), 1)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, riverPfx+"/?ticket="+ticket, nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("查角色失败应 500：%d %s", w.Code, w.Body.String())
	}
}
