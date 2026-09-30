//go:build legacy

package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	. "video-canvas/internal/handler"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/config"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/dsl"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// ---------------------------------------------------------------------------
// fake 依赖（内存版仓储 + 最小化的注册表 / 执行器 / 素材存储）
// ---------------------------------------------------------------------------

// fakeGenTaskRepo 是内存版 service.GenerationTaskRepo，WithTx 直接在自身上执行（handler 测试不关心回滚）。
type fakeGenTaskRepo struct {
	tasks     map[uint64]*model.GenerationTask
	credits   map[uint64]*model.UserCredit
	ledger    []model.CreditLedger
	nextID    uint64
	touched   []string
	touchRows int64
}

func newFakeGenTaskRepo() *fakeGenTaskRepo {
	return &fakeGenTaskRepo{tasks: map[uint64]*model.GenerationTask{}, credits: map[uint64]*model.UserCredit{}, nextID: 100}
}

func (f *fakeGenTaskRepo) add(t model.GenerationTask) *model.GenerationTask {
	f.nextID++
	t.ID = f.nextID
	t.Version = 1
	t.ConfigSnapshot = []byte(`{}`)
	cp := t
	f.tasks[t.ID] = &cp
	return &cp
}

func (f *fakeGenTaskRepo) WithTx(ctx context.Context, fn func(tx repository.GenerationTaskTx) error) error {
	return fn(f)
}
func (f *fakeGenTaskRepo) GetByID(ctx context.Context, userID, id uint64) (*model.GenerationTask, error) {
	t, ok := f.tasks[id]
	if !ok || t.UserID != userID {
		return nil, repository.ErrNotFound
	}
	cp := *t
	return &cp, nil
}
func (f *fakeGenTaskRepo) ListByIDs(ctx context.Context, userID uint64, ids []uint64) ([]model.GenerationTask, error) {
	var out []model.GenerationTask
	for _, id := range ids {
		if t, ok := f.tasks[id]; ok && t.UserID == userID && !t.IsTest {
			out = append(out, *t)
		}
	}
	return out, nil
}
func (f *fakeGenTaskRepo) ListActive(ctx context.Context, userID uint64) ([]model.GenerationTask, error) {
	var out []model.GenerationTask
	for _, t := range f.tasks {
		if t.UserID == userID && !t.IsTest && !model.IsTerminalStatus(t.Status) {
			out = append(out, *t)
		}
	}
	return out, nil
}
func (f *fakeGenTaskRepo) GetCredit(ctx context.Context, userID uint64) (*model.UserCredit, error) {
	c, ok := f.credits[userID]
	if !ok {
		return nil, repository.ErrNotFound
	}
	cp := *c
	return &cp, nil
}
func (f *fakeGenTaskRepo) ClaimDue(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]model.GenerationTask, error) {
	return nil, nil
}
func (f *fakeGenTaskRepo) ExtendLease(ctx context.Context, id uint64, until time.Time) (bool, error) {
	return true, nil
}
func (f *fakeGenTaskRepo) TouchByProviderTask(ctx context.Context, provider, pid string, now time.Time) (int64, error) {
	f.touched = append(f.touched, provider+"/"+pid)
	return f.touchRows, nil
}
func (f *fakeGenTaskRepo) FindByIdempotencyKey(ctx context.Context, userID uint64, key string) (*model.GenerationTask, error) {
	for _, t := range f.tasks {
		if t.UserID == userID && t.IdempotencyKey == key {
			cp := *t
			return &cp, nil
		}
	}
	return nil, repository.ErrNotFound
}
func (f *fakeGenTaskRepo) CountActive(ctx context.Context, userID uint64) (int64, error) {
	var n int64
	for _, t := range f.tasks {
		if t.UserID == userID && !t.IsTest && !model.IsTerminalStatus(t.Status) {
			n++
		}
	}
	return n, nil
}
func (f *fakeGenTaskRepo) EnsureCredit(ctx context.Context, userID uint64, initial int) error {
	if _, ok := f.credits[userID]; !ok {
		f.credits[userID] = &model.UserCredit{UserID: userID, Balance: initial}
	}
	return nil
}
func (f *fakeGenTaskRepo) LockCredit(ctx context.Context, userID uint64) (*model.UserCredit, error) {
	return f.GetCredit(ctx, userID)
}
func (f *fakeGenTaskRepo) AddCredit(ctx context.Context, userID uint64, dBalance, dFrozen int) error {
	c, ok := f.credits[userID]
	if !ok {
		return repository.ErrNotFound
	}
	c.Balance += dBalance
	c.Frozen += dFrozen
	return nil
}
func (f *fakeGenTaskRepo) InsertLedger(ctx context.Context, e *model.CreditLedger) (bool, error) {
	for _, l := range f.ledger {
		if l.TaskID == e.TaskID && l.Type == e.Type {
			return false, nil
		}
	}
	f.ledger = append(f.ledger, *e)
	return true, nil
}
func (f *fakeGenTaskRepo) InsertTask(ctx context.Context, t *model.GenerationTask) (bool, error) {
	f.nextID++
	t.ID = f.nextID
	cp := *t
	f.tasks[t.ID] = &cp
	return true, nil
}
func (f *fakeGenTaskRepo) UpdateIf(ctx context.Context, id uint64, from []string, fields map[string]any, bump bool) (*model.GenerationTask, error) {
	t, ok := f.tasks[id]
	if !ok {
		return nil, repository.ErrStateConflict
	}
	matched := false
	for _, s := range from {
		matched = matched || s == t.Status
	}
	if !matched {
		return nil, repository.ErrStateConflict
	}
	if v, ok := fields["status"].(string); ok {
		t.Status = v
	}
	if v, ok := fields["error_code"].(string); ok {
		t.ErrorCode = v
	}
	if v, ok := fields["error_message"].(string); ok {
		t.ErrorMessage = v
	}
	if bump {
		t.Version++
	}
	cp := *t
	return &cp, nil
}

