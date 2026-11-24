package service_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"path"
	"strings"
	"testing"
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// fakeArchiveCanvasRepo 是打包下载测试用的画布仓库：只实现按用户读画布。
type fakeArchiveCanvasRepo struct {
	canvases map[uint64]*model.CanvasProject
}

func (r *fakeArchiveCanvasRepo) GetByID(_ context.Context, userID, id uint64) (*model.CanvasProject, error) {
	if p, ok := r.canvases[id]; ok && p.UserID == userID {
		return p, nil
	}
	return nil, repository.ErrNotFound
}

// fakeArchiveAssets 是打包下载测试用的素材服务：Get 返回元信息，Open 返回预置的文件内容。
type fakeArchiveAssets struct {
	assets map[uint64]*model.Asset
	files  map[uint64][]byte
	// missing 里的素材元信息存在但文件读不到（对象被删了），用来验证跳过而不是中断整包
	missing map[uint64]bool
	opened  []uint64
}

func (f *fakeArchiveAssets) Get(_ context.Context, userID, assetID uint64) (*model.Asset, error) {
	a, ok := f.assets[assetID]
	if !ok || a.UserID != userID {
		return nil, errcode.ErrAssetNotFound
	}
	return a, nil
}

func (f *fakeArchiveAssets) Open(ctx context.Context, userID, assetID uint64) (*provider.AssetFile, error) {
	a, err := f.Get(ctx, userID, assetID)
	if err != nil {
		return nil, err
	}
	if f.missing[assetID] {
		return nil, errors.New("storage: object not found")
	}
	f.opened = append(f.opened, assetID)
	return &provider.AssetFile{Asset: a, Body: io.NopCloser(bytes.NewReader(f.files[assetID]))}, nil
}

const archiveScriptBody = "第一场 · 夜 · 外 · 旧收音机\n\n阿七拧开旋钮，电流声里浮出一段旋律。"

