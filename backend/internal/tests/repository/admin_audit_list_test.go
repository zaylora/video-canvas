package repository_test

import (
	"context"
	"testing"
	"time"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

func auditKeys(rows []model.AuditRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Source+":"+itoa(r.ID))
	}
	return out
}

func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

func TestAdminAuditRepo_ListMerged(t *testing.T) {
	ctx := context.Background()
	db := userDB(t)
	repo := NewAdminAuditRepository(db)
	root := mkUser(t, db, "root", "r@x.com", model.RoleSuperAdmin, model.UserStatusActive)
	ops := mkUser(t, db, "ops", "o@x.com", model.RoleAdmin, model.UserStatusActive)

	now := time.Now().Truncate(time.Millisecond)
	at := func(minutes int) time.Time { return now.Add(-time.Duration(minutes) * time.Minute) }
	mkAdmin := func(actor uint64, action, ttype string, tid uint64, detail string, created time.Time) *model.AdminAuditLog {
		l := &model.AdminAuditLog{ActorID: actor, Action: action, TargetType: ttype, TargetID: tid, DetailJSON: model.JSONText(detail), CreatedAt: created}
		if err := db.Create(l).Error; err != nil {
			t.Fatal(err)
		}
		return l
	}
	mkAI := func(actor uint64, action, ttype, key, detail string, created time.Time) *model.AIAuditLog {
		l := &model.AIAuditLog{ActorID: actor, Action: action, TargetType: ttype, TargetKey: key, DetailJSON: model.JSONText(detail), CreatedAt: created}
		if err := db.Create(l).Error; err != nil {
			t.Fatal(err)
		}
		return l
	}
	a1 := mkAdmin(root.ID, model.AdminAuditUserBan, model.AdminAuditTargetUser, 7, `{"reason":"x"}`, at(50)) // admin:1
	c1 := mkAI(root.ID, model.AuditChannelEnable, model.AuditTargetChannel, "kling", `{"enabled":false}`, at(40))
	a2 := mkAdmin(ops.ID, model.AdminAuditCreditAdjust, model.AdminAuditTargetUser, 8, ``, at(30))
	c2 := mkAI(0, model.AuditChannelAutoOff, model.AuditTargetChannel, "newapi", `{"failures":5}`, at(20)) // 系统动作
	// 与 c2 同一时刻、不同来源：游标要靠 (created_at, source, id) 稳定排序
	a3 := mkAdmin(root.ID, model.AdminAuditSystemUpgrade, model.AdminAuditTargetSystem, 0, `{"to":"v1"}`, at(20))
	old := mkAdmin(root.ID, model.AdminAuditUserRole, model.AdminAuditTargetUser, 9, ``, at(60*24*40)) // 40 天前

	base := AuditFilter{From: at(60 * 24 * 30), To: now.Add(time.Minute), Limit: 50}

	t.Run("合并两张表，按时间倒序；同一时刻按 source、id 倒序；字段映射", func(t *testing.T) {
		rows, err := repo.ListMerged(ctx, base)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"ai:" + itoa(c2.ID), "admin:" + itoa(a3.ID), "admin:" + itoa(a2.ID), "ai:" + itoa(c1.ID), "admin:" + itoa(a1.ID)}
		// 同一时刻 c2(ai) 与 a3(admin)：source 倒序，"ai" 在 "admin" 之后字典序更大，所以 ai 在前
		got := auditKeys(rows)
		if len(got) != len(want) {
			t.Fatalf("应有 %d 条（40 天前的不在范围内）：%v", len(want), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("顺序不对：got %v want %v", got, want)
			}
		}
		if r := rows[0]; r.Source != model.AuditSourceAI || r.TargetRef != "newapi" || r.TargetType != model.AuditTargetChannel || r.ActorID != 0 || string(r.DetailJSON) != `{"failures":5}` {
			t.Fatalf("ai 来源字段映射不对：%+v", r)
		}
		if r := rows[4]; r.Source != model.AuditSourceAdmin || r.TargetRef != "7" || r.TargetType != model.AdminAuditTargetUser || r.ActorID != root.ID {
			t.Fatalf("admin 来源 target_ref 应是 target_id 的十进制串：%+v", r)
		}
	})

	t.Run("筛选：操作人 id / 关键字、模块前缀、完整动作、时间范围", func(t *testing.T) {
		f := base
		id := ops.ID
		f.ActorID = &id
		if rows, _ := repo.ListMerged(ctx, f); len(rows) != 1 || rows[0].ID != a2.ID {
			t.Fatalf("按操作人 id：%v", auditKeys(rows))
		}
		zero := uint64(0)
		f.ActorID = &zero
		if rows, _ := repo.ListMerged(ctx, f); len(rows) != 1 || rows[0].ID != c2.ID {
			t.Fatalf("操作人 0 应是系统动作：%v", auditKeys(rows))
		}
		f = base
		f.ActorKeyword = "OP"
		if rows, _ := repo.ListMerged(ctx, f); len(rows) != 1 || rows[0].ID != a2.ID {
			t.Fatalf("按用户名片段（不分大小写）：%v", auditKeys(rows))
		}
		f.ActorKeyword = "%"
		if rows, _ := repo.ListMerged(ctx, f); len(rows) != 0 {
			t.Fatalf("通配符按字面量：%v", auditKeys(rows))
		}
		f = base
		f.ActionPrefix = "channel."
		if rows, _ := repo.ListMerged(ctx, f); len(rows) != 2 {
			t.Fatalf("channel 模块应有 2 条：%v", auditKeys(rows))
		}
		f = base
		f.Action = model.AdminAuditUserBan
		if rows, _ := repo.ListMerged(ctx, f); len(rows) != 1 || rows[0].ID != a1.ID {
			t.Fatalf("按完整动作：%v", auditKeys(rows))
		}
		f = base
		f.From = at(60 * 24 * 50)
		if rows, _ := repo.ListMerged(ctx, f); len(rows) != 6 || rows[5].ID != old.ID {
			t.Fatalf("放宽范围应多出 40 天前的一条：%v", auditKeys(rows))
		}
	})

	t.Run("分页：Offset + Limit 合起来不重复不遗漏，Count 与筛选一致", func(t *testing.T) {
		f := base
		f.Limit = 2
		var all []string
		for page := 0; page < 5; page++ {
			f.Offset = page * 2
			rows, err := repo.ListMerged(ctx, f)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) == 0 {
				break
			}
			all = append(all, auditKeys(rows)...)
		}
		full, _ := repo.ListMerged(ctx, base)
		want := auditKeys(full)
		if len(all) != len(want) {
			t.Fatalf("翻页合计应为 %d 条：%v", len(want), all)
		}
		for i := range want {
			if all[i] != want[i] {
				t.Fatalf("翻页顺序与整体不一致：%v vs %v", all, want)
			}
		}
		total, err := repo.CountMerged(ctx, f)
		if err != nil || total != int64(len(want)) {
			t.Fatalf("总数应为 %d（与分页参数无关）：%d %v", len(want), total, err)
		}
		f.Action = model.AuditChannelEnable
		if total, _ := repo.CountMerged(ctx, f); total != 1 {
			t.Fatalf("总数应遵守筛选：%d", total)
		}
	})

	t.Run("UsernamesByIDs：只返回存在的用户", func(t *testing.T) {
		m, err := repo.UsernamesByIDs(ctx, []uint64{root.ID, ops.ID, 99999})
		if err != nil || len(m) != 2 || m[root.ID] != "root" || m[ops.ID] != "ops" {
			t.Fatalf("%v %v", err, m)
		}
		if m, err := repo.UsernamesByIDs(ctx, nil); err != nil || len(m) != 0 {
			t.Fatalf("空入参应返回空 map：%v %v", err, m)
		}
	})
}
