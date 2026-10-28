package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"video-canvas/internal/canvasdoc"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/service"
)

const importUser = uint64(9)

// fakeImportAssets 是导入测试用的素材服务：按哈希造一份副本，记录每个哈希的复制次数和被回收的副本。
type fakeImportAssets struct {
	// kinds 指定某个哈希的素材种类，缺省为 image
	kinds map[string]string
	// missing 里的哈希视为找不到，errs 指定某个哈希要返回的其他错误
	missing map[string]bool
	errs    map[string]error
	calls   map[string]int
	copies  []*model.Asset
	// discarded 记录每次 DiscardCopies 收到的参数，discardCtxErr 记录当时 ctx.Err()
	discarded     [][]*model.Asset
	discardCtxErr []error
	// afterCopy 非空时在每次复制成功后调用（用来模拟复制途中请求被取消）
	afterCopy func()
	// honorCtx 为真时，ctx 已取消就返回 ctx 的错误：与真实 CopyBySha256 循环开头的 ctx.Err() 检查一致
	honorCtx bool
}

func newFakeImportAssets() *fakeImportAssets {
	return &fakeImportAssets{kinds: map[string]string{}, missing: map[string]bool{}, errs: map[string]error{}, calls: map[string]int{}}
}

func (f *fakeImportAssets) EnsureSha256(context.Context, uint64, uint64) (string, *model.Asset, error) {
	return "", nil, service.ErrAssetUnreadable
}

func (f *fakeImportAssets) CopyBySha256(ctx context.Context, userID uint64, sha string, byteSize int64) (*model.Asset, error) {
	f.calls[sha]++
	if f.honorCtx {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("按哈希复制素材: %w", err)
		}
	}
	if err := f.errs[sha]; err != nil {
		return nil, err
	}
	if f.missing[sha] {
		return nil, service.ErrAssetNotFoundByHash
	}
	kind := f.kinds[sha]
	if kind == "" {
		kind = "image"
	}
	a := &model.Asset{
		UserID: userID, Kind: kind, StorageKey: fmt.Sprintf("u%d/copy-%s-%d", userID, sha[60:], len(f.copies)),
		ByteSize: byteSize, Sha256: sha, Source: model.AssetSourceUpload,
	}
	f.copies = append(f.copies, a)
	if f.afterCopy != nil {
		f.afterCopy()
	}
	return a, nil
}

func (f *fakeImportAssets) DiscardCopies(ctx context.Context, assets []*model.Asset) {
	f.discarded = append(f.discarded, assets)
	f.discardCtxErr = append(f.discardCtxErr, ctx.Err())
}

func (f *fakeImportAssets) FileURL(key string) string { return "/files/" + key }

// importStore 是内存里的「数据库」：素材、画布。
type importStore struct {
	assets   []*model.Asset
	canvases []*model.CanvasProject
	nextID   uint64
}

// fakeImportTx 是内存实现的导入事务：回调出错时恢复到事务开始前的快照，可注入失败。
type fakeImportTx struct {
	st importStore
	// failCanvas 非空时 CreateCanvas 返回它
	failCanvas error
}

func newFakeImportTx() *fakeImportTx {
	return &fakeImportTx{st: importStore{nextID: 1000}}
}

func (f *fakeImportTx) WithTx(_ context.Context, fn func(tx service.TransferTx) error) error {
	snapshot := importStore{
		assets:   slices.Clone(f.st.assets),
		canvases: slices.Clone(f.st.canvases), nextID: f.st.nextID,
	}
	if err := fn(f); err != nil {
		f.st = snapshot
		return err
	}
	return nil
}

func (f *fakeImportTx) id() uint64 {
	f.st.nextID++
	return f.st.nextID
}

func (f *fakeImportTx) CreateAsset(_ context.Context, a *model.Asset) error {
	a.ID = f.id()
	f.st.assets = append(f.st.assets, a)
	return nil
}

func (f *fakeImportTx) CreateCanvas(_ context.Context, p *model.CanvasProject) error {
	if f.failCanvas != nil {
		return f.failCanvas
	}
	p.ID = f.id()
	f.st.canvases = append(f.st.canvases, p)
	return nil
}

