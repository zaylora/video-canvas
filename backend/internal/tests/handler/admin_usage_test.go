package handler_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
)

// uaUsageRepo 是内存版 service.AdminUsageRepo。
type uaUsageRepo struct {
	rows   []model.UsageRow
	counts []model.UsageStatusCount
	task   *model.GenerationTask
	events []model.GenerationTaskEvent
	filter repository.UsageFilter // 最近一次 List 收到的筛选
}

func (r *uaUsageRepo) List(_ context.Context, f repository.UsageFilter) ([]model.UsageRow, error) {
	r.filter = f
	return r.rows, nil
}

func (r *uaUsageRepo) Count(context.Context, repository.UsageFilter) (int64, error) {
	return int64(len(r.rows)), nil
}

func (r *uaUsageRepo) Summary(context.Context, repository.UsageFilter) ([]model.UsageStatusCount, error) {
	return r.counts, nil
}

func (r *uaUsageRepo) GetTaskByRef(_ context.Context, ref string) (*model.GenerationTask, error) {
	if r.task == nil || r.task.TaskRef != ref {
		return nil, repository.ErrNotFound
	}
	return r.task, nil
}

func (r *uaUsageRepo) GetUser(_ context.Context, id uint64) (*model.User, error) {
	return &model.User{BaseModel: model.BaseModel{ID: id}, Username: "tom", Email: "tom@x.com"}, nil
}

func (r *uaUsageRepo) ListEvents(context.Context, uint64) ([]model.GenerationTaskEvent, error) {
	return r.events, nil
}

func (r *uaUsageRepo) ListTaskLedger(context.Context, uint64) ([]model.CreditLedger, error) {
	return nil, nil
}

// uaAuditListRepo 是内存版 service.AuditLogRepo。
type uaAuditListRepo struct{ rows []model.AuditRow }

func (r *uaAuditListRepo) CountMerged(context.Context, repository.AuditFilter) (int64, error) {
	return int64(len(r.rows)), nil
}

func (r *uaAuditListRepo) ListMerged(context.Context, repository.AuditFilter) ([]model.AuditRow, error) {
	return r.rows, nil
}

func (r *uaAuditListRepo) UsernamesByIDs(context.Context, []uint64) (map[uint64]string, error) {
	return map[uint64]string{uaAdmin: "ops"}, nil
}

func TestAdminUsage_List(t *testing.T) {
	const path = "/api/v1/admin/usage"
	charged := 6
	newEnv := func() *uaEnv {
		env := newUAEnv(t)
		env.usage.rows = []model.UsageRow{
			{ID: 3, TaskRef: "ref3", UserID: 3, Username: "tom", Kind: "video", ModelKey: "m", ModelName: "可灵", Status: model.TaskSucceeded, Credits: 8, ChargedCredits: &charged, CreatedAt: time.Now()},
			{ID: 2, TaskRef: "ref2", UserID: 3, Username: "tom", Kind: "image", ModelKey: "m2", ModelName: "m2", Status: model.TaskRunning, Credits: 8, CreatedAt: time.Now()},
		}
		return env
	}

	t.Run("成功：admin 与 super_admin 都能读；list 嵌套用户，带 total / page / page_size", func(t *testing.T) {
		env := newEnv()
		for _, who := range []int{uaAdmin, uaSuper} {
			r := env.do(http.MethodGet, path, nil, who)
			r.want(t, 200, 0)
			v := decode[struct {
				List     []model.UsageItem `json:"list"`
				Total    int64             `json:"total"`
				Page     int               `json:"page"`
				PageSize int               `json:"page_size"`
			}](t, r)
			if v.Total != 2 || v.Page != 1 || v.PageSize != 20 || len(v.List) != 2 || v.List[0].User.Username != "tom" || v.List[0].ChargedCredits == nil || v.List[1].ChargedCredits != nil {
				t.Fatalf("%+v", v)
			}
		}
	})
	t.Run("查询参数传到筛选：用户、状态、类型、模型、含试跑、时间范围", func(t *testing.T) {
		env := newEnv()
		q := url.Values{"user_id": {"3"}, "status": {"failed"}, "kind": {"video"}, "model": {"kling-v2"}, "include_test": {"true"}, "q": {"tom"}, "page": {"3"}, "page_size": {"5"}}
		env.do(http.MethodGet, path+"?"+q.Encode(), nil, uaAdmin).want(t, 200, 0)
		f := env.usage.filter
		if f.UserID != 3 || len(f.Statuses) != 1 || f.Statuses[0] != model.TaskFailed || f.Kind != "video" || f.Model != "kling-v2" || !f.IncludeTest || f.UserKeyword != "tom" || f.Limit != 5 || f.Offset != 10 {
			t.Fatalf("%+v", f)
		}
	})
	t.Run("参数校验失败：400 + 10001", func(t *testing.T) {
		env := newEnv()
		for name, query := range map[string]string{
			"状态非法":        "status=done",
			"类型非法":        "kind=movie",
			"关键字超长":       "q=" + strings.Repeat("a", 65),
			"时间格式错误":      "from=yesterday",
			"跨度超过90天":     "from=2026-01-01T00:00:00Z&to=2026-10-01T00:00:00Z",
			"页码不是数字":      "page=abc",
			"user_id不是数字": "user_id=abc",
		} {
			t.Run(name, func(t *testing.T) {
				env.do(http.MethodGet, path+"?"+query, nil, uaAdmin).want(t, 400, errcode.ErrInvalidParams.Code)
			})
		}
	})
	t.Run("权限：普通用户 403，未登录 401", func(t *testing.T) {
		env := newEnv()
		env.do(http.MethodGet, path, nil, uaNormal).want(t, 403, errcode.ErrForbidden.Code)
		env.do(http.MethodGet, path, nil, 0).want(t, 401, errcode.ErrUnauthorized.Code)
	})
}

