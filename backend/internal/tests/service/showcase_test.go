package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	. "video-canvas/internal/service"
	"video-canvas/internal/service/showcasefake"
)

const (
	scAdmin   = uint64(1) // 当前管理员
	scOther   = uint64(2) // 另一个用户
	scVideo   = uint64(10)
	scPoster  = uint64(11)
	scVideo2  = uint64(14)
	scOthersV = uint64(13) // 别人的视频
	scOthersI = uint64(12) // 别人的图片
	scOthersG = uint64(15) // 别人的平台生成视频（source=generated）
	scOthersU = uint64(16) // 别人上传的视频（source=upload）
	scOthersX = uint64(17) // 别人的平台生成图片（source=generated）
)

// scEnv 把展示服务和它的全部 fake 放在一起，每个子用例各建一份，互不共享状态。
type scEnv struct {
	svc      *ShowcaseService
	repo     *showcasefake.Repo
	assets   *showcasefake.Assets
	tasks    *showcasefake.Tasks
	users    *showcasefake.Users
	settings *fakeSettingsRepo
	audit    *fakeAudit
}

// othersExtraAssets 是别人名下的三个素材：平台生成的视频、上传的视频、平台生成的图片，用来验证素材归属规则。
func othersExtraAssets() []model.Asset {
	return []model.Asset{
		{ID: scOthersG, UserID: scOther, Kind: "video", Source: model.AssetSourceGenerated, StorageKey: "u2/g15.mp4", ByteSize: 500, Width: 1920, Height: 1080, DurationMs: 8000, FileName: "gen.mp4"},
		{ID: scOthersU, UserID: scOther, Kind: "video", Source: model.AssetSourceUpload, StorageKey: "u2/u16.mp4"},
		{ID: scOthersX, UserID: scOther, Kind: "image", Source: model.AssetSourceGenerated, StorageKey: "u2/g17.png"},
	}
}

func newScEnv() *scEnv { return newScEnvWith(othersExtraAssets()...) }

// newScEnvWith 在默认素材之外再加 extra 素材，建一套全新的展示服务环境。
func newScEnvWith(extra ...model.Asset) *scEnv {
	rows := []model.Asset{
		{ID: scVideo, UserID: scAdmin, Kind: "video", StorageKey: "u1/v10.mp4", ByteSize: 3145728, Width: 1280, Height: 720, DurationMs: 5000, FileName: "开场.mp4"},
		{ID: scPoster, UserID: scAdmin, Kind: "image", StorageKey: "u1/p11.jpg", FileName: "封面.jpg"},
		{ID: scOthersI, UserID: scOther, Kind: "image", StorageKey: "u2/p12.jpg"},
		{ID: scOthersV, UserID: scOther, Kind: "video", StorageKey: "u2/v13.mp4"},
		{ID: scVideo2, UserID: scAdmin, Kind: "video", StorageKey: "u1/v14.mp4", ByteSize: 100, Width: 640, Height: 360, DurationMs: 1000, FileName: "二.mp4"},
	}
	assets := showcasefake.NewAssets(append(rows, extra...)...)
	repo := &showcasefake.Repo{}
	tasks := showcasefake.NewTasks()
	users := &showcasefake.Users{Names: map[uint64]string{}}
	settings := newFakeSettingsRepo()
	audit := &fakeAudit{}
	svc := NewShowcaseService(ShowcaseDeps{Items: repo, Assets: assets, Views: assets, Tasks: tasks, Users: users, Settings: settings, Audit: audit})
	return &scEnv{svc: svc, repo: repo, assets: assets, tasks: tasks, users: users, settings: settings, audit: audit}
}

// seed 直接往 fake 仓储塞条目（绕过 service 校验），返回写入后的条目 id。
func (e *scEnv) seed(it model.ShowcaseItem) uint64 {
	if err := e.repo.Create(context.Background(), &it); err != nil {
		panic(err)
	}
	return it.ID
}

func scPtr[T any](v T) *T { return &v }

// requireCode 断言 err 是指定错误码的业务错误；want 为 0 表示期望成功。
func requireCode(t *testing.T, err error, want int) {
	t.Helper()
	if want == 0 {
		if err != nil {
			t.Fatalf("期望成功，实际返回错误：%v", err)
		}
		return
	}
	var e *errcode.Error
	if !errors.As(err, &e) || e.Code != want {
		t.Fatalf("期望错误码 %d，实际：%v", want, err)
	}
}

// ---------- 公开读取 ----------

