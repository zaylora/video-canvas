package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// 固定素材：1、2 是用户 1 的图，3 是用户 1 的音频，4 是用户 2 的图，5 是用户 1 的视频。
const (
	avatarAsset  = uint64(1)
	extraAsset   = uint64(2)
	voiceAsset   = uint64(3)
	foreignAsset = uint64(4)
	videoAsset   = uint64(5)
)

// fakeCharacterCards 是内存版角色卡仓储，按 user_id 做隔离，可注入各方法的错误。
type fakeCharacterCards struct {
	cards     map[uint64]*model.CharacterCard
	count     int64
	next      uint64
	listErr   error
	createErr error
	updateErr error
	deleteErr error
	getErr    error
	countErr  error
}

func newFakeCharacterCards() *fakeCharacterCards {
	return &fakeCharacterCards{cards: map[uint64]*model.CharacterCard{}}
}

// put 直接写入一张卡，绕过服务的校验，用于准备「已存在」的状态。
func (f *fakeCharacterCards) put(card *model.CharacterCard) *model.CharacterCard {
	f.next++
	card.ID = f.next
	f.cards[card.ID] = card
	f.count++
	return card
}

func (f *fakeCharacterCards) List(_ context.Context, userID uint64, q string) ([]model.CharacterCard, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]model.CharacterCard, 0, len(f.cards))
	for _, c := range f.cards {
		if c.UserID == userID && c.DramaID == nil && strings.Contains(strings.ToLower(c.Name), strings.ToLower(q)) {
			out = append(out, *c)
		}
	}
	return out, nil
}

func (f *fakeCharacterCards) ListByIDs(_ context.Context, userID uint64, ids []uint64) ([]model.CharacterCard, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]model.CharacterCard, 0, len(ids))
	for _, id := range ids {
		if c := f.cards[id]; c != nil && c.UserID == userID {
			out = append(out, *c)
		}
	}
	return out, nil
}

func (f *fakeCharacterCards) GetByID(_ context.Context, userID, id uint64) (*model.CharacterCard, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	c := f.cards[id]
	if c == nil || c.UserID != userID {
		return nil, repository.ErrNotFound
	}
	copied := *c
	return &copied, nil
}

func (f *fakeCharacterCards) Count(_ context.Context, _ uint64) (int64, error) {
	return f.count, f.countErr
}

func (f *fakeCharacterCards) Create(_ context.Context, card *model.CharacterCard) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.put(card)
	return nil
}

func (f *fakeCharacterCards) Update(_ context.Context, userID uint64, card *model.CharacterCard) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	old := f.cards[card.ID]
	if old == nil || old.UserID != userID {
		return repository.ErrNotFound
	}
	saved := *card
	f.cards[card.ID] = &saved
	return nil
}

func (f *fakeCharacterCards) Delete(_ context.Context, userID, id uint64) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	c := f.cards[id]
	if c == nil || c.UserID != userID {
		return repository.ErrNotFound
	}
	delete(f.cards, id)
	f.count--
	return nil
}

// fakeCharacterAssets 是内存版素材仓储，按用户归属返回素材。
type fakeCharacterAssets struct {
	assets map[uint64]*model.Asset
	err    error
}

func newFakeCharacterAssets() *fakeCharacterAssets {
	return &fakeCharacterAssets{assets: map[uint64]*model.Asset{
		avatarAsset:  {ID: avatarAsset, UserID: 1, Kind: "image"},
		extraAsset:   {ID: extraAsset, UserID: 1, Kind: "image"},
		voiceAsset:   {ID: voiceAsset, UserID: 1, Kind: "audio"},
		foreignAsset: {ID: foreignAsset, UserID: 2, Kind: "image"},
		videoAsset:   {ID: videoAsset, UserID: 1, Kind: "video"},
	}}
}

func (f *fakeCharacterAssets) GetByID(_ context.Context, userID, id uint64) (*model.Asset, error) {
	if f.err != nil {
		return nil, f.err
	}
	a := f.assets[id]
	if a == nil || a.UserID != userID {
		return nil, repository.ErrNotFound
	}
	return a, nil
}

// fakeCharacterViewer 把素材转成稳定地址的视图。
type fakeCharacterViewer struct{ err error }

func (f fakeCharacterViewer) ViewOf(_ context.Context, a *model.Asset) (*model.AssetView, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &model.AssetView{ID: a.ID, Kind: a.Kind, URL: "/files/example"}, nil
}