// importEnv 是一个导入测试环境。
type importEnv struct {
	svc    *service.CanvasTransferService
	assets *fakeImportAssets
	tx     *fakeImportTx
}

func newImportEnv(t *testing.T, limits canvasdoc.Limits) *importEnv {
	t.Helper()
	e := &importEnv{assets: newFakeImportAssets(), tx: newFakeImportTx()}
	e.svc = service.NewCanvasTransferService(service.CanvasTransferDeps{
		Assets: e.assets, Tx: e.tx, Limits: limits,
		Now: func() time.Time { return time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC) },
	})
	return e
}

// importPayload 是原环境里一张真实形状的画布：图片节点 n1（主素材 101 同时是产物、参考图 102）、
// 内嵌角色数据的角色节点 n2（主图 201、补充图 202、音频音色 203）、导演台 n3（缩略图也是 101）、组节点 g1，以及 n1→n2 的连线。
func importPayload() string {
	return `{
  "nodes": [
    {"id":"g1","type":"group","position":{"x":0,"y":0},"data":{"label":"第一场"}},
    {"id":"n1","type":"canvas","position":{"x":10,"y":20},"data":{
      "kind":"image","assetId":"101","src":"/files/u1/a.png","status":"success","taskId":"77",
      "params":{"ratio":"16:9","images":["102"]},
      "paramAssets":{"102":{"url":"/files/u1/r.png","fileName":"参考.png","mediaType":"image"}},
      "outputs":[{"id":"101","src":"/files/u1/a.png","assetId":"101","mediaType":"image","createdAt":1761300000000}],
      "activeOutputId":"101"}},
    {"id":"n2","type":"canvas","position":{"x":300,"y":20},"data":{"kind":"character","label":"林夏","assetId":"201","src":"/files/u1/201","mediaType":"image",
      "character":{"name":"林夏","look":"基础形象","description":"短发，黑色风衣",
        "extras":[{"assetId":"202","src":"/files/u1/202"}],
        "voice":{"mode":"audio","assetId":"203","src":"/files/u1/203","origin":"upload"}}}},
    {"id":"n3","type":"canvas","position":{"x":600,"y":20},"data":{"kind":"director",
      "scene":{"version":1,"thumb":{"assetId":"101","url":"/files/u1/a.png"}}}}
  ],
  "edges": [{"id":"e1","source":"n1","target":"n2"}],
  "viewport": {"x":5,"y":6,"zoom":2}
}`
}

// importFixture 描述一份导出文档的原始内容，build 用 canvasdoc.Build 把它导出成文件字节。
type importFixture struct {
	title string
	// mutate 非空时在序列化前改写导出的文档，模拟手改过的文件
	mutate func(doc *canvasdoc.Document)
}

func defaultImportFixture() importFixture {
	return importFixture{
		title: "雨夜短片",
	}
}

