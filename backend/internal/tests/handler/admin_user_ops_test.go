package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// 本文件测试 /admin/users 的管控与记录接口。服务用真实实现，积分账户 / 取消任务 / 断连用内存 fake。

// uaCreditStore 是最小的内存积分账户，满足 service.AdminCreditTx（并发语义由 Postgres 集成测试覆盖）。
type uaCreditStore struct {
	accounts map[uint64]*model.UserCredit
	ledger   []model.CreditLedger
}

func newUACreditStore() *uaCreditStore {
	return &uaCreditStore{accounts: map[uint64]*model.UserCredit{}}
}

func (s *uaCreditStore) WithTx(_ context.Context, fn func(tx repository.GenerationTaskTx) error) error {
	return fn(s)
}

func (s *uaCreditStore) EnsureCredit(_ context.Context, id uint64, initial int) error {
	if _, ok := s.accounts[id]; !ok {
		s.accounts[id] = &model.UserCredit{UserID: id, Balance: initial}
	}
	return nil
}

func (s *uaCreditStore) LockCredit(_ context.Context, id uint64) (*model.UserCredit, error) {
	acc, ok := s.accounts[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	cp := *acc
	return &cp, nil
}

func (s *uaCreditStore) AddCredit(_ context.Context, id uint64, dBalance, dFrozen int) error {
	s.accounts[id].Balance += dBalance
	s.accounts[id].Frozen += dFrozen
	return nil
}

func (s *uaCreditStore) InsertLedger(_ context.Context, e *model.CreditLedger) (bool, error) {
	s.ledger = append(s.ledger, *e)
	return true, nil
}

func (s *uaCreditStore) FindByIdempotencyKey(context.Context, uint64, string) (*model.GenerationTask, error) {
	return nil, repository.ErrNotFound
}
func (s *uaCreditStore) CountActive(context.Context, uint64) (int64, error) { return 0, nil }
func (s *uaCreditStore) InsertTask(context.Context, *model.GenerationTask) (bool, error) {
	return true, nil
}

func (s *uaCreditStore) UpdateIf(context.Context, uint64, []string, map[string]any, bool) (*model.GenerationTask, error) {
	return nil, repository.ErrStateConflict
}

type uaCanceler struct{ calls []uint64 }

func (c *uaCanceler) CancelActiveByUser(_ context.Context, id uint64) (*service.CancelActiveResult, error) {
	c.calls = append(c.calls, id)
	return &service.CancelActiveResult{Canceled: 2, Failed: []uint64{9}}, nil
}

type uaConns struct{ ids []uint64 }

func (c *uaConns) DisconnectUser(id uint64) int { c.ids = append(c.ids, id); return 1 }

func decode[T any](t *testing.T, r uaResp) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(r.Data, &v); err != nil {
		t.Fatalf("解析 data 失败：%v %s", err, r.Raw)
	}
	return v
}

