package service_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/service"
)

func intPtr(n int) *int { return &n }

// 登记一个启用的视频模型与图片、文本模型；video 带可调的时长参数。
func dramaModelRegistry(registry *dramaRegistry, duration *modelcfg.ParamField) {
	video := &provider.Snapshot{ModelRevisionID: 20, Model: provider.ModelSnapshot{Key: "vid", Kind: "video"}}
	if duration != nil {
		video.Model.Capabilities.Params = modelcfg.ParamSet{{Name: "duration", ParamField: *duration}}
	}
	registry.snaps = map[string]*provider.Snapshot{
		"vid": video,
		"img": {ModelRevisionID: 21, Model: provider.ModelSnapshot{Key: "img", Kind: "image"}},
		"txt": {ModelRevisionID: 22, Model: provider.ModelSnapshot{Key: "txt", Kind: "text"}},
	}
	registry.unavailable = map[string]bool{"off": true}
}

func createWithModels(r *dramaMemory, s *service.DramaService, video string, images model.DramaImageModels) error {
	_, err := s.Create(context.Background(), 7, &model.CreateDramaReq{SourceToken: seedDramaDraft(r, "第一章\n正文"), SourceKind: "novel", StyleKey: "custom", StyleCustom: "写实", VideoModel: video, ImageModels: images})
	return err
}

func TestDramaCreateValidatesVideoAndImageModels(t *testing.T) {
	for _, tc := range []struct {
		name   string
		video  string
		images model.DramaImageModels
		code   int
	}{
		{"全空放行", "", model.DramaImageModels{}, 0},
		{"合法模型", "vid", model.DramaImageModels{Character: "img", Scene: "img", Prop: "img"}, 0},
		{"视频模型已停用", "off", model.DramaImageModels{}, 10001},
		{"视频模型类型不符", "img", model.DramaImageModels{}, 10001},
		{"图片模型是文本类型", "", model.DramaImageModels{Scene: "txt"}, 10001},
		{"图片模型是视频类型", "", model.DramaImageModels{Prop: "vid"}, 10001},
		{"图片模型已停用", "", model.DramaImageModels{Character: "off"}, 10001},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newDramaMemory()
			s, registry := dramaTestService(r, &dramaTextStreamer{}, nil)
			dramaModelRegistry(registry, nil)
			err := createWithModels(r, s, tc.video, tc.images)
			if tc.code != 0 {
				dramaCheckCode(t, err, tc.code)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if r.project.VideoModel != tc.video {
				t.Fatalf("video=%q", r.project.VideoModel)
			}
			var got model.DramaImageModels
			if err = json.Unmarshal(r.project.ImageModels, &got); err != nil || got != tc.images {
				t.Fatalf("images=%s err=%v", r.project.ImageModels, err)
			}
		})
	}
}

