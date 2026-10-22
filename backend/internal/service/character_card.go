package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
)

// 角色卡的业务上限，与接口契约一致。
const (
	maxCharacterCards       = 100 // 每用户最多 100 张
	maxCharacterNameLen     = 64  // 名称去首尾空白后 1–64 字
	maxCharacterDescLen     = 500 // 设定最多 500 字
	maxCharacterExtraAssets = 4   // 补充图最多 4 张
)

// 角色卡引用的素材种类。
const (
	characterAssetKindImage = "image"
	characterAssetKindAudio = "audio"
)

// CharacterCardRepo 是角色卡的数据访问接口（真实实现是 repository.CharacterCardRepository）。
type CharacterCardRepo interface {
	// List 返回该用户名称包含 q 的角色卡，更新时间倒序；q 为空表示不筛选。
	List(ctx context.Context, userID uint64, q string) ([]model.CharacterCard, error)
	// ListByIDs 返回该用户这些 ID 的角色卡，查不到的缺席，ids 为空返回 nil。
	ListByIDs(ctx context.Context, userID uint64, ids []uint64) ([]model.CharacterCard, error)
	// GetByID 按 id + user_id 查询，不存在或不属于该用户返回 repository.ErrNotFound。
	GetByID(ctx context.Context, userID, id uint64) (*model.CharacterCard, error)
	// Count 统计该用户的角色卡数。
	Count(ctx context.Context, userID uint64) (int64, error)
	// NameExists 判断该用户是否已有同名（忽略大小写）的角色卡，excludeID 非 0 时排除它自己。
	NameExists(ctx context.Context, userID uint64, name string, excludeID uint64) (bool, error)
	// Create 插入角色卡，成功后回填 id；重名返回 repository.ErrDuplicate。
	Create(ctx context.Context, card *model.CharacterCard) error
	// Update 更新该用户角色卡的全部可变字段，不存在返回 repository.ErrNotFound，重名返回 repository.ErrDuplicate。
	Update(ctx context.Context, userID uint64, card *model.CharacterCard) error
	// Delete 按 id + user_id 删除，不存在或不属于该用户返回 repository.ErrNotFound。
	Delete(ctx context.Context, userID, id uint64) error
}

// CharacterCardAssets 是角色卡依赖的素材查询接口（真实实现是 repository.AssetRepository）。
type CharacterCardAssets interface {
	// GetByID 按 id + user_id 查询，不存在或不属于该用户返回 repository.ErrNotFound。
	GetByID(ctx context.Context, userID, id uint64) (*model.Asset, error)
}

// CharacterCardViewer 把素材转成带稳定地址的视图（真实实现是 AssetService.ViewOf）。
type CharacterCardViewer interface {
	// ViewOf 返回素材视图，调用方必须已确认素材归属当前用户。
	ViewOf(ctx context.Context, a *model.Asset) (*model.AssetView, error)
}

// CharacterCardDeps 是角色卡服务的依赖。
type CharacterCardDeps struct {
	Cards  CharacterCardRepo
	Assets CharacterCardAssets
	Views  CharacterCardViewer
}

// CharacterCardService 是角色卡业务：只引用已有素材，不复制文件。
// 写入时校验素材归属和种类；读取时素材被清理的部分按契约降级，不阻断列表。
type CharacterCardService struct {
	cards  CharacterCardRepo
	assets CharacterCardAssets
	views  CharacterCardViewer
}

// NewCharacterCardService 创建角色卡服务。
func NewCharacterCardService(d CharacterCardDeps) *CharacterCardService {
	return &CharacterCardService{cards: d.Cards, assets: d.Assets, views: d.Views}
}

// List 返回当前用户的角色卡列表（不分页，最多 100 张），按更新时间倒序。
func (s *CharacterCardService) List(ctx context.Context, userID uint64, q string) (*model.CharacterListView, error) {
	cards, err := s.cards.List(ctx, userID, strings.TrimSpace(q))
	if err != nil {
		return nil, err
	}
	return s.toList(ctx, userID, cards)
}

