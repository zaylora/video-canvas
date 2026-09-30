package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

func TestAIChannelRepository_CreateAndGet(t *testing.T) {
	ctx := context.Background()
	db := pluginTestDB(t)
	pr := NewAIPluginRepository(db)
	r := NewAIChannelRepository(db)
	vid := plSave(t, pr, "ch", "1.0.0")

	t.Run("新建渠道回填时间，空 JSON 列补成 {}", func(t *testing.T) {
		c := plChannel("c-a", "ch", vid)
		c.Enabled = true
		c.TrustedInternal = true
		c.UpdatedBy = 3
		if err := r.CreateChannel(ctx, c); err != nil {
			t.Fatal(err)
		}
		if c.CreatedAt.IsZero() || c.UpdatedAt.IsZero() {
			t.Fatalf("时间未回填：%+v", c)
		}
		got, err := r.GetChannel(ctx, "c-a")
		if err != nil {
			t.Fatal(err)
		}
		if !got.Enabled || got.PluginVersionID != vid || got.BaseURL != c.BaseURL || !got.TrustedInternal || got.AllowCredentials || got.UpdatedBy != 3 {
			t.Fatalf("渠道不符合预期：%+v", got)
		}
		if string(got.SettingsJSON) != "{}" || string(got.RateLimitJSON) != "{}" {
			t.Fatalf("空 JSON 列应补成 {}：%s %s", got.SettingsJSON, got.RateLimitJSON)
		}
	})

	t.Run("enabled=false 如实写入，不被列默认值顶掉", func(t *testing.T) {
		c := plChannel("c-off", "ch", vid)
		if err := r.CreateChannel(ctx, c); err != nil {
			t.Fatal(err)
		}
		if got, _ := r.GetChannel(ctx, "c-off"); got.Enabled {
			t.Fatalf("应保持停用：%+v", got)
		}
	})

	t.Run("JSON 列原样保存", func(t *testing.T) {
		c := plChannel("c-json", "ch", vid)
		c.SettingsJSON = model.JSONText(`{"b": 1, "a": 2}`)
		c.RateLimitJSON = model.JSONText(`{"rps":5,"max_concurrency":20}`)
		if err := r.CreateChannel(ctx, c); err != nil {
			t.Fatal(err)
		}
		got, _ := r.GetChannel(ctx, "c-json")
		if string(got.SettingsJSON) != `{"b": 1, "a": 2}` || string(got.RateLimitJSON) != `{"rps":5,"max_concurrency":20}` {
			t.Fatalf("%s %s", got.SettingsJSON, got.RateLimitJSON)
		}
	})

	t.Run("key 重复返回 ErrDuplicate", func(t *testing.T) {
		if err := r.CreateChannel(ctx, plChannel("c-a", "ch", vid)); !errors.Is(err, ErrDuplicate) {
			t.Fatalf("期望 ErrDuplicate，实际 %v", err)
		}
	})

	t.Run("指向不存在的版本返回 ErrNotFound，渠道不会被创建", func(t *testing.T) {
		if err := r.CreateChannel(ctx, plChannel("c-bad", "ch", 999999)); !errors.Is(err, ErrNotFound) {
			t.Fatalf("期望 ErrNotFound，实际 %v", err)
		}
		if _, err := r.GetChannel(ctx, "c-bad"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("渠道不应存在：%v", err)
		}
	})

	t.Run("GetChannel 不存在返回 ErrNotFound", func(t *testing.T) {
		if _, err := r.GetChannel(ctx, "none"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("期望 ErrNotFound，实际 %v", err)
		}
	})

	t.Run("ListChannels 按 key 升序", func(t *testing.T) {
		list, err := r.ListChannels(ctx)
		if err != nil || len(list) != 3 || list[0].Key != "c-a" || list[1].Key != "c-json" || list[2].Key != "c-off" {
			t.Fatalf("%+v %v", list, err)
		}
	})
}

func TestAIChannelRepository_UpdateChannel(t *testing.T) {
	ctx := context.Background()
	db := pluginTestDB(t)
	pr := NewAIPluginRepository(db)
	r := NewAIChannelRepository(db)
	v1 := plSave(t, pr, "up", "1.0.0")
	v2 := plSave(t, pr, "up", "2.0.0")
	orig := plChannel("u-a", "up", v1)
	if err := r.CreateChannel(ctx, orig); err != nil {
		t.Fatal(err)
	}
	created, _ := r.GetChannel(ctx, "u-a")

	t.Run("更新可变字段（含清零 bool），key 与 created_at 不变", func(t *testing.T) {
		time.Sleep(5 * time.Millisecond) // 制造 updated_at 的先后差异
		c := &model.AIChannel{
			Key: "u-a", Name: "新名", PluginKey: "up", PluginVersionID: v2, BaseURL: "https://new.example.com",
			TrustedInternal: false, AllowCredentials: true, Enabled: false, UpdatedBy: 8,
			SettingsJSON: model.JSONText(`{"x":1}`),
		}
		if err := r.UpdateChannel(ctx, c); err != nil {
			t.Fatal(err)
		}
		got, _ := r.GetChannel(ctx, "u-a")
		if got.Name != "新名" || got.PluginVersionID != v2 || got.BaseURL != "https://new.example.com" ||
			got.TrustedInternal || !got.AllowCredentials || got.Enabled || got.UpdatedBy != 8 {
			t.Fatalf("更新不符合预期：%+v", got)
		}
		if string(got.SettingsJSON) != `{"x":1}` || string(got.RateLimitJSON) != "{}" {
			t.Fatalf("JSON 列不符合预期：%s %s", got.SettingsJSON, got.RateLimitJSON)
		}
		if !got.CreatedAt.Equal(created.CreatedAt) || !got.UpdatedAt.After(created.UpdatedAt) || c.UpdatedAt.IsZero() {
			t.Fatalf("时间字段不符合预期：created=%v updated=%v", got.CreatedAt, got.UpdatedAt)
		}
	})

	t.Run("渠道不存在返回 ErrNotFound", func(t *testing.T) {
		if err := r.UpdateChannel(ctx, plChannel("none", "up", v1)); !errors.Is(err, ErrNotFound) {
			t.Fatalf("期望 ErrNotFound，实际 %v", err)
		}
	})

	t.Run("切到不存在的版本返回 ErrNotFound，原渠道不变", func(t *testing.T) {
		if err := r.UpdateChannel(ctx, plChannel("u-a", "up", 999999)); !errors.Is(err, ErrNotFound) {
			t.Fatalf("期望 ErrNotFound，实际 %v", err)
		}
		got, _ := r.GetChannel(ctx, "u-a")
		if got.PluginVersionID != v2 {
			t.Fatalf("渠道不应被改动：%+v", got)
		}
	})

	t.Run("升级后旧版本不再被渠道引用，可以删除", func(t *testing.T) {
		if err := pr.DeleteVersion(ctx, v1); err != nil {
			t.Fatalf("旧版本应已无引用：%v", err)
		}
		if err := pr.DeleteVersion(ctx, v2); !errors.Is(err, ErrInUse) {
			t.Fatalf("新版本被渠道引用应返回 ErrInUse，实际 %v", err)
		}
	})
}

