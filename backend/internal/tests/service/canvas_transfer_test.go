package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/canvasdoc"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// fakeTransferCanvasRepo 是导出测试用的画布仓库：按 id + user_id 取，其余方法不会被导出用到。
type fakeTransferCanvasRepo struct {
	canvases map[uint64]*model.CanvasProject
}

func (r *fakeTransferCanvasRepo) Create(context.Context, *model.CanvasProject) error { return nil }

func (r *fakeTransferCanvasRepo) GetByID(_ context.Context, userID, id uint64) (*model.CanvasProject, error) {
	if p, ok := r.canvases[id]; ok && p.UserID == userID {
		return p, nil
	}
	return nil, repository.ErrNotFound
}

func (r *fakeTransferCanvasRepo) List(context.Context, uint64, string, int, int) ([]model.CanvasProjectItem, int64, error) {
	return nil, 0, nil
}

func (r *fakeTransferCanvasRepo) Update(context.Context, uint64, uint64, uint64, map[string]any) error {
	return nil
}

func (r *fakeTransferCanvasRepo) Delete(context.Context, uint64, uint64) error { return nil }

// fakeTransferAssets 是导出测试用的素材服务：按素材 ID 返回预置的素材，并记录被算过哈希的 ID。
type fakeTransferAssets struct {
	assets map[uint64]*model.Asset
	// errs 为某个素材 ID 指定 EnsureSha256 要返回的错误
	errs   map[uint64]error
	hashed []uint64
}

func (f *fakeTransferAssets) EnsureSha256(_ context.Context, userID, assetID uint64) (string, *model.Asset, error) {
	f.hashed = append(f.hashed, assetID)
	if err, ok := f.errs[assetID]; ok {
		return "", nil, err
	}
	a, ok := f.assets[assetID]
	if !ok || a.UserID != userID {
		return "", nil, service.ErrAssetUnreadable
	}
	return a.Sha256, a, nil
}

func (f *fakeTransferAssets) CopyBySha256(context.Context, uint64, string, int64) (*model.Asset, error) {
	return nil, service.ErrAssetNotFoundByHash
}

func (f *fakeTransferAssets) DiscardCopies(context.Context, []*model.Asset) {}

func (f *fakeTransferAssets) FileURL(key string) string { return "/files/" + key }

// fakeTransferCharacters 是导出测试用的角色卡仓库，只返回属于该用户且存在的卡。
type fakeTransferCharacters struct {
	cards map[uint64]model.CharacterCard
}

func (f *fakeTransferCharacters) ListByIDs(_ context.Context, userID uint64, ids []uint64) ([]model.CharacterCard, error) {
	var out []model.CharacterCard
	for _, id := range ids {
		if c, ok := f.cards[id]; ok && c.UserID == userID {
			out = append(out, c)
		}
	}
	return out, nil
}

const transferUser = uint64(1)

// transferHash 返回素材 ID 对应的假 SHA-256：64 位小写十六进制，能通过文档校验。
func transferHash(id uint64) string { return fmt.Sprintf("%064x", id) }

// transferEnv 是一个导出测试环境：用户 1 有画布 5，里面一张图片节点（素材 101）、一个角色卡节点和一个组节点。
type transferEnv struct {
	svc        *service.CanvasTransferService
	assets     *fakeTransferAssets
	characters *fakeTransferCharacters
	canvases   *fakeTransferCanvasRepo
}

// 一份接近真实保存格式的画布内容：有 canvas / group 节点、连线和视口。
func transferPayload(charHexID string) string {
	return fmt.Sprintf(`{
  "nodes": [
    {"id":"n1","type":"canvas","position":{"x":10,"y":20},"data":{"kind":"image","assetId":"101","src":"/files/u1/a.png","status":"success"}},
    {"id":"n2","type":"canvas","position":{"x":300,"y":20},"data":{"kind":"character","characterId":%q}},
    {"id":"g1","type":"group","position":{"x":0,"y":0},"data":{"label":"分组"}}
  ],
  "edges": [{"id":"e1","source":"n1","target":"n2"}],
  "viewport": {"x":0,"y":0,"zoom":1}
}`, charHexID)
}