// archiveFixture 是一套典型画布：剧本、视频节点两版产物、老图片节点只有 assetId、
// 成片节点带 finalAssetId、分镜表带旧手传图、角色卡带设定 / 补充图 / 声音。
func archiveFixture(t *testing.T) (*service.CanvasArchiveService, *fakeArchiveAssets) {
	t.Helper()
	payload := `{
	  "nodes": [
	    {"id":"n_script","type":"canvas","position":{"x":0,"y":-200},"data":{
	      "kind":"script","label":"第一场 剧本","text":"第一场 · 夜 · 外 · 旧收音机\n\n阿七拧开旋钮，电流声里浮出一段旋律。"}},
	    {"id":"n_video","type":"canvas","position":{"x":0,"y":0},"data":{
	      "kind":"video","label":"S1 开场",
	      "outputs":[
	        {"id":"11","assetId":"11","mediaType":"video","src":"/files/a.mp4"},
	        {"id":"12","assetId":"12","mediaType":"video","src":"/files/b.mp4"}
	      ],
	      "assetId":"12","mediaType":"video","activeOutputId":"12"}},
	    {"id":"n_old","type":"canvas","position":{"x":0,"y":200},"data":{
	      "kind":"image","label":"老图 / 带斜杠","assetId":"40","mediaType":"image","src":"/files/c.png"}},
	    {"id":"n_compose","type":"canvas","position":{"x":0,"y":400},"data":{
	      "kind":"compose","label":"成片合成","finalAssetId":"20","finalDurationMs":14848}},
	    {"id":"n_board","type":"canvas","position":{"x":0,"y":600},"data":{
	      "kind":"storyboard","label":"分镜表","storyboard":{"shots":[
	        {"id":"s1","scene":"阿七拧开旋钮","shot":"特写","camera":"推","action":"手部入画","dialogue":"（电流声）","duration":3.5,
	         "image":{"assetId":"30","src":"/files/d.png"}},
	        {"id":"s2","scene":"旋律浮出","shot":"全景","camera":"拉","action":"","dialogue":"","duration":4,
	         "videoNodeId":"n_video"}
	      ]}}},
	    {"id":"n_role","type":"canvas","position":{"x":0,"y":800},"data":{
	      "kind":"character","label":"角色卡","assetId":"50","mediaType":"image",
	      "character":{"name":"阿七","look":"基础形象","description":"二十出头的修表匠，手指有老茧。",
	        "voicePrompt":"低沉男声，略微沙哑",
	        "extras":[{"assetId":"51","src":"/files/e.png"}],
	        "voice":{"mode":"audio","assetId":"52","src":"/files/f.mp3","origin":"generated","name":"沙哑男声"}}}},
	    {"id":"g1","type":"group","position":{"x":-40,"y":-40},"data":{"kind":"group","label":"第一场"}}
	  ]
	}`
	assets := &fakeArchiveAssets{
		assets: map[uint64]*model.Asset{
			11: {ID: 11, UserID: 7, Kind: "video", MimeType: "video/mp4", ByteSize: 1000, Width: 1080, Height: 1920, DurationMs: 5000},
			12: {ID: 12, UserID: 7, Kind: "video", MimeType: "video/mp4", ByteSize: 2000, Width: 1080, Height: 1920, DurationMs: 6000},
			20: {ID: 20, UserID: 7, Kind: "video", MimeType: "video/mp4", ByteSize: 9000, DurationMs: 14848},
			30: {ID: 30, UserID: 7, Kind: "image", MimeType: "image/png", ByteSize: 300, Width: 512, Height: 512},
			40: {ID: 40, UserID: 7, Kind: "image", MimeType: "image/jpeg", ByteSize: 400, Width: 800, Height: 600},
			50: {ID: 50, UserID: 7, Kind: "image", MimeType: "image/png", ByteSize: 500, Width: 512, Height: 512},
			51: {ID: 51, UserID: 7, Kind: "image", MimeType: "image/png", ByteSize: 600, Width: 512, Height: 512},
			52: {ID: 52, UserID: 7, Kind: "audio", MimeType: "audio/mpeg", ByteSize: 700, DurationMs: 3000},
		},
		files: map[uint64][]byte{
			11: []byte("video11"), 12: []byte("video12"), 20: []byte("final20"),
			30: []byte("img30"), 40: []byte("img40"), 50: []byte("img50"),
			51: []byte("img51"), 52: []byte("audio52"),
		},
		missing: map[uint64]bool{},
	}
	repo := &fakeArchiveCanvasRepo{canvases: map[uint64]*model.CanvasProject{
		9: {
			BaseModel: model.BaseModel{ID: 9}, UserID: 7, Title: "MV 技能 v4 · 旧收音机",
			Revision: 97, PayloadJSON: datatypes.JSON(payload),
		},
	}}
	svc := service.NewCanvasArchiveService(service.CanvasArchiveDeps{
		Canvases: repo, Assets: assets, Secret: "test-secret",
		Now: func() time.Time { return time.Date(2026, 11, 22, 21, 30, 0, 0, time.UTC) },
	})
	return svc, assets
}

func archivePaths(a *service.CanvasArchive) []string {
	out := make([]string, 0, len(a.Entries))
	for _, e := range a.Entries {
		out = append(out, e.Path())
	}
	return out
}

// archiveZip 是这套 fixture 的压缩包名：日期_项目名_版本号（版本号取画布修订号）。
const archiveZip = "20261122_MV_技能_v4_旧收音机_v97.zip"