func TestShowcaseService_Public(t *testing.T) {
	ctx := context.Background()

	t.Run("只返回启用条目，按 sort 升序（sort 相同按 id 升序），字段取自素材", func(t *testing.T) {
		e := newScEnv()
		a := e.seed(model.ShowcaseItem{AssetID: scVideo, Prompt: "后面", Enabled: true, Sort: 5, PosterAssetID: scPtr(scPoster), ModelLabel: "猴子三代", StartSec: 1.5})
		b := e.seed(model.ShowcaseItem{AssetID: scVideo2, Prompt: "前面", Enabled: true, Sort: 1})
		c := e.seed(model.ShowcaseItem{AssetID: scVideo, Prompt: "同序后建", Enabled: true, Sort: 1})
		e.seed(model.ShowcaseItem{AssetID: scVideo, Prompt: "禁用的", Enabled: false, Sort: 0})

		v, err := e.svc.Public(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(v.Items) != 3 || v.Items[0].ID != b || v.Items[1].ID != c || v.Items[2].ID != a {
			t.Fatalf("顺序不对：%+v", v.Items)
		}
		last := v.Items[2]
		if last.VideoURL != "/files/u1/v10.mp4" || last.PosterURL != "/files/u1/p11.jpg" || last.Prompt != "后面" ||
			last.ModelLabel != "猴子三代" || last.StartSec != 1.5 || last.Width != 1280 || last.Height != 720 || last.ByteSize != 3145728 {
			t.Fatalf("字段不对：%+v", last)
		}
		if v.Items[0].PosterURL != "" {
			t.Fatalf("没有封面素材时 poster_url 应为空串：%+v", v.Items[0])
		}
		if v.Settings.ClipSeconds != 7 || !v.Settings.ShowOnLogin || !v.Settings.PosterOnlyOnSaveData {
			t.Fatalf("默认设置不对：%+v", v.Settings)
		}
	})

	t.Run("视频素材已被删除的条目直接跳过，封面素材被删除则 poster_url 为空", func(t *testing.T) {
		e := newScEnv()
		e.seed(model.ShowcaseItem{AssetID: 999, Prompt: "视频没了", Enabled: true})
		e.seed(model.ShowcaseItem{AssetID: scVideo, Prompt: "封面没了", Enabled: true, PosterAssetID: scPtr(uint64(998))})
		v, err := e.svc.Public(ctx)
		if err != nil || len(v.Items) != 1 || v.Items[0].Prompt != "封面没了" || v.Items[0].PosterURL != "" {
			t.Fatalf("%+v %v", v, err)
		}
	})

	t.Run("show_on_login=false 时 items 是空数组而不是 null，设置仍然返回", func(t *testing.T) {
		e := newScEnv()
		e.seed(model.ShowcaseItem{AssetID: scVideo, Prompt: "x", Enabled: true})
		e.settings.kv[model.SettingShowcaseShowOnLogin] = "false"
		e.settings.kv[model.SettingShowcaseClipSeconds] = "9"
		v, err := e.svc.Public(ctx)
		if err != nil || v.Items == nil || len(v.Items) != 0 || v.Settings.ShowOnLogin || v.Settings.ClipSeconds != 9 {
			t.Fatalf("%+v %v", v, err)
		}
		raw, _ := json.Marshal(v)
		if !strings.Contains(string(raw), `"items":[]`) {
			t.Fatalf("items 应序列化为 []：%s", raw)
		}
	})

	t.Run("没有任何条目也返回 []", func(t *testing.T) {
		v, err := newScEnv().svc.Public(ctx)
		if err != nil || v.Items == nil || len(v.Items) != 0 {
			t.Fatalf("%+v %v", v, err)
		}
	})

	t.Run("绝不暴露内部字段", func(t *testing.T) {
		e := newScEnv()
		e.seed(model.ShowcaseItem{AssetID: scVideo, Prompt: "x", Enabled: true, CreatedBy: 77, PosterAssetID: scPtr(scPoster)})
		v, _ := e.svc.Public(ctx)
		raw, _ := json.Marshal(v)
		for _, banned := range []string{"asset_id", "created_by", "created_at", "enabled", "sort", "file_name", "77"} {
			if strings.Contains(string(raw), banned) {
				t.Fatalf("公开响应不应包含 %q：%s", banned, raw)
			}
		}
	})

	t.Run("读取失败原样透传", func(t *testing.T) {
		e := newScEnv()
		e.repo.Err = errBoom
		if _, err := e.svc.Public(ctx); !errors.Is(err, errBoom) {
			t.Fatalf("条目读取失败应透传：%v", err)
		}
		e = newScEnv()
		e.settings.getErr = errBoom
		if _, err := e.svc.Public(ctx); !errors.Is(err, errBoom) {
			t.Fatalf("设置读取失败应透传：%v", err)
		}
		e = newScEnv()
		e.seed(model.ShowcaseItem{AssetID: scVideo, Prompt: "x", Enabled: true})
		e.assets.Err = errBoom
		if _, err := e.svc.Public(ctx); !errors.Is(err, errBoom) {
			t.Fatalf("素材读取失败应透传：%v", err)
		}
	})
}

func TestShowcaseService_Settings(t *testing.T) {
	ctx := context.Background()
	def := model.ShowcaseSettingsView{ClipSeconds: 7, ShowOnLogin: true, PosterOnlyOnSaveData: true}
	tests := []struct {
		name string
		kv   map[string]string
		want model.ShowcaseSettingsView
	}{
		{"库里没有值回落默认", nil, def},
		{"合法库值优先", map[string]string{model.SettingShowcaseClipSeconds: "12", model.SettingShowcaseShowOnLogin: "false", model.SettingShowcasePosterOnSaveData: "false"},
			model.ShowcaseSettingsView{ClipSeconds: 12}},
		{"边界 4 合法", map[string]string{model.SettingShowcaseClipSeconds: "4"}, model.ShowcaseSettingsView{ClipSeconds: 4, ShowOnLogin: true, PosterOnlyOnSaveData: true}},
		{"边界 15 合法", map[string]string{model.SettingShowcaseClipSeconds: "15"}, model.ShowcaseSettingsView{ClipSeconds: 15, ShowOnLogin: true, PosterOnlyOnSaveData: true}},
		{"秒数低于 4 回落默认", map[string]string{model.SettingShowcaseClipSeconds: "3"}, def},
		{"秒数高于 15 回落默认", map[string]string{model.SettingShowcaseClipSeconds: "16"}, def},
		{"秒数不是数字回落默认", map[string]string{model.SettingShowcaseClipSeconds: "abc"}, def},
		{"开关里非 false 的任何值都按开", map[string]string{model.SettingShowcaseShowOnLogin: "oops", model.SettingShowcasePosterOnSaveData: ""}, def},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newScEnv()
			for k, v := range tt.kv {
				e.settings.kv[k] = v
			}
			got, err := e.svc.Settings(ctx)
			if err != nil || *got != tt.want {
				t.Fatalf("期望 %+v，实际 %+v %v", tt.want, got, err)
			}
		})
	}
}