func TestDramaGetReturnsVideoAndImageModels(t *testing.T) {
	r := newDramaMemory()
	s, registry := dramaTestService(r, &dramaTextStreamer{}, nil)
	dramaModelRegistry(registry, nil)
	if err := createWithModels(r, s, "vid", model.DramaImageModels{Character: "img", Scene: "img"}); err != nil {
		t.Fatal(err)
	}
	view, err := s.Get(context.Background(), 7, r.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.VideoModel != "vid" || view.ImageModels != (model.DramaImageModels{Character: "img", Scene: "img"}) {
		t.Fatalf("view=%+v", view)
	}
	raw, _ := json.Marshal(view)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	if string(fields["video_model"]) != `"vid"` || string(fields["image_models"]) != `{"character":"img","scene":"img","prop":""}` {
		t.Fatalf("json=%s", raw)
	}
}

func TestDramaUpdateVideoAndImageModels(t *testing.T) {
	r := newDramaMemory()
	s, registry := dramaTestService(r, &dramaTextStreamer{}, nil)
	dramaModelRegistry(registry, nil)
	vid, imgs := "vid", model.DramaImageModels{Prop: "img"}
	view, err := s.Update(context.Background(), 7, 1, &model.UpdateDramaReq{Revision: 1, VideoModel: &vid, ImageModels: &imgs})
	if err != nil {
		t.Fatal(err)
	}
	if view.VideoModel != "vid" || view.ImageModels.Prop != "img" {
		t.Fatalf("view=%+v", view)
	}
	// 不带字段时保持原值；带空串清除
	if view, err = s.Update(context.Background(), 7, 1, &model.UpdateDramaReq{Revision: view.Revision}); err != nil || view.VideoModel != "vid" {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	empty := ""
	if view, err = s.Update(context.Background(), 7, 1, &model.UpdateDramaReq{Revision: view.Revision, VideoModel: &empty}); err != nil || view.VideoModel != "" {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	wrongKind := "img"
	for _, req := range []*model.UpdateDramaReq{
		{Revision: view.Revision, VideoModel: &wrongKind},
		{Revision: view.Revision, ImageModels: &model.DramaImageModels{Scene: "vid"}},
	} {
		_, err = s.Update(context.Background(), 7, 1, req)
		dramaCheckCode(t, err, 10001)
	}
}

func TestDramaStoryboardDefaultSecondsFromVideoModel(t *testing.T) {
	enumField := modelcfg.ParamField{Type: modelcfg.ParamEnum, Options: []any{"5", float64(10), "8"}}
	for _, tc := range []struct {
		name     string
		videoKey string
		field    *modelcfg.ParamField
		explicit int
		want     int
	}{
		{"number取max", "vid", &modelcfg.ParamField{Type: modelcfg.ParamNumber, Max: intPtr(12)}, 0, 12},
		{"number上限夹到60", "vid", &modelcfg.ParamField{Type: modelcfg.ParamNumber, Max: intPtr(120)}, 0, 60},
		{"number下限夹到4", "vid", &modelcfg.ParamField{Type: modelcfg.ParamNumber, Max: intPtr(2)}, 0, 4},
		{"enum取最大选项", "vid", &enumField, 0, 10},
		{"无时长参数回落15", "vid", nil, 0, 15},
		{"未配视频模型回落15", "", &enumField, 0, 15},
		{"视频模型已停用回落15", "off", &enumField, 0, 15},
		{"显式值优先", "vid", &enumField, 8, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newDramaMemory()
			storyboardReady(r)
			r.project.VideoModel = tc.videoKey
			s, registry := dramaTestService(r, &dramaTextStreamer{}, nil)
			dramaModelRegistry(registry, tc.field)
			req := storyboardRequest(t, tc.explicit, []int{1})
			// 估价与提交走同一套校验与解析
			if _, err := s.Estimate(context.Background(), 7, 1, req); err != nil {
				t.Fatal(err)
			}
			if _, err := s.CreateJob(context.Background(), 7, 1, req); err != nil {
				t.Fatal(err)
			}
			var input struct {
				MaxSeconds int `json:"max_seconds"`
			}
			if err := json.Unmarshal(r.jobs[0].InputJSON, &input); err != nil || input.MaxSeconds != tc.want {
				t.Fatalf("input=%s err=%v want %d", r.jobs[0].InputJSON, err, tc.want)
			}
			if req.MaxSeconds != tc.explicit {
				t.Fatalf("不应改写调用方的请求: %d", req.MaxSeconds)
			}
		})
	}
}

// 新建资产节点按类型带上项目的默认图片模型；已有节点不动。
func TestDramaCanvasBreakdownAppliesDefaultImageModels(t *testing.T) {
	f := newDramaCanvasFake()
	f.canvas.PayloadJSON = []byte(`{"nodes":[{"id":"old","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"image","label":"街道","model":"keep","drama":{"type":"scene","name":"街道","look":"","episodes":[2],"aliases":[],"description":""}}}],"edges":[]}`)
	svc := service.NewDramaCanvasService(f, &dramaCanvasBroadcast{})
	assets := append(dramaCanvasAssets(1), drama.Asset{Type: "prop", Name: "玉佩", Aliases: []string{}, Episodes: []int{1}, Description: "双鱼玉佩"})
	models := map[string]string{"character": "img-c", "scene": "img-s", "prop": "img-p"}
	created, err := svc.ApplyBreakdown(context.Background(), 1, 3, 7, 9, []int{1}, assets, models)
	if err != nil {
		t.Fatal(err)
	}
	g, err := canvasgraph.Parse(f.canvas.PayloadJSON)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]any{}
	for _, c := range created {
		got[c.Asset.Type] = g.Node(c.NodeID).Data()["model"]
	}
	// 街道合并进已有节点，不属于本次新增
	if !reflect.DeepEqual(got, map[string]any{"character": "img-c", "prop": "img-p"}) {
		t.Fatalf("new nodes=%v created=%+v", got, created)
	}
	if g.Node("old").Data()["model"] != "keep" {
		t.Fatalf("已有节点被改: %+v", g.Node("old").Data())
	}
}

func TestDramaCanvasBreakdownWithoutModelsLeavesModelUnset(t *testing.T) {
	f := newDramaCanvasFake()
	created, err := service.NewDramaCanvasService(f, &dramaCanvasBroadcast{}).ApplyBreakdown(context.Background(), 1, 3, 7, 9, []int{1}, dramaCanvasAssets(1), map[string]string{"prop": "img-p"})
	if err != nil {
		t.Fatal(err)
	}
	g, _ := canvasgraph.Parse(f.canvas.PayloadJSON)
	for _, c := range created {
		if _, ok := g.Node(c.NodeID).Data()["model"]; ok {
			t.Fatalf("未指定的类型不应写 model: %+v", g.Node(c.NodeID).Data())
		}
	}
}

// 拆解步骤把项目配置的图片模型交给画布服务。
func TestDramaBreakdownPassesProjectImageModels(t *testing.T) {
	r := newDramaMemory()
	dramaAddEpisode(r, 1, "剧本")
	r.project.Ratio = "9:16"
	r.project.ImageModels = []byte(`{"character":"img","scene":"","prop":"img"}`)
	canvas := &dramaPipelineCanvas{}
	raw, _ := json.Marshal(drama.BreakdownOutput{Assets: []drama.Asset{{Type: "scene", Name: "街道", Aliases: []string{}, Episodes: []int{1}, Description: "街道"}}})
	e := &dramaTextStreamer{outputs: []string{string(raw)}}
	s, _ := dramaTestService(r, e, canvas)
	j, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "breakdown", Episodes: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); err != nil {
		t.Fatal(err)
	}
	if canvas.applied != 1 || !reflect.DeepEqual(canvas.models, map[string]string{"character": "img", "prop": "img"}) {
		t.Fatalf("applied=%d models=%v job=%+v", canvas.applied, canvas.models, r.jobs[0])
	}
}

