package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/model"
)

// 本文件测试后台“从素材库添加”用的素材库列表（ShowcaseService.Library）。

// libEnv 建一个素材库测试环境：5 个平台生成视频（素材 101–105，用户 1 / 2 交替，创建时间递增）、
// 默认环境里别人的生成视频 scOthersG（创建时间为零值，排最后）、1 个上传视频与 1 张生成图片（都不应出现在列表里）。
func libEnv() *scEnv {
	base := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	var extra []model.Asset
	for i := uint64(1); i <= 5; i++ {
		tid := 100 + i
		extra = append(extra, model.Asset{
			ID: 100 + i, UserID: 1 + i%2, Kind: "video", Source: model.AssetSourceGenerated, TaskID: &tid,
			StorageKey: "g/" + strconv.Itoa(int(i)) + ".mp4", ByteSize: int64(i) * 10, Width: 1280, Height: 720, DurationMs: 5000,
			CreatedAt: base.Add(time.Duration(i) * time.Hour),
		})
	}
	extra = append(extra,
		model.Asset{ID: 200, UserID: 1, Kind: "video", Source: model.AssetSourceUpload, StorageKey: "g/up.mp4", CreatedAt: base.Add(10 * time.Hour)},
		model.Asset{ID: 201, UserID: 1, Kind: "image", Source: model.AssetSourceGenerated, StorageKey: "g/img.png", CreatedAt: base.Add(11 * time.Hour)},
	)
	e := newScEnvWith(append(extra, othersExtraAssets()...)...)
	e.users.Names = map[uint64]string{1: "alice", 2: "bob"}
	return e
}

func libIDs(v *model.ShowcaseLibraryView) []uint64 {
	ids := make([]uint64, 0, len(v.Items))
	for _, it := range v.Items {
		ids = append(ids, it.AssetID)
	}
	return ids
}