// ---------- 后台读取 ----------

func TestShowcaseService_AdminView(t *testing.T) {
	ctx := context.Background()
	e := newScEnv()
	e.seed(model.ShowcaseItem{AssetID: scVideo, Prompt: "启用", Enabled: true, Sort: 2, PosterAssetID: scPtr(scPoster), CreatedBy: 1})
	e.seed(model.ShowcaseItem{AssetID: scVideo2, Prompt: "禁用", Enabled: false, Sort: 1})
	e.seed(model.ShowcaseItem{AssetID: 999, Prompt: "视频没了", Enabled: true, Sort: 3})
	e.settings.kv[model.SettingShowcaseShowOnLogin] = "false" // 后台读取不受“登录页是否展示”影响

	v, err := e.svc.AdminView(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Items) != 3 || v.Items[0].Prompt != "禁用" || v.Items[1].Prompt != "启用" || v.Items[2].Prompt != "视频没了" {
		t.Fatalf("应含全部条目并按 sort 升序：%+v", v.Items)
	}
	it := v.Items[1]
	if it.AssetID != scVideo || it.PosterAssetID == nil || *it.PosterAssetID != scPoster || it.VideoURL != "/files/u1/v10.mp4" ||
		it.PosterURL != "/files/u1/p11.jpg" || it.Width != 1280 || it.Height != 720 || it.ByteSize != 3145728 ||
		it.DurationMs != 5000 || it.FileName != "开场.mp4" || !it.Enabled || it.CreatedAt.IsZero() {
		t.Fatalf("后台视图字段不对：%+v", it)
	}
	if v.Items[0].PosterAssetID != nil {
		t.Fatalf("无封面时 poster_asset_id 应为 nil：%+v", v.Items[0])
	}
	if gone := v.Items[2]; gone.VideoURL != "" || gone.FileName != "" {
		t.Fatalf("视频素材被删的条目仍要列出（方便管理员删除），但没有视频信息：%+v", gone)
	}
	if v.Settings.ShowOnLogin {
		t.Fatal("设置应如实返回")
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), "created_by") {
		t.Fatalf("后台视图也不应暴露 created_by：%s", raw)
	}

	e.repo.Err = errBoom
	if _, err := e.svc.AdminView(ctx); !errors.Is(err, errBoom) {
		t.Fatalf("读取失败应透传：%v", err)
	}
}

// ---------- 新增 ----------