type fakeGenTaskRegistry struct {
	snap     *dsl.Snapshot
	err      error
	provider *dsl.ProviderConfig
}

func (r *fakeGenTaskRegistry) ListModels(context.Context, string) ([]provider.ModelInfo, error) {
	return nil, nil
}
func (r *fakeGenTaskRegistry) Snapshot(context.Context, string) (*dsl.Snapshot, error) {
	return r.snap, r.err
}
func (r *fakeGenTaskRegistry) Provider(context.Context, string) (*dsl.ProviderConfig, error) {
	if r.provider == nil {
		return nil, provider.ErrModelUnavailable
	}
	return r.provider, nil
}

type fakeGenTaskExecutor struct{}

func (fakeGenTaskExecutor) Submit(context.Context, *dsl.Snapshot, provider.SubmitInput) (string, error) {
	return "", errors.New("unused")
}
func (fakeGenTaskExecutor) Query(context.Context, *dsl.Snapshot, provider.TaskRef) (*provider.QueryResult, error) {
	return nil, errors.New("unused")
}
func (fakeGenTaskExecutor) Cancel(context.Context, *dsl.Snapshot, provider.TaskRef) error {
	return provider.ErrCancelUnsupported
}
func (fakeGenTaskExecutor) Download(context.Context, *dsl.Snapshot, string) (*provider.Download, error) {
	return nil, errors.New("unused")
}

type fakeGenTaskAssets struct{}

func (fakeGenTaskAssets) Get(context.Context, uint64, uint64) (*model.Asset, error) {
	return nil, provider.ErrAssetNotFound
}
func (fakeGenTaskAssets) Open(context.Context, uint64, uint64) (*provider.AssetFile, error) {
	return nil, provider.ErrAssetNotFound
}

// ---------------------------------------------------------------------------
// 测试装配
// ---------------------------------------------------------------------------

type genTaskEnv struct {
	router   *gin.Engine
	repo     *fakeGenTaskRepo
	registry *fakeGenTaskRegistry
	svc      *service.GenerationTaskService
	userID   uint
}