func newTransferEnv(t *testing.T, payload string) *transferEnv {
	t.Helper()
	asset := func(id uint64, kind, mime string) *model.Asset {
		return &model.Asset{ID: id, UserID: transferUser, Kind: kind, MimeType: mime, ByteSize: int64(id), FileName: fmt.Sprintf("f%d", id), Sha256: transferHash(id), StorageKey: fmt.Sprintf("u1/%d", id)}
	}
	e := &transferEnv{
		canvases: &fakeTransferCanvasRepo{canvases: map[uint64]*model.CanvasProject{
			5: {BaseModel: model.BaseModel{ID: 5}, UserID: transferUser, Title: "我的画布", PayloadJSON: datatypes.JSON(payload), Revision: 3},
		}},
		assets: &fakeTransferAssets{
			assets: map[uint64]*model.Asset{
				101: asset(101, "image", "image/png"),
				201: asset(201, "image", "image/png"),
				202: asset(202, "image", "image/jpeg"),
				203: asset(203, "audio", "audio/mpeg"),
			},
			errs: map[uint64]error{},
		},
		characters: &fakeTransferCharacters{cards: map[uint64]model.CharacterCard{}},
	}
	e.svc = service.NewCanvasTransferService(service.CanvasTransferDeps{
		Canvases:   e.canvases,
		Assets:     e.assets,
		Characters: e.characters,
		AppVersion: "v1.2.3",
		Limits:     canvasdoc.DefaultLimits(),
		Now:        func() time.Time { return time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC) },
	})
	return e
}

func (e *transferEnv) addCard(id uint64) {
	voice := uint64(203)
	e.characters.cards[id] = model.CharacterCard{
		ID: id, UserID: transferUser, Name: "小明", Description: "一个少年",
		AvatarAssetID: 201, ExtraAssetIDs: datatypes.JSONSlice[uint64]{202}, VoiceAssetID: &voice,
	}
}

func hashedSet(f *fakeTransferAssets) map[uint64]bool {
	m := map[uint64]bool{}
	for _, id := range f.hashed {
		m[id] = true
	}
	return m
}

func TestCanvasTransfer_Export_Success(t *testing.T) {
	e := newTransferEnv(t, transferPayload(idcodec.Encode(7)))
	e.addCard(7)

	res, err := e.svc.Export(context.Background(), transferUser, 5)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	doc := res.Document
	if doc.Format != canvasdoc.Format || doc.AppVersion != "v1.2.3" || doc.ExportedAt != "2026-10-24T08:00:00Z" {
		t.Errorf("文档头不对: %+v", doc)
	}
	if doc.Canvas.Title != "我的画布" || len(doc.Canvas.Nodes) != 3 || len(doc.Canvas.Edges) != 1 {
		t.Errorf("画布内容不对: %+v", doc.Canvas)
	}
	for _, hash := range []string{transferHash(101), transferHash(201), transferHash(202), transferHash(203)} {
		if _, ok := doc.Assets[hash]; !ok {
			t.Errorf("assets 缺少 %s: %v", hash, doc.Assets)
		}
	}
	if len(doc.Assets) != 4 {
		t.Errorf("assets 应恰为 4 个: %v", doc.Assets)
	}
	if info := doc.Assets[transferHash(101)]; info.Kind != "image" || info.MimeType != "image/png" || info.FileName != "f101" {
		t.Errorf("素材元信息不对: %+v", info)
	}
	c1, ok := doc.Characters["c1"]
	if !ok || c1.Name != "小明" || c1.Description != "一个少年" || c1.Avatar != transferHash(201) {
		t.Errorf("角色卡不对: %+v", doc.Characters)
	}
	if res.Report.SkippedAssets != 0 {
		t.Errorf("skipped_assets = %d，want 0", res.Report.SkippedAssets)
	}
	// 画布里不能留下原环境的素材 ID、文件地址和角色卡 ID
	node := doc.Canvas.Nodes[0]["data"].(map[string]any)
	if node["assetHash"] != transferHash(101) {
		t.Errorf("图片节点应带 assetHash: %v", node)
	}
	if err := doc.Validate(canvasdoc.DefaultLimits()); err != nil {
		t.Errorf("导出的文档应能通过校验: %v", err)
	}
}