func TestAdminUsage_Summary(t *testing.T) {
	const path = "/api/v1/admin/usage/summary"
	env := newUAEnv(t)
	env.usage.counts = []model.UsageStatusCount{{Status: model.TaskSucceeded, Count: 4, Charged: 30}, {Status: model.TaskFailed, Count: 1}, {Status: model.TaskRunning, Count: 2}}
	r := env.do(http.MethodGet, path, nil, uaAdmin)
	r.want(t, 200, 0)
	if v := decode[model.UsageSummary](t, r); v.Total != 7 || v.Succeeded != 4 || v.Failed != 1 || v.Active != 2 || v.ChargedCredits != 30 {
		t.Fatalf("%+v", v)
	}
	env.do(http.MethodGet, path+"?status=done", nil, uaAdmin).want(t, 400, errcode.ErrInvalidParams.Code)
	env.do(http.MethodGet, path, nil, uaNormal).want(t, 403, errcode.ErrForbidden.Code)
	env.do(http.MethodGet, path, nil, 0).want(t, 401, errcode.ErrUnauthorized.Code)
}

func TestAdminUsage_Get(t *testing.T) {
	ref := idcodec.Encode(9)
	newEnv := func() *uaEnv {
		env := newUAEnv(t)
		env.usage.task = &model.GenerationTask{
			ID: 9, TaskRef: ref, UserID: 3, Kind: model.KindVideo, ModelKey: "m", Status: model.TaskFailed, ErrorCode: "plugin_error", ErrorMessage: "平台繁忙",
			InputJSON: datatypes.JSON(`{"prompt":"海边日落"}`), ConfigSnapshot: datatypes.JSON(`{}`), CreatedAt: time.Now(),
		}
		env.usage.events = []model.GenerationTaskEvent{{ID: 1, TaskID: 9, Event: model.TaskEventFailed, ToStatus: model.TaskFailed, DetailJSON: datatypes.JSON(`{"upstream":"HTTP 503"}`)}}
		return env
	}

	t.Run("成功：详情含用户、输入、时间线与上游原文；数组字段不是 null", func(t *testing.T) {
		env := newEnv()
		r := env.do(http.MethodGet, "/api/v1/admin/usage/"+ref, nil, uaAdmin)
		r.want(t, 200, 0)
		v := decode[model.UsageDetail](t, r)
		if v.Task.TaskRef != ref || v.User.Username != "tom" || v.Input["prompt"] != "海边日落" || v.Error.Upstream == nil || *v.Error.Upstream != "HTTP 503" || len(v.Events) != 1 {
			t.Fatalf("%+v", v)
		}
		for _, field := range []string{`"outputs":[]`, `"ledger":[]`, `"trace":null`} {
			if !strings.Contains(r.Raw, field) {
				t.Fatalf("响应里应有 %s：%s", field, r.Raw)
			}
		}
	})
	t.Run("业务错误：任务不存在 -> 404 + 40004", func(t *testing.T) {
		env := newEnv()
		env.do(http.MethodGet, "/api/v1/admin/usage/"+idcodec.Encode(77), nil, uaAdmin).want(t, 404, errcode.ErrTaskNotFound.Code)
		env.do(http.MethodGet, "/api/v1/admin/usage/not-a-ref", nil, uaAdmin).want(t, 404, errcode.ErrTaskNotFound.Code)
	})
	t.Run("权限：普通用户 403，未登录 401", func(t *testing.T) {
		env := newEnv()
		env.do(http.MethodGet, "/api/v1/admin/usage/"+ref, nil, uaNormal).want(t, 403, errcode.ErrForbidden.Code)
		env.do(http.MethodGet, "/api/v1/admin/usage/"+ref, nil, 0).want(t, 401, errcode.ErrUnauthorized.Code)
	})
}