func newGenTaskEnv(t *testing.T) *genTaskEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	env := &genTaskEnv{
		repo: newFakeGenTaskRepo(),
		registry: &fakeGenTaskRegistry{
			snap: &dsl.Snapshot{
				Provider: dsl.ProviderConfig{Key: "runninghub", Webhook: &dsl.WebhookConfig{TaskID: "req.taskId"}},
				Model: dsl.ModelConfig{
					Key: "m1", Kind: model.KindVideo, Provider: "runninghub", Credits: 10,
					InputSchema: dsl.InputSchema{{Name: "prompt", InputField: dsl.InputField{Type: dsl.FieldText, Required: true}}},
				},
			},
			provider: &dsl.ProviderConfig{Key: "runninghub", Webhook: &dsl.WebhookConfig{TaskID: "req.taskId"}},
		},
		userID: 1,
	}
	env.svc = service.NewGenerationTaskService(service.GenerationTaskDeps{
		Repo: env.repo, Registry: env.registry, Executor: fakeGenTaskExecutor{}, Assets: fakeGenTaskAssets{},
		Broadcaster: ws.NopBroadcaster{},
		Config:      config.AI{MaxActiveTasksPerUser: 2, InitialCredits: 50, WebhookSecret: "s3cret"},
	},
		service.WithInputValidator(func(_ dsl.InputSchema, in map[string]any) (map[string]any, []dsl.FieldError) {
			if s, _ := in["prompt"].(string); s == "" {
				return nil, []dsl.FieldError{{Field: "prompt", Message: "必填"}}
			}
			return in, nil
		}),
		service.WithMediaFieldNames(func(dsl.InputSchema) []string { return nil }),
		service.WithExprEvaluator(func(_ string, rc *dsl.RenderContext) (any, error) {
			m, ok := rc.Req.(map[string]any)
			if !ok {
				return nil, errors.New("回调体不是对象")
			}
			return m["taskId"], nil
		}),
	)
	h := NewGenerationTaskHandler(env.svc)

	r := gin.New()
	// 测试中间件模拟登录，不依赖真实 JWT
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, env.userID); c.Next() })
	v1 := r.Group("/api/v1")
	v1.POST("/generation-tasks", h.Create)
	v1.GET("/generation-tasks", h.List)
	v1.GET("/generation-tasks/:id", h.Get)
	v1.POST("/generation-tasks/:id/cancel", h.Cancel)
	v1.GET("/credits", h.Credits)
	v1.POST("/webhooks/:provider/:secret", h.Webhook)
	env.router = r
	return env
}

// call 发请求并解出统一响应。
func (e *genTaskEnv) call(t *testing.T, method, path, body string, headers map[string]string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是 JSON：%v %s", err, w.Body.String())
	}
	return w.Code, resp
}

func genTaskRespCode(resp map[string]any) int {
	c, _ := resp["code"].(float64)
	return int(c)
}

const genTaskValidBody = `{"kind":"video","model_id":"m1","canvas_id":3,"node_id":"n1","input":{"prompt":"一只猫"}}`

// ---------------------------------------------------------------------------
// 测试
// ---------------------------------------------------------------------------

