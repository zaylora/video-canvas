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

func (r *fakeTransferCanvasRepo) Update(context.Context, uint64, uint64, uint64, map[string]any) (*model.CanvasProject, error) {
	return nil, nil
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

const transferUser = uint64(1)

// transferHash 返回素材 ID 对应的假 SHA-256：64 位小写十六进制，能通过文档校验。
func transferHash(id uint64) string { return fmt.Sprintf("%064x", id) }

// transferEnv 是一个导出测试环境：用户 1 有画布 5，里面一张图片节点（素材 101）、一个内嵌角色数据的角色节点（主图 201、补充图 202、音频音色 203）和一个组节点。
type transferEnv struct {
	svc      *service.CanvasTransferService
	assets   *fakeTransferAssets
	canvases *fakeTransferCanvasRepo
}

// 一份接近真实保存格式的画布内容：有 canvas / group 节点、连线和视口。
func transferPayload() string {
	return `{
  "nodes": [
    {"id":"n1","type":"canvas","position":{"x":10,"y":20},"data":{"kind":"image","assetId":"101","src":"/files/u1/a.png","status":"success"}},
    {"id":"n2","type":"canvas","position":{"x":300,"y":20},"data":{"kind":"character","label":"小明","assetId":"201","src":"/files/u1/201",
      "character":{"name":"小明","look":"基础形象","description":"一个少年",
        "extras":[{"assetId":"202","src":"/files/u1/202"}],
        "voice":{"mode":"audio","assetId":"203","src":"/files/u1/203","origin":"upload"}}}},
    {"id":"g1","type":"group","position":{"x":0,"y":0},"data":{"label":"分组"}}
  ],
  "edges": [{"id":"e1","source":"n1","target":"n2"}],
  "viewport": {"x":0,"y":0,"zoom":1}
}`
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
	}
	e.svc = service.NewCanvasTransferService(service.CanvasTransferDeps{
		Canvases:   e.canvases,
		Assets:     e.assets,
		AppVersion: "v1.2.3",
		Limits:     canvasdoc.DefaultLimits(),
		Now:        func() time.Time { return time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC) },
	})
	return e
}

func hashedSet(f *fakeTransferAssets) map[uint64]bool {
	m := map[uint64]bool{}
	for _, id := range f.hashed {
		m[id] = true
	}
	return m
}

func TestCanvasTransfer_Export_Success(t *testing.T) {
	e := newTransferEnv(t, transferPayload())

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
	char := doc.Canvas.Nodes[1]["data"].(map[string]any)
	if char["assetHash"] != transferHash(201) {
		t.Errorf("角色节点主图应换成哈希: %v", char)
	}
	if c, _ := char["character"].(map[string]any); c["name"] != "小明" || c["description"] != "一个少年" {
		t.Errorf("角色数据应随节点导出: %v", char)
	}
	if res.Report.SkippedAssets != 0 {
		t.Errorf("skipped_assets = %d，want 0", res.Report.SkippedAssets)
	}
	// 画布里不能留下原环境的素材 ID、文件地址
	node := doc.Canvas.Nodes[0]["data"].(map[string]any)
	if node["assetHash"] != transferHash(101) {
		t.Errorf("图片节点应带 assetHash: %v", node)
	}
	if err := doc.Validate(canvasdoc.DefaultLimits()); err != nil {
		t.Errorf("导出的文档应能通过校验: %v", err)
	}
}

func TestCanvasTransfer_Export_NoOriginalIDsLeak(t *testing.T) {
	e := newTransferEnv(t, transferPayload())

	res, err := e.svc.Export(context.Background(), transferUser, 5)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	// 序列化后的整份文档里不能出现原环境的素材 ID（101/201/202/203）
	b, err := json.Marshal(res.Document)
	if err != nil {
		t.Fatalf("序列化文档: %v", err)
	}
	raw := string(b)
	for _, leak := range []string{`"101"`, `"201"`, `"202"`, `"203"`, "assetId", "characterId", "/files/"} {
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
	e := newTransferEnv(t, transferPayload())

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
	e := newTransferEnv(t, transferPayload())
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
	e := newTransferEnv(t, transferPayload())
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

func TestCanvasTransfer_Export_CharacterAssetsAreHashed(t *testing.T) {
	e := newTransferEnv(t, transferPayload())

	res, err := e.svc.Export(context.Background(), transferUser, 5)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	// 角色节点内嵌的主图、补充图（extras[].assetId）和音频音色（voice.assetId）都要经过 EnsureSha256，否则导出漏文件
	hashed := hashedSet(e.assets)
	for _, id := range []uint64{201, 202, 203} {
		if !hashed[id] {
			t.Errorf("角色素材 %d 应经过 EnsureSha256", id)
		}
	}
	c := res.Document.Canvas.Nodes[1]["data"].(map[string]any)["character"].(map[string]any)
	extras, _ := c["extras"].([]any)
	if len(extras) != 1 || extras[0].(map[string]any)["assetHash"] != transferHash(202) {
		t.Errorf("补充图引用不对: %v", c["extras"])
	}
	voice, _ := c["voice"].(map[string]any)
	if voice["mode"] != "audio" || voice["assetHash"] != transferHash(203) || voice["origin"] != "upload" {
		t.Errorf("音频音色引用不对: %v", c["voice"])
	}
	for _, hash := range []string{transferHash(201), transferHash(202), transferHash(203)} {
		if _, ok := res.Document.Assets[hash]; !ok {
			t.Errorf("assets 缺少角色素材 %s", hash)
		}
	}
}

func TestCanvasTransfer_Export_UnreadableCharacterAssetsDegrade(t *testing.T) {
	e := newTransferEnv(t, transferPayload())
	e.assets.errs[202] = fmt.Errorf("%w: 文件丢了", service.ErrAssetUnreadable)
	e.assets.errs[203] = fmt.Errorf("%w: 文件丢了", service.ErrAssetUnreadable)

	res, err := e.svc.Export(context.Background(), transferUser, 5)
	if err != nil {
		t.Fatalf("角色素材读不到不该让导出失败: %v", err)
	}
	c := res.Document.Canvas.Nodes[1]["data"].(map[string]any)["character"].(map[string]any)
	if extras, _ := c["extras"].([]any); len(extras) != 0 {
		t.Errorf("读不到的补充图应去掉: %v", c["extras"])
	}
	if v, ok := c["voice"]; !ok || v != nil {
		t.Errorf("读不到的音频音色应为 null: %v", c["voice"])
	}
	if res.Report.SkippedAssets != 2 {
		t.Errorf("skipped_assets = %d，want 2", res.Report.SkippedAssets)
	}
}

// withLimits 用给定上限重建导出服务，其余依赖不变。
func (e *transferEnv) withLimits(l canvasdoc.Limits) *transferEnv {
	e.svc = service.NewCanvasTransferService(service.CanvasTransferDeps{
		Canvases: e.canvases, Assets: e.assets, AppVersion: "v1.2.3", Limits: l,
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
			e := newTransferEnv(t, transferPayload())
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

func TestCanvasTransfer_Export_DramaCanvasExcluded(t *testing.T) {
	e := newTransferEnv(t, transferPayload())
	e.canvases.canvases[5].DramaID = new(uint64(42))
	res, err := e.svc.Export(context.Background(), transferUser, 5)
	var businessErr *errcode.Error
	if !errors.As(err, &businessErr) || businessErr.Code != errcode.ErrCanvasNotFound.Code || res != nil || len(e.assets.hashed) != 0 {
		t.Fatalf("短剧画布不应可导出：%+v %v", res, err)
	}
}
