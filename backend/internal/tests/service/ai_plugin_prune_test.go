package service_test

import (
	"context"
	"testing"

	"video-canvas/internal/model"
)

// 发版时删掉的内置插件：库里的旧行没人用就清掉；还被渠道固定或有进行中任务的保留；
// 上传来源的插件和本次仍随发版的内置插件永远不动。
func TestAIPluginService_PruneBuiltin(t *testing.T) {
	ctx := context.Background()

	seed := func(t *testing.T) (*aplEnv, map[string]uint64) {
		e := newAplEnv()
		ids := map[string]uint64{
			"shipped":  adminSeedVersion(t, e.repo, adminMetaLoose("shipped", "1.0.0"), model.PluginSourceBuiltin, true),
			"gone":     adminSeedVersion(t, e.repo, adminMetaLoose("gone", "1.0.0"), model.PluginSourceBuiltin, true),
			"gone-old": adminSeedVersion(t, e.repo, adminMetaLoose("gone-old", "1.0.0"), model.PluginSourceBuiltin, false),
			"pinned":   adminSeedVersion(t, e.repo, adminMetaLoose("pinned", "1.0.0"), model.PluginSourceBuiltin, true),
			"busy":     adminSeedVersion(t, e.repo, adminMetaLoose("busy", "1.0.0"), model.PluginSourceBuiltin, true),
			"mine":     adminSeedVersion(t, e.repo, adminMetaLoose("mine", "1.0.0"), model.PluginSourceUploaded, true),
		}
		e.repo.Channels["c1"] = &model.AIChannel{Key: "c1", Name: "渠道一", PluginKey: "pinned", PluginVersionID: ids["pinned"]}
		e.repo.ActiveTaskRefs[ids["busy"]] = 2
		return e, ids
	}

	t.Run("清掉不再随发版且没人用的内置插件，被引用的与上传的保留", func(t *testing.T) {
		e, _ := seed(t)
		res, err := e.svc.PruneBuiltin(ctx, []string{"shipped"})
		if err != nil {
			t.Fatal(err)
		}
		if got := res.Removed; len(got) != 2 || got[0] != "gone" || got[1] != "gone-old" {
			t.Fatalf("应清掉 gone、gone-old：%+v", res)
		}
		if got := res.InUse; len(got) != 2 || got[0] != "busy" || got[1] != "pinned" {
			t.Fatalf("被引用的应保留并报出：%+v", res)
		}
		for _, key := range []string{"gone", "gone-old"} {
			if _, ok := e.repo.Plugins[key]; ok {
				t.Fatalf("%s 应已删除", key)
			}
		}
		for _, key := range []string{"shipped", "pinned", "busy", "mine"} {
			if _, ok := e.repo.Plugins[key]; !ok {
				t.Fatalf("%s 不应被删除", key)
			}
		}
		for _, v := range e.repo.Versions {
			if v.PluginKey == "gone" || v.PluginKey == "gone-old" {
				t.Fatalf("旧插件的版本应一并删除：%+v", v)
			}
		}
	})

	t.Run("每个被清掉的插件写一条删除审计（操作人 0 = 系统）", func(t *testing.T) {
		e, _ := seed(t)
		if _, err := e.svc.PruneBuiltin(ctx, []string{"shipped"}); err != nil {
			t.Fatal(err)
		}
		var n int
		for _, a := range e.repo.Audits {
			if a.Action == model.AuditPluginDelete {
				n++
				if a.ActorID != 0 || a.TargetType != model.AuditTargetPlugin || (a.TargetKey != "gone" && a.TargetKey != "gone-old") {
					t.Fatalf("审计内容不符合预期：%+v", a)
				}
			}
		}
		if n != 2 {
			t.Fatalf("应有 2 条删除审计，实际 %d", n)
		}
	})

	t.Run("随发版的清单为空时什么都不删：防止清单读取失败时误清全部内置插件", func(t *testing.T) {
		e, _ := seed(t)
		res, err := e.svc.PruneBuiltin(ctx, nil)
		if err != nil || len(res.Removed) != 0 {
			t.Fatalf("清单为空不应清理：%+v %v", res, err)
		}
		if _, ok := e.repo.Plugins["gone"]; !ok {
			t.Fatal("清单为空时 gone 不应被删除")
		}
	})

	t.Run("清理时被并发引用：保留并算作在用，不报错", func(t *testing.T) {
		e, _ := seed(t)
		e.repo.DeleteInUse = true
		res, err := e.svc.PruneBuiltin(ctx, []string{"shipped"})
		if err != nil || len(res.Removed) != 0 {
			t.Fatalf("并发被引用时不应删除也不应报错：%+v %v", res, err)
		}
		if _, ok := e.repo.Plugins["gone"]; !ok {
			t.Fatal("gone 应保留")
		}
	})
}