// ListByIDs 按 ID 批量返回当前用户的角色卡；查不到的 ID 直接缺席，由前端判断已删除。
func (s *CharacterCardService) ListByIDs(ctx context.Context, userID uint64, ids []uint64) (*model.CharacterListView, error) {
	// 1. ids 为空时不发查询，返回空列表而不是 null
	if len(ids) == 0 {
		return &model.CharacterListView{Items: []model.CharacterView{}}, nil
	}

	// 2. 仓储同时限定 user_id，他人的角色卡等同于不存在
	cards, err := s.cards.ListByIDs(ctx, userID, ids)
	if err != nil {
		return nil, err
	}
	return s.toList(ctx, userID, cards)
}

// Get 返回当前用户的一张角色卡；不存在和属于他人统一返回 66004。
func (s *CharacterCardService) Get(ctx context.Context, userID, id uint64) (*model.CharacterView, error) {
	card, err := s.getOwned(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	return s.toView(ctx, userID, card)
}

// Create 新建角色卡并返回视图。
// 规则：名称去首尾空白后 1–64 字且忽略大小写唯一；设定 ≤ 500 字；补充图 ≤ 4 张、去重、不含主图；
// 主图必须是当前用户的 image，补充图同，声音必须是当前用户的 audio；每用户最多 100 张。
func (s *CharacterCardService) Create(ctx context.Context, userID uint64, req *model.CreateCharacterReq) (*model.CharacterView, error) {
	// 1. 名称与设定：长度按字符数算，去首尾空白后判空
	name, err := normalizeCharacterName(req.Name)
	if err != nil {
		return nil, err
	}
	if err := checkCharacterDescription(req.Description); err != nil {
		return nil, err
	}

	// 2. 补充图的结构性规则先验，避免为明显非法的请求查库
	if err := checkCharacterExtras(req.AvatarAssetID, req.ExtraAssetIDs); err != nil {
		return nil, err
	}

	// 3. 数量上限：先查再建，并发下可能略微超出；上限只用于防滥用，不承担计费语义
	n, err := s.cards.Count(ctx, userID)
	if err != nil {
		return nil, err
	}
	if n >= maxCharacterCards {
		return nil, errcode.ErrCharacterLimit
	}

	// 4. 重名先查给出 409；并发撞上唯一索引时 repository 返回 ErrDuplicate，同样翻成 409
	if err := s.checkNameFree(ctx, userID, name, 0); err != nil {
		return nil, err
	}

	// 5. 写入前校验素材存在、属于当前用户、种类正确：角色卡只存 ID，素材以后可能被清理
	if err := s.checkAssets(ctx, userID, req.AvatarAssetID, req.ExtraAssetIDs, req.VoiceAssetID); err != nil {
		return nil, err
	}

	card := &model.CharacterCard{
		UserID:        userID,
		Name:          name,
		Description:   req.Description,
		AvatarAssetID: req.AvatarAssetID,
		ExtraAssetIDs: append(datatypes.JSONSlice[uint64]{}, req.ExtraAssetIDs...),
		VoiceAssetID:  req.VoiceAssetID,
	}
	if err := s.cards.Create(ctx, card); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			return nil, errcode.ErrCharacterNameDup
		}
		return nil, err
	}

	// 6. 返回带素材地址的视图；素材刚校验过，这里仍按读取规则处理缺失
	return s.toView(ctx, userID, card)
}

// Update 部分更新角色卡并返回最新视图；没传的字段保持不变，声音传 null 清空、补充图传 [] 清空。
func (s *CharacterCardService) Update(ctx context.Context, userID, id uint64, req *model.UpdateCharacterReq) (*model.CharacterView, error) {
	// 1. 先确认归属：不存在和属于他人都是 66004
	card, err := s.getOwned(ctx, userID, id)
	if err != nil {
		return nil, err
	}

	// 2. 按字段是否出现更新副本，再用更新后的完整状态重新校验
	voiceChanged, err := applyCharacterPatch(card, req)
	if err != nil {
		return nil, err
	}
	if err := s.checkCharacterPatch(ctx, userID, id, card, req, voiceChanged); err != nil {
		return nil, err
	}

	// 3. 写入：重名撞唯一索引、并发删除都翻译成契约里的错误
	if err := s.cards.Update(ctx, userID, card); err != nil {
		switch {
		case errors.Is(err, repository.ErrDuplicate):
			return nil, errcode.ErrCharacterNameDup
		case errors.Is(err, repository.ErrNotFound):
			return nil, errcode.ErrCharacterNotFound
		}
		return nil, err
	}
	return s.toView(ctx, userID, card)
}