func TestShowcaseService_CreateItem(t *testing.T) {
	ctx := context.Background()
	long80 := strings.Repeat("好", 80)
	tests := []struct {
		name     string
		prepare  func(e *scEnv)
		req      model.CreateShowcaseItemReq
		wantCode int  // 0 表示期望成功
		wantBoom bool // 期望透传 errBoom
	}{
		{"成功：去首尾空白、默认启用、sort 取最大值加一", func(e *scEnv) {
			e.seed(model.ShowcaseItem{AssetID: scVideo, Prompt: "a", Sort: 2, Enabled: true})
			e.seed(model.ShowcaseItem{AssetID: scVideo, Prompt: "b", Sort: 0, Enabled: true})
		}, model.CreateShowcaseItemReq{AssetID: scVideo, PosterAssetID: scPtr(scPoster), Prompt: "  一只猫  ", ModelLabel: " 猴子 ", StartSec: 1.5}, 0, false},
		{"成功：80 个汉字（按字符数而不是字节数）", nil, model.CreateShowcaseItemReq{AssetID: scVideo, Prompt: long80}, 0, false},
		{"成功：显式禁用", nil, model.CreateShowcaseItemReq{AssetID: scVideo, Prompt: "x", Enabled: scPtr(false)}, 0, false},
		{"成功：起始秒取上界 3600", nil, model.CreateShowcaseItemReq{AssetID: scVideo, Prompt: "x", StartSec: 3600}, 0, false},
		{"文案为空", nil, model.CreateShowcaseItemReq{AssetID: scVideo}, errcode.ErrShowcaseInvalid.Code, false},
		{"文案只有空白", nil, model.CreateShowcaseItemReq{AssetID: scVideo, Prompt: " \t\n "}, errcode.ErrShowcaseInvalid.Code, false},
		{"文案 81 个字符", nil, model.CreateShowcaseItemReq{AssetID: scVideo, Prompt: long80 + "好"}, errcode.ErrShowcaseInvalid.Code, false},
		{"模型标注 41 个字符", nil, model.CreateShowcaseItemReq{AssetID: scVideo, Prompt: "x", ModelLabel: strings.Repeat("a", 41)}, errcode.ErrShowcaseInvalid.Code, false},
		{"起始秒为负", nil, model.CreateShowcaseItemReq{AssetID: scVideo, Prompt: "x", StartSec: -0.1}, errcode.ErrShowcaseInvalid.Code, false},
		{"起始秒超过 3600", nil, model.CreateShowcaseItemReq{AssetID: scVideo, Prompt: "x", StartSec: 3600.5}, errcode.ErrShowcaseInvalid.Code, false},
		{"视频素材不存在", nil, model.CreateShowcaseItemReq{AssetID: 999, Prompt: "x"}, errcode.ErrAssetNotFound.Code, false},
		{"视频素材属于别人按不存在处理", nil, model.CreateShowcaseItemReq{AssetID: scOthersV, Prompt: "x"}, errcode.ErrAssetNotFound.Code, false},
		{"成功：别人的平台生成视频可以用（素材库挑选）", nil, model.CreateShowcaseItemReq{AssetID: scOthersG, Prompt: "x"}, 0, false},
		{"别人上传的视频被拒，按不存在处理", nil, model.CreateShowcaseItemReq{AssetID: scOthersU, Prompt: "x"}, errcode.ErrAssetNotFound.Code, false},
		{"别人的平台生成图片当视频：类型不符 54002", nil, model.CreateShowcaseItemReq{AssetID: scOthersX, Prompt: "x"}, errcode.ErrShowcaseInvalid.Code, false},
		{"别人的平台生成图片仍不能当封面（封面只认自己的图片）", nil, model.CreateShowcaseItemReq{AssetID: scVideo, PosterAssetID: scPtr(scOthersX), Prompt: "x"}, errcode.ErrAssetNotFound.Code, false},
		{"视频素材其实是图片", nil, model.CreateShowcaseItemReq{AssetID: scPoster, Prompt: "x"}, errcode.ErrShowcaseInvalid.Code, false},
		{"封面素材不存在", nil, model.CreateShowcaseItemReq{AssetID: scVideo, PosterAssetID: scPtr(uint64(999)), Prompt: "x"}, errcode.ErrAssetNotFound.Code, false},
		{"封面素材属于别人按不存在处理", nil, model.CreateShowcaseItemReq{AssetID: scVideo, PosterAssetID: scPtr(scOthersI), Prompt: "x"}, errcode.ErrAssetNotFound.Code, false},
		{"封面素材不是图片", nil, model.CreateShowcaseItemReq{AssetID: scVideo, PosterAssetID: scPtr(scVideo2), Prompt: "x"}, errcode.ErrShowcaseInvalid.Code, false},
		{"素材读取失败透传", func(e *scEnv) { e.assets.Err = errBoom }, model.CreateShowcaseItemReq{AssetID: scVideo, Prompt: "x"}, 0, true},
		{"写库失败透传", func(e *scEnv) { e.repo.Err = errBoom }, model.CreateShowcaseItemReq{AssetID: scVideo, Prompt: "x"}, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newScEnv()
			if tt.prepare != nil {
				tt.prepare(e)
			}
			before := len(e.repo.Items)
			got, err := e.svc.CreateItem(ctx, scAdmin, &tt.req)
			if tt.wantBoom {
				if !errors.Is(err, errBoom) {
					t.Fatalf("期望透传下层错误，实际：%v", err)
				}
			} else {
				requireCode(t, err, tt.wantCode)
			}
			if tt.wantBoom || tt.wantCode != 0 {
				if len(e.repo.Items) != before || len(e.audit.logs) != 0 {
					t.Fatalf("失败时不应写库也不应写审计：items=%d audit=%v", len(e.repo.Items), e.audit.actions())
				}
				return
			}
			if got.ID == 0 || got.AssetID != tt.req.AssetID || got.VideoURL == "" {
				t.Fatalf("返回的后台视图不对：%+v", got)
			}
			row := e.repo.Items[len(e.repo.Items)-1]
			if row.CreatedBy != scAdmin {
				t.Fatalf("created_by 应为当前管理员：%+v", row)
			}
			if row.Prompt != strings.TrimSpace(tt.req.Prompt) || row.ModelLabel != strings.TrimSpace(tt.req.ModelLabel) || got.Prompt != row.Prompt {
				t.Fatalf("文案应去掉首尾空白：%+v", row)
			}
			wantEnabled := tt.req.Enabled == nil || *tt.req.Enabled
			if row.Enabled != wantEnabled || got.Enabled != wantEnabled {
				t.Fatalf("enabled 缺省应为 true：%+v", row)
			}
			wantSort := 0
			if before > 0 {
				wantSort = 3 // 预置条目最大 sort 为 2
			}
			if row.Sort != wantSort {
				t.Fatalf("新条目 sort 应为最大值加一（%d），实际 %d", wantSort, row.Sort)
			}
			if tt.req.PosterAssetID != nil && (got.PosterAssetID == nil || got.PosterURL == "") {
				t.Fatalf("应返回封面：%+v", got)
			}
			if acts := e.audit.actions(); len(acts) != 1 || acts[0] != model.AdminAuditShowcaseCreate || e.audit.logs[0].ActorID != scAdmin ||
				e.audit.logs[0].TargetType != model.AdminAuditTargetShowcase || e.audit.logs[0].TargetID != got.ID {
				t.Fatalf("审计不对：%+v", e.audit.logs)
			}
		})
	}
}