func TestAdminUser_Credits(t *testing.T) {
	const path = "/api/v1/admin/users/3/credits"
	t.Run("成功：返回最新 balance / frozen / available", func(t *testing.T) {
		env := newUAEnv(t)
		r := env.do(http.MethodPost, path, map[string]any{"mode": "add", "amount": 50, "note": "活动补偿"}, uaAdmin)
		r.want(t, 200, 0)
		v := decode[model.AdminCreditView](t, r)
		if v.Balance != 150 || v.Frozen != 30 || v.Available != 120 {
			t.Fatalf("%+v", v)
		}
		if len(env.credit.ledger) != 1 || env.credit.ledger[0].Type != model.LedgerAdminAdjust {
			t.Fatalf("%+v", env.credit.ledger)
		}
	})
	t.Run("set 为 0 合法（amount=0 不被 required 拦掉）", func(t *testing.T) {
		env := newUAEnv(t)
		env.do(http.MethodPost, path, map[string]any{"mode": "set", "amount": 0, "note": "清零"}, uaSuper).want(t, 200, 0)
	})
	t.Run("参数校验失败：400 + 10001", func(t *testing.T) {
		env := newUAEnv(t)
		for name, body := range map[string]any{
			"方式非法":     map[string]any{"mode": "mul", "amount": 1, "note": "x"},
			"缺方式":      map[string]any{"amount": 1, "note": "x"},
			"缺 amount": map[string]any{"mode": "set", "note": "x"},
			"负数":       map[string]any{"mode": "add", "amount": -1, "note": "x"},
			"缺备注":      map[string]any{"mode": "add", "amount": 1},
			"备注超长":     map[string]any{"mode": "add", "amount": 1, "note": strings.Repeat("好", 101)},
			"不是 JSON":  "oops",
		} {
			t.Run(name, func(t *testing.T) {
				env.do(http.MethodPost, path, body, uaAdmin).want(t, 400, errcode.ErrInvalidParams.Code)
			})
		}
	})
	t.Run("业务错误：扣减超过可用 -> 400 + 53005，文案含最多可扣", func(t *testing.T) {
		env := newUAEnv(t)
		r := env.do(http.MethodPost, path, map[string]any{"mode": "sub", "amount": 71, "note": "x"}, uaAdmin)
		r.want(t, 400, errcode.ErrCreditAdjustInvalid.Code)
		if !strings.Contains(r.Msg, "最多可扣 70") {
			t.Fatal(r.Msg)
		}
	})
	t.Run("权限：admin 不能操作管理员 / 给自己调整 -> 403；目标不存在 -> 404；普通用户 / 未登录被中间件挡住", func(t *testing.T) {
		env := newUAEnv(t)
		body := map[string]any{"mode": "add", "amount": 1, "note": "x"}
		r := env.do(http.MethodPost, "/api/v1/admin/users/1/credits", body, uaAdmin)
		r.want(t, 403, errcode.ErrForbidden.Code)
		if !strings.Contains(r.Msg, "admin 不能操作管理员账号") {
			t.Fatal(r.Msg)
		}
		r = env.do(http.MethodPost, "/api/v1/admin/users/2/credits", body, uaAdmin)
		r.want(t, 403, errcode.ErrForbidden.Code)
		if !strings.Contains(r.Msg, "不能给自己调整积分") {
			t.Fatal(r.Msg)
		}
		env.do(http.MethodPost, "/api/v1/admin/users/99/credits", body, uaAdmin).want(t, 404, errcode.ErrUserNotFound.Code)
		env.do(http.MethodPost, path, body, uaNormal).want(t, 403, errcode.ErrForbidden.Code)
		env.do(http.MethodPost, path, body, 0).want(t, 401, errcode.ErrUnauthorized.Code)
	})
}

func TestAdminUser_Limits(t *testing.T) {
	const path = "/api/v1/admin/users/3/limits"
	env := newUAEnv(t)
	env.do(http.MethodPut, path, map[string]any{"max_active_tasks": 6}, uaAdmin).want(t, 200, 0)
	if n := env.users.users[3].MaxActiveTasks; n == nil || *n != 6 {
		t.Fatalf("%v", n)
	}
	env.do(http.MethodPut, path, map[string]any{"max_active_tasks": nil}, uaAdmin).want(t, 200, 0)
	if env.users.users[3].MaxActiveTasks != nil {
		t.Fatal("null 应清除覆盖")
	}
	for _, v := range []int{0, 65, -1} {
		env.do(http.MethodPut, path, map[string]any{"max_active_tasks": v}, uaAdmin).want(t, 400, errcode.ErrInvalidParams.Code)
	}
	env.do(http.MethodPut, "/api/v1/admin/users/1/limits", map[string]any{"max_active_tasks": 3}, uaAdmin).want(t, 403, errcode.ErrForbidden.Code)
	env.do(http.MethodPut, "/api/v1/admin/users/2/limits", map[string]any{"max_active_tasks": 3}, uaAdmin).want(t, 200, 0) // 自己改允许
}