func TestCanvasArchivePlanGroupsEveryKind(t *testing.T) {
	svc, _ := archiveFixture(t)
	a, err := svc.Plan(context.Background(), 7, 9)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	got := archivePaths(a)
	want := []string{
		"剧本/第一场_剧本.txt",
		"分镜表/分镜表.md",
		"分镜表/分镜表.csv",
		"角色/角色卡.md",
		"图片/老图_带斜杠.jpg",
		"图片/分镜表_第01镜.png",
		"图片/01_角色卡/v1.png",
		"图片/01_角色卡/v2.png",
		"视频/01_S1_开场/v1.mp4",
		"视频/01_S1_开场/v2.mp4",
		"音频/角色卡.mp3",
		"成片/成片合成.mp4",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("包内路径不对:\n got %v\nwant %v", got, want)
	}
	var sum int64
	for _, e := range a.Entries {
		sum += e.Size()
	}
	if a.TotalBytes != sum || sum <= 0 {
		t.Fatalf("合计字节 = %d，条目之和 = %d", a.TotalBytes, sum)
	}
	if a.FileName != archiveZip {
		t.Fatalf("文件名 = %q", a.FileName)
	}
	// 视频节点两版产物要标成 1/2、2/2；成片与文本不参与版本编号
	for _, e := range a.Entries {
		if e.Node == "S1 开场" && e.Versions != 2 {
			t.Fatalf("S1 开场版本总数 = %d", e.Versions)
		}
		if e.Final && e.Version != 0 {
			t.Fatalf("成片不该有版本号: %+v", e)
		}
	}
	if len(a.Skipped) != 0 {
		t.Fatalf("不该有跳过项: %v", a.Skipped)
	}
	// 目录名和文件名完全同名是白多一层（分镜表/01_分镜表/分镜表.md），这条守住不退回老结构
	for _, e := range a.Entries {
		if e.Dir == e.Group {
			continue
		}
		folder := path.Base(e.Dir)
		if len(folder) > 3 && folder[2] == '_' {
			folder = folder[3:]
		}
		if stem := strings.TrimSuffix(e.Name, path.Ext(e.Name)); folder == stem {
			t.Fatalf("目录与文件同名，多了一层无用目录: %s", e.Path())
		}
	}
}

func TestCanvasArchiveWriteZipAndManifests(t *testing.T) {
	svc, assets := archiveFixture(t)
	a, err := svc.Plan(context.Background(), 7, 9)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	got := archiveUnzip(t, svc, a)
	if string(got["视频/01_S1_开场/v2.mp4"]) != "video12" {
		t.Fatalf("视频内容不对: %q", got["视频/01_S1_开场/v2.mp4"])
	}
	if string(got["成片/成片合成.mp4"]) != "final20" {
		t.Fatalf("成片内容不对: %q", got["成片/成片合成.mp4"])
	}
	if string(got["剧本/第一场_剧本.txt"]) != archiveScriptBody {
		t.Fatalf("剧本内容不对: %q", got["剧本/第一场_剧本.txt"])
	}
	board := string(got["分镜表/分镜表.md"])
	for _, want := range []string{
		"# 分镜表",
		"2 镜 · 总时长 7.5s",
		"| 01 | 特写 | 推 | 3.5s | 阿七拧开旋钮 | 手部入画 | （电流声） | ✓ | - |",
		"| 02 | 全景 | 拉 | 4.0s | 旋律浮出 | - | - | - | ✓ |",
	} {
		if !strings.Contains(board, want) {
			t.Fatalf("分镜表.md 缺 %q\n---\n%s", want, board)
		}
	}
	if !strings.Contains(string(got["分镜表/分镜表.csv"]), "镜号,景别,运镜,时长(秒),画面,动作,对白,分镜图,视频") {
		t.Fatalf("分镜表.csv 表头不对: %s", got["分镜表/分镜表.csv"])
	}
	role := string(got["角色/角色卡.md"])
	for _, want := range []string{"# 阿七 · 基础形象", "补充图：1 张", "声音：沙哑男声", "二十出头的修表匠", "低沉男声，略微沙哑"} {
		if !strings.Contains(role, want) {
			t.Fatalf("角色.md 缺 %q\n---\n%s", want, role)
		}
	}
	md := string(got["manifest.md"])
	for _, want := range []string{
		"# MV 技能 v4 · 旧收音机 · 画布素材",
		"- 画布修订号：v97",
		"## 剧本（1）",
		"## 分镜表（2）",
		"## 角色（1）",
		"## 图片（4）",
		"## 视频（2）",
		"## 音频（1）",
		"## 成片（1）",
		"| 视频/01_S1_开场 | v2.mp4 | S1 开场 | 2 / 2 | 1080×1920 · 6.0s | 2 KB | 12 |",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("manifest.md 缺 %q\n---\n%s", want, md)
		}
	}
	if !strings.Contains(string(got["manifest.csv"]), "分组,目录,文件,节点,节点ID,版本,成片,分镜") {
		t.Fatalf("manifest.csv 表头不对: %s", got["manifest.csv"])
	}
	// 文本条目没有素材，不该去打开任何文件
	if len(assets.opened) != 8 {
		t.Fatalf("打开媒体文件次数 = %d，期望 8", len(assets.opened))
	}
}