// ---------- 修改 ----------

func TestShowcaseService_UpdateItem(t *testing.T) {
	ctx := context.Background()
	newItem := func(e *scEnv) uint64 {
		return e.seed(model.ShowcaseItem{AssetID: scVideo, PosterAssetID: scPtr(scPoster), Prompt: "旧文案", ModelLabel: "旧模型", StartSec: 2, Enabled: true})
	}
	tests := []struct {
		name     string
		req      model.UpdateShowcaseItemReq
		wantCode int
		check    func(t *testing.T, it model.ShowcaseItem)
	}{
		{"只改文案，其他字段不动", model.UpdateShowcaseItemReq{Prompt: scPtr(" 新文案 ")}, 0, func(t *testing.T, it model.ShowcaseItem) {
			if it.Prompt != "新文案" || it.ModelLabel != "旧模型" || it.StartSec != 2 || !it.Enabled || it.PosterAssetID == nil {
				t.Fatalf("%+v", it)
			}
		}},
		{"禁用", model.UpdateShowcaseItemReq{Enabled: scPtr(false)}, 0, func(t *testing.T, it model.ShowcaseItem) {
			if it.Enabled {
				t.Fatalf("%+v", it)
			}
		}},
		{"清空模型标注、起始秒归零", model.UpdateShowcaseItemReq{ModelLabel: scPtr(""), StartSec: scPtr(0.0)}, 0, func(t *testing.T, it model.ShowcaseItem) {
			if it.ModelLabel != "" || it.StartSec != 0 {
				t.Fatalf("%+v", it)
			}
		}},
		{"显式传 null 清空封面", model.UpdateShowcaseItemReq{PosterAssetID: model.OptionalID{Set: true}}, 0, func(t *testing.T, it model.ShowcaseItem) {
			if it.PosterAssetID != nil {
				t.Fatalf("封面应被清空：%+v", it)
			}
		}},
		{"没传封面字段不改封面", model.UpdateShowcaseItemReq{Enabled: scPtr(true)}, 0, func(t *testing.T, it model.ShowcaseItem) {
			if it.PosterAssetID == nil || *it.PosterAssetID != scPoster {
				t.Fatalf("封面不应变：%+v", it)
			}
		}},
		{"换成别的图片封面", model.UpdateShowcaseItemReq{PosterAssetID: model.OptionalID{Set: true, ID: scPtr(scPoster)}}, 0, nil},
		{"替换成自己的另一个视频：sort/enabled/封面保持不变", model.UpdateShowcaseItemReq{AssetID: scPtr(scVideo2)}, 0, func(t *testing.T, it model.ShowcaseItem) {
			if it.AssetID != scVideo2 || it.PosterAssetID == nil || *it.PosterAssetID != scPoster || !it.Enabled || it.Prompt != "旧文案" || it.Sort != 0 {
				t.Fatalf("%+v", it)
			}
		}},
		{"替换成别人的平台生成视频，同时清空封面", model.UpdateShowcaseItemReq{AssetID: scPtr(scOthersG), PosterAssetID: model.OptionalID{Set: true}}, 0, func(t *testing.T, it model.ShowcaseItem) {
			if it.AssetID != scOthersG || it.PosterAssetID != nil {
				t.Fatalf("%+v", it)
			}
		}},
		{"替换视频同时换封面", model.UpdateShowcaseItemReq{AssetID: scPtr(scOthersG), PosterAssetID: model.OptionalID{Set: true, ID: scPtr(scPoster)}}, 0, nil},
		{"替换成别人上传的视频被拒", model.UpdateShowcaseItemReq{AssetID: scPtr(scOthersU)}, errcode.ErrAssetNotFound.Code, nil},
		{"替换成不存在的素材被拒", model.UpdateShowcaseItemReq{AssetID: scPtr(uint64(999))}, errcode.ErrAssetNotFound.Code, nil},
		{"替换成图片被拒", model.UpdateShowcaseItemReq{AssetID: scPtr(scPoster)}, errcode.ErrShowcaseInvalid.Code, nil},
		{"一个字段都没传", model.UpdateShowcaseItemReq{}, errcode.ErrShowcaseInvalid.Code, nil},
		{"文案改成空白", model.UpdateShowcaseItemReq{Prompt: scPtr("  ")}, errcode.ErrShowcaseInvalid.Code, nil},
		{"文案超长", model.UpdateShowcaseItemReq{Prompt: scPtr(strings.Repeat("x", 81))}, errcode.ErrShowcaseInvalid.Code, nil},
		{"模型标注超长", model.UpdateShowcaseItemReq{ModelLabel: scPtr(strings.Repeat("x", 41))}, errcode.ErrShowcaseInvalid.Code, nil},
		{"起始秒越界", model.UpdateShowcaseItemReq{StartSec: scPtr(3601.0)}, errcode.ErrShowcaseInvalid.Code, nil},
		{"封面是别人的图片", model.UpdateShowcaseItemReq{PosterAssetID: model.OptionalID{Set: true, ID: scPtr(scOthersI)}}, errcode.ErrAssetNotFound.Code, nil},
		{"封面不是图片", model.UpdateShowcaseItemReq{PosterAssetID: model.OptionalID{Set: true, ID: scPtr(scVideo2)}}, errcode.ErrShowcaseInvalid.Code, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newScEnv()
			id := newItem(e)
			got, err := e.svc.UpdateItem(ctx, scAdmin, id, &tt.req)
			requireCode(t, err, tt.wantCode)
			if tt.wantCode != 0 {
				if e.repo.Items[0].Prompt != "旧文案" || len(e.audit.logs) != 0 {
					t.Fatalf("失败时不应改库也不应写审计：%+v %v", e.repo.Items[0], e.audit.actions())
				}
				return
			}
			if got.ID != id {
				t.Fatalf("返回的后台视图不对：%+v", got)
			}
			if tt.check != nil {
				tt.check(t, e.repo.Items[0])
			}
			if acts := e.audit.actions(); len(acts) != 1 || acts[0] != model.AdminAuditShowcaseUpdate || e.audit.logs[0].TargetID != id || e.audit.logs[0].ActorID != scAdmin {
				t.Fatalf("审计不对：%+v", e.audit.logs)
			}
		})
	}

	t.Run("清空封面后后台视图的封面为 null、poster_url 为空", func(t *testing.T) {
		e := newScEnv()
		id := newItem(e)
		got, err := e.svc.UpdateItem(ctx, scAdmin, id, &model.UpdateShowcaseItemReq{PosterAssetID: model.OptionalID{Set: true}})
		if err != nil || got.PosterAssetID != nil || got.PosterURL != "" {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("条目不存在返回 54001", func(t *testing.T) {
		e := newScEnv()
		_, err := e.svc.UpdateItem(ctx, scAdmin, 404, &model.UpdateShowcaseItemReq{Enabled: scPtr(true)})
		requireCode(t, err, errcode.ErrShowcaseNotFound.Code)
	})
	t.Run("读取失败透传", func(t *testing.T) {
		e := newScEnv()
		id := newItem(e)
		e.repo.Err = errBoom
		if _, err := e.svc.UpdateItem(ctx, scAdmin, id, &model.UpdateShowcaseItemReq{Enabled: scPtr(true)}); !errors.Is(err, errBoom) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("修改后公开接口立即反映", func(t *testing.T) {
		e := newScEnv()
		id := newItem(e)
		if _, err := e.svc.UpdateItem(ctx, scAdmin, id, &model.UpdateShowcaseItemReq{Enabled: scPtr(false)}); err != nil {
			t.Fatal(err)
		}
		if v, _ := e.svc.Public(ctx); len(v.Items) != 0 {
			t.Fatalf("禁用后公开接口不应再返回：%+v", v.Items)
		}
	})
}

// ---------- 删除 ----------

func TestShowcaseService_DeleteItem(t *testing.T) {
	ctx := context.Background()

	t.Run("只删条目，不删素材，写审计", func(t *testing.T) {
		e := newScEnv()
		id := e.seed(model.ShowcaseItem{AssetID: scVideo, Prompt: "x", Enabled: true})
		keep := e.seed(model.ShowcaseItem{AssetID: scVideo2, Prompt: "y", Enabled: true})
		if err := e.svc.DeleteItem(ctx, scAdmin, id); err != nil {
			t.Fatal(err)
		}
		if len(e.repo.Items) != 1 || e.repo.Items[0].ID != keep || len(e.assets.Rows) != 8 { // 默认 5 个素材 + 3 个别人的素材
			t.Fatalf("items=%+v assets=%d", e.repo.Items, len(e.assets.Rows))
		}
		if acts := e.audit.actions(); len(acts) != 1 || acts[0] != model.AdminAuditShowcaseDelete || e.audit.logs[0].TargetID != id {
			t.Fatalf("审计不对：%+v", e.audit.logs)
		}
	})
	t.Run("条目不存在返回 54001，不写审计", func(t *testing.T) {
		e := newScEnv()
		requireCode(t, e.svc.DeleteItem(ctx, scAdmin, 404), errcode.ErrShowcaseNotFound.Code)
		if len(e.audit.logs) != 0 {
			t.Fatal("不应写审计")
		}
	})
	t.Run("删除失败透传", func(t *testing.T) {
		e := newScEnv()
		e.repo.Err = errBoom
		if err := e.svc.DeleteItem(ctx, scAdmin, 1); !errors.Is(err, errBoom) {
			t.Fatalf("%v", err)
		}
	})
}

// ---------- 排序 ----------

func TestShowcaseService_Reorder(t *testing.T) {
	ctx := context.Background()
	seed3 := func(e *scEnv) (a, b, c uint64) {
		a = e.seed(model.ShowcaseItem{AssetID: scVideo, Prompt: "a", Sort: 0, Enabled: true})
		b = e.seed(model.ShowcaseItem{AssetID: scVideo, Prompt: "b", Sort: 1, Enabled: true})
		c = e.seed(model.ShowcaseItem{AssetID: scVideo, Prompt: "c", Sort: 2, Enabled: true})
		return
	}
	sortOf := func(e *scEnv) map[uint64]int {
		m := map[uint64]int{}
		for _, it := range e.repo.Items {
			m[it.ID] = it.Sort
		}
		return m
	}

	t.Run("按数组顺序把 sort 重写为 0..n-1，并写审计", func(t *testing.T) {
		e := newScEnv()
		a, b, c := seed3(e)
		if err := e.svc.Reorder(ctx, scAdmin, []uint64{c, a, b}); err != nil {
			t.Fatal(err)
		}
		if got := sortOf(e); got[c] != 0 || got[a] != 1 || got[b] != 2 {
			t.Fatalf("%v", got)
		}
		if acts := e.audit.actions(); len(acts) != 1 || acts[0] != model.AdminAuditShowcaseOrder || e.audit.logs[0].ActorID != scAdmin {
			t.Fatalf("审计不对：%+v", e.audit.logs)
		}
		if e.repo.Calls != 1 {
			t.Fatalf("应在一个事务里完成，WithTx 调用 %d 次", e.repo.Calls)
		}
	})
	t.Run("空列表对应没有条目时成功", func(t *testing.T) {
		if err := newScEnv().svc.Reorder(ctx, scAdmin, []uint64{}); err != nil {
			t.Fatal(err)
		}
	})

	mismatch := []struct {
		name string
		ids  func(a, b, c uint64) []uint64
	}{
		{"少了一个", func(a, b, c uint64) []uint64 { return []uint64{a, b} }},
		{"多了一个不存在的 id", func(a, b, c uint64) []uint64 { return []uint64{a, b, c, 99} }},
		{"数量对但有不存在的 id", func(a, b, c uint64) []uint64 { return []uint64{a, b, 99} }},
		{"重复了一个（数量对）", func(a, b, c uint64) []uint64 { return []uint64{a, a, b} }},
		{"重复且数量多", func(a, b, c uint64) []uint64 { return []uint64{a, b, c, c} }},
		{"传空列表但有条目", func(a, b, c uint64) []uint64 { return []uint64{} }},
	}
	for _, tt := range mismatch {
		t.Run(tt.name+"返回 54003 且不改顺序", func(t *testing.T) {
			e := newScEnv()
			a, b, c := seed3(e)
			requireCode(t, e.svc.Reorder(ctx, scAdmin, tt.ids(a, b, c)), errcode.ErrShowcaseOrderMismatch.Code)
			if got := sortOf(e); got[a] != 0 || got[b] != 1 || got[c] != 2 || len(e.audit.logs) != 0 {
				t.Fatalf("不应有任何改动：%v %v", got, e.audit.actions())
			}
		})
	}

	t.Run("事务中途失败整体回滚，错误透传", func(t *testing.T) {
		e := newScEnv()
		a, b, c := seed3(e)
		e.repo.FailSetSortAt = 2
		err := e.svc.Reorder(ctx, scAdmin, []uint64{c, b, a})
		if !errors.Is(err, showcasefake.ErrInjected) {
			t.Fatalf("应透传下层错误：%v", err)
		}
		if got := sortOf(e); got[a] != 0 || got[b] != 1 || got[c] != 2 || len(e.audit.logs) != 0 {
			t.Fatalf("应整体回滚：%v", got)
		}
	})
	t.Run("事务本身失败透传", func(t *testing.T) {
		e := newScEnv()
		e.repo.Err = errBoom
		if err := e.svc.Reorder(ctx, scAdmin, []uint64{1}); !errors.Is(err, errBoom) {
			t.Fatalf("%v", err)
		}
	})
}

// ---------- 设置 ----------

func TestShowcaseService_UpdateSettings(t *testing.T) {
	ctx := context.Background()
	req := func(sec int, show, poster bool) *model.UpdateShowcaseSettingsReq {
		return &model.UpdateShowcaseSettingsReq{ClipSeconds: &sec, ShowOnLogin: &show, PosterOnlyOnSaveData: &poster}
	}

	t.Run("成功：一次写入三个键，写审计（含前后值），返回最新设置", func(t *testing.T) {
		e := newScEnv()
		got, err := e.svc.UpdateSettings(ctx, 9, req(12, false, false))
		if err != nil || *got != (model.ShowcaseSettingsView{ClipSeconds: 12}) {
			t.Fatalf("%+v %v", got, err)
		}
		if e.settings.kv[model.SettingShowcaseClipSeconds] != "12" || e.settings.kv[model.SettingShowcaseShowOnLogin] != "false" ||
			e.settings.kv[model.SettingShowcasePosterOnSaveData] != "false" || e.settings.by != 9 {
			t.Fatalf("库值不对：%v by=%d", e.settings.kv, e.settings.by)
		}
		if acts := e.audit.actions(); len(acts) != 1 || acts[0] != model.AdminAuditShowcaseSettings || e.audit.logs[0].ActorID != 9 ||
			e.audit.logs[0].TargetType != model.AdminAuditTargetSettings {
			t.Fatalf("审计不对：%+v", e.audit.logs)
		}
		if v, _ := e.svc.Settings(ctx); *v != *got {
			t.Fatalf("再读应与返回一致：%+v", v)
		}
	})
	t.Run("边界 4 和 15 成功", func(t *testing.T) {
		for _, sec := range []int{4, 15} {
			if _, err := newScEnv().svc.UpdateSettings(ctx, 1, req(sec, true, true)); err != nil {
				t.Fatalf("%d 秒应合法：%v", sec, err)
			}
		}
	})
	t.Run("秒数越界返回 54002，不写库不写审计", func(t *testing.T) {
		for _, sec := range []int{-1, 0, 3, 16, 100} {
			e := newScEnv()
			_, err := e.svc.UpdateSettings(ctx, 1, req(sec, true, true))
			requireCode(t, err, errcode.ErrShowcaseInvalid.Code)
			if len(e.settings.kv) != 0 || len(e.audit.logs) != 0 {
				t.Fatalf("%d 秒不应有写入", sec)
			}
		}
	})
	t.Run("缺字段按参数错误处理", func(t *testing.T) {
		_, err := newScEnv().svc.UpdateSettings(ctx, 1, &model.UpdateShowcaseSettingsReq{})
		requireCode(t, err, errcode.ErrInvalidParams.Code)
	})
	t.Run("写库失败透传，不写审计", func(t *testing.T) {
		e := newScEnv()
		e.settings.setErr = errBoom
		if _, err := e.svc.UpdateSettings(ctx, 1, req(7, true, true)); !errors.Is(err, errBoom) || len(e.audit.logs) != 0 {
			t.Fatalf("%v %v", err, e.audit.actions())
		}
	})
	t.Run("读取失败透传", func(t *testing.T) {
		e := newScEnv()
		e.settings.getErr = errBoom
		if _, err := e.svc.UpdateSettings(ctx, 1, req(7, true, true)); !errors.Is(err, errBoom) {
			t.Fatalf("%v", err)
		}
	})
}