func TestGenerationTaskHandler_Create(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		headers    map[string]string
		setup      func(e *genTaskEnv)
		wantStatus int
		wantCode   int
	}{
		{"成功返回 202 和任务快照", genTaskValidBody, nil, nil, http.StatusAccepted, 0},
		{"带 Idempotency-Key 成功", genTaskValidBody, map[string]string{"Idempotency-Key": "k1"}, nil, http.StatusAccepted, 0},
		{"缺少必填参数返回 400 + 10001", `{"kind":"video"}`, nil, nil, http.StatusBadRequest, errcode.ErrInvalidParams.Code},
		{"kind 取值非法返回 400 + 10001", `{"kind":"text","model_id":"m1","input":{}}`, nil, nil, http.StatusBadRequest, errcode.ErrInvalidParams.Code},
		{"请求体不是 JSON 返回 400 + 10001", `not json`, nil, nil, http.StatusBadRequest, errcode.ErrInvalidParams.Code},
		{"生成参数不合法返回 400 + 40006", `{"kind":"video","model_id":"m1","input":{}}`, nil, nil, http.StatusBadRequest, errcode.ErrTaskInput.Code},
		{"模型不可用返回 400 + 40003", genTaskValidBody, nil, func(e *genTaskEnv) { e.registry.err = provider.ErrModelUnavailable }, http.StatusBadRequest, errcode.ErrModelUnavailable.Code},
		{"积分不足返回 402 + 40001", genTaskValidBody, nil, func(e *genTaskEnv) { e.repo.credits[1] = &model.UserCredit{UserID: 1, Balance: 5} }, http.StatusPaymentRequired, errcode.ErrInsufficientCredits.Code},
		{"进行中任务达上限返回 429 + 40002", genTaskValidBody, nil, func(e *genTaskEnv) {
			e.repo.add(model.GenerationTask{UserID: 1, Status: model.TaskRunning})
			e.repo.add(model.GenerationTask{UserID: 1, Status: model.TaskPending})
		}, http.StatusTooManyRequests, errcode.ErrTooManyTasks.Code},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newGenTaskEnv(t)
			if tt.setup != nil {
				tt.setup(env)
			}
			status, resp := env.call(t, http.MethodPost, "/api/v1/generation-tasks", tt.body, tt.headers)
			if status != tt.wantStatus || genTaskRespCode(resp) != tt.wantCode {
				t.Fatalf("期望 HTTP %d / code %d，实际 %d / %v", tt.wantStatus, tt.wantCode, status, resp)
			}
			if tt.wantCode != 0 {
				return
			}
			data := resp["data"].(map[string]any)
			if data["status"] != "pending" || data["model_id"] != "m1" || data["node_id"] != "n1" || data["canvas_id"] != float64(3) {
				t.Fatalf("任务快照不对：%v", data)
			}
			if outs, ok := data["outputs"].([]any); !ok || len(outs) != 0 {
				t.Fatalf("outputs 应是空数组：%v", data["outputs"])
			}
		})
	}

	t.Run("同一个 Idempotency-Key 重复提交返回同一个任务且只冻结一次", func(t *testing.T) {
		env := newGenTaskEnv(t)
		h := map[string]string{"Idempotency-Key": "same"}
		_, first := env.call(t, http.MethodPost, "/api/v1/generation-tasks", genTaskValidBody, h)
		status, second := env.call(t, http.MethodPost, "/api/v1/generation-tasks", genTaskValidBody, h)
		if status != http.StatusAccepted {
			t.Fatalf("重复提交也应是 202：%d", status)
		}
		if first["data"].(map[string]any)["id"] != second["data"].(map[string]any)["id"] {
			t.Fatal("应返回同一个任务")
		}
		if env.repo.credits[1].Frozen != 10 || len(env.repo.tasks) != 1 {
			t.Fatalf("只应创建一个任务、冻结一次：%+v", env.repo.credits[1])
		}
	})

	t.Run("Idempotency-Key 过长返回 400", func(t *testing.T) {
		env := newGenTaskEnv(t)
		status, resp := env.call(t, http.MethodPost, "/api/v1/generation-tasks", genTaskValidBody, map[string]string{"Idempotency-Key": strings.Repeat("k", 200)})
		if status != http.StatusBadRequest || genTaskRespCode(resp) != errcode.ErrInvalidParams.Code {
			t.Fatalf("实际 %d / %v", status, resp)
		}
	})
}

func TestGenerationTaskHandler_Get(t *testing.T) {
	env := newGenTaskEnv(t)
	mine := env.repo.add(model.GenerationTask{UserID: 1, Status: model.TaskSucceeded, OutputJSON: []byte(`[{"asset_id":9,"url":"https://cdn/9","media_type":"video"}]`)})
	theirs := env.repo.add(model.GenerationTask{UserID: 2, Status: model.TaskRunning})
	test := env.repo.add(model.GenerationTask{UserID: 1, Status: model.TaskRunning, IsTest: true})

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantCode   int
	}{
		{"查询自己的任务", "/api/v1/generation-tasks/" + genTaskItoa(mine.ID), http.StatusOK, 0},
		{"别人的任务返回 404 + 40004", "/api/v1/generation-tasks/" + genTaskItoa(theirs.ID), http.StatusNotFound, errcode.ErrTaskNotFound.Code},
		{"试跑任务返回 404 + 40004", "/api/v1/generation-tasks/" + genTaskItoa(test.ID), http.StatusNotFound, errcode.ErrTaskNotFound.Code},
		{"不存在的任务返回 404", "/api/v1/generation-tasks/99999", http.StatusNotFound, errcode.ErrTaskNotFound.Code},
		{"id 非法返回 400 + 10001", "/api/v1/generation-tasks/abc", http.StatusBadRequest, errcode.ErrInvalidParams.Code},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, resp := env.call(t, http.MethodGet, tt.path, "", nil)
			if status != tt.wantStatus || genTaskRespCode(resp) != tt.wantCode {
				t.Fatalf("期望 HTTP %d / code %d，实际 %d / %v", tt.wantStatus, tt.wantCode, status, resp)
			}
			if tt.wantCode == 0 {
				data := resp["data"].(map[string]any)
				outs := data["outputs"].([]any)
				if len(outs) != 1 || outs[0].(map[string]any)["asset_id"] != float64(9) {
					t.Fatalf("outputs 不对：%v", outs)
				}
			}
		})
	}
}