func TestCanvasArchiveSkipsUnreadableAsset(t *testing.T) {
	svc, assets := archiveFixture(t)
	assets.missing[12] = true
	a, err := svc.Plan(context.Background(), 7, 9)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	got := archiveUnzip(t, svc, a)
	if _, ok := got["视频/01_S1_开场/v2.mp4"]; ok {
		t.Fatal("读不到的文件不该出现在包里")
	}
	if _, ok := got["视频/01_S1_开场/v1.mp4"]; !ok {
		t.Fatal("其余文件应该照常产出")
	}
	if _, ok := got["manifest.md"]; !ok {
		t.Fatal("清单应该照常产出")
	}
	failed := 0
	for _, e := range a.Entries {
		if e.Failed {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("清单里标记失败的文件数 = %d", failed)
	}
	if !strings.Contains(string(got["manifest.md"]), "文件读取失败，已跳过") {
		t.Fatal("清单里应该标出读不到的文件")
	}
}

func TestCanvasArchiveSkipsMissingAssetMeta(t *testing.T) {
	svc, assets := archiveFixture(t)
	delete(assets.assets, 40)
	a, err := svc.Plan(context.Background(), 7, 9)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(a.Skipped) != 1 || a.Skipped[0].Node != "老图 / 带斜杠" {
		t.Fatalf("跳过项不对: %+v", a.Skipped)
	}
	for _, e := range a.Entries {
		if e.Asset != nil && e.Asset.ID == 40 {
			t.Fatal("素材元信息查不到就不该进包")
		}
	}
}

func TestCanvasArchiveEmpty(t *testing.T) {
	repo := &fakeArchiveCanvasRepo{canvases: map[uint64]*model.CanvasProject{
		9: {BaseModel: model.BaseModel{ID: 9}, UserID: 7, Title: "空画布", PayloadJSON: datatypes.JSON(`{"nodes":[]}`)},
	}}
	svc := service.NewCanvasArchiveService(service.CanvasArchiveDeps{Canvases: repo, Assets: &fakeArchiveAssets{}, Secret: "s"})
	if _, err := svc.Plan(context.Background(), 7, 9); !errors.Is(err, errcode.ErrCanvasArchiveEmpty) {
		t.Fatalf("空画布应该报没有可打包素材，实际 %v", err)
	}
}

func TestCanvasArchiveCanvasNotOwned(t *testing.T) {
	svc, _ := archiveFixture(t)
	if _, err := svc.Plan(context.Background(), 8, 9); !errors.Is(err, errcode.ErrCanvasNotFound) {
		t.Fatalf("别人的画布应该报不存在，实际 %v", err)
	}
}

func TestCanvasArchiveTicket(t *testing.T) {
	svc, _ := archiveFixture(t)
	ticket, exp := svc.Sign(7, 9)
	if ticket == "" || exp.IsZero() {
		t.Fatal("票据没签出来")
	}
	userID, canvasID, err := svc.ParseTicket(ticket)
	if err != nil || userID != 7 || canvasID != 9 {
		t.Fatalf("票据解析 = (%d, %d, %v)", userID, canvasID, err)
	}
	broken := ticket[:len(ticket)-1] + "x"
	if _, _, err := svc.ParseTicket(broken); !errors.Is(err, errcode.ErrCanvasArchiveTicket) {
		t.Fatalf("被改过的票据应该无效，实际 %v", err)
	}
	if _, _, err := svc.ParseTicket(""); !errors.Is(err, errcode.ErrCanvasArchiveTicket) {
		t.Fatalf("空票据应该无效，实际 %v", err)
	}
}

func TestCanvasArchiveTicketExpires(t *testing.T) {
	now := time.Date(2026, 11, 22, 21, 30, 0, 0, time.UTC)
	repo := &fakeArchiveCanvasRepo{}
	assets := &fakeArchiveAssets{}
	svc := service.NewCanvasArchiveService(service.CanvasArchiveDeps{
		Canvases: repo, Assets: assets, Secret: "s", Now: func() time.Time { return now },
	})
	ticket, _ := svc.Sign(7, 9)
	later := service.NewCanvasArchiveService(service.CanvasArchiveDeps{
		Canvases: repo, Assets: assets, Secret: "s", Now: func() time.Time { return now.Add(11 * time.Minute) },
	})
	if _, _, err := later.ParseTicket(ticket); !errors.Is(err, errcode.ErrCanvasArchiveTicket) {
		t.Fatalf("过期票据应该无效，实际 %v", err)
	}
}

func TestCanvasArchiveSafeName(t *testing.T) {
	svc, _ := archiveFixture(t)
	a, err := svc.Plan(context.Background(), 7, 9)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	for _, e := range a.Entries {
		// 每一级目录都只留中文、字母、数字、下划线和连字符
		for _, seg := range strings.Split(e.Dir, "/") {
			if seg == "" {
				t.Fatalf("目录里有空段: %q", e.Dir)
			}
			if strings.ContainsAny(seg, " \\:·*?\"<>|") {
				t.Fatalf("目录名里还有特殊符号: %q（来自 %q）", seg, e.Dir)
			}
		}
	}
}

// 标题是纯数字时补「画布_」前缀：否则文件夹名和版本号、序号混在一起看不出是什么。
func TestCanvasArchiveNumericTitle(t *testing.T) {
	repo := &fakeArchiveCanvasRepo{canvases: map[uint64]*model.CanvasProject{
		9: {
			BaseModel: model.BaseModel{ID: 9}, UserID: 7, Title: "2026",
			Revision: 3, PayloadJSON: datatypes.JSON(`{"nodes":[
				{"id":"t1","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"script","label":"剧本","text":"第一场"}}]}`),
		},
	}}
	svc := service.NewCanvasArchiveService(service.CanvasArchiveDeps{
		Canvases: repo, Assets: &fakeArchiveAssets{}, Secret: "s",
		Now: func() time.Time { return time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC) },
	})
	a, err := svc.Plan(context.Background(), 7, 9)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if a.FileName != "20261122_画布_2026_v3.zip" {
		t.Fatalf("文件名 = %q", a.FileName)
	}
}

// archiveUnzip 把服务写出的 zip 读成「路径 → 内容」，顺便校验 zip 本身可读。
func archiveUnzip(t *testing.T, svc *service.CanvasArchiveService, a *service.CanvasArchive) map[string][]byte {
	t.Helper()
	var buf bytes.Buffer
	// Write 会改写 Failed 标记，测试里用副本，避免影响后续断言
	if err := svc.Write(context.Background(), 7, a, &buf); err != nil {
		t.Fatalf("Write: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip 读不回来: %v", err)
	}
	out := make(map[string][]byte, len(zr.File))
	for _, f := range zr.File {
		rc, openErr := f.Open()
		if openErr != nil {
			t.Fatalf("打开 %s: %v", f.Name, openErr)
		}
		body, _ := io.ReadAll(rc)
		_ = rc.Close()
		out[f.Name] = body
	}
	return out
}