func TestAdminUser_Status(t *testing.T) {
	const path = "/api/v1/admin/users/3/status"
	t.Run("封禁并取消任务：返回取消数与失败 id，断开连接", func(t *testing.T) {
		env := newUAEnv(t)
		r := env.do(http.MethodPut, path, map[string]any{"status": "disabled", "cancel_active": true}, uaAdmin)
		r.want(t, 200, 0)
		v := decode[model.AdminStatusResult](t, r)
		if v.Status != "disabled" || v.Canceled != 2 || v.CancelFailed != 1 || len(v.FailedTaskIDs) != 1 || v.FailedTaskIDs[0] != 9 {
			t.Fatalf("%+v", v)
		}
		if env.users.users[3].Status != "disabled" || len(env.conns.ids) != 1 || len(env.cancel.calls) != 1 {
			t.Fatal("应更新状态、断连并取消任务")
		}
	})
	t.Run("启用", func(t *testing.T) {
		env := newUAEnv(t)
		env.do(http.MethodPut, "/api/v1/admin/users/4/status", map[string]any{"status": "active", "cancel_active": true}, uaAdmin).want(t, 200, 0)
		if env.users.users[4].Status != "active" || len(env.cancel.calls) != 0 {
			t.Fatal("启用时忽略 cancel_active")
		}
	})
	t.Run("参数校验失败", func(t *testing.T) {
		env := newUAEnv(t)
		for _, body := range []any{map[string]any{"status": "frozen"}, map[string]any{}, "oops"} {
			env.do(http.MethodPut, path, body, uaAdmin).want(t, 400, errcode.ErrInvalidParams.Code)
		}
	})
	t.Run("权限：不能封禁自己；admin 不能封禁超管", func(t *testing.T) {
		env := newUAEnv(t)
		r := env.do(http.MethodPut, "/api/v1/admin/users/2/status", map[string]any{"status": "disabled"}, uaAdmin)
		r.want(t, 403, errcode.ErrForbidden.Code)
		if !strings.Contains(r.Msg, "不能封禁自己") {
			t.Fatal(r.Msg)
		}
		env.do(http.MethodPut, "/api/v1/admin/users/1/status", map[string]any{"status": "disabled"}, uaAdmin).want(t, 403, errcode.ErrForbidden.Code)
		env.do(http.MethodPut, "/api/v1/admin/users/99/status", map[string]any{"status": "disabled"}, uaAdmin).want(t, 404, errcode.ErrUserNotFound.Code)
	})
}

