package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service"
)

// 本文件测试后台用户管控：积分调整、并发上限、封禁 / 启用（含取消任务、断 WS）、批量、三个记录列表。

// ---- fakes ----

// fakeCreditStore 是内存版积分账户 + 流水，满足 service.AdminCreditTx。
// WithTx 在进入时拍快照，fn 返回错误就恢复快照，模拟事务回滚；行锁与并发语义由真实 Postgres 的集成测试覆盖。
type fakeCreditStore struct {
	mu       sync.Mutex
	accounts map[uint64]*model.UserCredit
	ledger   []model.CreditLedger
	errs     map[string]error
}

func newFakeCreditStore() *fakeCreditStore {
	return &fakeCreditStore{accounts: map[uint64]*model.UserCredit{}, errs: map[string]error{}}
}

func (s *fakeCreditStore) put(userID uint64, balance, frozen int) {
	s.accounts[userID] = &model.UserCredit{UserID: userID, Balance: balance, Frozen: frozen}
}

func (s *fakeCreditStore) WithTx(_ context.Context, fn func(tx repository.GenerationTaskTx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := map[uint64]model.UserCredit{}
	for k, v := range s.accounts {
		snap[k] = *v
	}
	nLedger := len(s.ledger)
	if err := fn(s); err != nil {
		s.accounts = map[uint64]*model.UserCredit{}
		for k, v := range snap {
			c := v
			s.accounts[k] = &c
		}
		s.ledger = s.ledger[:nLedger]
		return err
	}
	return nil
}

func (s *fakeCreditStore) EnsureCredit(_ context.Context, userID uint64, initial int) error {
	if err := s.errs["EnsureCredit"]; err != nil {
		return err
	}
	if _, ok := s.accounts[userID]; ok {
		return nil
	}
	s.accounts[userID] = &model.UserCredit{UserID: userID, Balance: initial}
	s.ledger = append(s.ledger, model.CreditLedger{UserID: userID, Type: model.LedgerInitial, Amount: initial})
	return nil
}

func (s *fakeCreditStore) LockCredit(_ context.Context, userID uint64) (*model.UserCredit, error) {
	if err := s.errs["LockCredit"]; err != nil {
		return nil, err
	}
	acc, ok := s.accounts[userID]
	if !ok {
		return nil, repository.ErrNotFound
	}
	cp := *acc
	return &cp, nil
}

func (s *fakeCreditStore) AddCredit(_ context.Context, userID uint64, dBalance, dFrozen int) error {
	if err := s.errs["AddCredit"]; err != nil {
		return err
	}
	acc, ok := s.accounts[userID]
	if !ok {
		return repository.ErrNotFound
	}
	acc.Balance += dBalance
	acc.Frozen += dFrozen
	return nil
}

func (s *fakeCreditStore) InsertLedger(_ context.Context, e *model.CreditLedger) (bool, error) {
	if err := s.errs["InsertLedger"]; err != nil {
		return false, err
	}
	s.ledger = append(s.ledger, *e)
	return true, nil
}

func (s *fakeCreditStore) FindByIdempotencyKey(context.Context, uint64, string) (*model.GenerationTask, error) {
	return nil, repository.ErrNotFound
}
func (s *fakeCreditStore) CountActive(context.Context, uint64) (int64, error) { return 0, nil }
func (s *fakeCreditStore) InsertTask(context.Context, *model.GenerationTask) (bool, error) {
	return true, nil
}

func (s *fakeCreditStore) UpdateIf(context.Context, uint64, []string, map[string]any, bool) (*model.GenerationTask, error) {
	return nil, repository.ErrStateConflict
}

func (s *fakeCreditStore) adjustLedger() []model.CreditLedger {
	var out []model.CreditLedger
	for _, l := range s.ledger {
		if l.Type == model.LedgerAdminAdjust {
			out = append(out, l)
		}
	}
	return out
}

type fakeInvalidator struct{ ids []uint64 }

func (f *fakeInvalidator) InvalidateUser(_ context.Context, id uint64) { f.ids = append(f.ids, id) }

type fakeDisconnector struct{ ids []uint64 }

func (f *fakeDisconnector) DisconnectUser(id uint64) int { f.ids = append(f.ids, id); return 1 }

type fakeCanceler struct {
	res   *CancelActiveResult
	err   error
	calls []uint64
}

func (f *fakeCanceler) CancelActiveByUser(_ context.Context, id uint64) (*CancelActiveResult, error) {
	f.calls = append(f.calls, id)
	if f.err != nil {
		return nil, f.err
	}
	if f.res == nil {
		return &CancelActiveResult{}, nil
	}
	return f.res, nil
}

// opsEnv 组装带全部管控依赖的后台用户服务。用户：1 root(super_admin) 2 ops(admin) 3 tom(user) 4 ops2(admin) 5 root2(super_admin)。
type opsEnv struct {
	svc      *AdminUserService
	repo     *fakeUserRepo
	audit    *fakeAudit
	credits  *fakeCreditStore
	inval    *fakeInvalidator
	conns    *fakeDisconnector
	canceler *fakeCanceler
}

const (
	opsRoot  = 1
	opsAdmin = 2
	opsTom   = 3
	opsAdm2  = 4
	opsRoot2 = 5
)

func newOpsEnv() *opsEnv {
	repo := newFakeUserRepo()
	repo.seed(model.User{Username: "root", Role: model.RoleSuperAdmin})
	repo.seed(model.User{Username: "ops", Role: model.RoleAdmin})
	repo.seed(model.User{Username: "tom", Role: model.RoleUser})
	repo.seed(model.User{Username: "ops2", Role: model.RoleAdmin})
	repo.seed(model.User{Username: "root2", Role: model.RoleSuperAdmin})
	e := &opsEnv{repo: repo, audit: &fakeAudit{}, credits: newFakeCreditStore(), inval: &fakeInvalidator{}, conns: &fakeDisconnector{}, canceler: &fakeCanceler{}}
	settings := NewSettingsService(newFakeSettingsRepo(), nil)
	e.svc = NewAdminUserService(repo, e.audit, settings, WithAdminControls(AdminControls{
		Credits: e.credits, Users: e.inval, Tasks: e.canceler, Conns: e.conns,
	}))
	e.credits.put(opsTom, 100, 30) // 余额 100，冻结 30，可用 70
	return e
}

func intp(n int) *int { return &n }

// wantBizErr 断言 err 是业务错误且 Code 一致。
func wantBizErr(t *testing.T, err error, want *errcode.Error) {
	t.Helper()
	_ = bizErrOf(t, err, want) // 这里只关心 Code，Msg 由调用 bizErrOf 的用例自己断言
}

// bizErrOf 同 wantBizErr，并返回业务错误，供断言 Msg。
func bizErrOf(t *testing.T, err error, want *errcode.Error) *errcode.Error {
	t.Helper()
	var ec *errcode.Error
	if !errors.As(err, &ec) || ec.Code != want.Code {
		t.Fatalf("期望业务错误 %d，实际 %v", want.Code, err)
	}
	return ec
}

// ---- 积分调整 ----

func TestAdminUser_AdjustCredits(t *testing.T) {
	ctx := context.Background()

	t.Run("成功：add / sub / set 的 delta、余额、流水与审计", func(t *testing.T) {
		cases := []struct {
			name         string
			mode         string
			amount       int
			wantBalance  int
			wantDelta    int
			wantAvailabl int
		}{
			{"add 50", model.CreditModeAdd, 50, 150, 50, 120},
			{"sub 到刚好用完", model.CreditModeSub, 70, 30, -70, 0},
			{"set 20：可用设为 20，余额 = 20 + 冻结 30", model.CreditModeSet, 20, 50, -50, 20},
			{"set 0 允许", model.CreditModeSet, 0, 30, -70, 0},
			{"set 比当前可用大", model.CreditModeSet, 200, 230, 130, 200},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				e := newOpsEnv()
				v, err := e.svc.AdjustCredits(ctx, opsAdmin, opsTom, &model.AdjustCreditsReq{Mode: c.mode, Amount: intp(c.amount), Note: "  补偿  "})
				if err != nil {
					t.Fatal(err)
				}
				if v.Balance != c.wantBalance || v.Frozen != 30 || v.Available != c.wantAvailabl {
					t.Fatalf("返回值不对：%+v", v)
				}
				led := e.credits.adjustLedger()
				if len(led) != 1 || led[0].Amount != c.wantDelta || led[0].TaskID != nil || led[0].Note != "补偿" ||
					led[0].OperatorID == nil || *led[0].OperatorID != opsAdmin || led[0].UserID != opsTom {
					t.Fatalf("admin_adjust 流水不对：%+v", led)
				}
				if got := e.credits.accounts[opsTom]; got.Balance != c.wantBalance {
					t.Fatalf("账户余额不对：%+v", got)
				}
				if len(e.audit.logs) != 1 || e.audit.logs[0].Action != model.AdminAuditCreditAdjust || e.audit.logs[0].ActorID != opsAdmin ||
					e.audit.logs[0].TargetID != opsTom || e.audit.logs[0].TargetType != model.AdminAuditTargetUser {
					t.Fatalf("审计不对：%+v", e.audit.logs)
				}
				var d map[string]any
				_ = json.Unmarshal(e.audit.logs[0].DetailJSON, &d)
				if d["mode"] != c.mode || int(d["amount"].(float64)) != c.amount || int(d["delta"].(float64)) != c.wantDelta || d["note"] != "补偿" {
					t.Fatalf("审计 detail 应为 {mode, amount, delta, note}：%v", d)
				}
			})
		}
	})

	t.Run("业务错误：不合法的调整不写任何东西", func(t *testing.T) {
		long := strings.Repeat("好", 101)
		cases := []struct {
			name    string
			req     model.AdjustCreditsReq
			wantMsg string
		}{
			{"sub 超过可用：提示最多可扣 70", model.AdjustCreditsReq{Mode: "sub", Amount: intp(71), Note: "x"}, "最多可扣 70"},
			{"add 0", model.AdjustCreditsReq{Mode: "add", Amount: intp(0), Note: "x"}, ""},
			{"sub 0", model.AdjustCreditsReq{Mode: "sub", Amount: intp(0), Note: "x"}, ""},
			{"负数", model.AdjustCreditsReq{Mode: "set", Amount: intp(-1), Note: "x"}, ""},
			{"缺 amount", model.AdjustCreditsReq{Mode: "set", Note: "x"}, ""},
			{"未知方式", model.AdjustCreditsReq{Mode: "mul", Amount: intp(1), Note: "x"}, ""},
			{"备注空白", model.AdjustCreditsReq{Mode: "add", Amount: intp(1), Note: "   "}, ""},
			{"备注超长", model.AdjustCreditsReq{Mode: "add", Amount: intp(1), Note: long}, ""},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				e := newOpsEnv()
				_, err := e.svc.AdjustCredits(ctx, opsAdmin, opsTom, &c.req)
				ec := bizErrOf(t, err, errcode.ErrCreditAdjustInvalid)
				if c.wantMsg != "" && !strings.Contains(ec.Msg, c.wantMsg) {
					t.Fatalf("Msg 应含 %q：%s", c.wantMsg, ec.Msg)
				}
				if len(e.credits.adjustLedger()) != 0 || e.credits.accounts[opsTom].Balance != 100 || len(e.audit.logs) != 0 {
					t.Fatal("失败时不应改账户 / 流水 / 审计")
				}
			})
		}
	})

	t.Run("没有积分账户的老用户：先按初始积分建账户并写 initial 流水，再调整", func(t *testing.T) {
		e := newOpsEnv()
		delete(e.credits.accounts, opsTom)
		v, err := e.svc.AdjustCredits(ctx, opsAdmin, opsTom, &model.AdjustCreditsReq{Mode: "add", Amount: intp(10), Note: "补发"})
		if err != nil || v.Balance != 60 || v.Available != 60 { // 初始 50 + 10
			t.Fatalf("%v %+v", err, v)
		}
		var types []string
		for _, l := range e.credits.ledger {
			types = append(types, l.Type)
		}
		if strings.Join(types, ",") != "initial,admin_adjust" {
			t.Fatalf("流水应为 initial + admin_adjust：%v", types)
		}
	})

	t.Run("sub 失败时连同刚建的账户一起回滚", func(t *testing.T) {
		e := newOpsEnv()
		delete(e.credits.accounts, opsTom)
		_, err := e.svc.AdjustCredits(ctx, opsAdmin, opsTom, &model.AdjustCreditsReq{Mode: "sub", Amount: intp(51), Note: "x"})
		ec := bizErrOf(t, err, errcode.ErrCreditAdjustInvalid)
		if !strings.Contains(ec.Msg, "最多可扣 50") {
			t.Fatal(ec.Msg)
		}
		if len(e.credits.accounts) != 1+0 && e.credits.accounts[opsTom] != nil {
			t.Fatal("事务应整体回滚")
		}
	})

	t.Run("目标用户不存在", func(t *testing.T) {
		e := newOpsEnv()
		_, err := e.svc.AdjustCredits(ctx, opsAdmin, 99, &model.AdjustCreditsReq{Mode: "add", Amount: intp(1), Note: "x"})
		wantBizErr(t, err, errcode.ErrUserNotFound)
	})

	t.Run("权限：admin 不能操作管理员账号，也不能给自己调整；super_admin 可以", func(t *testing.T) {
		add := &model.AdjustCreditsReq{Mode: "add", Amount: intp(5), Note: "x"}
		for _, c := range []struct {
			name           string
			actor, target  uint64
			wantForbid     bool
			wantMsgContain string
		}{
			{"admin -> 普通用户", opsAdmin, opsTom, false, ""},
			{"admin -> 另一个 admin", opsAdmin, opsAdm2, true, "admin 不能操作管理员账号"},
			{"admin -> super_admin", opsAdmin, opsRoot, true, "admin 不能操作管理员账号"},
			{"admin -> 自己", opsAdmin, opsAdmin, true, "admin 不能给自己调整积分"},
			{"super_admin -> 自己", opsRoot, opsRoot, false, ""},
			{"super_admin -> admin", opsRoot, opsAdmin, false, ""},
			{"super_admin -> 另一个 super_admin", opsRoot, opsRoot2, false, ""},
		} {
			t.Run(c.name, func(t *testing.T) {
				e := newOpsEnv()
				_, err := e.svc.AdjustCredits(ctx, c.actor, c.target, add)
				if !c.wantForbid {
					if err != nil {
						t.Fatal(err)
					}
					return
				}
				ec := bizErrOf(t, err, errcode.ErrForbidden)
				if !strings.Contains(ec.Msg, c.wantMsgContain) {
					t.Fatalf("Msg 应含 %q：%s", c.wantMsgContain, ec.Msg)
				}
				if len(e.credits.adjustLedger()) != 0 || len(e.audit.logs) != 0 {
					t.Fatal("无权限不应写流水 / 审计")
				}
			})
		}
	})

	t.Run("存储错误原样返回；审计失败不影响调整结果", func(t *testing.T) {
		e := newOpsEnv()
		e.credits.errs["AddCredit"] = errBoom
		if _, err := e.svc.AdjustCredits(ctx, opsAdmin, opsTom, &model.AdjustCreditsReq{Mode: "add", Amount: intp(1), Note: "x"}); !errors.Is(err, errBoom) {
			t.Fatalf("%v", err)
		}
		if len(e.audit.logs) != 0 || e.credits.accounts[opsTom].Balance != 100 {
			t.Fatal("出错应回滚且不写审计")
		}
		e = newOpsEnv()
		e.audit.err = errBoom
		if _, err := e.svc.AdjustCredits(ctx, opsAdmin, opsTom, &model.AdjustCreditsReq{Mode: "add", Amount: intp(1), Note: "x"}); err != nil {
			t.Fatalf("审计失败不应让已提交的调整报错：%v", err)
		}
	})
}