func TestGenerationTaskHandler_List(t *testing.T) {
	env := newGenTaskEnv(t)
	a := env.repo.add(model.GenerationTask{UserID: 1, Status: model.TaskRunning})
	b := env.repo.add(model.GenerationTask{UserID: 1, Status: model.TaskSucceeded})
	theirs := env.repo.add(model.GenerationTask{UserID: 2, Status: model.TaskRunning})

	manyIDs := make([]string, 101)
	for i := range manyIDs {
		manyIDs[i] = genTaskItoa(uint64(i + 1))
	}

	tests := []struct {
		name       string
		query      string
		wantStatus int
		wantCode   int
		wantLen    int
	}{
		{"按 ids 对账", "?ids=" + genTaskItoa(a.ID) + "," + genTaskItoa(b.ID), http.StatusOK, 0, 2},
		{"ids 中别人的任务不出现", "?ids=" + genTaskItoa(a.ID) + "," + genTaskItoa(theirs.ID), http.StatusOK, 0, 1},
		{"status=active 返回进行中的任务", "?status=active", http.StatusOK, 0, 1},
		{"ids 与 status 都没传返回 400", "", http.StatusBadRequest, errcode.ErrInvalidParams.Code, 0},
		{"ids 与 status 同时传返回 400", "?ids=1&status=active", http.StatusBadRequest, errcode.ErrInvalidParams.Code, 0},
		{"ids 含非数字返回 400", "?ids=1,abc", http.StatusBadRequest, errcode.ErrInvalidParams.Code, 0},
		{"ids 超过 100 个返回 400", "?ids=" + strings.Join(manyIDs, ","), http.StatusBadRequest, errcode.ErrInvalidParams.Code, 0},
		{"status 取值非法返回 400", "?status=done", http.StatusBadRequest, errcode.ErrInvalidParams.Code, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, resp := env.call(t, http.MethodGet, "/api/v1/generation-tasks"+tt.query, "", nil)
			if status != tt.wantStatus || genTaskRespCode(resp) != tt.wantCode {
				t.Fatalf("期望 HTTP %d / code %d，实际 %d / %v", tt.wantStatus, tt.wantCode, status, resp)
			}
			if tt.wantCode == 0 {
				list, ok := resp["data"].([]any)
				if !ok || len(list) != tt.wantLen {
					t.Fatalf("data 应是长度 %d 的数组：%v", tt.wantLen, resp["data"])
				}
			}
		})
	}

	t.Run("结果为空时 data 是空数组而不是 null", func(t *testing.T) {
		env := newGenTaskEnv(t)
		status, resp := env.call(t, http.MethodGet, "/api/v1/generation-tasks?status=active", "", nil)
		if status != http.StatusOK {
			t.Fatalf("HTTP %d", status)
		}
		list, ok := resp["data"].([]any)
		if !ok || len(list) != 0 {
			t.Fatalf("data 应是空数组：%#v", resp["data"])
		}
	})
}

