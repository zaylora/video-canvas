package repository_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

// 本文件是素材库列表用到的几个查询的集成测试（AssetRepository.ListByKindSource、GenerationTaskRepository.ListBriefByIDs、
// UserRepository.ListUsernames、ShowcaseRepository.ReferencedAssetIDs），连接专用测试库（TEST_DATABASE_DSN），未设置时跳过。

func assetIDsOf(rows []model.Asset) []uint64 {
	ids := make([]uint64, 0, len(rows))
	for _, a := range rows {
		ids = append(ids, a.ID)
	}
	return ids
}

func TestAssetRepository_ListByKindSource(t *testing.T) {
	db := isolatedDB(t, &model.Asset{})
	r := NewAssetRepository(db)
	ctx := context.Background()
	base := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)

	mk := func(key, kind, source string, user uint64, at time.Time) uint64 {
		a := &model.Asset{UserID: user, Kind: kind, Source: source, StorageKey: key, MimeType: "x/y", CreatedAt: at}
		if err := r.Create(ctx, a); err != nil {
			t.Fatal(err)
		}
		return a.ID
	}
	oldest := mk("a", "video", model.AssetSourceGenerated, 1, base)
	// 同一时刻创建的两个：id 大的排前面
	tieLow := mk("b", "video", model.AssetSourceGenerated, 2, base.Add(time.Hour))
	tieHigh := mk("c", "video", model.AssetSourceGenerated, 3, base.Add(time.Hour))
	newest := mk("d", "video", model.AssetSourceGenerated, 1, base.Add(2*time.Hour))
	mk("up", "video", model.AssetSourceUpload, 1, base.Add(5*time.Hour))     // 上传的视频：不在结果里
	mk("img", "image", model.AssetSourceGenerated, 1, base.Add(6*time.Hour)) // 生成的图片：不在结果里

	t.Run("只返回 kind+source 都匹配的素材，全用户，created_at 倒序、id 倒序，总数不受分页影响", func(t *testing.T) {
		rows, total, err := r.ListByKindSource(ctx, "video", model.AssetSourceGenerated, 0, 10)
		if err != nil || total != 4 {
			t.Fatalf("total=%d err=%v", total, err)
		}
		if want := []uint64{newest, tieHigh, tieLow, oldest}; !slices.Equal(assetIDsOf(rows), want) {
			t.Fatalf("期望 %v，实际 %v", want, assetIDsOf(rows))
		}
	})
	t.Run("分页：offset / limit 生效，越界返回空并保留总数", func(t *testing.T) {
		rows, total, err := r.ListByKindSource(ctx, "video", model.AssetSourceGenerated, 1, 2)
		if err != nil || total != 4 || !slices.Equal(assetIDsOf(rows), []uint64{tieHigh, tieLow}) {
			t.Fatalf("%v %d %v", assetIDsOf(rows), total, err)
		}
		rows, total, err = r.ListByKindSource(ctx, "video", model.AssetSourceGenerated, 100, 2)
		if err != nil || total != 4 || len(rows) != 0 {
			t.Fatalf("%v %d %v", assetIDsOf(rows), total, err)
		}
	})
	t.Run("没有匹配的素材返回空与 0", func(t *testing.T) {
		rows, total, err := r.ListByKindSource(ctx, "audio", model.AssetSourceGenerated, 0, 10)
		if err != nil || total != 0 || len(rows) != 0 {
			t.Fatalf("%v %d %v", rows, total, err)
		}
	})
}