// newCharacterSvc 组装一套独立的 fake 依赖；viewer 与 assets 的错误按需注入。
func newCharacterSvc(t *testing.T) (*service.CharacterCardService, *fakeCharacterCards, *fakeCharacterAssets) {
	t.Helper()
	cards := newFakeCharacterCards()
	assets := newFakeCharacterAssets()
	svc := service.NewCharacterCardService(service.CharacterCardDeps{Cards: cards, Assets: assets, Views: fakeCharacterViewer{}})
	return svc, cards, assets
}

// wantCharacterCode 断言返回的是指定错误码的业务错误。
func wantCharacterCode(t *testing.T, err error, code int) {
	t.Helper()
	var e *errcode.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("期望错误码 %d，实际：%v", code, err)
	}
}

func TestCharacterCardService_Create(t *testing.T) {
	tests := []struct {
		name string
		req  model.CreateCharacterReq
		code int // 0 表示期望成功
	}{
		{"名称为空返回 66005", model.CreateCharacterReq{Name: "  ", AvatarAssetID: new(uint64(avatarAsset))}, errcode.ErrCharacterInvalid.Code},
		{"名称过长返回 66005", model.CreateCharacterReq{Name: strings.Repeat("名", 65), AvatarAssetID: new(uint64(avatarAsset))}, errcode.ErrCharacterInvalid.Code},
		{"设定过长返回 66005", model.CreateCharacterReq{Name: "林晚", Description: strings.Repeat("字", 501), AvatarAssetID: new(uint64(avatarAsset))}, errcode.ErrCharacterInvalid.Code},
		{"主图缺失允许创建", model.CreateCharacterReq{Name: "林晚"}, 0},
		{"主图不是图片返回 66003", model.CreateCharacterReq{Name: "林晚", AvatarAssetID: new(uint64(videoAsset))}, errcode.ErrCharacterAssetInvalid.Code},
		{"主图属于他人返回 66003", model.CreateCharacterReq{Name: "林晚", AvatarAssetID: new(uint64(foreignAsset))}, errcode.ErrCharacterAssetInvalid.Code},
		{"补充图重复返回 66005", model.CreateCharacterReq{Name: "林晚", AvatarAssetID: new(uint64(avatarAsset)), ExtraAssetIDs: []uint64{extraAsset, extraAsset}}, errcode.ErrCharacterInvalid.Code},
		{"补充图含主图返回 66005", model.CreateCharacterReq{Name: "林晚", AvatarAssetID: new(uint64(avatarAsset)), ExtraAssetIDs: []uint64{avatarAsset}}, errcode.ErrCharacterInvalid.Code},
		{"补充图超过 4 张返回 66005", model.CreateCharacterReq{Name: "林晚", AvatarAssetID: new(uint64(avatarAsset)), ExtraAssetIDs: []uint64{extraAsset, 6, 7, 8, 9}}, errcode.ErrCharacterInvalid.Code},
		{"补充图不是图片返回 66003", model.CreateCharacterReq{Name: "林晚", AvatarAssetID: new(uint64(avatarAsset)), ExtraAssetIDs: []uint64{voiceAsset}}, errcode.ErrCharacterAssetInvalid.Code},
		{"补充图属于他人返回 66003", model.CreateCharacterReq{Name: "林晚", AvatarAssetID: new(uint64(avatarAsset)), ExtraAssetIDs: []uint64{foreignAsset}}, errcode.ErrCharacterAssetInvalid.Code},
		{"声音不是音频返回 66003", model.CreateCharacterReq{Name: "林晚", AvatarAssetID: new(uint64(avatarAsset)), VoiceAssetID: new(extraAsset)}, errcode.ErrCharacterAssetInvalid.Code},
		{"声音属于他人返回 66003", model.CreateCharacterReq{Name: "林晚", AvatarAssetID: new(uint64(avatarAsset)), VoiceAssetID: new(foreignAsset)}, errcode.ErrCharacterAssetInvalid.Code},
		{"造型名过长", model.CreateCharacterReq{Name: "林晚", LookName: strings.Repeat("型", 65)}, errcode.ErrCharacterInvalid.Code},
		{"音色描述过长", model.CreateCharacterReq{Name: "林晚", VoicePrompt: strings.Repeat("音", 501)}, errcode.ErrCharacterInvalid.Code},
		{"无主图仍校验补充图归属", model.CreateCharacterReq{Name: "林晚", ExtraAssetIDs: []uint64{foreignAsset}}, errcode.ErrCharacterAssetInvalid.Code},
		{"无主图仍校验声音种类", model.CreateCharacterReq{Name: "林晚", VoiceAssetID: new(extraAsset)}, errcode.ErrCharacterAssetInvalid.Code},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _ := newCharacterSvc(t)
			if _, err := svc.Create(context.Background(), 1, &tt.req); tt.code == 0 {
				if err != nil {
					t.Fatalf("期望成功，实际返回错误：%v", err)
				}
			} else {
				wantCharacterCode(t, err, tt.code)
			}
		})
	}

	t.Run("成功：名称去首尾空白并返回素材视图", func(t *testing.T) {
		svc, cards, _ := newCharacterSvc(t)
		v, err := svc.Create(context.Background(), 1, &model.CreateCharacterReq{Name: "  林晚 ", Description: "28 岁，齐耳短发", AvatarAssetID: new(uint64(avatarAsset)), ExtraAssetIDs: []uint64{extraAsset}, VoiceAssetID: new(voiceAsset)})
		if err != nil {
			t.Fatal(err)
		}
		if v.Name != "林晚" || v.ID == 0 || v.Avatar == nil || v.Avatar.ID != avatarAsset {
			t.Fatalf("名称或主图不对：%+v", v)
		}
		if len(v.Extras) != 1 || v.Extras[0].ID != extraAsset || v.Voice == nil || v.Voice.ID != voiceAsset {
			t.Fatalf("补充图或声音不对：%+v", v)
		}
		if cards.cards[uint64(v.ID)].Description != "28 岁，齐耳短发" {
			t.Fatalf("描述没有落库：%+v", cards.cards[uint64(v.ID)])
		}
	})

	t.Run("名称重复（忽略大小写）允许共存", func(t *testing.T) {
		svc, cards, _ := newCharacterSvc(t)
		cards.put(&model.CharacterCard{UserID: 1, Name: "LinWan", AvatarAssetID: new(uint64(avatarAsset))})
		v, err := svc.Create(context.Background(), 1, &model.CreateCharacterReq{Name: "linwan", AvatarAssetID: new(uint64(avatarAsset))})
		if err != nil || v == nil || len(cards.cards) != 2 {
			t.Fatalf("同名卡应共存：%+v %v", v, err)
		}
	})

	t.Run("创建仓储错误原样返回", func(t *testing.T) {
		svc, cards, _ := newCharacterSvc(t)
		boom := errors.New("create failed")
		cards.createErr = boom
		_, err := svc.Create(context.Background(), 1, &model.CreateCharacterReq{Name: "林晚"})
		if !errors.Is(err, boom) {
			t.Fatalf("创建错误没有保留：%v", err)
		}
	})

	t.Run("已有 100 张返回 66002", func(t *testing.T) {
		svc, cards, _ := newCharacterSvc(t)
		cards.count = 100
		_, err := svc.Create(context.Background(), 1, &model.CreateCharacterReq{Name: "林晚", AvatarAssetID: new(uint64(avatarAsset))})
		wantCharacterCode(t, err, errcode.ErrCharacterLimit.Code)
	})

	t.Run("仓储未知错误原样透传", func(t *testing.T) {
		svc, cards, _ := newCharacterSvc(t)
		boom := errors.New("db down")
		cards.countErr = boom
		if _, err := svc.Create(context.Background(), 1, &model.CreateCharacterReq{Name: "林晚", AvatarAssetID: new(uint64(avatarAsset))}); !errors.Is(err, boom) {
			t.Fatalf("期望透传仓储错误，实际：%v", err)
		}
	})
}