// applyCharacterPatch 把请求里出现过的字段写进副本；返回声音是否被改动。
// 部分字段变更可能让补充图与主图冲突，所以调用方要基于更新后的完整状态再校验一次。
func applyCharacterPatch(card *model.CharacterCard, req *model.UpdateCharacterReq) (bool, error) {
	if req.Name != nil {
		name, err := normalizeCharacterName(*req.Name)
		if err != nil {
			return false, err
		}
		card.Name = name
	}
	if req.Description != nil {
		card.Description = *req.Description
	}
	if req.AvatarAssetID != nil {
		card.AvatarAssetID = *req.AvatarAssetID
	}
	if req.ExtraAssetIDs != nil {
		card.ExtraAssetIDs = append(datatypes.JSONSlice[uint64]{}, req.ExtraAssetIDs...)
	}
	return applyCharacterVoice(&card.VoiceAssetID, req.VoiceAssetID)
}

// checkCharacterPatch 校验更新后的完整状态。
// 只有本次改动过的素材才重新校验归属：旧素材被清理后，用户仍能改名、改设定，删除失效引用也不需要先补素材。
func (s *CharacterCardService) checkCharacterPatch(ctx context.Context, userID, id uint64, card *model.CharacterCard, req *model.UpdateCharacterReq, voiceChanged bool) error {
	if err := checkCharacterDescription(card.Description); err != nil {
		return err
	}
	if err := checkCharacterExtras(card.AvatarAssetID, card.ExtraAssetIDs); err != nil {
		return err
	}
	// 只有改了名称才查重名，并排除它自己：只改大小写不算重名
	if req.Name != nil {
		if err := s.checkNameFree(ctx, userID, card.Name, id); err != nil {
			return err
		}
	}
	if voiceChanged && card.VoiceAssetID != nil {
		if err := s.checkAsset(ctx, userID, *card.VoiceAssetID, characterAssetKindAudio, "声音"); err != nil {
			return err
		}
	}
	if req.AvatarAssetID != nil || req.ExtraAssetIDs != nil {
		return s.checkAssets(ctx, userID, card.AvatarAssetID, card.ExtraAssetIDs, nil)
	}
	return nil
}

// Delete 删除当前用户的角色卡；只删角色卡行，不删素材，也不反查画布引用。
func (s *CharacterCardService) Delete(ctx context.Context, userID, id uint64) error {
	// 1. SQL 内限定 user_id，他人的角色卡按不存在处理
	err := s.cards.Delete(ctx, userID, id)
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrCharacterNotFound
	}
	return err
}

// getOwned 查询当前用户的角色卡，不存在和属于他人统一返回 66004。
func (s *CharacterCardService) getOwned(ctx context.Context, userID, id uint64) (*model.CharacterCard, error) {
	card, err := s.cards.GetByID(ctx, userID, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrCharacterNotFound
	}
	if err != nil {
		return nil, err
	}
	return card, nil
}

// checkNameFree 确认用户名下没有同名（忽略大小写）的角色卡，excludeID 非 0 时排除它自己；重名返回 66001。
func (s *CharacterCardService) checkNameFree(ctx context.Context, userID uint64, name string, excludeID uint64) error {
	exists, err := s.cards.NameExists(ctx, userID, name, excludeID)
	if err != nil {
		return err
	}
	if exists {
		return errcode.ErrCharacterNameDup
	}
	return nil
}

// checkAssets 逐项校验素材归属与种类；voice 为 nil 时不校验声音。
func (s *CharacterCardService) checkAssets(ctx context.Context, userID, avatar uint64, extras []uint64, voice *uint64) error {
	if err := s.checkAsset(ctx, userID, avatar, characterAssetKindImage, "主图"); err != nil {
		return err
	}
	for _, id := range extras {
		if err := s.checkAsset(ctx, userID, id, characterAssetKindImage, "补充图"); err != nil {
			return err
		}
	}
	if voice == nil {
		return nil
	}
	return s.checkAsset(ctx, userID, *voice, characterAssetKindAudio, "声音")
}

// checkAsset 校验一条素材：不存在、属于他人、种类不对都返回 66003。
// 「不存在」与「不属于你」给同一句文案，避免通过错误信息探测别人的素材。
func (s *CharacterCardService) checkAsset(ctx context.Context, userID, id uint64, kind, label string) error {
	if id == 0 {
		return errcode.ErrCharacterAssetInvalid.WithMsg(label + "不存在")
	}
	a, err := s.assets.GetByID(ctx, userID, id)
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrCharacterAssetInvalid.WithMsg(label + "不存在")
	}
	if err != nil {
		return err
	}
	if a.Kind != kind {
		return errcode.ErrCharacterAssetInvalid.WithMsg(label + "种类不对")
	}
	return nil
}