func TestShowcaseService_Library(t *testing.T) {
	ctx := context.Background()

	t.Run("只列平台生成的视频（排除上传视频、生成图片），按创建时间倒序，字段齐全", func(t *testing.T) {
		e := libEnv()
		e.tasks.Rows[105] = model.GenerationTask{
			ID: 105, ModelKey: "monkey-v3",
			InputJSON:      []byte(`{"prompt":"  一只猫在跳舞  ","duration":5}`),
			ConfigSnapshot: []byte(`{"model":{"key":"monkey-v3","label":"天才猴子三代"}}`),
		}
		v, err := e.svc.Library(ctx, &model.ListShowcaseLibraryReq{})
		if err != nil {
			t.Fatal(err)
		}
		if want := []uint64{105, 104, 103, 102, 101, scOthersG}; !slices.Equal(libIDs(v), want) {
			t.Fatalf("期望 %v，实际 %v", want, libIDs(v))
		}
		if v.Total != 6 || v.Page != 1 || v.PageSize != 48 {
			t.Fatalf("分页信息不对：%+v", v)
		}
		first := v.Items[0]
		if first.VideoURL != "/files/g/5.mp4" || first.Prompt != "一只猫在跳舞" || first.ModelLabel != "天才猴子三代" || first.Owner != "bob" ||
			first.Width != 1280 || first.Height != 720 || first.ByteSize != 50 || first.DurationMs != 5000 || first.Added || first.CreatedAt.IsZero() {
			t.Fatalf("字段不对：%+v", first)
		}
	})

	t.Run("分页边界与非法值回落", func(t *testing.T) {
		all := []uint64{105, 104, 103, 102, 101, scOthersG}
		tests := []struct {
			name      string
			req       model.ListShowcaseLibraryReq
			wantIDs   []uint64
			wantPage  int
			wantSize  int
			wantTotal int64
		}{
			{"第 1 页", model.ListShowcaseLibraryReq{Page: 1, PageSize: 2}, []uint64{105, 104}, 1, 2, 6},
			{"第 2 页", model.ListShowcaseLibraryReq{Page: 2, PageSize: 2}, []uint64{103, 102}, 2, 2, 6},
			{"最后一页", model.ListShowcaseLibraryReq{Page: 3, PageSize: 2}, []uint64{101, scOthersG}, 3, 2, 6},
			{"超出范围返回空数组但保留总数", model.ListShowcaseLibraryReq{Page: 4, PageSize: 2}, []uint64{}, 4, 2, 6},
			{"page<1 回落 1", model.ListShowcaseLibraryReq{Page: -3, PageSize: 1}, []uint64{105}, 1, 1, 6},
			{"page_size<1 回落 48", model.ListShowcaseLibraryReq{Page: 1, PageSize: 0}, all, 1, 48, 6},
			{"page_size 为负回落 48", model.ListShowcaseLibraryReq{Page: 1, PageSize: -5}, all, 1, 48, 6},
			{"page_size 等于上限 100", model.ListShowcaseLibraryReq{Page: 1, PageSize: 100}, all, 1, 100, 6},
			{"page_size>100 收敛到 100", model.ListShowcaseLibraryReq{Page: 1, PageSize: 5000}, all, 1, 100, 6},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				v, err := libEnv().svc.Library(ctx, &tt.req)
				if err != nil {
					t.Fatal(err)
				}
				if v.Items == nil {
					t.Fatal("items 不能是 nil")
				}
				if !slices.Equal(libIDs(v), tt.wantIDs) {
					t.Fatalf("期望 %v，实际 %v", tt.wantIDs, libIDs(v))
				}
				if v.Page != tt.wantPage || v.PageSize != tt.wantSize || v.Total != tt.wantTotal {
					t.Fatalf("分页信息不对：%+v", v)
				}
			})
		}
	})

	t.Run("提示词超过 80 个字符按字符数截断（恰好 80 不截断），模型名超过 40 个字符也截断", func(t *testing.T) {
		e := libEnv()
		input, _ := json.Marshal(map[string]any{"prompt": strings.Repeat("好", 100)})
		snap, _ := json.Marshal(map[string]any{"model": map[string]any{"label": strings.Repeat("模", 50)}})
		e.tasks.Rows[105] = model.GenerationTask{ID: 105, InputJSON: input, ConfigSnapshot: snap}
		input80, _ := json.Marshal(map[string]any{"prompt": strings.Repeat("好", 80)})
		e.tasks.Rows[104] = model.GenerationTask{ID: 104, InputJSON: input80}
		v, err := e.svc.Library(ctx, &model.ListShowcaseLibraryReq{PageSize: 2})
		if err != nil {
			t.Fatal(err)
		}
		if got := []rune(v.Items[0].Prompt); len(got) != 80 {
			t.Fatalf("提示词应截到 80 个字符，实际 %d", len(got))
		}
		if got := []rune(v.Items[0].ModelLabel); len(got) != 40 {
			t.Fatalf("模型名应截到 40 个字符，实际 %d", len(got))
		}
		if v.Items[1].Prompt != strings.Repeat("好", 80) {
			t.Fatalf("恰好 80 个字符不应被截断：%q", v.Items[1].Prompt)
		}
	})

	t.Run("模型展示名：快照里没有时退回模型 key，都没有为空串", func(t *testing.T) {
		e := libEnv()
		e.tasks.Rows[105] = model.GenerationTask{ID: 105, ModelKey: "monkey-v3", InputJSON: []byte(`{"prompt":"a"}`), ConfigSnapshot: []byte(`{"model":{"key":"monkey-v3"}}`)}
		e.tasks.Rows[104] = model.GenerationTask{ID: 104, ModelKey: "monkey-v2", InputJSON: []byte(`{"prompt":"b"}`), ConfigSnapshot: []byte(`not json`)}
		e.tasks.Rows[103] = model.GenerationTask{ID: 103, InputJSON: []byte(`{"prompt":"c"}`)}
		v, err := e.svc.Library(ctx, &model.ListShowcaseLibraryReq{PageSize: 3})
		if err != nil {
			t.Fatal(err)
		}
		if v.Items[0].ModelLabel != "monkey-v3" || v.Items[1].ModelLabel != "monkey-v2" || v.Items[2].ModelLabel != "" {
			t.Fatalf("%+v", v.Items)
		}
		if v.Items[0].Prompt != "a" || v.Items[1].Prompt != "b" || v.Items[2].Prompt != "c" {
			t.Fatalf("%+v", v.Items)
		}
	})

	t.Run("拿不到任务、输入损坏、提示词不是字符串时 prompt 与 model_label 为空串，不报错", func(t *testing.T) {
		e := libEnv()
		// 105 没有对应任务；104 输入损坏；103 提示词是数字；102 没有 prompt 键
		e.tasks.Rows[104] = model.GenerationTask{ID: 104, InputJSON: []byte(`{oops`)}
		e.tasks.Rows[103] = model.GenerationTask{ID: 103, InputJSON: []byte(`{"prompt":123}`)}
		e.tasks.Rows[102] = model.GenerationTask{ID: 102, InputJSON: []byte(`{"duration":5}`)}
		v, err := e.svc.Library(ctx, &model.ListShowcaseLibraryReq{PageSize: 4})
		if err != nil {
			t.Fatal(err)
		}
		if len(v.Items) != 4 {
			t.Fatalf("%+v", v.Items)
		}
		for _, it := range v.Items {
			if it.Prompt != "" || it.ModelLabel != "" {
				t.Fatalf("应回落空串：%+v", it)
			}
		}
	})

	t.Run("素材没有 task_id 时不查任务也不出错", func(t *testing.T) {
		e := newScEnv() // 只有别人的生成视频 scOthersG，没有 TaskID
		e.users.Names[scOther] = "bob"
		v, err := e.svc.Library(ctx, &model.ListShowcaseLibraryReq{})
		if err != nil || len(v.Items) != 1 || v.Items[0].Prompt != "" || v.Items[0].Owner != "bob" {
			t.Fatalf("%+v %v", v, err)
		}
		if e.tasks.Calls != 0 {
			t.Fatalf("没有任务 id 时不应查询任务：%d", e.tasks.Calls)
		}
	})

	t.Run("owner 取不到用户名时用 用户#id", func(t *testing.T) {
		e := libEnv()
		delete(e.users.Names, 2)
		v, err := e.svc.Library(ctx, &model.ListShowcaseLibraryReq{PageSize: 2})
		if err != nil {
			t.Fatal(err)
		}
		// 105 属于用户 1+5%2=2，104 属于用户 1
		if v.Items[0].Owner != "用户#2" || v.Items[1].Owner != "alice" {
			t.Fatalf("%+v", v.Items)
		}
	})

	t.Run("added：被条目引用的素材为 true（含禁用条目、重复引用），其余 false", func(t *testing.T) {
		e := libEnv()
		e.seed(model.ShowcaseItem{AssetID: 104, Prompt: "x", Enabled: true})
		e.seed(model.ShowcaseItem{AssetID: 104, Prompt: "同一素材被引用两次", Enabled: false})
		e.seed(model.ShowcaseItem{AssetID: 999, Prompt: "引用别的素材", Enabled: true})
		v, err := e.svc.Library(ctx, &model.ListShowcaseLibraryReq{})
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range v.Items {
			if it.Added != (it.AssetID == 104) {
				t.Fatalf("added 标记不对：%+v", it)
			}
		}
	})

	t.Run("批量取数：任务与用户各只查一次，不是 N+1", func(t *testing.T) {
		e := libEnv()
		if _, err := e.svc.Library(ctx, &model.ListShowcaseLibraryReq{}); err != nil {
			t.Fatal(err)
		}
		if e.tasks.Calls != 1 || e.users.Calls != 1 {
			t.Fatalf("任务查询 %d 次、用户查询 %d 次，各应只有 1 次", e.tasks.Calls, e.users.Calls)
		}
	})

	t.Run("没有任何平台生成视频：返回空数组，不查任务和用户", func(t *testing.T) {
		e := newScEnv()
		delete(e.assets.Rows, scOthersG)
		v, err := e.svc.Library(ctx, &model.ListShowcaseLibraryReq{})
		if err != nil || v.Items == nil || len(v.Items) != 0 || v.Total != 0 {
			t.Fatalf("%+v %v", v, err)
		}
		raw, _ := json.Marshal(v)
		if !strings.Contains(string(raw), `"items":[]`) {
			t.Fatalf("items 应序列化为 []：%s", raw)
		}
		if e.tasks.Calls != 0 || e.users.Calls != 0 {
			t.Fatalf("空列表不应再查任务 / 用户：%d %d", e.tasks.Calls, e.users.Calls)
		}
	})

	t.Run("下层读取失败原样透传", func(t *testing.T) {
		injects := map[string]func(e *scEnv){
			"素材": func(e *scEnv) { e.assets.Err = errBoom },
			"任务": func(e *scEnv) { e.tasks.Err = errBoom },
			"用户": func(e *scEnv) { e.users.Err = errBoom },
			"条目": func(e *scEnv) { e.repo.Err = errBoom },
		}
		for name, inject := range injects {
			e := libEnv()
			inject(e)
			if _, err := e.svc.Library(ctx, &model.ListShowcaseLibraryReq{}); !errors.Is(err, errBoom) {
				t.Fatalf("%s读取失败应透传：%v", name, err)
			}
		}
	})
}