func TestCharacterCardService_Update(t *testing.T) {
	// seed 准备一张完整的卡：主图 1、补充图 2、声音 3。
	seed := func(t *testing.T) (*service.CharacterCardService, *fakeCharacterCards, *model.CharacterCard) {
		t.Helper()
		svc, cards, _ := newCharacterSvc(t)
		card := cards.put(&model.CharacterCard{UserID: 1, Name: "林晚", Description: "旧设定", AvatarAssetID: new(uint64(avatarAsset)), ExtraAssetIDs: []uint64{extraAsset}, VoiceAssetID: new(voiceAsset)})
		return svc, cards, card
	}

	t.Run("不存在返回 66004", func(t *testing.T) {
		svc, _, _ := newCharacterSvc(t)
		_, err := svc.Update(context.Background(), 1, 99, &model.UpdateCharacterReq{})
		wantCharacterCode(t, err, errcode.ErrCharacterNotFound.Code)
	})

	t.Run("他人的卡返回 66004", func(t *testing.T) {
		svc, _, card := seed(t)
		_, err := svc.Update(context.Background(), 2, card.ID, &model.UpdateCharacterReq{})
		wantCharacterCode(t, err, errcode.ErrCharacterNotFound.Code)
	})

	t.Run("未出现的字段保持不变", func(t *testing.T) {
		svc, _, card := seed(t)
		desc := "新设定"
		v, err := svc.Update(context.Background(), 1, card.ID, &model.UpdateCharacterReq{Description: &desc})
		if err != nil {
			t.Fatal(err)
		}
		if v.Description != "新设定" || v.Name != "林晚" || v.Avatar == nil || len(v.Extras) != 1 || v.Voice == nil {
			t.Fatalf("其他字段被改动：%+v", v)
		}
	})

	t.Run("voice_asset_id 为 null 清空声音、extra_asset_ids 为 [] 清空补充图", func(t *testing.T) {
		svc, _, card := seed(t)
		v, err := svc.Update(context.Background(), 1, card.ID, &model.UpdateCharacterReq{
			VoiceAssetID: []byte("null"), ExtraAssetIDs: []uint64{},
		})
		if err != nil {
			t.Fatal(err)
		}
		if v.Voice != nil || len(v.Extras) != 0 || v.Avatar == nil {
			t.Fatalf("清空失败：%+v", v)
		}
	})

	t.Run("没传声音时保留原声音", func(t *testing.T) {
		svc, _, card := seed(t)
		name := "林晚·改"
		v, err := svc.Update(context.Background(), 1, card.ID, &model.UpdateCharacterReq{Name: &name})
		if err != nil || v.Voice == nil || v.Voice.ID != voiceAsset {
			t.Fatalf("声音不应被清空：%+v, %v", v, err)
		}
	})

	t.Run("名称为空返回 66005", func(t *testing.T) {
		svc, _, card := seed(t)
		_, err := svc.Update(context.Background(), 1, card.ID, &model.UpdateCharacterReq{Name: new("   ")})
		wantCharacterCode(t, err, errcode.ErrCharacterInvalid.Code)
	})

	t.Run("改成其他角色名称允许，只改大小写允许", func(t *testing.T) {
		svc, cards, card := seed(t)
		cards.put(&model.CharacterCard{UserID: 1, Name: "周行", AvatarAssetID: new(uint64(avatarAsset))})
		if _, err := svc.Update(context.Background(), 1, card.ID, &model.UpdateCharacterReq{Name: new("周行")}); err != nil {
			t.Fatalf("同名更新应成功：%v", err)
		}
		if _, err := svc.Update(context.Background(), 1, card.ID, &model.UpdateCharacterReq{Name: new("林晚")}); err != nil {
			t.Fatalf("只改大小写或改成原名不应失败：%v", err)
		}
	})

	t.Run("补充图含主图返回 66005", func(t *testing.T) {
		svc, _, card := seed(t)
		_, err := svc.Update(context.Background(), 1, card.ID, &model.UpdateCharacterReq{ExtraAssetIDs: []uint64{avatarAsset}})
		wantCharacterCode(t, err, errcode.ErrCharacterInvalid.Code)
	})

	t.Run("换成不可用的声音返回 66003", func(t *testing.T) {
		svc, _, card := seed(t)
		_, err := svc.Update(context.Background(), 1, card.ID, &model.UpdateCharacterReq{VoiceAssetID: []byte("2")})
		wantCharacterCode(t, err, errcode.ErrCharacterAssetInvalid.Code)
	})

	t.Run("声音 ID 不是数字返回参数错误", func(t *testing.T) {
		svc, _, card := seed(t)
		_, err := svc.Update(context.Background(), 1, card.ID, &model.UpdateCharacterReq{VoiceAssetID: []byte(`"abc"`)})
		wantCharacterCode(t, err, errcode.ErrInvalidParams.Code)
	})

	t.Run("并发删除返回 66004", func(t *testing.T) {
		svc, cards, card := seed(t)
		cards.updateErr = repository.ErrNotFound
		_, err := svc.Update(context.Background(), 1, card.ID, &model.UpdateCharacterReq{})
		wantCharacterCode(t, err, errcode.ErrCharacterNotFound.Code)
	})

	t.Run("更新仓储错误原样返回", func(t *testing.T) {
		svc, cards, card := seed(t)
		boom := errors.New("update failed")
		cards.updateErr = boom
		_, err := svc.Update(context.Background(), 1, card.ID, &model.UpdateCharacterReq{})
		if !errors.Is(err, boom) {
			t.Fatalf("更新错误没有保留：%v", err)
		}
	})
}