// ---- 并发上限 ----

func TestAdminUser_SetMaxActiveTasks(t *testing.T) {
	ctx := context.Background()
	t.Run("成功：写入、清缓存、审计记前后值；null 改回默认", func(t *testing.T) {
		e := newOpsEnv()
		if err := e.svc.SetMaxActiveTasks(ctx, opsAdmin, opsTom, intp(5)); err != nil {
			t.Fatal(err)
		}
		u, _ := e.repo.GetByID(ctx, opsTom)
		if u.MaxActiveTasks == nil || *u.MaxActiveTasks != 5 {
			t.Fatalf("%+v", u.MaxActiveTasks)
		}
		if len(e.inval.ids) != 1 || e.inval.ids[0] != opsTom {
			t.Fatalf("应清用户缓存：%v", e.inval.ids)
		}
		if err := e.svc.SetMaxActiveTasks(ctx, opsAdmin, opsTom, nil); err != nil {
			t.Fatal(err)
		}
		u, _ = e.repo.GetByID(ctx, opsTom)
		if u.MaxActiveTasks != nil {
			t.Fatal("null 应清除覆盖")
		}
		if len(e.audit.logs) != 2 || e.audit.logs[0].Action != model.AdminAuditUserLimit {
			t.Fatalf("%v", e.audit.actions())
		}
		var d0, d1 map[string]any
		_ = json.Unmarshal(e.audit.logs[0].DetailJSON, &d0)
		_ = json.Unmarshal(e.audit.logs[1].DetailJSON, &d1)
		if d0["from"] != nil || int(d0["to"].(float64)) != 5 || int(d1["from"].(float64)) != 5 || d1["to"] != nil {
			t.Fatalf("detail 应记 from / to：%v %v", d0, d1)
		}
	})
	t.Run("业务错误", func(t *testing.T) {
		e := newOpsEnv()
		wantBizErr(t, e.svc.SetMaxActiveTasks(ctx, opsAdmin, opsTom, intp(0)), errcode.ErrInvalidParams)
		wantBizErr(t, e.svc.SetMaxActiveTasks(ctx, opsAdmin, opsTom, intp(65)), errcode.ErrInvalidParams)
		wantBizErr(t, e.svc.SetMaxActiveTasks(ctx, opsAdmin, 99, intp(3)), errcode.ErrUserNotFound)
		if len(e.inval.ids) != 0 || len(e.audit.logs) != 0 {
			t.Fatal("失败不应清缓存或写审计")
		}
	})
	t.Run("权限：admin 不能改管理员的上限，但可以改自己的", func(t *testing.T) {
		e := newOpsEnv()
		wantBizErr(t, e.svc.SetMaxActiveTasks(ctx, opsAdmin, opsAdm2, intp(3)), errcode.ErrForbidden)
		wantBizErr(t, e.svc.SetMaxActiveTasks(ctx, opsAdmin, opsRoot, intp(3)), errcode.ErrForbidden)
		if err := e.svc.SetMaxActiveTasks(ctx, opsAdmin, opsAdmin, intp(3)); err != nil {
			t.Fatalf("自己改并发上限应允许：%v", err)
		}
		if err := e.svc.SetMaxActiveTasks(ctx, opsRoot, opsAdmin, intp(3)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("库错误透传且不清缓存", func(t *testing.T) {
		e := newOpsEnv()
		e.repo.errs["Update"] = errBoom
		if err := e.svc.SetMaxActiveTasks(ctx, opsAdmin, opsTom, intp(3)); !errors.Is(err, errBoom) {
			t.Fatal(err)
		}
		if len(e.inval.ids) != 0 {
			t.Fatal("写失败不应清缓存")
		}
	})
}

// ---- 封禁 / 启用 ----

func TestAdminUser_SetStatus(t *testing.T) {
	ctx := context.Background()

	t.Run("封禁：更新状态、清缓存、断开 WebSocket、审计 user.ban", func(t *testing.T) {
		e := newOpsEnv()
		res, err := e.svc.SetStatus(ctx, opsAdmin, opsTom, model.UserStatusDisabled, false)
		if err != nil {
			t.Fatal(err)
		}
		u, _ := e.repo.GetByID(ctx, opsTom)
		if u.Status != model.UserStatusDisabled || res.Status != model.UserStatusDisabled {
			t.Fatalf("%s %+v", u.Status, res)
		}
		if len(e.inval.ids) != 1 || len(e.conns.ids) != 1 || e.conns.ids[0] != opsTom {
			t.Fatalf("应清缓存并断开 WS：%v %v", e.inval.ids, e.conns.ids)
		}
		if len(e.canceler.calls) != 0 {
			t.Fatal("没要求取消任务就不能取消")
		}
		if len(e.audit.logs) != 1 || e.audit.logs[0].Action != model.AdminAuditUserBan {
			t.Fatalf("%v", e.audit.actions())
		}
		var d map[string]any
		_ = json.Unmarshal(e.audit.logs[0].DetailJSON, &d)
		if d["from"] != "active" || d["to"] != "disabled" || d["cancel_active"] != false || d["canceled"].(float64) != 0 {
			t.Fatalf("%v", d)
		}
		if res.FailedTaskIDs == nil {
			t.Fatal("failed_task_ids 不能是 nil（JSON 要输出 []）")
		}
	})

	t.Run("封禁并取消任务：返回取消数；部分失败体现在返回里且不回滚封禁", func(t *testing.T) {
		e := newOpsEnv()
		e.canceler.res = &CancelActiveResult{Canceled: 2, Failed: []uint64{9}}
		res, err := e.svc.SetStatus(ctx, opsAdmin, opsTom, model.UserStatusDisabled, true)
		if err != nil {
			t.Fatal(err)
		}
		if res.Canceled != 2 || res.CancelFailed != 1 || len(res.FailedTaskIDs) != 1 || res.FailedTaskIDs[0] != 9 {
			t.Fatalf("%+v", res)
		}
		u, _ := e.repo.GetByID(ctx, opsTom)
		if u.Status != model.UserStatusDisabled {
			t.Fatal("取消部分失败不能回滚封禁")
		}
		var d map[string]any
		_ = json.Unmarshal(e.audit.logs[0].DetailJSON, &d)
		if d["cancel_active"] != true || d["canceled"].(float64) != 2 || d["cancel_failed"].(float64) != 1 {
			t.Fatalf("审计应记 cancel_active 与取消数量：%v", d)
		}
	})

	t.Run("取消流程整体出错：封禁已生效，错误体现在 cancel_error", func(t *testing.T) {
		e := newOpsEnv()
		e.canceler.err = errBoom
		res, err := e.svc.SetStatus(ctx, opsAdmin, opsTom, model.UserStatusDisabled, true)
		if err != nil || res.CancelError == "" {
			t.Fatalf("%v %+v", err, res)
		}
		if u, _ := e.repo.GetByID(ctx, opsTom); u.Status != model.UserStatusDisabled {
			t.Fatal("封禁应已生效")
		}
	})

	t.Run("启用：cancel_active 被忽略，不断开、不取消，审计 user.unban", func(t *testing.T) {
		e := newOpsEnv()
		e.repo.users[opsTom-1].Status = model.UserStatusDisabled
		res, err := e.svc.SetStatus(ctx, opsAdmin, opsTom, model.UserStatusActive, true)
		if err != nil || res.Status != model.UserStatusActive {
			t.Fatalf("%v %+v", err, res)
		}
		if len(e.canceler.calls) != 0 || len(e.conns.ids) != 0 {
			t.Fatal("启用时不能取消任务或断开连接")
		}
		if len(e.inval.ids) != 1 || e.audit.logs[0].Action != model.AdminAuditUserUnban {
			t.Fatalf("%v %v", e.inval.ids, e.audit.actions())
		}
	})

	t.Run("权限与业务错误", func(t *testing.T) {
		for _, c := range []struct {
			name          string
			actor, target uint64
			status        string
			want          *errcode.Error
			msg           string
		}{
			{"任何人不能封禁自己（admin）", opsAdmin, opsAdmin, "disabled", errcode.ErrForbidden, "不能封禁自己"},
			{"任何人不能封禁自己（super_admin）", opsRoot, opsRoot, "disabled", errcode.ErrForbidden, "不能封禁自己"},
			{"admin 不能封禁管理员", opsAdmin, opsAdm2, "disabled", errcode.ErrForbidden, "admin 不能操作管理员账号"},
			{"admin 不能封禁 super_admin", opsAdmin, opsRoot, "disabled", errcode.ErrForbidden, "admin 不能操作管理员账号"},
			{"admin 不能启用管理员", opsAdmin, opsAdm2, "active", errcode.ErrForbidden, "admin 不能操作管理员账号"},
			{"目标不存在", opsAdmin, 99, "disabled", errcode.ErrUserNotFound, ""},
			{"非法状态", opsAdmin, opsTom, "frozen", errcode.ErrInvalidParams, ""},
		} {
			t.Run(c.name, func(t *testing.T) {
				e := newOpsEnv()
				_, err := e.svc.SetStatus(ctx, c.actor, c.target, c.status, false)
				ec := bizErrOf(t, err, c.want)
				if c.msg != "" && !strings.Contains(ec.Msg, c.msg) {
					t.Fatalf("Msg 应含 %q：%s", c.msg, ec.Msg)
				}
				if len(e.inval.ids)+len(e.conns.ids)+len(e.audit.logs) != 0 {
					t.Fatal("失败不应有副作用")
				}
			})
		}
		// super_admin 可以封禁 admin
		e := newOpsEnv()
		if _, err := e.svc.SetStatus(ctx, opsRoot, opsAdmin, "disabled", false); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("更新失败：透传错误，不清缓存不断连", func(t *testing.T) {
		e := newOpsEnv()
		e.repo.errs["Update"] = errBoom
		if _, err := e.svc.SetStatus(ctx, opsAdmin, opsTom, "disabled", true); !errors.Is(err, errBoom) {
			t.Fatal(err)
		}
		if len(e.inval.ids)+len(e.conns.ids)+len(e.canceler.calls) != 0 {
			t.Fatal("写失败不应有后续动作")
		}
	})
}

// ---- 批量 ----

func TestAdminUser_BatchCredits(t *testing.T) {
	ctx := context.Background()
	e := newOpsEnv()
	e.credits.put(opsAdm2, 0, 0)
	res, err := e.svc.BatchAddCredits(ctx, opsAdmin, []uint64{opsTom, opsAdm2, 99, opsTom, opsAdmin}, 10, "活动")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 4 { // 去重后 tom, adm2, 99, admin
		t.Fatalf("应去重为 4 项：%+v", res.Results)
	}
	want := []struct {
		id      uint64
		ok      bool
		errPart string
	}{{opsTom, true, ""}, {opsAdm2, false, "admin 不能操作管理员账号"}, {99, false, "用户不存在"}, {opsAdmin, false, "不能给自己调整积分"}}
	for i, w := range want {
		g := res.Results[i]
		if g.ID != w.id || g.OK != w.ok || !strings.Contains(g.Error, w.errPart) || (w.ok && g.Error != "") {
			t.Fatalf("第 %d 项不对：%+v", i, g)
		}
	}
	if e.credits.accounts[opsTom].Balance != 110 || e.credits.accounts[opsAdm2].Balance != 0 {
		t.Fatal("只有成功项入账")
	}
	if len(e.audit.logs) != 1 {
		t.Fatalf("成功项各写一条审计：%d", len(e.audit.logs))
	}

	t.Run("单项内部错误不泄露细节、不影响其他项", func(t *testing.T) {
		e := newOpsEnv()
		e.credits.errs["LockCredit"] = errBoom
		res, err := e.svc.BatchAddCredits(ctx, opsRoot, []uint64{opsTom}, 1, "x")
		if err != nil || res.Results[0].OK || strings.Contains(res.Results[0].Error, "boom") || res.Results[0].Error == "" {
			t.Fatalf("%v %+v", err, res)
		}
	})
	t.Run("整体参数错误：空列表、超过 200、amount<=0、备注不合法", func(t *testing.T) {
		e := newOpsEnv()
		many := make([]uint64, 201)
		for i := range many {
			many[i] = uint64(i + 1)
		}
		for _, c := range []struct {
			ids    []uint64
			amount int
			note   string
		}{{nil, 1, "x"}, {many, 1, "x"}, {[]uint64{opsTom}, 0, "x"}, {[]uint64{opsTom}, 1, " "}} {
			_, err := e.svc.BatchAddCredits(ctx, opsAdmin, c.ids, c.amount, c.note)
			if err == nil {
				t.Fatalf("应报错：%+v", c)
			}
		}
	})
	t.Run("重复 id 在 200 的上限之前先去重", func(t *testing.T) {
		e := newOpsEnv()
		ids := make([]uint64, 250)
		for i := range ids {
			ids[i] = opsTom
		}
		// 250 个重复 id：超过 200 的是“请求体条数”，由 handler 的 max=200 拦；service 以去重后数量为准
		res, err := e.svc.BatchAddCredits(ctx, opsAdmin, ids, 1, "x")
		if err != nil || len(res.Results) != 1 {
			t.Fatalf("%v %+v", err, res)
		}
	})
}

func TestAdminUser_BatchStatus(t *testing.T) {
	ctx := context.Background()
	e := newOpsEnv()
	res, err := e.svc.BatchSetStatus(ctx, opsAdmin, []uint64{opsTom, opsAdmin, opsAdm2, 99}, model.UserStatusDisabled)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 4 || !res.Results[0].OK || res.Results[1].OK || res.Results[2].OK || res.Results[3].OK {
		t.Fatalf("%+v", res.Results)
	}
	if !strings.Contains(res.Results[1].Error, "不能封禁自己") || !strings.Contains(res.Results[2].Error, "admin 不能操作管理员账号") ||
		!strings.Contains(res.Results[3].Error, "用户不存在") {
		t.Fatalf("原因文案不对：%+v", res.Results)
	}
	if len(e.canceler.calls) != 0 {
		t.Fatal("批量封禁不支持取消任务")
	}
	if len(e.conns.ids) != 1 || e.conns.ids[0] != opsTom {
		t.Fatalf("成功封禁的用户要断 WS：%v", e.conns.ids)
	}
	if _, err := e.svc.BatchSetStatus(ctx, opsAdmin, nil, "active"); err == nil {
		t.Fatal("空列表应报错")
	}
	if _, err := e.svc.BatchSetStatus(ctx, opsAdmin, []uint64{opsTom}, "frozen"); err == nil {
		t.Fatal("非法状态应报错")
	}
}

// ---- 记录列表 ----

func TestAdminUser_Records(t *testing.T) {
	ctx := context.Background()

	t.Run("生成记录：筛选映射、limit 修正、游标分页（多取一条判断是否有下一页）", func(t *testing.T) {
		e := newOpsEnv()
		for id := 30; id >= 1; id-- {
			e.repo.tasks = append(e.repo.tasks, model.AdminTaskItem{ID: uint64(id)})
		}
		page, err := e.svc.ListTasks(ctx, opsTom, &model.ListUserTasksReq{Status: "failed"})
		if err != nil {
			t.Fatal(err)
		}
		f := e.repo.lastTF
		if f.UserID != opsTom || f.Limit != 21 || f.BeforeID != 0 || strings.Join(f.Statuses, ",") != "failed,expired,canceled" {
			t.Fatalf("默认 limit 20（多取 1 条）、failed 含 expired 与 canceled：%+v", f)
		}
		if len(page.Items) != 20 || page.NextCursor == "" || page.Items[0].ID != 30 {
			t.Fatalf("%d %q", len(page.Items), page.NextCursor)
		}
		page2, err := e.svc.ListTasks(ctx, opsTom, &model.ListUserTasksReq{Cursor: page.NextCursor, Limit: 100})
		if err != nil || len(page2.Items) != 10 || page2.NextCursor != "" || page2.Items[0].ID != 10 {
			t.Fatalf("第二页应接着 id 10，且没有下一页：%v %+v", err, page2)
		}
		if e.repo.lastTF.BeforeID != 11 && e.repo.lastTF.BeforeID != 10 {
			t.Fatalf("游标应解成上一页最后一条 id：%d", e.repo.lastTF.BeforeID)
		}
		// 其余筛选值
		for status, want := range map[string]string{"success": "succeeded", "running": strings.Join(model.ActiveTaskStatuses, ","), "all": "", "": ""} {
			if _, err := e.svc.ListTasks(ctx, opsTom, &model.ListUserTasksReq{Status: status}); err != nil || strings.Join(e.repo.lastTF.Statuses, ",") != want {
				t.Fatalf("%q -> %v err=%v", status, e.repo.lastTF.Statuses, err)
			}
		}
		if _, err := e.svc.ListTasks(ctx, opsTom, &model.ListUserTasksReq{Limit: 1000}); err != nil || e.repo.lastTF.Limit != 101 {
			t.Fatalf("limit 超过 100 按 100：%d", e.repo.lastTF.Limit)
		}
	})

	t.Run("空结果返回空切片与空游标，不是 nil", func(t *testing.T) {
		e := newOpsEnv()
		page, err := e.svc.ListTasks(ctx, opsTom, &model.ListUserTasksReq{})
		if err != nil || page.Items == nil || page.NextCursor != "" {
			t.Fatalf("%v %+v", err, page)
		}
	})

	t.Run("积分流水与登录记录：类型 / 结果映射", func(t *testing.T) {
		e := newOpsEnv()
		for id := 3; id >= 1; id-- {
			e.repo.ledgers = append(e.repo.ledgers, model.AdminLedgerItem{ID: uint64(id)})
			e.repo.logins = append(e.repo.logins, model.AdminLoginItem{ID: uint64(id)})
		}
		for typ, want := range map[string]string{"admin": "admin_adjust", "task": "freeze,settle,refund", "all": "", "": ""} {
			if _, err := e.svc.ListLedger(ctx, opsTom, &model.ListUserLedgerReq{Type: typ}); err != nil || strings.Join(e.repo.lastLF.Types, ",") != want {
				t.Fatalf("%q -> %v err=%v", typ, e.repo.lastLF.Types, err)
			}
		}
		page, err := e.svc.ListLedger(ctx, opsTom, &model.ListUserLedgerReq{Limit: 2})
		if err != nil || len(page.Items) != 2 || page.NextCursor == "" {
			t.Fatalf("%v %+v", err, page)
		}
		for res, want := range map[string]string{"fail": "badpw,blocked", "all": "", "": ""} {
			if _, err := e.svc.ListLogins(ctx, opsTom, &model.ListUserLoginsReq{Result: res}); err != nil || strings.Join(e.repo.lastGF.Results, ",") != want {
				t.Fatalf("%q -> %v err=%v", res, e.repo.lastGF.Results, err)
			}
		}
		lp, err := e.svc.ListLogins(ctx, opsTom, &model.ListUserLoginsReq{Limit: 3})
		if err != nil || len(lp.Items) != 3 || lp.NextCursor != "" {
			t.Fatalf("恰好取完时没有下一页：%v %+v", err, lp)
		}
	})

	t.Run("错误：用户不存在、游标非法、库错误", func(t *testing.T) {
		e := newOpsEnv()
		_, err := e.svc.ListTasks(ctx, 99, &model.ListUserTasksReq{})
		wantBizErr(t, err, errcode.ErrUserNotFound)
		_, err = e.svc.ListLedger(ctx, 99, &model.ListUserLedgerReq{})
		wantBizErr(t, err, errcode.ErrUserNotFound)
		_, err = e.svc.ListLogins(ctx, 99, &model.ListUserLoginsReq{})
		wantBizErr(t, err, errcode.ErrUserNotFound)
		for _, bad := range []string{"!!!", "YWJj", "MA"} { // 非 base64、非数字、解出 0
			_, err = e.svc.ListTasks(ctx, opsTom, &model.ListUserTasksReq{Cursor: bad})
			wantBizErr(t, err, errcode.ErrInvalidParams)
		}
		e.repo.errs["ListLogins"] = errBoom
		if _, err := e.svc.ListLogins(ctx, opsTom, &model.ListUserLoginsReq{}); !errors.Is(err, errBoom) {
			t.Fatal(err)
		}
	})
}