func TestAdminUser_Batch(t *testing.T) {
	t.Run("批量积分：路由不与 /:id 冲突，逐项返回结果", func(t *testing.T) {
		env := newUAEnv(t)
		r := env.do(http.MethodPost, "/api/v1/admin/users/batch/credits", map[string]any{"ids": []int{3, 1, 99, 3}, "amount": 10, "note": "活动"}, uaAdmin)
		r.want(t, 200, 0)
		res := decode[model.BatchResult](t, r)
		if len(res.Results) != 3 || !res.Results[0].OK || res.Results[1].OK || res.Results[2].OK {
			t.Fatalf("%+v", res)
		}
		if !strings.Contains(res.Results[1].Error, "admin 不能操作管理员账号") || !strings.Contains(res.Results[2].Error, "用户不存在") {
			t.Fatalf("%+v", res)
		}
		if env.credit.accounts[3].Balance != 110 {
			t.Fatal("成功项应入账")
		}
	})
	t.Run("批量状态：逐项返回，不取消任务", func(t *testing.T) {
		env := newUAEnv(t)
		r := env.do(http.MethodPut, "/api/v1/admin/users/batch/status", map[string]any{"ids": []int{3, 2}, "status": "disabled"}, uaAdmin)
		r.want(t, 200, 0)
		res := decode[model.BatchResult](t, r)
		if len(res.Results) != 2 || !res.Results[0].OK || res.Results[1].OK || !strings.Contains(res.Results[1].Error, "不能封禁自己") {
			t.Fatalf("%+v", res)
		}
		if len(env.cancel.calls) != 0 {
			t.Fatal("批量不取消任务")
		}
	})
	t.Run("参数校验失败：空列表、超过 200 个、amount<=0、非法状态、缺备注", func(t *testing.T) {
		env := newUAEnv(t)
		many := make([]int, 201)
		for i := range many {
			many[i] = i + 1
		}
		for name, body := range map[string]any{
			"空列表":      map[string]any{"ids": []int{}, "amount": 1, "note": "x"},
			"超过 200":   map[string]any{"ids": many, "amount": 1, "note": "x"},
			"amount 0": map[string]any{"ids": []int{3}, "amount": 0, "note": "x"},
			"缺备注":      map[string]any{"ids": []int{3}, "amount": 1},
			"id 为 0":   map[string]any{"ids": []int{0}, "amount": 1, "note": "x"},
		} {
			t.Run(name, func(t *testing.T) {
				env.do(http.MethodPost, "/api/v1/admin/users/batch/credits", body, uaAdmin).want(t, 400, errcode.ErrInvalidParams.Code)
			})
		}
		env.do(http.MethodPut, "/api/v1/admin/users/batch/status", map[string]any{"ids": []int{3}, "status": "frozen"}, uaAdmin).want(t, 400, errcode.ErrInvalidParams.Code)
		env.do(http.MethodPut, "/api/v1/admin/users/batch/status", map[string]any{"ids": []int{}, "status": "active"}, uaAdmin).want(t, 400, errcode.ErrInvalidParams.Code)
	})
	t.Run("普通用户 403", func(t *testing.T) {
		env := newUAEnv(t)
		env.do(http.MethodPost, "/api/v1/admin/users/batch/credits", map[string]any{"ids": []int{3}, "amount": 1, "note": "x"}, uaNormal).want(t, 403, errcode.ErrForbidden.Code)
	})
}

func TestAdminUser_Records(t *testing.T) {
	type page struct {
		Items      []map[string]any `json:"items"`
		NextCursor string           `json:"next_cursor"`
	}
	env := newUAEnv(t)

	t.Run("生成记录：游标分页形态 {items, next_cursor}", func(t *testing.T) {
		r := env.do(http.MethodGet, "/api/v1/admin/users/3/tasks?status=all&limit=2", nil, uaAdmin)
		r.want(t, 200, 0)
		p := decode[page](t, r)
		if len(p.Items) != 2 || p.NextCursor == "" {
			t.Fatalf("%+v", p)
		}
		r = env.do(http.MethodGet, "/api/v1/admin/users/3/tasks", nil, uaAdmin)
		p = decode[page](t, r)
		if len(p.Items) != 3 || p.NextCursor != "" {
			t.Fatalf("取完时 next_cursor 应为空串：%+v", p)
		}
		if !strings.Contains(r.Raw, `"next_cursor":""`) {
			t.Fatalf("next_cursor 应序列化为空串：%s", r.Raw)
		}
	})
	t.Run("积分流水与登录记录", func(t *testing.T) {
		r := env.do(http.MethodGet, "/api/v1/admin/users/3/ledger?type=admin", nil, uaAdmin)
		r.want(t, 200, 0)
		if p := decode[page](t, r); len(p.Items) != 2 || p.Items[0]["operator_name"] != "ops" {
			t.Fatalf("%+v", p)
		}
		r = env.do(http.MethodGet, "/api/v1/admin/users/3/logins?result=fail", nil, uaAdmin)
		r.want(t, 200, 0)
		if p := decode[page](t, r); len(p.Items) != 1 {
			t.Fatalf("%+v", p)
		}
	})
	t.Run("参数校验失败：非法筛选、非法游标", func(t *testing.T) {
		for _, p := range []string{"tasks?status=bogus", "ledger?type=bogus", "logins?result=bogus", "tasks?cursor=!!!", "ledger?cursor=!!!", "logins?limit=abc"} {
			env.do(http.MethodGet, "/api/v1/admin/users/3/"+p, nil, uaAdmin).want(t, 400, errcode.ErrInvalidParams.Code)
		}
	})
	t.Run("用户不存在 404；普通用户 403", func(t *testing.T) {
		env.do(http.MethodGet, "/api/v1/admin/users/99/tasks", nil, uaAdmin).want(t, 404, errcode.ErrUserNotFound.Code)
		env.do(http.MethodGet, "/api/v1/admin/users/3/ledger", nil, uaNormal).want(t, 403, errcode.ErrForbidden.Code)
	})
}