func (fx importFixture) build(t *testing.T) []byte {
	t.Helper()
	assets := map[string]canvasdoc.AssetEntry{}
	for _, id := range []uint64{101, 102, 201, 202, 203} {
		kind := "image"
		if id == 203 {
			kind = "audio"
		}
		assets[fmt.Sprint(id)] = canvasdoc.AssetEntry{Hash: transferHash(id), Info: canvasdoc.AssetInfo{
			Kind: kind, MimeType: kind + "/x", ByteSize: int64(id), FileName: fmt.Sprintf("f%d", id),
		}}
	}
	doc, _, err := canvasdoc.Build(canvasdoc.ExportInput{
		Title: fx.title, Payload: []byte(importPayload()), Assets: assets,
		AppVersion: "v1", Now: time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if fx.mutate != nil {
		fx.mutate(doc)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// newDefaultImportEnv 创建默认上限的环境，并把 203 标成音频（与夹具一致）。
func newDefaultImportEnv(t *testing.T) *importEnv {
	e := newImportEnv(t, canvasdoc.DefaultLimits())
	e.assets.kinds[transferHash(203)] = "audio"
	return e
}

// importedNodes 解析导入画布的 payload，按节点 ID 返回 data。
func importedNodes(t *testing.T, p *model.CanvasProject) map[string]map[string]any {
	t.Helper()
	var payload struct {
		Nodes []map[string]any `json:"nodes"`
	}
	if err := json.Unmarshal(p.PayloadJSON, &payload); err != nil {
		t.Fatalf("payload 不是合法 JSON: %v", err)
	}
	out := map[string]map[string]any{}
	for _, n := range payload.Nodes {
		data, _ := n["data"].(map[string]any)
		out[n["id"].(string)] = data
	}
	return out
}

// assetKey 返回导入后哈希对应的新素材存储 key（FileURL 的入参）。
func (e *importEnv) assetKey(t *testing.T, hash string) string {
	t.Helper()
	for _, a := range e.tx.st.assets {
		if a.Sha256 == hash {
			return a.StorageKey
		}
	}
	t.Fatalf("没有为哈希 %s 建素材", hash[:8])
	return ""
}

// newAssetID 返回导入后哈希对应的新素材 ID。
func (e *importEnv) newAssetID(t *testing.T, hash string) uint64 {
	t.Helper()
	for _, a := range e.tx.st.assets {
		if a.Sha256 == hash {
			return a.ID
		}
	}
	t.Fatalf("没有为哈希 %s 建素材", hash[:8])
	return 0
}

func TestImport_Success(t *testing.T) {
	e := newDefaultImportEnv(t)
	res, err := e.svc.Import(context.Background(), importUser, defaultImportFixture().build(t))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(e.tx.st.canvases) != 1 {
		t.Fatalf("应建 1 张画布，实际 %d", len(e.tx.st.canvases))
	}
	p := e.tx.st.canvases[0]
	if p.UserID != importUser || p.Revision != 1 || p.Title != "雨夜短片（导入）" {
		t.Errorf("画布归属/版本/标题不对: user=%d rev=%d title=%q", p.UserID, p.Revision, p.Title)
	}
	if uint64(res.Canvas.ID) != p.ID || res.Canvas.Title != p.Title || res.Canvas.Revision != 1 {
		t.Errorf("返回的画布信息不对: %+v", res.Canvas)
	}

	// 素材：5 个哈希全部复制入库，都归导入者
	if res.Report.Assets.Total != 5 || res.Report.Assets.Copied != 5 || len(res.Report.Assets.Missing) != 0 {
		t.Errorf("素材报告不对: %+v", res.Report.Assets)
	}
	for _, a := range e.tx.st.assets {
		if a.UserID != importUser {
			t.Errorf("素材应归导入者: %+v", a)
		}
	}

	// 画布内容里的素材 ID 是新素材 ID
	nodes := importedNodes(t, p)
	mainID := fmt.Sprint(e.newAssetID(t, transferHash(101)))
	if nodes["n1"]["assetId"] != mainID || nodes["n1"]["activeOutputId"] != mainID {
		t.Errorf("n1 素材 ID 应是新 ID %s: %v", mainID, nodes["n1"])
	}
	refID := fmt.Sprint(e.newAssetID(t, transferHash(102)))
	if images := nodes["n1"]["params"].(map[string]any)["images"].([]any); len(images) != 1 || images[0] != refID {
		t.Errorf("参考图应换成新 ID %s: %v", refID, images)
	}
	if _, ok := nodes["n1"]["taskId"]; ok {
		t.Error("原环境任务 ID 不应带进新画布")
	}
	thumb := nodes["n3"]["scene"].(map[string]any)["thumb"].(map[string]any)
	if thumb["assetId"] != mainID {
		t.Errorf("导演台缩略图应换成新 ID: %v", thumb)
	}

	// 角色节点：数据随节点还原，主图、补充图、音频音色都指向新素材，没有任何角色卡行
	char := nodes["n2"]
	if char["assetId"] != fmt.Sprint(e.newAssetID(t, transferHash(201))) || char["src"] != "/files/"+e.assetKey(t, transferHash(201)) {
		t.Errorf("角色节点主图应是新素材: %v", char)
	}
	wantChar := map[string]any{
		"name": "林夏", "look": "基础形象", "description": "短发，黑色风衣",
		"extras": []any{map[string]any{
			"assetId": fmt.Sprint(e.newAssetID(t, transferHash(202))), "src": "/files/" + e.assetKey(t, transferHash(202)),
		}},
		"voice": map[string]any{
			"mode": "audio", "assetId": fmt.Sprint(e.newAssetID(t, transferHash(203))),
			"src": "/files/" + e.assetKey(t, transferHash(203)), "origin": "upload",
		},
	}
	if !reflect.DeepEqual(char["character"], wantChar) {
		t.Errorf("角色数据不对: %#v，期望 %#v", char["character"], wantChar)
	}
	if _, ok := char["characterId"]; ok {
		t.Errorf("角色节点不应带 characterId: %v", char)
	}
	if res.Report.EdgesDropped != 0 || len(e.assets.discarded) != 0 {
		t.Errorf("不应丢连线或回收副本: %+v %v", res.Report, e.assets.discarded)
	}

	// 报告的 JSON 键是 snake_case，空列表输出 [] 而不是 null
	raw, _ := json.Marshal(res)
	for _, want := range []string{`"edges_dropped":0`, `"missing":[]`, `"revision":1`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("结果 JSON 缺少 %s: %s", want, raw)
		}
	}
	if strings.Contains(string(raw), `"characters"`) {
		t.Errorf("导入报告不应再有 characters: %s", raw)
	}
}

func TestImport_SameHashCopiedOnce(t *testing.T) {
	e := newDefaultImportEnv(t)
	if _, err := e.svc.Import(context.Background(), importUser, defaultImportFixture().build(t)); err != nil {
		t.Fatalf("Import: %v", err)
	}
	// 101 出现在 n1 主素材、n1 产物、n3 缩略图三处
	if n := e.assets.calls[transferHash(101)]; n != 1 {
		t.Errorf("同一哈希应只复制一次，实际 %d 次", n)
	}
	if len(e.assets.calls) != 5 {
		t.Errorf("应复制 5 个不同哈希，实际 %v", e.assets.calls)
	}
}

func TestImport_MissingAssetReported(t *testing.T) {
	e := newDefaultImportEnv(t)
	e.assets.missing[transferHash(102)] = true
	res, err := e.svc.Import(context.Background(), importUser, defaultImportFixture().build(t))
	if err != nil {
		t.Fatalf("缺素材不应导致导入失败: %v", err)
	}
	a := res.Report.Assets
	if a.Total != 5 || a.Copied != 4 || len(a.Missing) != 1 || a.Missing[0].NodeID != "n1" || a.Missing[0].FileName != "f102" {
		t.Errorf("缺失报告不对: %+v", a)
	}
	nodes := importedNodes(t, e.tx.st.canvases[0])
	if images := nodes["n1"]["params"].(map[string]any)["images"].([]any); len(images) != 0 {
		t.Errorf("缺失的参考图应被去掉: %v", images)
	}
}

func TestImport_CharacterAssetsMissing(t *testing.T) {
	t.Run("主图找不到：节点保留、角色数据保留，主图引用去掉并报告缺失", func(t *testing.T) {
		e := newDefaultImportEnv(t)
		e.assets.missing[transferHash(201)] = true
		res, err := e.svc.Import(context.Background(), importUser, defaultImportFixture().build(t))
		if err != nil {
			t.Fatalf("Import: %v", err)
		}
		char := importedNodes(t, e.tx.st.canvases[0])["n2"]
		if _, ok := char["assetId"]; ok {
			t.Errorf("主图缺失时不应有 assetId: %v", char)
		}
		if c := char["character"].(map[string]any); c["name"] != "林夏" || len(c["extras"].([]any)) != 1 {
			t.Errorf("其余角色数据应保留: %v", c)
		}
		want := []service.ImportMissingAsset{{NodeID: "n2", FileName: "f201"}}
		if !slices.Equal(res.Report.Assets.Missing, want) {
			t.Errorf("missing = %+v，期望 %+v", res.Report.Assets.Missing, want)
		}
	})

	t.Run("补充图找不到只少这一张", func(t *testing.T) {
		e := newDefaultImportEnv(t)
		e.assets.missing[transferHash(202)] = true
		res, err := e.svc.Import(context.Background(), importUser, defaultImportFixture().build(t))
		if err != nil {
			t.Fatalf("Import: %v", err)
		}
		c := importedNodes(t, e.tx.st.canvases[0])["n2"]["character"].(map[string]any)
		if extras, ok := c["extras"].([]any); !ok || len(extras) != 0 || c["voice"] == nil {
			t.Errorf("应只少补充图: %v", c)
		}
		if len(res.Report.Assets.Missing) != 1 || res.Report.Assets.Missing[0].FileName != "f202" {
			t.Errorf("missing = %+v", res.Report.Assets.Missing)
		}
	})

	t.Run("音频音色找不到时按无音色", func(t *testing.T) {
		e := newDefaultImportEnv(t)
		e.assets.missing[transferHash(203)] = true
		if _, err := e.svc.Import(context.Background(), importUser, defaultImportFixture().build(t)); err != nil {
			t.Fatalf("Import: %v", err)
		}
		c := importedNodes(t, e.tx.st.canvases[0])["n2"]["character"].(map[string]any)
		if v, ok := c["voice"]; !ok || v != nil {
			t.Errorf("voice = %#v，缺失时应为 null", c["voice"])
		}
	})
}

// 角色卡下线前导出的旧文件：顶层带 characters 字典、节点带 characterRef。仍能导入，字典被忽略，节点保留且不带 characterId。
func TestImport_LegacyCharacterFileStillImports(t *testing.T) {
	e := newDefaultImportEnv(t)
	var doc map[string]any
	if err := json.Unmarshal(defaultImportFixture().build(t), &doc); err != nil {
		t.Fatal(err)
	}
	doc["characters"] = map[string]any{"c1": map[string]any{
		"name": "林夏", "description": "", "avatar": transferHash(201), "extras": []string{}, "voice": nil,
	}}
	for _, n := range doc["canvas"].(map[string]any)["nodes"].([]any) {
		if node := n.(map[string]any); node["id"] == "n2" {
			data := node["data"].(map[string]any)
			delete(data, "character")
			data["characterRef"] = "c1"
		}
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Import(context.Background(), importUser, raw); err != nil {
		t.Fatalf("旧文件应能导入: %v", err)
	}
	n2 := importedNodes(t, e.tx.st.canvases[0])["n2"]
	for _, key := range []string{"characterRef", "characterId"} {
		if _, ok := n2[key]; ok {
			t.Errorf("旧角色节点不应带 %s: %v", key, n2)
		}
	}
}

func TestImport_TxFailureDiscardsCopies(t *testing.T) {
	t.Run("建画布失败回收全部副本、不留任何行", func(t *testing.T) {
		e := newDefaultImportEnv(t)
		e.tx.failCanvas = errors.New("db down")
		_, err := e.svc.Import(context.Background(), importUser, defaultImportFixture().build(t))
		if err == nil || !strings.Contains(err.Error(), "db down") {
			t.Fatalf("应返回事务错误，实际 %v", err)
		}
		if len(e.assets.discarded) != 1 || !slices.Equal(e.assets.discarded[0], e.assets.copies) || len(e.assets.copies) != 5 {
			t.Errorf("应回收本次复制出的 5 份副本: discarded=%v copies=%v", e.assets.discarded, e.assets.copies)
		}
		if len(e.tx.st.canvases) != 0 || len(e.tx.st.assets) != 0 {
			t.Errorf("事务失败后不应留下任何行: %+v", e.tx.st)
		}
	})

	t.Run("复制出错回收已复制的副本", func(t *testing.T) {
		e := newDefaultImportEnv(t)
		// 哈希按字典序复制：101 < 102 < 201，让第三个出错
		e.assets.errs[transferHash(201)] = errors.New("storage down")
		_, err := e.svc.Import(context.Background(), importUser, defaultImportFixture().build(t))
		if err == nil || !strings.Contains(err.Error(), "storage down") {
			t.Fatalf("应返回复制错误，实际 %v", err)
		}
		var ce *errcode.Error
		if errors.As(err, &ce) {
			t.Errorf("存储故障不是业务错误，不应转成错误码: %v", err)
		}
		if len(e.assets.discarded) != 1 || len(e.assets.discarded[0]) != 2 || !slices.Equal(e.assets.discarded[0], e.assets.copies) {
			t.Errorf("应回收已复制的 2 份副本: %v", e.assets.discarded)
		}
		if len(e.tx.st.canvases) != 0 {
			t.Error("不应建画布")
		}
	})
}

func TestImport_RejectsBadDocuments(t *testing.T) {
	valid := defaultImportFixture().build(t)
	small := canvasdoc.DefaultLimits()
	small.MaxNodes = 2
	tiny := canvasdoc.DefaultLimits()
	tiny.MaxBytes = int64(len(valid) - 1)
	replace := func(old, repl string) []byte {
		if !strings.Contains(string(valid), old) {
			t.Fatalf("夹具里找不到 %s", old)
		}
		return []byte(strings.Replace(string(valid), old, repl, 1))
	}

	cases := []struct {
		name   string
		raw    []byte
		limits canvasdoc.Limits
		code   int
	}{
		{"非 JSON", []byte("not json"), canvasdoc.DefaultLimits(), 30005},
		{"format 不对", replace(`"format":"video-canvas/canvas"`, `"format":"other"`), canvasdoc.DefaultLimits(), 30005},
		{"版本过高", replace(`"version":1`, `"version":2`), canvasdoc.DefaultLimits(), 30006},
		{"角色补充图引用悬空", replace(transferHash(202), transferHash(999)), canvasdoc.DefaultLimits(), 30005},
		{"夹带原环境字段", replace(`"kind":"director"`, `"kind":"director","x-extra":{"assetId":"42"}`), canvasdoc.DefaultLimits(), 30005},
		{"节点超限", valid, small, 30007},
		{"原文超过大小上限", valid, tiny, 30007},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newImportEnv(t, tc.limits)
			_, err := e.svc.Import(context.Background(), importUser, tc.raw)
			var ce *errcode.Error
			if !errors.As(err, &ce) || ce.Code != tc.code {
				t.Fatalf("应返回 %d，实际 %v", tc.code, err)
			}
			if ce.Msg == "" || strings.Contains(ce.Msg, "画布文件不合法") {
				t.Errorf("提示应是具体原因且不带哨兵前缀: %q", ce.Msg)
			}
			if len(e.assets.calls) != 0 || len(e.tx.st.canvases) != 0 {
				t.Error("文件不合法时不应复制素材或建画布")
			}
		})
	}
}

func TestImport_TitleTruncation(t *testing.T) {
	cases := []struct {
		name, title, want string
	}{
		{"70 字截断原标题部分", strings.Repeat("标", 70), strings.Repeat("标", 56) + "（导入）"},
		{"空标题", "   ", "未命名画布（导入）"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newDefaultImportEnv(t)
			fx := defaultImportFixture()
			fx.title = tc.title
			res, err := e.svc.Import(context.Background(), importUser, fx.build(t))
			if err != nil {
				t.Fatalf("Import: %v", err)
			}
			if res.Canvas.Title != tc.want || utf8.RuneCountInString(res.Canvas.Title) > 60 {
				t.Errorf("标题 = %q，期望 %q", res.Canvas.Title, tc.want)
			}
		})
	}
}

func TestImport_IgnoresUnreferencedEntries(t *testing.T) {
	e := newDefaultImportEnv(t)
	fx := defaultImportFixture()
	// 手改文件：多塞一个没人引用的素材
	unrefAsset := transferHash(901)
	fx.mutate = func(doc *canvasdoc.Document) {
		doc.Assets[unrefAsset] = canvasdoc.AssetInfo{Kind: "image", MimeType: "image/png", ByteSize: 901, FileName: "x.png"}
	}
	res, err := e.svc.Import(context.Background(), importUser, fx.build(t))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if n := e.assets.calls[unrefAsset]; n != 0 {
		t.Errorf("未被引用的素材 %s 不应复制，实际复制 %d 次", unrefAsset[60:], n)
	}
	if len(e.assets.calls) != 5 || len(e.tx.st.assets) != 5 {
		t.Errorf("应只复制、入库被引用的 5 个素材: calls=%v rows=%d", e.assets.calls, len(e.tx.st.assets))
	}
	if a := res.Report.Assets; a.Total != 5 || a.Copied != 5 {
		t.Errorf("素材总数应只算被引用的: %+v", a)
	}
}

func TestImport_DiscardCopiesSurvivesCancelledRequest(t *testing.T) {
	t.Run("事务失败时请求已取消", func(t *testing.T) {
		e := newDefaultImportEnv(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		e.assets.afterCopy = cancel // 第一份复制完请求就被取消（如用户关掉页面）
		e.tx.failCanvas = errors.New("db down")
		if _, err := e.svc.Import(ctx, importUser, defaultImportFixture().build(t)); err == nil {
			t.Fatal("应返回事务错误")
		}
		if len(e.assets.discardCtxErr) != 1 || e.assets.discardCtxErr[0] != nil {
			t.Errorf("回收副本应拿到未取消的 ctx，实际 %v", e.assets.discardCtxErr)
		}
	})

	t.Run("复制出错时请求已取消", func(t *testing.T) {
		e := newDefaultImportEnv(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		e.assets.afterCopy = cancel
		e.assets.errs[transferHash(102)] = errors.New("storage down")
		if _, err := e.svc.Import(ctx, importUser, defaultImportFixture().build(t)); err == nil {
			t.Fatal("应返回复制错误")
		}
		if len(e.assets.discardCtxErr) != 1 || e.assets.discardCtxErr[0] != nil {
			t.Errorf("回收副本应拿到未取消的 ctx，实际 %v", e.assets.discardCtxErr)
		}
	})
}

// 用户在导入途中点了取消：请求上下文被取消，复制应当停下，已复制的副本全部回收，不留画布、素材和角色卡。
func TestImport_CancelledMidwayLeavesNothing(t *testing.T) {
	t.Run("复制途中取消：停止复制、回收已复制的副本、不建任何行", func(t *testing.T) {
		e := newDefaultImportEnv(t)
		e.assets.honorCtx = true
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := 0
		e.assets.afterCopy = func() {
			done++
			if done == 2 { // 哈希按字典序复制：第二份复制完时用户点了取消
				cancel()
			}
		}

		_, err := e.svc.Import(ctx, importUser, defaultImportFixture().build(t))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("应返回 context.Canceled，实际 %v", err)
		}
		var ce *errcode.Error
		if errors.As(err, &ce) {
			t.Errorf("取消不是业务错误，不应转成错误码: %v", err)
		}
		if len(e.assets.copies) != 2 {
			t.Errorf("取消后不应再复制：已复制 %d 份，期望 2 份", len(e.assets.copies))
		}
		if len(e.assets.discarded) != 1 || !slices.Equal(e.assets.discarded[0], e.assets.copies) {
			t.Errorf("应回收已复制的 2 份副本: %v", e.assets.discarded)
		}
		if len(e.assets.discardCtxErr) != 1 || e.assets.discardCtxErr[0] != nil {
			t.Errorf("回收副本应拿到未取消的 ctx，实际 %v", e.assets.discardCtxErr)
		}
		if len(e.tx.st.canvases) != 0 || len(e.tx.st.assets) != 0 {
			t.Errorf("取消后不应留下任何行: %+v", e.tx.st)
		}
	})

	t.Run("请求在开始前就已取消：一份都不复制，也没有可回收的东西", func(t *testing.T) {
		e := newDefaultImportEnv(t)
		e.assets.honorCtx = true
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := e.svc.Import(ctx, importUser, defaultImportFixture().build(t))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("应返回 context.Canceled，实际 %v", err)
		}
		if len(e.assets.copies) != 0 || len(e.tx.st.canvases) != 0 {
			t.Errorf("不应复制也不应建画布: copies=%d canvases=%d", len(e.assets.copies), len(e.tx.st.canvases))
		}
	})
}
