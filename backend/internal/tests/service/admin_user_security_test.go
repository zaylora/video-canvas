package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
)

// 本文件测试后台用户的“高危操作”：调整角色（仅 super_admin）、重置密码（仅 super_admin），以及积分流水金额的带符号展示。
// 用户沿用 newOpsEnv：1 root(super_admin) 2 ops(admin) 3 tom(user) 4 ops2(admin) 5 root2(super_admin)。

func TestAdminUser_SetRole(t *testing.T) {
	ctx := context.Background()

	t.Run("成功：角色变更、写审计 {from,to}、清缓存", func(t *testing.T) {
		cases := []struct {
			name     string
			target   uint64
			to       string
			wantFrom string
		}{
			{"user 升 admin", opsTom, model.RoleAdmin, model.RoleUser},
			{"user 升 super_admin", opsTom, model.RoleSuperAdmin, model.RoleUser},
			{"admin 降 user", opsAdmin, model.RoleUser, model.RoleAdmin},
			{"admin 升 super_admin", opsAdm2, model.RoleSuperAdmin, model.RoleAdmin},
			{"super_admin 降 admin（还剩别的超管）", opsRoot2, model.RoleAdmin, model.RoleSuperAdmin},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				e := newOpsEnv()
				if err := e.svc.SetRole(ctx, opsRoot, c.target, c.to); err != nil {
					t.Fatal(err)
				}
				got, _ := e.repo.GetByID(ctx, c.target)
				if got.Role != c.to {
					t.Fatalf("角色没改：%s", got.Role)
				}
				if e.repo.roleLocked != 1 {
					t.Fatalf("改角色必须先取角色咨询锁：%d", e.repo.roleLocked)
				}
				if len(e.inval.ids) != 1 || e.inval.ids[0] != c.target {
					t.Fatalf("必须清目标用户缓存：%v", e.inval.ids)
				}
				if len(e.audit.logs) != 1 {
					t.Fatalf("应写 1 条审计：%+v", e.audit.logs)
				}
				l := e.audit.logs[0]
				if l.Action != model.AdminAuditUserRole || l.ActorID != opsRoot || l.TargetID != c.target || l.TargetType != model.AdminAuditTargetUser {
					t.Fatalf("审计不对：%+v", l)
				}
				var d map[string]any
				_ = json.Unmarshal(l.DetailJSON, &d)
				if d["from"] != c.wantFrom || d["to"] != c.to {
					t.Fatalf("detail 应为 {from,to}：%v", d)
				}
			})
		}
	})

	t.Run("角色没变化：直接成功，不写库、不写审计、不清缓存", func(t *testing.T) {
		e := newOpsEnv()
		if err := e.svc.SetRole(ctx, opsRoot, opsTom, model.RoleUser); err != nil {
			t.Fatal(err)
		}
		if len(e.repo.updates) != 0 || len(e.audit.logs) != 0 || len(e.inval.ids) != 0 {
			t.Fatalf("不该有副作用：%v %v %v", e.repo.updates, e.audit.logs, e.inval.ids)
		}
	})

	t.Run("业务错误：不改角色、不写审计、不清缓存", func(t *testing.T) {
		cases := []struct {
			name    string
			actor   uint64
			target  uint64
			role    string
			prep    func(e *opsEnv)
			want    *errcode.Error
			wantMsg string
		}{
			{"admin 调用：没有权限", opsAdmin, opsTom, model.RoleAdmin, nil, errcode.ErrForbidden, ""},
			{"普通用户调用：没有权限", opsTom, opsAdmin, model.RoleUser, nil, errcode.ErrForbidden, ""},
			{"操作人账号不存在", 99, opsTom, model.RoleAdmin, nil, errcode.ErrForbidden, "操作人账号不存在"},
			{"不能改自己的角色", opsRoot, opsRoot, model.RoleAdmin, nil, errcode.ErrForbidden, "不能修改自己的角色"},
			{"角色取值非法", opsRoot, opsTom, "root", nil, errcode.ErrInvalidParams, ""},
			{"角色为空", opsRoot, opsTom, "", nil, errcode.ErrInvalidParams, ""},
			{"目标不存在", opsRoot, 99, model.RoleAdmin, nil, errcode.ErrUserNotFound, ""},
			{"不能降级最后一个超级管理员", opsRoot, opsRoot2, model.RoleAdmin, func(e *opsEnv) {
				// 竞态：操作人刚加载完，另一个请求就把他自己降成了 admin，库里只剩 root2 一个超管
				e.repo.onTx = func() { e.repo.users[0].Role = model.RoleAdmin }
			}, errcode.ErrForbidden, "不能降级最后一个超级管理员"},
			{"取角色锁失败", opsRoot, opsTom, model.RoleAdmin, func(e *opsEnv) { e.repo.errs["LockRoleChange"] = errBoom }, nil, ""},
			{"统计超管数失败", opsRoot, opsRoot2, model.RoleAdmin, func(e *opsEnv) { e.repo.errs["CountByRole"] = errBoom }, nil, ""},
			{"写库失败", opsRoot, opsTom, model.RoleAdmin, func(e *opsEnv) { e.repo.errs["Update"] = errBoom }, nil, ""},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				e := newOpsEnv()
				if c.prep != nil {
					c.prep(e)
				}
				before, _ := e.repo.GetByID(ctx, c.target)
				err := e.svc.SetRole(ctx, c.actor, c.target, c.role)
				if err == nil {
					t.Fatal("应返回错误")
				}
				if c.want == nil {
					if _, ok := err.(*errcode.Error); ok { //nolint:errorlint // 只判断类型：期望的是未包装的内部错误
						t.Fatalf("应是内部错误：%v", err)
					}
				} else {
					ec := bizErrOf(t, err, c.want)
					if c.wantMsg != "" && ec.Msg != c.wantMsg {
						t.Fatalf("Msg = %q", ec.Msg)
					}
				}
				if before != nil {
					if after, _ := e.repo.GetByID(ctx, c.target); after.Role != before.Role && c.name != "不能降级最后一个超级管理员" {
						t.Fatalf("角色被改了：%s -> %s", before.Role, after.Role)
					}
				}
				if len(e.audit.logs) != 0 || len(e.inval.ids) != 0 {
					t.Fatalf("失败不应写审计 / 清缓存：%v %v", e.audit.logs, e.inval.ids)
				}
			})
		}
	})

	t.Run("降级最后一个超管被拒后目标仍是 super_admin", func(t *testing.T) {
		e := newOpsEnv()
		e.repo.onTx = func() { e.repo.users[0].Role = model.RoleAdmin }
		_ = e.svc.SetRole(ctx, opsRoot, opsRoot2, model.RoleAdmin)
		if got, _ := e.repo.GetByID(ctx, opsRoot2); got.Role != model.RoleSuperAdmin {
			t.Fatalf("最后一个超管被降级了：%s", got.Role)
		}
	})
}