func TestCanvasTransfer_Export_NoOriginalIDsLeak(t *testing.T) {
	e := newTransferEnv(t, transferPayload(idcodec.Encode(7)))
	e.addCard(7)

	res, err := e.svc.Export(context.Background(), transferUser, 5)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	// 序列化后的整份文档里不能出现原环境的素材 ID（101/201/202/203）和角色卡十六进制 ID
	b, err := json.Marshal(res.Document)
	if err != nil {
		t.Fatalf("序列化文档: %v", err)
	}
	raw := string(b)
	for _, leak := range []string{`"101"`, `"201"`, `"202"`, `"203"`, "assetId", "characterId", idcodec.Encode(7), "/files/"} {
		if strings.Contains(raw, leak) {
			t.Errorf("文档里残留了原环境内容 %s", leak)
		}
	}
	for id := range res.Document.Assets {
		if id == "101" || id == "201" || id == "202" || id == "203" {
			t.Errorf("assets 的键应是哈希而不是素材 ID: %s", id)
		}
	}
}

func TestCanvasTransfer_Export_OtherUsersCanvas(t *testing.T) {
	e := newTransferEnv(t, transferPayload(idcodec.Encode(7)))

	_, err := e.svc.Export(context.Background(), 2, 5)
	var ec *errcode.Error
	if !errors.As(err, &ec) || ec.Code != errcode.ErrCanvasNotFound.Code || ec.Code != 30001 {
		t.Fatalf("别人的画布应返回 30001: %v", err)
	}
	if len(e.assets.hashed) != 0 {
		t.Errorf("没有权限时不该去算素材哈希: %v", e.assets.hashed)
	}
	// 不存在的画布同样返回 30001
	_, err = e.svc.Export(context.Background(), transferUser, 99)
	if !errors.As(err, &ec) || ec.Code != 30001 {
		t.Fatalf("不存在的画布应返回 30001: %v", err)
	}
}

func TestCanvasTransfer_Export_UnreadableAssetIsSkipped(t *testing.T) {
	e := newTransferEnv(t, transferPayload(idcodec.Encode(7)))
	e.addCard(7)
	e.assets.errs[101] = fmt.Errorf("%w: 文件丢了", service.ErrAssetUnreadable)

	res, err := e.svc.Export(context.Background(), transferUser, 5)
	if err != nil {
		t.Fatalf("读不到的素材不该让导出失败: %v", err)
	}
	if res.Report.SkippedAssets != 1 {
		t.Errorf("skipped_assets = %d，want 1", res.Report.SkippedAssets)
	}
	if _, ok := res.Document.Assets[transferHash(101)]; ok {
		t.Errorf("读不到的素材不该出现在 assets 里: %v", res.Document.Assets)
	}
	data := res.Document.Canvas.Nodes[0]["data"].(map[string]any)
	if _, ok := data["assetHash"]; ok {
		t.Errorf("读不到的素材节点不该带 assetHash: %v", data)
	}
}

func TestCanvasTransfer_Export_NonUnreadableHashErrorFailsExport(t *testing.T) {
	e := newTransferEnv(t, transferPayload(idcodec.Encode(7)))
	e.addCard(7)
	dbErr := errors.New("数据库连接断开")
	e.assets.errs[101] = dbErr

	res, err := e.svc.Export(context.Background(), transferUser, 5)
	if !errors.Is(err, dbErr) {
		t.Fatalf("非“不可读”错误应让整个导出失败并保留原错误: res=%v err=%v", res, err)
	}
	if res != nil {
		t.Errorf("失败时不该返回结果: %+v", res)
	}
}

func TestCanvasTransfer_Export_DeletedCharacter(t *testing.T) {
	// 节点引用了角色卡 7，但库里已查不到
	e := newTransferEnv(t, transferPayload(idcodec.Encode(7)))

	res, err := e.svc.Export(context.Background(), transferUser, 5)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if res.Document.Characters == nil || len(res.Document.Characters) != 0 {
		t.Errorf("characters 应是空 map: %#v", res.Document.Characters)
	}
	data := res.Document.Canvas.Nodes[1]["data"].(map[string]any)
	if _, ok := data["characterRef"]; ok {
		t.Errorf("已删除的角色卡节点不该有 characterRef: %v", data)
	}
	if _, ok := data["characterId"]; ok {
		t.Errorf("节点不该残留 characterId: %v", data)
	}

	// 解不开的角色卡 ID 同样按已删除处理，不报错
	e2 := newTransferEnv(t, transferPayload("not-a-valid-id"))
	res, err = e2.svc.Export(context.Background(), transferUser, 5)
	if err != nil {
		t.Fatalf("无效角色卡 ID 不该让导出失败: %v", err)
	}
	if len(res.Document.Characters) != 0 {
		t.Errorf("characters 应为空: %v", res.Document.Characters)
	}
}