func TestAdminLogs_Audits(t *testing.T) {
	const path = "/api/v1/admin/logs/audits"
	newEnv := func() *uaEnv {
		env := newUAEnv(t)
		env.audits.rows = []model.AuditRow{
			{Source: "admin", ID: 2, ActorID: uaAdmin, Action: "user.ban", TargetType: "user", TargetRef: "3", DetailJSON: model.JSONText(`{"reason":"x"}`), CreatedAt: time.Now()},
			{Source: "ai", ID: 1, ActorID: 0, Action: "channel.auto_off", TargetType: "channel", TargetRef: "newapi", CreatedAt: time.Now()},
		}
		return env
	}

	t.Run("成功：合并列表，操作人名与对象名已解析，带 total / page / page_size", func(t *testing.T) {
		env := newEnv()
		for _, who := range []int{uaAdmin, uaSuper} {
			r := env.do(http.MethodGet, path, nil, who)
			r.want(t, 200, 0)
			v := decode[struct {
				List     []model.AuditItem `json:"list"`
				Total    int64             `json:"total"`
				Page     int               `json:"page"`
				PageSize int               `json:"page_size"`
			}](t, r)
			if v.Total != 2 || v.Page != 1 || v.PageSize != 20 || len(v.List) != 2 || v.List[0].ActorName != "ops" || v.List[1].ActorID != 0 || v.List[1].TargetRef != "newapi" {
				t.Fatalf("%+v", v)
			}
			if !strings.Contains(r.Raw, `"detail_json":{"reason":"x"}`) || !strings.Contains(r.Raw, `"detail_json":null`) {
				t.Fatalf("detail_json 应是原样 JSON 或 null：%s", r.Raw)
			}
		}
	})
	// 导出 Agent 轨迹写审计 agent.trace.export，审计页要能按「Agent」模块筛出来
	t.Run("module=agent 是合法模块", func(t *testing.T) {
		env := newEnv()
		env.do(http.MethodGet, path+"?module=agent", nil, uaAdmin).want(t, 200, 0)
	})
	t.Run("参数校验失败：400 + 10001", func(t *testing.T) {
		env := newEnv()
		for name, query := range map[string]string{
			"模块非法":   "module=nope",
			"操作人超长":  "actor=" + strings.Repeat("a", 65),
			"时间格式错误": "from=x",
			"页码不是数字": "page=abc",
		} {
			t.Run(name, func(t *testing.T) {
				env.do(http.MethodGet, path+"?"+query, nil, uaAdmin).want(t, 400, errcode.ErrInvalidParams.Code)
			})
		}
	})
	t.Run("权限：普通用户 403，未登录 401", func(t *testing.T) {
		env := newEnv()
		env.do(http.MethodGet, path, nil, uaNormal).want(t, 403, errcode.ErrForbidden.Code)
		env.do(http.MethodGet, path, nil, 0).want(t, 401, errcode.ErrUnauthorized.Code)
	})
}