// ---- 角色 / 重置密码 / 流水符号 ----

func TestAdminUser_SetRole(t *testing.T) {
	const path = "/api/v1/admin/users/3/role"
	t.Run("成功：super_admin 调整角色，目标立即读到新角色", func(t *testing.T) {
		env := newUAEnv(t)
		env.do(http.MethodPut, path, map[string]any{"role": "admin"}, uaSuper).want(t, 200, 0)
		if env.users.users[uaNormal].Role != model.RoleAdmin {
			t.Fatalf("角色没改：%s", env.users.users[uaNormal].Role)
		}
		// 新角色立即生效：tom 现在能访问 admin 接口
		env.do(http.MethodGet, "/api/v1/admin/ai/me", nil, uaNormal).want(t, 200, 0)
	})
	t.Run("参数校验失败：400 + 10001", func(t *testing.T) {
		env := newUAEnv(t)
		for name, body := range map[string]any{
			"角色非法":    map[string]any{"role": "root"},
			"缺角色":     map[string]any{},
			"角色为空":    map[string]any{"role": ""},
			"不是 JSON": "oops",
		} {
			t.Run(name, func(t *testing.T) {
				env.do(http.MethodPut, path, body, uaSuper).want(t, 400, errcode.ErrInvalidParams.Code)
			})
		}
		env.do(http.MethodPut, "/api/v1/admin/users/abc/role", map[string]any{"role": "admin"}, uaSuper).want(t, 400, errcode.ErrInvalidParams.Code)
	})
	t.Run("权限：admin 与普通用户 403，未登录 401", func(t *testing.T) {
		env := newUAEnv(t)
		env.do(http.MethodPut, path, map[string]any{"role": "admin"}, uaAdmin).want(t, 403, errcode.ErrForbidden.Code)
		env.do(http.MethodPut, path, map[string]any{"role": "admin"}, uaNormal).want(t, 403, errcode.ErrForbidden.Code)
		env.do(http.MethodPut, path, map[string]any{"role": "admin"}, 0).want(t, 401, errcode.ErrUnauthorized.Code)
	})
	t.Run("业务错误：自己改自己 403、目标不存在 404", func(t *testing.T) {
		env := newUAEnv(t)
		r := env.do(http.MethodPut, "/api/v1/admin/users/1/role", map[string]any{"role": "admin"}, uaSuper)
		r.want(t, 403, errcode.ErrForbidden.Code)
		if r.Msg != "不能修改自己的角色" {
			t.Fatalf("msg=%q", r.Msg)
		}
		env.do(http.MethodPut, "/api/v1/admin/users/99/role", map[string]any{"role": "admin"}, uaSuper).want(t, 404, errcode.ErrUserNotFound.Code)
	})
}