func TestCanvasTransfer_Export_CharacterAssetsAreHashed(t *testing.T) {
	e := newTransferEnv(t, transferPayload(idcodec.Encode(7)))
	e.addCard(7)

	res, err := e.svc.Export(context.Background(), transferUser, 5)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	hashed := hashedSet(e.assets)
	for _, id := range []uint64{201, 202, 203} {
		if !hashed[id] {
			t.Errorf("角色卡素材 %d 应经过 EnsureSha256", id)
		}
	}
	c1 := res.Document.Characters["c1"]
	if c1.Avatar != transferHash(201) || len(c1.Extras) != 1 || c1.Extras[0] != transferHash(202) || c1.Voice == nil || *c1.Voice != transferHash(203) {
		t.Errorf("角色卡素材引用不对: %+v", c1)
	}
	for _, hash := range []string{transferHash(201), transferHash(202), transferHash(203)} {
		if _, ok := res.Document.Assets[hash]; !ok {
			t.Errorf("assets 缺少角色卡素材 %s", hash)
		}
	}
}

// withLimits 用给定上限重建导出服务，其余依赖不变。
func (e *transferEnv) withLimits(l canvasdoc.Limits) *transferEnv {
	e.svc = service.NewCanvasTransferService(service.CanvasTransferDeps{
		Canvases: e.canvases, Assets: e.assets, Characters: e.characters, AppVersion: "v1.2.3", Limits: l,
		Now: func() time.Time { return time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC) },
	})
	return e
}

func TestCanvasTransfer_Export_ChecksImportLimits(t *testing.T) {
	// 默认画布：3 个节点、4 个素材（101、201、202、203），素材总大小 101+201+202+203 = 707 字节
	limit := func(edit func(l *canvasdoc.Limits)) canvasdoc.Limits {
		l := canvasdoc.DefaultLimits()
		edit(&l)
		return l
	}
	cases := []struct {
		name   string
		limits canvasdoc.Limits
		reason string // 提示里应出现的具体原因，空表示不应超限
	}{
		{"未超限照常导出", canvasdoc.DefaultLimits(), ""},
		{"恰好等于上限照常导出", limit(func(l *canvasdoc.Limits) { l.MaxNodes, l.MaxAssets, l.MaxAssetBytes = 3, 4, 707 }), ""},
		{"节点超限", limit(func(l *canvasdoc.Limits) { l.MaxNodes = 2 }), "节点数"},
		{"素材数超限", limit(func(l *canvasdoc.Limits) { l.MaxAssets = 3 }), "素材数"},
		{"素材总大小超限", limit(func(l *canvasdoc.Limits) { l.MaxAssetBytes = 706 }), "素材总大小"},
		{"文件超过大小上限", limit(func(l *canvasdoc.Limits) { l.MaxBytes = 100 }), "文件"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTransferEnv(t, transferPayload(idcodec.Encode(7)))
			e.addCard(7)
			res, err := e.withLimits(tc.limits).svc.Export(context.Background(), transferUser, 5)
			if tc.reason == "" {
				if err != nil || res == nil {
					t.Fatalf("不应超限: %v", err)
				}
				return
			}
			var ce *errcode.Error
			if !errors.As(err, &ce) || ce.Code != 30008 {
				t.Fatalf("应返回 30008，实际 %v", err)
			}
			if !strings.Contains(ce.Msg, tc.reason) || strings.Contains(ce.Msg, "画布文件超出限制") {
				t.Errorf("提示应带具体原因 %q 且不带哨兵前缀: %q", tc.reason, ce.Msg)
			}
			if ce.HTTPStatus() != 413 || res != nil {
				t.Errorf("状态码 %d、结果 %v", ce.HTTPStatus(), res)
			}
		})
	}
}