func TestAIChannelRepository_SetChannelEnabled(t *testing.T) {
	ctx := context.Background()
	db := pluginTestDB(t)
	pr := NewAIPluginRepository(db)
	r := NewAIChannelRepository(db)
	vid := plSave(t, pr, "se", "1.0.0")
	if err := r.CreateChannel(ctx, plChannel("s-a", "se", vid)); err != nil {
		t.Fatal(err)
	}

	t.Run("启停并记录操作人", func(t *testing.T) {
		before, _ := r.GetChannel(ctx, "s-a")
		time.Sleep(5 * time.Millisecond) // 制造 updated_at 的先后差异
		if err := r.SetChannelEnabled(ctx, "s-a", false, 11); err != nil {
			t.Fatal(err)
		}
		got, _ := r.GetChannel(ctx, "s-a")
		if got.Enabled || got.UpdatedBy != 11 || !got.UpdatedAt.After(before.UpdatedAt) {
			t.Fatalf("停用不符合预期：%+v", got)
		}
		if err := r.SetChannelEnabled(ctx, "s-a", true, 12); err != nil {
			t.Fatal(err)
		}
		if got, _ = r.GetChannel(ctx, "s-a"); !got.Enabled || got.UpdatedBy != 12 {
			t.Fatalf("启用不符合预期：%+v", got)
		}
	})
	t.Run("渠道不存在返回 ErrNotFound", func(t *testing.T) {
		if err := r.SetChannelEnabled(ctx, "none", true, 1); !errors.Is(err, ErrNotFound) {
			t.Fatalf("期望 ErrNotFound，实际 %v", err)
		}
	})
}

func TestAIChannelRepository_Audit(t *testing.T) {
	ctx := context.Background()
	r := NewAIChannelRepository(pluginTestDB(t))
	insert := func(actor uint64, action, target string) uint64 {
		t.Helper()
		l := &model.AIAuditLog{
			ActorID: actor, Action: action, TargetType: model.AuditTargetChannel, TargetKey: target,
			DetailJSON: model.JSONText(`{"from":false,"to":true}`),
		}
		if err := r.InsertAudit(ctx, l); err != nil {
			t.Fatal(err)
		}
		if l.ID == 0 || l.CreatedAt.IsZero() {
			t.Fatalf("id / created_at 未回填：%+v", l)
		}
		return l.ID
	}
	id1 := insert(1, model.AuditChannelCreate, "ca")
	insert(1, model.AuditChannelTrusted, "cb")
	id3 := insert(2, model.AuditChannelSecret, "ca")

	t.Run("不过滤时按 id 倒序返回全部", func(t *testing.T) {
		list, err := r.ListAudit(ctx, "", 0)
		if err != nil || len(list) != 3 || list[0].ID != id3 {
			t.Fatalf("%+v %v", list, err)
		}
		if string(list[0].DetailJSON) != `{"from":false,"to":true}` || list[0].ActorID != 2 || list[0].Action != model.AuditChannelSecret {
			t.Fatalf("日志内容不符合预期：%+v", list[0])
		}
	})
	t.Run("按 target_key 过滤", func(t *testing.T) {
		list, err := r.ListAudit(ctx, "ca", 10)
		if err != nil || len(list) != 2 || list[0].ID != id3 || list[1].ID != id1 {
			t.Fatalf("%+v %v", list, err)
		}
	})
	t.Run("limit 生效", func(t *testing.T) {
		list, err := r.ListAudit(ctx, "", 1)
		if err != nil || len(list) != 1 || list[0].ID != id3 {
			t.Fatalf("%+v %v", list, err)
		}
	})
	t.Run("limit 缺省为 50", func(t *testing.T) {
		for range 55 {
			insert(1, model.AuditChannelEnable, "bulk")
		}
		list, err := r.ListAudit(ctx, "", -1)
		if err != nil || len(list) != 50 {
			t.Fatalf("应默认返回 50 条，实际 %d %v", len(list), err)
		}
	})
	t.Run("没有匹配时返回空列表", func(t *testing.T) {
		list, err := r.ListAudit(ctx, "none", 10)
		if err != nil || len(list) != 0 {
			t.Fatalf("%+v %v", list, err)
		}
	})
}