// 用指定提示词写法的视频模型跑一次分镜，返回发给文本模型的提示词与任务。
func runStoryboardWithFormat(t *testing.T, format string, promptSettings string) string {
	t.Helper()
	r := newDramaMemory()
	storyboardReady(r)
	e := &dramaTextStreamer{outputs: []string{`{"segments":[{"seconds":12,"prompt":"@[村口](n_scene) 0-12秒：中景，村民走近。"}]}`}}
	s, registry := dramaTestService(r, e, nil)
	dramaModelRegistry(registry, &modelcfg.ParamField{Type: modelcfg.ParamNumber, Max: intPtr(12)})
	registry.snaps["vid"].Model.Capabilities.PromptFormat = format
	r.project.VideoModel = "vid"
	if promptSettings != "" {
		r.project.PromptSettings = []byte(promptSettings)
	}
	job, err := s.CreateJob(context.Background(), 7, 1, storyboardRequest(t, 0, []int{1}))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.HandleDrama(context.Background(), 7, 1, uint64(job.ID)); err != nil {
		t.Fatal(err)
	}
	if r.jobs[0].Status != "succeeded" || len(e.prompts) != 1 {
		t.Fatalf("status=%s code=%d prompts=%d", r.jobs[0].Status, r.jobs[0].ErrorCode, len(e.prompts))
	}
	return e.prompts[0]
}

// 分镜固定写 Seedance 原稿：不管项目视频模型声明哪种写法，都用 Seedance 模板，平台适配交给片场转写。
func TestDramaStoryboardAlwaysSeedance(t *testing.T) {
	const seedanceMark, h3Mark = "即梦 / Seedance", "integrated_multimodal_description"
	for _, format := range []string{"seedance", "h3", "", "sora"} {
		t.Run("模型写法="+format, func(t *testing.T) {
			prompt := runStoryboardWithFormat(t, format, "")
			if !strings.Contains(prompt, seedanceMark) || strings.Contains(prompt, h3Mark) {
				t.Fatalf("应固定用 Seedance 模板:\n%s", prompt)
			}
			for _, want := range []string{`"prompt_format":"seedance"`, `"max_seconds":12`, "硬约束：每段seconds不得超过max_seconds", `"required":["segments"]`} {
				if !strings.Contains(prompt, want) {
					t.Fatalf("提示词缺少 %s", want)
				}
			}
		})
	}
}

// 内置来源的项目快照里存的是创建当时的内置正文副本；发版后内置提示词更新了，
// 已有项目也要用最新的内置提示词，而不是继续用旧副本。
func TestDramaBuiltinPromptIgnoresStaleSnapshotBody(t *testing.T) {
	stale := `{"video":{"source":"builtin","label":"内置","bodies":{"storyboard":"旧版内置正文：已过期"}}}`
	for _, format := range []string{"", "seedance", "h3"} {
		prompt := runStoryboardWithFormat(t, format, stale)
		if strings.Contains(prompt, "旧版内置正文：已过期") {
			t.Fatalf("format=%q 仍在用快照里的旧内置正文:\n%s", format, prompt)
		}
		if !strings.Contains(prompt, "硬约束：每段seconds不得超过max_seconds") {
			t.Fatalf("format=%q 没有用当前内置提示词", format)
		}
	}
}

func TestDramaStoryboardCustomPromptNotOverriddenByFormat(t *testing.T) {
	custom := `{"video":{"source":"mine","id":3,"label":"我的 · 自写","bodies":{"storyboard":"自写正文：只写一句话"}}}`
	for _, format := range []string{"seedance", "h3"} {
		prompt := runStoryboardWithFormat(t, format, custom)
		if !strings.Contains(prompt, "自写正文：只写一句话") || strings.Contains(prompt, "目标平台是即梦 / Seedance") || strings.Contains(prompt, "integrated_multimodal_description") {
			t.Fatalf("用户正文被平台模板覆盖:\n%s", prompt)
		}
		// 用户提示词也能拿到时长上限；写法固定为 seedance
		for _, want := range []string{`"prompt_format":"seedance"`, `"max_seconds":12`} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("变量缺少 %s", want)
			}
		}
	}
}