func TestAdminUser_ResetPassword(t *testing.T) {
	ctx := context.Background()

	t.Run("缺省：生成强随机临时密码，bcrypt 存储，token_version +1，清缓存，审计不含明文", func(t *testing.T) {
		e := newOpsEnv()
		before, _ := e.repo.GetByID(ctx, opsTom)
		view, err := e.svc.ResetPassword(ctx, opsRoot, opsTom, "")
		if err != nil {
			t.Fatal(err)
		}
		pwd := view.TempPassword
		if len(pwd) != 16 {
			t.Fatalf("不传新密码时仍生成 16 位临时密码：%q", pwd)
		}
		var hasLetter, hasDigit bool
		for _, r := range pwd {
			switch {
			case r >= '0' && r <= '9':
				hasDigit = true
			case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
				hasLetter = true
			default:
				t.Fatalf("只应含字母数字：%q", pwd)
			}
		}
		if !hasLetter || !hasDigit {
			t.Fatalf("必须同时含字母与数字：%q", pwd)
		}
		after, _ := e.repo.GetByID(ctx, opsTom)
		if after.TokenVersion != before.TokenVersion+1 {
			t.Fatalf("token_version 应 +1：%d -> %d", before.TokenVersion, after.TokenVersion)
		}
		if after.Password == pwd || bcrypt.CompareHashAndPassword([]byte(after.Password), []byte(pwd)) != nil {
			t.Fatalf("库里应存 bcrypt 哈希：%q", after.Password)
		}
		if len(e.inval.ids) != 1 || e.inval.ids[0] != opsTom {
			t.Fatalf("必须清缓存让旧 token 立即失效：%v", e.inval.ids)
		}
		if len(e.audit.logs) != 1 {
			t.Fatalf("应写 1 条审计：%+v", e.audit.logs)
		}
		l := e.audit.logs[0]
		if l.Action != model.AdminAuditResetPassword || l.ActorID != opsRoot || l.TargetID != opsTom {
			t.Fatalf("审计不对：%+v", l)
		}
		if strings.Contains(string(l.DetailJSON), pwd) {
			t.Fatalf("审计里不能有密码明文：%s", l.DetailJSON)
		}
		var d map[string]any
		_ = json.Unmarshal(l.DetailJSON, &d)
		if len(d) != 1 || d["generated"] != true {
			t.Fatalf("detail 只应是 {generated:true}：%v", d)
		}
	})

	t.Run("每次生成的临时密码都不同", func(t *testing.T) {
		e := newOpsEnv()
		seen := map[string]bool{}
		for range 20 {
			v, err := e.svc.ResetPassword(ctx, opsRoot, opsTom, "")
			if err != nil || seen[v.TempPassword] {
				t.Fatalf("重复或失败：%v %q", err, v.TempPassword)
			}
			seen[v.TempPassword] = true
		}
	})

	t.Run("指定新密码：按指定值存储并原样返回，审计 generated=false", func(t *testing.T) {
		e := newOpsEnv()
		view, err := e.svc.ResetPassword(ctx, opsRoot, opsTom, "my-new-pass")
		if err != nil || view.TempPassword != "my-new-pass" {
			t.Fatalf("%v %+v", err, view)
		}
		after, _ := e.repo.GetByID(ctx, opsTom)
		if bcrypt.CompareHashAndPassword([]byte(after.Password), []byte("my-new-pass")) != nil || after.TokenVersion != 1 {
			t.Fatalf("%+v", after)
		}
		var d map[string]any
		_ = json.Unmarshal(e.audit.logs[0].DetailJSON, &d)
		if d["generated"] != false || strings.Contains(string(e.audit.logs[0].DetailJSON), "my-new-pass") {
			t.Fatalf("%s", e.audit.logs[0].DetailJSON)
		}
	})

	t.Run("super_admin 可以重置自己的密码", func(t *testing.T) {
		e := newOpsEnv()
		if _, err := e.svc.ResetPassword(ctx, opsRoot, opsRoot, ""); err != nil {
			t.Fatal(err)
		}
		if got, _ := e.repo.GetByID(ctx, opsRoot); got.TokenVersion != 1 {
			t.Fatalf("%d", got.TokenVersion)
		}
	})

	t.Run("业务错误：不改密码、不写审计、不清缓存", func(t *testing.T) {
		cases := []struct {
			name   string
			actor  uint64
			target uint64
			pwd    string
			prep   func(e *opsEnv)
			want   *errcode.Error // nil 表示内部错误
		}{
			{"admin 调用：没有权限", opsAdmin, opsTom, "", nil, errcode.ErrForbidden},
			{"普通用户调用：没有权限", opsTom, opsAdmin, "", nil, errcode.ErrForbidden},
			{"admin 想重置超管：没有权限", opsAdmin, opsRoot, "", nil, errcode.ErrForbidden},
			{"目标不存在", opsRoot, 99, "", nil, errcode.ErrUserNotFound},
			{"密码太短", opsRoot, opsTom, "12345", nil, errcode.ErrInvalidParams},
			{"手填 7 位：统一规则要求至少 8 位", opsRoot, opsTom, "Ab3$xyz", nil, errcode.ErrInvalidParams},
			{"手填弱密码：55003", opsRoot, opsTom, "12345678", nil, errcode.ErrPasswordWeak},
			{"手填等于目标用户名：55003", opsRoot, opsTom, "tom-2026", func(e *opsEnv) { e.repo.users[2].Username = "tom-2026" }, errcode.ErrPasswordWeak},
			{"密码太长（>128）", opsRoot, opsTom, strings.Repeat("a", 129), nil, errcode.ErrInvalidParams},
			{"密码超过 bcrypt 的 72 字节上限", opsRoot, opsTom, strings.Repeat("a", 73), nil, errcode.ErrInvalidParams},
			{"写库失败", opsRoot, opsTom, "", func(e *opsEnv) { e.repo.errs["ResetPassword"] = errBoom }, nil},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				e := newOpsEnv()
				if c.prep != nil {
					c.prep(e)
				}
				view, err := e.svc.ResetPassword(ctx, c.actor, c.target, c.pwd)
				if err == nil || view != nil {
					t.Fatalf("应返回错误且无结果：%v %v", view, err)
				}
				if c.want != nil {
					wantBizErr(t, err, c.want)
				}
				if tom, _ := e.repo.GetByID(ctx, opsTom); tom.TokenVersion != 0 || tom.Password != "" {
					t.Fatalf("密码被改了：%+v", tom)
				}
				if len(e.audit.logs) != 0 || len(e.inval.ids) != 0 {
					t.Fatalf("失败不应写审计 / 清缓存：%v %v", e.audit.logs, e.inval.ids)
				}
			})
		}
	})
}

