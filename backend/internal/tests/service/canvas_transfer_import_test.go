package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"video-canvas/internal/canvasdoc"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
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
	// discarded 记录每次 DiscardCopies 收到的参数
	discarded [][]*model.Asset
}

func newFakeImportAssets() *fakeImportAssets {
	return &fakeImportAssets{kinds: map[string]string{}, missing: map[string]bool{}, errs: map[string]error{}, calls: map[string]int{}}
}

func (f *fakeImportAssets) EnsureSha256(context.Context, uint64, uint64) (string, *model.Asset, error) {
	return "", nil, service.ErrAssetUnreadable
}

func (f *fakeImportAssets) CopyBySha256(_ context.Context, userID uint64, sha string, byteSize int64) (*model.Asset, error) {
	f.calls[sha]++
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
	return a, nil
}

func (f *fakeImportAssets) DiscardCopies(_ context.Context, assets []*model.Asset) {
	f.discarded = append(f.discarded, assets)
}

func (f *fakeImportAssets) FileURL(key string) string { return "/files/" + key }

// importStore 是内存里的「数据库」：素材、角色卡、画布。
type importStore struct {
	assets   []*model.Asset
	cards    []*model.CharacterCard
	canvases []*model.CanvasProject
	nextID   uint64
}

// fakeImportTx 是内存实现的导入事务：回调出错时恢复到事务开始前的快照，可注入失败。
type fakeImportTx struct {
	st importStore
	// failCanvas 非空时 CreateCanvas 返回它
	failCanvas error
	// dupOnce 里的名字在 CreateCharacter 时第一次假装撞唯一索引（模拟并发下另一个请求抢先建了同名卡）
	dupOnce map[string]bool
}

func newFakeImportTx() *fakeImportTx {
	return &fakeImportTx{st: importStore{nextID: 1000}, dupOnce: map[string]bool{}}
}