func TestGenerationTaskRepository_ListBriefByIDs(t *testing.T) {
	db := userDB(t)
	r := NewGenerationTaskRepository(db)
	ctx := context.Background()
	u1 := mkUser(t, db, "u1", "u1@x.com", model.RoleUser, model.UserStatusActive)
	u2 := mkUser(t, db, "u2", "u2@x.com", model.RoleUser, model.UserStatusActive)
	mk := func(user uint64, isTest bool, modelKey, input, snap string) uint64 {
		tk := model.GenerationTask{
			UserID: user, Kind: "video", ModelKey: modelKey, Provider: "p", Status: model.TaskSucceeded, IsTest: isTest,
			InputJSON: datatypes.JSON(input), ConfigSnapshot: datatypes.JSON(snap), DeadlineAt: time.Now().Add(time.Hour), NextPollAt: time.Now(),
			ProviderState: datatypes.JSON(`{"big":"x"}`),
		}
		if err := db.Create(&tk).Error; err != nil {
			t.Fatal(err)
		}
		return tk.ID
	}
	a := mk(u1.ID, false, "m1", `{"prompt":"一"}`, `{"model":{"label":"L1"}}`)
	b := mk(u2.ID, true, "m2", `{"prompt":"二"}`, `{"model":{"label":"L2"}}`) // 试跑任务也要能查到，且不限用户

	t.Run("按 id 批量取，不限用户、不排除试跑任务，只带展示用的列", func(t *testing.T) {
		got, err := r.ListBriefByIDs(ctx, []uint64{a, b, 987654321})
		if err != nil || len(got) != 2 {
			t.Fatalf("%+v %v", got, err)
		}
		byID := map[uint64]model.GenerationTask{got[0].ID: got[0], got[1].ID: got[1]}
		if byID[a].ModelKey != "m1" || !jsonEqual(byID[a].InputJSON, `{"prompt":"一"}`) || !jsonEqual(byID[a].ConfigSnapshot, `{"model":{"label":"L1"}}`) {
			t.Fatalf("字段不对：%+v", byID[a])
		}
		if byID[b].ModelKey != "m2" {
			t.Fatalf("%+v", byID[b])
		}
		if len(byID[a].ProviderState) != 0 || byID[a].UserID != 0 {
			t.Fatalf("不应读取无关的列：%+v", byID[a])
		}
	})
	t.Run("空 id 列表返回空", func(t *testing.T) {
		got, err := r.ListBriefByIDs(ctx, nil)
		if err != nil || len(got) != 0 {
			t.Fatalf("%+v %v", got, err)
		}
	})
}

func TestUserRepository_ListUsernames(t *testing.T) {
	db := userDB(t)
	r := NewUserRepository(db)
	ctx := context.Background()
	alice := mkUser(t, db, "alice", "a@x.com", model.RoleUser, model.UserStatusActive)
	bob := mkUser(t, db, "bob", "b@x.com", model.RoleUser, model.UserStatusActive)
	gone := mkUser(t, db, "gone", "g@x.com", model.RoleUser, model.UserStatusActive)
	if err := db.Delete(gone).Error; err != nil { // 软删除
		t.Fatal(err)
	}

	t.Run("批量取用户名；不存在与已软删除的 id 缺席；重复 id 不影响", func(t *testing.T) {
		got, err := r.ListUsernames(ctx, []uint64{alice.ID, bob.ID, alice.ID, gone.ID, 987654321})
		if err != nil || len(got) != 2 || got[alice.ID] != "alice" || got[bob.ID] != "bob" {
			t.Fatalf("%v %v", got, err)
		}
	})
	t.Run("空 id 列表返回空 map", func(t *testing.T) {
		got, err := r.ListUsernames(ctx, nil)
		if err != nil || got == nil || len(got) != 0 {
			t.Fatalf("%v %v", got, err)
		}
	})
}

func TestShowcaseRepository_ReferencedAssetIDs(t *testing.T) {
	db := isolatedDB(t, &model.ShowcaseItem{})
	r := NewShowcaseRepository(db)
	ctx := context.Background()
	add := func(assetID uint64, enabled bool) uint64 {
		it := &model.ShowcaseItem{AssetID: assetID, Prompt: "p", Enabled: enabled}
		if err := r.Create(ctx, it); err != nil {
			t.Fatal(err)
		}
		return it.ID
	}
	add(1, true)
	add(1, false) // 同一素材被引用两次：结果去重
	add(2, false) // 禁用的条目也算已引用
	deleted := add(3, true)
	add(9, true) // 不在查询范围内
	if err := r.Delete(ctx, deleted); err != nil {
		t.Fatal(err)
	}

	t.Run("返回被未删除条目引用的素材 id（含禁用条目，去重），已删除条目的引用不算", func(t *testing.T) {
		got, err := r.ReferencedAssetIDs(ctx, []uint64{1, 2, 3, 4})
		if err != nil {
			t.Fatal(err)
		}
		slices.Sort(got)
		if !slices.Equal(got, []uint64{1, 2}) {
			t.Fatalf("期望 [1 2]，实际 %v", got)
		}
	})
	t.Run("空列表返回空", func(t *testing.T) {
		got, err := r.ReferencedAssetIDs(ctx, nil)
		if err != nil || len(got) != 0 {
			t.Fatalf("%v %v", got, err)
		}
	})
}