// 积分流水的金额：库里 freeze / settle / refund / initial 存正数，只有 admin_adjust 带符号；
// 列表接口统一映射成“对可用积分的影响”（带符号），前端才能正确着色。
func TestAdminUser_ListLedger_SignedAmount(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		typ    string
		stored int
		want   int
	}{
		{model.LedgerFreeze, 8, -8},
		{model.LedgerSettle, 6, -6},
		{model.LedgerRefund, 2, 2},
		{model.LedgerInitial, 50, 50},
		{model.LedgerAdminAdjust, 5, 5},
		{model.LedgerAdminAdjust, -7, -7},
	}
	e := newOpsEnv()
	for i, c := range cases {
		e.repo.ledgers = append(e.repo.ledgers, model.AdminLedgerItem{ID: uint64(len(cases) - i), Type: c.typ, Amount: c.stored})
	}
	page, err := e.svc.ListLedger(ctx, opsTom, &model.ListUserLedgerReq{})
	if err != nil || len(page.Items) != len(cases) {
		t.Fatalf("%v %+v", err, page)
	}
	for i, c := range cases {
		if got := page.Items[i]; got.Type != c.typ || got.Amount != c.want {
			t.Errorf("%s 存 %d 应展示 %d，实际 %d", c.typ, c.stored, c.want, got.Amount)
		}
	}
	// 只改响应，不能改库里（也就是 fake 持有的）原值：对账仍用原值
	for i, c := range cases {
		if e.repo.ledgers[i].Amount != c.stored {
			t.Errorf("不应改动源数据：%s %d -> %d", c.typ, c.stored, e.repo.ledgers[i].Amount)
		}
	}
}