func (f *fakeImportTx) WithTx(_ context.Context, fn func(tx service.TransferTx) error) error {
	snapshot := importStore{
		assets: slices.Clone(f.st.assets), cards: slices.Clone(f.st.cards),
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

func (f *fakeImportTx) CountCharacters(_ context.Context, userID uint64) (int64, error) {
	var n int64
	for _, c := range f.st.cards {
		if c.UserID == userID {
			n++
		}
	}
	return n, nil
}

func (f *fakeImportTx) CharacterNameExists(_ context.Context, userID uint64, name string) (bool, error) {
	for _, c := range f.st.cards {
		if c.UserID == userID && strings.EqualFold(c.Name, name) {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeImportTx) CreateCharacter(ctx context.Context, c *model.CharacterCard) error {
	if f.dupOnce[c.Name] {
		delete(f.dupOnce, c.Name)
		return repository.ErrDuplicate
	}
	if exists, _ := f.CharacterNameExists(ctx, c.UserID, c.Name); exists {
		return repository.ErrDuplicate
	}
	c.ID = f.id()
	f.st.cards = append(f.st.cards, c)
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

// addCard 给用户预置一张角色卡。
func (e *importEnv) addCard(userID uint64, name string) {
	e.tx.st.nextID++
	e.tx.st.cards = append(e.tx.st.cards, &model.CharacterCard{ID: e.tx.st.nextID, UserID: userID, Name: name, AvatarAssetID: 1})
}

// importPayload 是原环境里一张真实形状的画布：图片节点 n1（主素材 101 同时是产物、参考图 102）、
// 角色卡节点 n2、导演台 n3（缩略图也是 101）、组节点 g1，以及 n1→n2 的连线。
func importPayload(cardHex string) string {
	return fmt.Sprintf(`{
  "nodes": [
    {"id":"g1","type":"group","position":{"x":0,"y":0},"data":{"label":"第一场"}},
    {"id":"n1","type":"canvas","position":{"x":10,"y":20},"data":{
      "kind":"image","assetId":"101","src":"/files/u1/a.png","status":"success","taskId":"77",
      "params":{"ratio":"16:9","images":["102"]},
      "paramAssets":{"102":{"url":"/files/u1/r.png","fileName":"参考.png","mediaType":"image"}},
      "outputs":[{"id":"101","src":"/files/u1/a.png","assetId":"101","mediaType":"image","createdAt":1761300000000}],
      "activeOutputId":"101"}},
    {"id":"n2","type":"canvas","position":{"x":300,"y":20},"data":{"kind":"character","characterId":%q}},
    {"id":"n3","type":"canvas","position":{"x":600,"y":20},"data":{"kind":"director",
      "scene":{"version":1,"thumb":{"assetId":"101","url":"/files/u1/a.png"}}}}
  ],
  "edges": [{"id":"e1","source":"n1","target":"n2"}],
  "viewport": {"x":5,"y":6,"zoom":2}
}`, cardHex)
}

// importFixture 描述一份导出文档的原始内容，build 用 canvasdoc.Build 把它导出成文件字节。
type importFixture struct {
	title string
	card  canvasdoc.CharacterEntry
	// mutate 非空时在序列化前改写导出的文档，模拟手改过的文件
	mutate func(doc *canvasdoc.Document)
}

func defaultImportFixture() importFixture {
	return importFixture{
		title: "雨夜短片",
		card: canvasdoc.CharacterEntry{
			Name: "林夏", Description: "短发，黑色风衣", AvatarAssetID: "201",
			ExtraAssetIDs: []string{"202"}, VoiceAssetID: "203",
		},
	}
}

func (fx importFixture) build(t *testing.T) []byte {
	t.Helper()
	cardHex := idcodec.Encode(7)
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
		Title: fx.title, Payload: []byte(importPayload(cardHex)), Assets: assets,
		Characters: map[string]canvasdoc.CharacterEntry{cardHex: fx.card},
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

// cardsOf 返回用户的全部角色卡。
func (e *importEnv) cardsOf(userID uint64) []*model.CharacterCard {
	var out []*model.CharacterCard
	for _, c := range e.tx.st.cards {
		if c.UserID == userID {
			out = append(out, c)
		}
	}
	return out
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

	// 角色卡：复制成导入者的新卡，素材都指向新素材
	cards := e.cardsOf(importUser)
	if len(cards) != 1 || res.Report.Characters.Created != 1 || len(res.Report.Characters.Renamed) != 0 || len(res.Report.Characters.Invalid) != 0 {
		t.Fatalf("角色卡结果不对: cards=%v report=%+v", cards, res.Report.Characters)
	}
	c := cards[0]
	if c.Name != "林夏" || c.Description != "短发，黑色风衣" || c.AvatarAssetID != e.newAssetID(t, transferHash(201)) {
		t.Errorf("角色卡内容不对: %+v", c)
	}
	if !slices.Equal([]uint64(c.ExtraAssetIDs), []uint64{e.newAssetID(t, transferHash(202))}) {
		t.Errorf("补充图不对: %v", c.ExtraAssetIDs)
	}
	if c.VoiceAssetID == nil || *c.VoiceAssetID != e.newAssetID(t, transferHash(203)) {
		t.Errorf("声音不对: %v", c.VoiceAssetID)
	}
	if nodes["n2"]["characterId"] != idcodec.Encode(c.ID) {
		t.Errorf("角色卡节点应指向新卡: %v", nodes["n2"])
	}
	if res.Report.EdgesDropped != 0 || len(e.assets.discarded) != 0 {
		t.Errorf("不应丢连线或回收副本: %+v %v", res.Report, e.assets.discarded)
	}

	// 报告的 JSON 键是 snake_case，空列表输出 [] 而不是 null
	raw, _ := json.Marshal(res)
	for _, want := range []string{`"edges_dropped":0`, `"missing":[]`, `"renamed":[]`, `"invalid":[]`, `"revision":1`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("结果 JSON 缺少 %s: %s", want, raw)
		}
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

func TestImport_CharacterRename(t *testing.T) {
	t.Run("同名加后缀，第二次导入继续递增", func(t *testing.T) {
		e := newDefaultImportEnv(t)
		e.addCard(importUser, "林夏")
		e.addCard(importUser+1, "林夏 (2)") // 别人的卡不影响改名
		doc := defaultImportFixture().build(t)

		res, err := e.svc.Import(context.Background(), importUser, doc)
		if err != nil {
			t.Fatalf("Import: %v", err)
		}
		want := []service.ImportRename{{From: "林夏", To: "林夏 (2)"}}
		if !slices.Equal(res.Report.Characters.Renamed, want) {
			t.Errorf("renamed = %+v，期望 %+v", res.Report.Characters.Renamed, want)
		}
		res, err = e.svc.Import(context.Background(), importUser, doc)
		if err != nil {
			t.Fatalf("第二次 Import: %v", err)
		}
		want = []service.ImportRename{{From: "林夏", To: "林夏 (3)"}}
		if !slices.Equal(res.Report.Characters.Renamed, want) {
			t.Errorf("第二次 renamed = %+v，期望 %+v", res.Report.Characters.Renamed, want)
		}
	})

	t.Run("不区分大小写", func(t *testing.T) {
		e := newDefaultImportEnv(t)
		e.addCard(importUser, "alice")
		fx := defaultImportFixture()
		fx.card.Name = "Alice"
		res, err := e.svc.Import(context.Background(), importUser, fx.build(t))
		if err != nil {
			t.Fatalf("Import: %v", err)
		}
		if r := res.Report.Characters.Renamed; len(r) != 1 || r[0].To != "Alice (2)" {
			t.Errorf("renamed = %+v", r)
		}
	})

	t.Run("64 字的名字改名后仍不超过 64 字", func(t *testing.T) {
		e := newDefaultImportEnv(t)
		long := strings.Repeat("长", 64)
		e.addCard(importUser, long)
		fx := defaultImportFixture()
		fx.card.Name = long
		res, err := e.svc.Import(context.Background(), importUser, fx.build(t))
		if err != nil {
			t.Fatalf("Import: %v", err)
		}
		got := res.Report.Characters.Renamed[0].To
		if utf8.RuneCountInString(got) > 64 || got != strings.Repeat("长", 60)+" (2)" {
			t.Errorf("改名结果 %q（%d 字）", got, utf8.RuneCountInString(got))
		}
	})

	t.Run("撞唯一索引视为重名换下一个后缀", func(t *testing.T) {
		e := newDefaultImportEnv(t)
		e.tx.dupOnce["林夏"] = true
		res, err := e.svc.Import(context.Background(), importUser, defaultImportFixture().build(t))
		if err != nil {
			t.Fatalf("Import: %v", err)
		}
		want := []service.ImportRename{{From: "林夏", To: "林夏 (2)"}}
		if !slices.Equal(res.Report.Characters.Renamed, want) {
			t.Errorf("renamed = %+v，期望 %+v", res.Report.Characters.Renamed, want)
		}
	})

	t.Run("超长名字和设定截断", func(t *testing.T) {
		e := newDefaultImportEnv(t)
		fx := defaultImportFixture()
		fx.card.Name = "  " + strings.Repeat("名", 70) + "  "
		fx.card.Description = strings.Repeat("设", 600)
		if _, err := e.svc.Import(context.Background(), importUser, fx.build(t)); err != nil {
			t.Fatalf("Import: %v", err)
		}
		c := e.cardsOf(importUser)[0]
		if c.Name != strings.Repeat("名", 64) || utf8.RuneCountInString(c.Description) != 500 {
			t.Errorf("名字 %d 字、设定 %d 字", utf8.RuneCountInString(c.Name), utf8.RuneCountInString(c.Description))
		}
	})
}

func TestImport_CharacterAvatarMissing(t *testing.T) {
	t.Run("主图找不到不建卡", func(t *testing.T) {
		e := newDefaultImportEnv(t)
		e.assets.missing[transferHash(201)] = true
		res, err := e.svc.Import(context.Background(), importUser, defaultImportFixture().build(t))
		if err != nil {
			t.Fatalf("Import: %v", err)
		}
		if len(e.cardsOf(importUser)) != 0 || res.Report.Characters.Created != 0 {
			t.Errorf("不应建卡: %+v", res.Report.Characters)
		}
		want := []service.ImportInvalidCharacter{{NodeID: "n2", Name: "林夏"}}
		if !slices.Equal(res.Report.Characters.Invalid, want) {
			t.Errorf("invalid = %+v，期望 %+v", res.Report.Characters.Invalid, want)
		}
		if _, ok := importedNodes(t, e.tx.st.canvases[0])["n2"]["characterId"]; ok {
			t.Error("失效的角色卡节点不应带 characterId")
		}
	})

	t.Run("补充图找不到只少这一张", func(t *testing.T) {
		e := newDefaultImportEnv(t)
		e.assets.missing[transferHash(202)] = true
		res, err := e.svc.Import(context.Background(), importUser, defaultImportFixture().build(t))
		if err != nil {
			t.Fatalf("Import: %v", err)
		}
		cards := e.cardsOf(importUser)
		if len(cards) != 1 || len(cards[0].ExtraAssetIDs) != 0 || cards[0].VoiceAssetID == nil || res.Report.Characters.Created != 1 {
			t.Errorf("卡应照建、只少补充图: %+v", cards)
		}
	})

	t.Run("声音找不到时声音为空", func(t *testing.T) {
		e := newDefaultImportEnv(t)
		e.assets.missing[transferHash(203)] = true
		if _, err := e.svc.Import(context.Background(), importUser, defaultImportFixture().build(t)); err != nil {
			t.Fatalf("Import: %v", err)
		}
		if cards := e.cardsOf(importUser); len(cards) != 1 || cards[0].VoiceAssetID != nil {
			t.Errorf("声音应为空: %+v", cards)
		}
	})

	t.Run("主图不是图片视为失效", func(t *testing.T) {
		e := newDefaultImportEnv(t)
		e.assets.kinds[transferHash(201)] = "video"
		res, err := e.svc.Import(context.Background(), importUser, defaultImportFixture().build(t))
		if err != nil {
			t.Fatalf("Import: %v", err)
		}
		if len(e.cardsOf(importUser)) != 0 || len(res.Report.Characters.Invalid) != 1 {
			t.Errorf("主图种类不对不应建卡: %+v", res.Report.Characters)
		}
	})
}

func TestImport_CharacterLimit(t *testing.T) {
	e := newDefaultImportEnv(t)
	for i := range 100 {
		e.addCard(importUser, fmt.Sprintf("卡%d", i))
	}
	res, err := e.svc.Import(context.Background(), importUser, defaultImportFixture().build(t))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(e.cardsOf(importUser)) != 100 || res.Report.Characters.Created != 0 {
		t.Errorf("已满 100 张不应再建卡: %d 张", len(e.cardsOf(importUser)))
	}
	want := []service.ImportInvalidCharacter{{NodeID: "n2", Name: "林夏"}}
	if !slices.Equal(res.Report.Characters.Invalid, want) {
		t.Errorf("invalid = %+v，期望 %+v", res.Report.Characters.Invalid, want)
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
		if len(e.tx.st.canvases) != 0 || len(e.tx.st.assets) != 0 || len(e.tx.st.cards) != 0 {
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
		{"引用悬空", replace(`"characterRef":"c1"`, `"characterRef":"c9"`), canvasdoc.DefaultLimits(), 30005},
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
	// 手改文件：多塞一个没人引用的素材，和一张没有节点引用的角色卡（它的主图也只有它用）
	unrefAsset, unrefAvatar := transferHash(901), transferHash(902)
	fx.mutate = func(doc *canvasdoc.Document) {
		doc.Assets[unrefAsset] = canvasdoc.AssetInfo{Kind: "image", MimeType: "image/png", ByteSize: 901, FileName: "x.png"}
		doc.Assets[unrefAvatar] = canvasdoc.AssetInfo{Kind: "image", MimeType: "image/png", ByteSize: 902, FileName: "y.png"}
		doc.Characters["c2"] = canvasdoc.CharacterInfo{Name: "无人引用", Avatar: unrefAvatar, Extras: []string{}}
	}
	res, err := e.svc.Import(context.Background(), importUser, fx.build(t))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	for _, h := range []string{unrefAsset, unrefAvatar} {
		if n := e.assets.calls[h]; n != 0 {
			t.Errorf("未被引用的素材 %s 不应复制，实际复制 %d 次", h[60:], n)
		}
	}
	if len(e.assets.calls) != 5 || len(e.tx.st.assets) != 5 {
		t.Errorf("应只复制、入库被引用的 5 个素材: calls=%v rows=%d", e.assets.calls, len(e.tx.st.assets))
	}
	if cards := e.cardsOf(importUser); len(cards) != 1 || cards[0].Name != "林夏" || res.Report.Characters.Created != 1 {
		t.Errorf("应只建被引用的角色卡: %+v created=%d", cards, res.Report.Characters.Created)
	}
	if a := res.Report.Assets; a.Total != 5 || a.Copied != 5 {
		t.Errorf("素材总数应只算被引用的: %+v", a)
	}
}