func TestGenerationTaskHandler_Cancel(t *testing.T) {
	tests := []struct {
		name       string
		task       model.GenerationTask
		path       string
		wantStatus int
		wantCode   int
	}{
		{"取消进行中的任务", model.GenerationTask{UserID: 1, Status: model.TaskRunning, Credits: 10}, "", http.StatusOK, 0},
		{"已结束的任务返回 409 + 40005", model.GenerationTask{UserID: 1, Status: model.TaskSucceeded, Credits: 10}, "", http.StatusConflict, errcode.ErrTaskNotCancelable.Code},
		{"别人的任务返回 404 + 40004", model.GenerationTask{UserID: 2, Status: model.TaskRunning}, "", http.StatusNotFound, errcode.ErrTaskNotFound.Code},
		{"id 非法返回 400", model.GenerationTask{}, "/abc", http.StatusBadRequest, errcode.ErrInvalidParams.Code},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newGenTaskEnv(t)
			env.repo.credits[1] = &model.UserCredit{UserID: 1, Balance: 50, Frozen: 10}
			task := env.repo.add(tt.task)
			path := "/api/v1/generation-tasks/" + genTaskItoa(task.ID) + "/cancel"
			if tt.path != "" {
				path = "/api/v1/generation-tasks" + tt.path + "/cancel"
			}
			status, resp := env.call(t, http.MethodPost, path, "", nil)
			if status != tt.wantStatus || genTaskRespCode(resp) != tt.wantCode {
				t.Fatalf("期望 HTTP %d / code %d，实际 %d / %v", tt.wantStatus, tt.wantCode, status, resp)
			}
			if tt.wantCode == 0 {
				data := resp["data"].(map[string]any)
				if data["status"] != "canceled" {
					t.Fatalf("状态应为 canceled：%v", data)
				}
				if env.repo.credits[1].Frozen != 0 {
					t.Fatalf("应退回冻结积分：%+v", env.repo.credits[1])
				}
			}
		})
	}
}

func TestGenerationTaskHandler_Credits(t *testing.T) {
	t.Run("返回余额 / 冻结 / 可用", func(t *testing.T) {
		env := newGenTaskEnv(t)
		env.repo.credits[1] = &model.UserCredit{UserID: 1, Balance: 40, Frozen: 15}
		status, resp := env.call(t, http.MethodGet, "/api/v1/credits", "", nil)
		if status != http.StatusOK || genTaskRespCode(resp) != 0 {
			t.Fatalf("实际 %d / %v", status, resp)
		}
		data := resp["data"].(map[string]any)
		if data["balance"] != float64(40) || data["frozen"] != float64(15) || data["available"] != float64(25) {
			t.Fatalf("积分不对：%v", data)
		}
	})
	t.Run("账户不存在时惰性创建并返回初始积分", func(t *testing.T) {
		env := newGenTaskEnv(t)
		_, resp := env.call(t, http.MethodGet, "/api/v1/credits", "", nil)
		data := resp["data"].(map[string]any)
		if data["balance"] != float64(50) || data["available"] != float64(50) {
			t.Fatalf("积分不对：%v", data)
		}
	})
}

func TestGenerationTaskHandler_Webhook(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		body       string
		touchRows  int64
		wantStatus int
		wantCode   int
		wantTouch  string
	}{
		{"密钥正确：触发任务立即查询并返回 200", "/api/v1/webhooks/runninghub/s3cret", `{"taskId":"abc"}`, 1, http.StatusOK, 0, "runninghub/abc"},
		{"找不到任务也返回 200（避免被探测）", "/api/v1/webhooks/runninghub/s3cret", `{"taskId":"nope"}`, 0, http.StatusOK, 0, "runninghub/nope"},
		{"回调体不是 JSON 也返回 200", "/api/v1/webhooks/runninghub/s3cret", `garbage`, 0, http.StatusOK, 0, ""},
		{"密钥错误返回 404", "/api/v1/webhooks/runninghub/wrong", `{"taskId":"abc"}`, 1, http.StatusNotFound, errcode.ErrNotFound.Code, ""},
		{"未知平台返回 200 但不处理", "/api/v1/webhooks/unknown/s3cret", `{"taskId":"abc"}`, 1, http.StatusOK, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newGenTaskEnv(t)
			env.repo.touchRows = tt.touchRows
			if strings.Contains(tt.path, "/unknown/") {
				env.registry.provider = nil
			}
			status, resp := env.call(t, http.MethodPost, tt.path, tt.body, nil)
			if status != tt.wantStatus || genTaskRespCode(resp) != tt.wantCode {
				t.Fatalf("期望 HTTP %d / code %d，实际 %d / %v", tt.wantStatus, tt.wantCode, status, resp)
			}
			if tt.wantTouch == "" && len(env.repo.touched) != 0 {
				t.Fatalf("不应触碰任务：%v", env.repo.touched)
			}
			if tt.wantTouch != "" && (len(env.repo.touched) != 1 || env.repo.touched[0] != tt.wantTouch) {
				t.Fatalf("期望触碰 %s，实际 %v", tt.wantTouch, env.repo.touched)
			}
		})
	}
}

func genTaskItoa(n uint64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