func TestAdminUser_ResetPassword(t *testing.T) {
	const path = "/api/v1/admin/users/2/reset-password" // 重置 ops（admin）
	type view struct {
		TempPassword string `json:"temp_password"`
	}
	t.Run("缺省：返回生成的临时密码，目标旧 token 立即失效", func(t *testing.T) {
		env := newUAEnv(t)
		env.do(http.MethodGet, "/api/v1/admin/ai/me", nil, uaAdmin).want(t, 200, 0) // 重置前旧 token 可用
		r := env.do(http.MethodPost, path, nil, uaSuper)
		r.want(t, 200, 0)
		v := decode[view](t, r)
		if len(v.TempPassword) < 12 {
			t.Fatalf("临时密码太短：%q", v.TempPassword)
		}
		if bcrypt.CompareHashAndPassword([]byte(env.users.users[uaAdmin].Password), []byte(v.TempPassword)) != nil {
			t.Fatal("库里应是该临时密码的 bcrypt 哈希")
		}
		env.do(http.MethodGet, "/api/v1/admin/ai/me", nil, uaAdmin).want(t, 401, errcode.ErrUnauthorized.Code) // 旧 token 失效
	})
	t.Run("指定新密码：原样返回", func(t *testing.T) {
		env := newUAEnv(t)
		r := env.do(http.MethodPost, path, map[string]any{"new_password": "abcdef123"}, uaSuper)
		r.want(t, 200, 0)
		if v := decode[view](t, r); v.TempPassword != "abcdef123" {
			t.Fatalf("%+v", v)
		}
	})
	t.Run("super_admin 可重置自己", func(t *testing.T) {
		env := newUAEnv(t)
		env.do(http.MethodPost, "/api/v1/admin/users/1/reset-password", nil, uaSuper).want(t, 200, 0)
	})
	t.Run("参数校验失败：400 + 10001", func(t *testing.T) {
		env := newUAEnv(t)
		for name, body := range map[string]any{
			"太短":      map[string]any{"new_password": "12345"},
			"太长":      map[string]any{"new_password": strings.Repeat("a", 129)},
			"不是 JSON": "oops",
		} {
			t.Run(name, func(t *testing.T) {
				env.do(http.MethodPost, path, body, uaSuper).want(t, 400, errcode.ErrInvalidParams.Code)
			})
		}
		env.do(http.MethodPost, "/api/v1/admin/users/abc/reset-password", nil, uaSuper).want(t, 400, errcode.ErrInvalidParams.Code)
	})
	t.Run("权限与业务错误", func(t *testing.T) {
		env := newUAEnv(t)
		env.do(http.MethodPost, path, nil, uaAdmin).want(t, 403, errcode.ErrForbidden.Code)
		env.do(http.MethodPost, path, nil, 0).want(t, 401, errcode.ErrUnauthorized.Code)
		env.do(http.MethodPost, "/api/v1/admin/users/99/reset-password", nil, uaSuper).want(t, 404, errcode.ErrUserNotFound.Code)
	})
}

func TestAdminUser_Ledger_SignedAmount(t *testing.T) {
	env := newUAEnv(t)
	env.users.ledgers = []model.AdminLedgerItem{
		{ID: 5, Type: model.LedgerRefund, Amount: 2},
		{ID: 4, Type: model.LedgerSettle, Amount: 6},
		{ID: 3, Type: model.LedgerFreeze, Amount: 8},
		{ID: 2, Type: model.LedgerAdminAdjust, Amount: -5},
		{ID: 1, Type: model.LedgerInitial, Amount: 50},
	}
	r := env.do(http.MethodGet, "/api/v1/admin/users/3/ledger", nil, uaAdmin)
	r.want(t, 200, 0)
	page := decode[model.CursorPage[model.AdminLedgerItem]](t, r)
	want := []int{2, -6, -8, -5, 50}
	if len(page.Items) != len(want) {
		t.Fatalf("%s", r.Raw)
	}
	for i, w := range want {
		if page.Items[i].Amount != w {
			t.Errorf("第 %d 条 %s 金额 = %d，期望 %d", i, page.Items[i].Type, page.Items[i].Amount, w)
		}
	}
}
