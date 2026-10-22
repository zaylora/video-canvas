package repository_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

func TestCharacterCardRepository(t *testing.T) {
	db := isolatedDB(t, &model.CharacterCard{})
	r := NewCharacterCardRepository(db)
	ctx := context.Background()
	const owner, other = uint64(11), uint64(22)

	mk := func(user uint64, name string, at time.Time) *model.CharacterCard {
		card := &model.CharacterCard{
			UserID: user, Name: name, Description: "初始设定", AvatarAssetID: 100,
			ExtraAssetIDs: datatypes.JSONSlice[uint64]{101, 102}, VoiceAssetID: new(uint64(103)),
			CreatedAt: at, UpdatedAt: at,
		}
		if err := db.Create(card).Error; err != nil {
			t.Fatalf("插入角色卡失败：%v", err)
		}
		return card
	}
	base := time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)
	first := mk(owner, "林晚", base)
	second := mk(owner, "周行", base.Add(time.Minute))
	otherCard := mk(other, "林晚", base.Add(2*time.Minute))

	t.Run("List only returns own cards in updated order and escapes ILIKE wildcards", func(t *testing.T) {
		cards, err := r.List(ctx, owner, "林%")
		if err != nil {
			t.Fatal(err)
		}
		if len(cards) != 0 {
			t.Fatalf("百分号不应作为通配符：%+v", cards)
		}
		cards, err = r.List(ctx, owner, "林")
		if err != nil || len(cards) != 1 || cards[0].ID != first.ID {
			t.Fatalf("搜索结果不对：%+v, %v", cards, err)
		}
		cards, err = r.List(ctx, owner, "")
		if err != nil || len(cards) != 2 || cards[0].ID != second.ID || cards[1].ID != first.ID {
			t.Fatalf("列表排序或用户隔离不对：%+v, %v", cards, err)
		}
	})

	t.Run("ListByIDs omits missing and other-user cards", func(t *testing.T) {
		cards, err := r.ListByIDs(ctx, owner, []uint64{otherCard.ID, second.ID, 999999})
		if err != nil || len(cards) != 1 || cards[0].ID != second.ID {
			t.Fatalf("批量查询结果不对：%+v, %v", cards, err)
		}
		cards, err = r.ListByIDs(ctx, owner, nil)
		if err != nil || cards != nil {
			t.Fatalf("空 ids 应返回 nil：%+v, %v", cards, err)
		}
	})

	t.Run("GetByID and mutations are user scoped", func(t *testing.T) {
		got, err := r.GetByID(ctx, owner, first.ID)
		if err != nil || got.Description != "初始设定" || !slices.Equal([]uint64(got.ExtraAssetIDs), []uint64{101, 102}) || got.VoiceAssetID == nil || *got.VoiceAssetID != 103 {
			t.Fatalf("读取字段不对：%+v, %v", got, err)
		}
		if _, err := r.GetByID(ctx, other, first.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("他人读取应返回 ErrNotFound：%v", err)
		}
		update := &model.CharacterCard{ID: first.ID, Name: "林晚·新", Description: "更新后", AvatarAssetID: 200, ExtraAssetIDs: datatypes.JSONSlice[uint64]{201}, VoiceAssetID: nil}
		if err := r.Update(ctx, owner, update); err != nil {
			t.Fatalf("更新失败：%v", err)
		}
		got, err = r.GetByID(ctx, owner, first.ID)
		if err != nil || got.Name != update.Name || got.AvatarAssetID != 200 || len(got.ExtraAssetIDs) != 1 || got.VoiceAssetID != nil {
			t.Fatalf("更新字段不对：%+v, %v", got, err)
		}
		if err := r.Update(ctx, other, update); !errors.Is(err, ErrNotFound) {
			t.Fatalf("他人更新应返回 ErrNotFound：%v", err)
		}
		if err := r.Delete(ctx, other, first.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("他人删除应返回 ErrNotFound：%v", err)
		}
	})

	t.Run("count name existence and duplicate errors", func(t *testing.T) {
		count, err := r.Count(ctx, owner)
		if err != nil || count != 2 {
			t.Fatalf("计数不对：%d, %v", count, err)
		}
		exists, err := r.NameExists(ctx, owner, "林晚·新", 0)
		if err != nil || !exists {
			t.Fatalf("名称应存在：%v, %v", exists, err)
		}
		exists, err = r.NameExists(ctx, owner, "林晚·新", first.ID)
		if err != nil || exists {
			t.Fatalf("排除自身后名称不应存在：%v, %v", exists, err)
		}
		dup := &model.CharacterCard{UserID: owner, Name: "周行", AvatarAssetID: 300}
		if err := r.Create(ctx, dup); !errors.Is(err, ErrDuplicate) {
			t.Fatalf("重复名称应返回 ErrDuplicate：%v", err)
		}
	})

	t.Run("delete and not found", func(t *testing.T) {
		if err := r.Delete(ctx, owner, second.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := r.GetByID(ctx, owner, second.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("删除后应不存在：%v", err)
		}
		if err := r.Delete(ctx, owner, second.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("重复删除应返回 ErrNotFound：%v", err)
		}
		count, err := r.Count(ctx, owner)
		if err != nil || count != 1 {
			t.Fatalf("删除后计数不对：%d, %v", count, err)
		}
	})
}