func TestCharacterCardService_ReadAndViews(t *testing.T) {
	// seed 准备两张卡：第一张完整，第二张的补充图与声音已被清理。
	seed := func(t *testing.T) (*service.CharacterCardService, *fakeCharacterCards, *fakeCharacterAssets, *model.CharacterCard) {
		t.Helper()
		svc, cards, assets := newCharacterSvc(t)
		full := cards.put(&model.CharacterCard{UserID: 1, Name: "林晚", AvatarAssetID: new(uint64(avatarAsset)), ExtraAssetIDs: []uint64{extraAsset}, VoiceAssetID: new(voiceAsset)})
		cards.put(&model.CharacterCard{UserID: 1, Name: "周行", AvatarAssetID: new(uint64(avatarAsset)), ExtraAssetIDs: []uint64{99}, VoiceAssetID: new(uint64(98))})
		cards.put(&model.CharacterCard{UserID: 2, Name: "别人", AvatarAssetID: new(uint64(avatarAsset))})
		return svc, cards, assets, full
	}

	t.Run("列表只返回自己的卡", func(t *testing.T) {
		svc, _, _, _ := seed(t)
		list, err := svc.List(context.Background(), 1, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(list.Items) != 2 {
			t.Fatalf("期望 2 张，实际：%+v", list.Items)
		}
		for _, item := range list.Items {
			if item.Name == "别人" {
				t.Fatalf("读到了他人的角色卡：%+v", item)
			}
		}
	})

	t.Run("按名称搜索", func(t *testing.T) {
		svc, _, _, _ := seed(t)
		list, err := svc.List(context.Background(), 1, "林")
		if err != nil || len(list.Items) != 1 || list.Items[0].Name != "林晚" {
			t.Fatalf("搜索结果不对：%+v, %v", list, err)
		}
	})

	t.Run("ids 批量读取：缺席的不报错", func(t *testing.T) {
		svc, _, _, full := seed(t)
		list, err := svc.ListByIDs(context.Background(), 1, []uint64{full.ID, 99999})
		if err != nil || len(list.Items) != 1 || uint64(list.Items[0].ID) != full.ID {
			t.Fatalf("批量读取结果不对：%+v, %v", list, err)
		}
	})

	t.Run("空 ids 返回空数组而不是 null", func(t *testing.T) {
		svc, _, _, _ := seed(t)
		list, err := svc.ListByIDs(context.Background(), 1, nil)
		if err != nil || list.Items == nil || len(list.Items) != 0 {
			t.Fatalf("空 ids 应返回空数组：%+v, %v", list, err)
		}
	})

	t.Run("素材被清理时主图与声音为 null、补充图略过", func(t *testing.T) {
		svc, _, assets, _ := seed(t)
		list, err := svc.List(context.Background(), 1, "周行")
		if err != nil {
			t.Fatal(err)
		}
		if len(list.Items) != 1 {
			t.Fatalf("期望 1 张，实际：%+v", list.Items)
		}
		v := list.Items[0]
		if v.Extras == nil || len(v.Extras) != 0 || v.Voice != nil {
			t.Fatalf("失效素材应降级：%+v", v)
		}

		delete(assets.assets, avatarAsset)
		got, err := svc.Get(context.Background(), 1, uint64(v.ID))
		if err != nil || got.Avatar != nil {
			t.Fatalf("主图被清理后应为 null：%+v, %v", got, err)
		}
	})

	t.Run("他人的卡按不存在处理", func(t *testing.T) {
		svc, cards, _, _ := seed(t)
		var foreignID uint64
		for id, c := range cards.cards {
			if c.UserID == 2 {
				foreignID = id
			}
		}
		_, err := svc.Get(context.Background(), 1, foreignID)
		wantCharacterCode(t, err, errcode.ErrCharacterNotFound.Code)
	})

	t.Run("仓储未知错误原样透传", func(t *testing.T) {
		svc, cards, _, _ := seed(t)
		boom := errors.New("db down")
		cards.listErr = boom
		if _, err := svc.List(context.Background(), 1, ""); !errors.Is(err, boom) {
			t.Fatalf("期望透传仓储错误，实际：%v", err)
		}
	})
}

func TestCharacterCardService_Delete(t *testing.T) {
	t.Run("删除成功后详情返回 66004", func(t *testing.T) {
		svc, cards, _ := newCharacterSvc(t)
		card := cards.put(&model.CharacterCard{UserID: 1, Name: "林晚", AvatarAssetID: new(uint64(avatarAsset))})
		if err := svc.Delete(context.Background(), 1, card.ID); err != nil {
			t.Fatal(err)
		}
		_, err := svc.Get(context.Background(), 1, card.ID)
		wantCharacterCode(t, err, errcode.ErrCharacterNotFound.Code)
	})

	t.Run("他人的卡返回 66004 且不删除", func(t *testing.T) {
		svc, cards, _ := newCharacterSvc(t)
		card := cards.put(&model.CharacterCard{UserID: 2, Name: "别人", AvatarAssetID: new(uint64(avatarAsset))})
		wantCharacterCode(t, svc.Delete(context.Background(), 1, card.ID), errcode.ErrCharacterNotFound.Code)
		if cards.cards[card.ID] == nil {
			t.Fatal("他人的角色卡被删除了")
		}
	})

	t.Run("仓储未知错误原样透传", func(t *testing.T) {
		svc, cards, _ := newCharacterSvc(t)
		boom := errors.New("db down")
		cards.deleteErr = boom
		if err := svc.Delete(context.Background(), 1, 1); !errors.Is(err, boom) {
			t.Fatalf("期望透传仓储错误，实际：%v", err)
		}
	})
}

func TestCharacterCardService_DramaFieldsAndNullableAvatar(t *testing.T) {
	svc, cards, assets := newCharacterSvc(t)
	ctx := context.Background()
	v, err := svc.Create(ctx, 1, &model.CreateCharacterReq{Name: "林晚", LookName: strings.Repeat("型", 64), VoicePrompt: strings.Repeat("音", 500)})
	if err != nil || v.Avatar != nil || v.DramaID != nil || len([]rune(v.LookName)) != 64 || len([]rune(v.VoicePrompt)) != 500 {
		t.Fatalf("无主图及边界长度应有效：%+v %v", v, err)
	}
	id := uint64(v.ID)
	v, err = svc.Update(ctx, 1, id, &model.UpdateCharacterReq{AvatarAssetID: []byte("1"), LookName: new("童年"), VoicePrompt: new("清亮少年音")})
	if err != nil || v.Avatar == nil || v.LookName != "童年" || v.VoicePrompt != "清亮少年音" {
		t.Fatalf("生成后写回主图和角色字段失败：%+v %v", v, err)
	}
	delete(assets.assets, avatarAsset)
	v, err = svc.Update(ctx, 1, id, &model.UpdateCharacterReq{AvatarAssetID: []byte("null")})
	if err != nil || v.Avatar != nil || cards.cards[id].AvatarAssetID != nil {
		t.Fatalf("清空失效主图失败：%+v %v", v, err)
	}
	for _, req := range []*model.UpdateCharacterReq{
		{LookName: new(strings.Repeat("型", 65))},
		{VoicePrompt: new(strings.Repeat("音", 501))},
	} {
		_, err := svc.Update(ctx, 1, id, req)
		wantCharacterCode(t, err, errcode.ErrCharacterInvalid.Code)
	}
	for _, raw := range []string{`"bad"`, "0", "false"} {
		_, err := svc.Update(ctx, 1, id, &model.UpdateCharacterReq{AvatarAssetID: []byte(raw)})
		wantCharacterCode(t, err, errcode.ErrInvalidParams.Code)
	}
	for _, raw := range []string{"3", "4", "5", "999"} {
		_, err := svc.Update(ctx, 1, id, &model.UpdateCharacterReq{AvatarAssetID: []byte(raw)})
		wantCharacterCode(t, err, errcode.ErrCharacterAssetInvalid.Code)
	}
	drama := cards.put(&model.CharacterCard{UserID: 1, Name: "短剧角色", DramaID: new(uint64(42))})
	list, err := svc.List(ctx, 1, "")
	if err != nil || len(list.Items) != 1 || uint64(list.Items[0].ID) != id {
		t.Fatalf("默认列表应排除短剧：%+v %v", list, err)
	}
	list, err = svc.ListByIDs(ctx, 1, []uint64{drama.ID})
	if err != nil || len(list.Items) != 1 || list.Items[0].DramaID == nil || uint64(*list.Items[0].DramaID) != 42 || list.Items[0].Avatar != nil {
		t.Fatalf("ids 应包含无主图短剧角色：%+v %v", list, err)
	}
}