// toList 组装列表视图，素材缺失的按单张卡的规则处理。
func (s *CharacterCardService) toList(ctx context.Context, userID uint64, cards []model.CharacterCard) (*model.CharacterListView, error) {
	out := &model.CharacterListView{Items: make([]model.CharacterView, 0, len(cards))}
	for i := range cards {
		v, err := s.toView(ctx, userID, &cards[i])
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, *v)
	}
	return out, nil
}

// toView 组装一张角色卡的视图：主图、声音不可用为 null，补充图不可用直接略过。
// 依据契约「素材已不可用」：主图缺失等同卡片失效，由前端按 avatar=null 判断。
func (s *CharacterCardService) toView(ctx context.Context, userID uint64, card *model.CharacterCard) (*model.CharacterView, error) {
	out := &model.CharacterView{
		ID:          idcodec.ID(card.ID),
		Name:        card.Name,
		Description: card.Description,
		Extras:      make([]model.AssetView, 0, len(card.ExtraAssetIDs)),
		CreatedAt:   card.CreatedAt,
		UpdatedAt:   card.UpdatedAt,
	}

	avatar, err := s.assetView(ctx, userID, card.AvatarAssetID)
	if err != nil {
		return nil, err
	}
	out.Avatar = avatar
	for _, id := range card.ExtraAssetIDs {
		extra, err := s.assetView(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		if extra != nil {
			out.Extras = append(out.Extras, *extra)
		}
	}
	if card.VoiceAssetID != nil {
		if out.Voice, err = s.assetView(ctx, userID, *card.VoiceAssetID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// assetView 把已引用素材转成视图；素材已被清理时返回 nil（读取降级），其他错误照常返回。
func (s *CharacterCardService) assetView(ctx context.Context, userID, id uint64) (*model.AssetView, error) {
	a, err := s.assets.GetByID(ctx, userID, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取角色卡素材失败：%w", err)
	}
	return s.views.ViewOf(ctx, a)
}

// normalizeCharacterName 去首尾空白后按字符数校验名称，不合法返回 66005 并写明原因。
func normalizeCharacterName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" {
		return "", errcode.ErrCharacterInvalid.WithMsg("名称为空")
	}
	if utf8.RuneCountInString(name) > maxCharacterNameLen {
		return "", errcode.ErrCharacterInvalid.WithMsg("名称过长")
	}
	return name, nil
}

// checkCharacterDescription 校验设定长度，不合法返回 66005。
func checkCharacterDescription(desc string) error {
	if utf8.RuneCountInString(desc) > maxCharacterDescLen {
		return errcode.ErrCharacterInvalid.WithMsg("设定过长")
	}
	return nil
}

// checkCharacterExtras 校验补充图的结构规则：≤ 4 张、不重复、不含主图。
func checkCharacterExtras(avatar uint64, extras []uint64) error {
	if len(extras) > maxCharacterExtraAssets {
		return errcode.ErrCharacterInvalid.WithMsg("补充图超过 4 张")
	}
	seen := make(map[uint64]struct{}, len(extras))
	for _, id := range extras {
		if id == avatar {
			return errcode.ErrCharacterInvalid.WithMsg("补充图含主图")
		}
		if _, ok := seen[id]; ok {
			return errcode.ErrCharacterInvalid.WithMsg("补充图重复")
		}
		seen[id] = struct{}{}
	}
	return nil
}

// applyCharacterVoice 解析 PATCH 里的声音字段：没传不改、null 清空、数字改成新声音。
// 返回是否改动了声音，供调用方只对新声音做归属校验。格式不对按参数错误返回。
func applyCharacterVoice(dst **uint64, raw json.RawMessage) (bool, error) {
	if raw == nil {
		return false, nil
	}
	if string(raw) == "null" {
		*dst = nil
		return true, nil
	}
	var id uint64
	if err := json.Unmarshal(raw, &id); err != nil || id == 0 {
		return false, errcode.ErrInvalidParams.WithMsg("声音 ID 格式错误")
	}
	*dst = &id
	return true, nil
}
