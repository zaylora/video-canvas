package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/service"
)

// dramaTargetRegistry 在文本模型之外登记一个图片模型和一个视频模型
func dramaTargetRegistry(registry *dramaRegistry) {
	registry.models = append(registry.models,
		provider.ModelInfo{Key: "img", Kind: "image", Label: "即梦 5.0", Vendor: "bytedance"},
		provider.ModelInfo{Key: "vid", Kind: "video", Label: "Seedance 2.0", Vendor: "bytedance"},
	)
	registry.snaps = map[string]*provider.Snapshot{
		"img": {Model: provider.ModelSnapshot{Key: "img", Kind: "image", UpstreamModel: "doubao-seedream-5-0"}},
		"vid": {Model: provider.ModelSnapshot{Key: "vid", Kind: "video", UpstreamModel: "doubao-seedance-2-0"}},
	}
}

// 拆解时选的图片 / 视频模型存到项目上，资产提示词按图片模型的写法写
func TestDramaBreakdownTargetModels(t *testing.T) {
	r := newDramaMemory()
	r.project.Ratio = "9:16"
	dramaAddEpisode(r, 1, "正文")
	asset := drama.Asset{Type: "character", Name: "林川", Aliases: []string{}, Episodes: []int{1}, Description: "人物", VoicePrompt: "低沉"}
	raw, _ := json.Marshal(drama.BreakdownOutput{Assets: []drama.Asset{asset}})
	prompts, _ := json.Marshal(drama.AssetPromptsOutput{Prompts: []drama.AssetPrompt{{NodeID: "new", Prompt: "角色正向描述"}}})
	e := &dramaTextStreamer{outputs: []string{string(raw), string(prompts)}}
	c := &dramaPipelineCanvas{created: []service.DramaCanvasAsset{{NodeID: "new", Asset: asset}}}
	s, registry := dramaTestService(r, e, c)
	dramaTargetRegistry(registry)

	j, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "breakdown", Episodes: []int{1}, ImageModel: "img", VideoModel: "vid"})
	if err != nil {
		t.Fatal(err)
	}
	if r.project.ImageModel != "img" || r.project.VideoModel != "vid" {
		t.Fatalf("项目没有记下所选模型：%q %q", r.project.ImageModel, r.project.VideoModel)
	}
	if err = s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); err != nil {
		t.Fatal(err)
	}
	if err = s.HandleDrama(context.Background(), 7, 1, r.jobs[1].ID); err != nil {
		t.Fatal(err)
	}
	prompt := e.prompts[1]
	for _, want := range []string{`"target_models"`, `"doubao-seedream-5-0"`, "### Seedream 5.0 Pro", "name: short-drama-image-prompts"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("资产提示词请求缺少 %s", want)
		}
	}
}

// 传了不存在或类型不对的模型直接拒绝，不建任务
func TestDramaJobRejectsUnknownTargetModel(t *testing.T) {
	for name, req := range map[string]*model.DramaJobReq{
		"不存在的图片模型":   {Step: "breakdown", Episodes: []int{1}, ImageModel: "missing"},
		"视频模型填成图片模型": {Step: "breakdown", Episodes: []int{1}, VideoModel: "img"},
	} {
		t.Run(name, func(t *testing.T) {
			r := newDramaMemory()
			r.project.Ratio = "9:16"
			dramaAddEpisode(r, 1, "正文")
			s, registry := dramaTestService(r, &dramaTextStreamer{}, &dramaPipelineCanvas{})
			dramaTargetRegistry(registry)
			_, err := s.CreateJob(context.Background(), 7, 1, req)
			dramaCheckCode(t, err, errcode.ErrInvalidParams.Code)
			if len(r.jobs) != 0 {
				t.Fatalf("不该建任务：%+v", r.jobs)
			}
		})
	}
}

// 视频平台在新建短剧时就选：存进项目；填了不可用或不是视频的模型直接拒绝
func TestDramaCreateKeepsVideoPlatform(t *testing.T) {
	r := newDramaMemory()
	s, registry := dramaTestService(r, &dramaTextStreamer{}, nil)
	dramaTargetRegistry(registry)
	if _, err := s.Create(context.Background(), 7, &model.CreateDramaReq{Text: "第一章\n正文", StyleKey: "custom", StyleCustom: "写实", VideoModel: "vid"}); err != nil {
		t.Fatal(err)
	}
	if r.project.VideoModel != "vid" {
		t.Fatalf("项目没记下视频平台：%q", r.project.VideoModel)
	}

	r = newDramaMemory()
	s, registry = dramaTestService(r, &dramaTextStreamer{}, nil)
	dramaTargetRegistry(registry)
	_, err := s.Create(context.Background(), 7, &model.CreateDramaReq{Text: "第一章\n正文", StyleKey: "custom", StyleCustom: "写实", VideoModel: "img"})
	dramaCheckCode(t, err, errcode.ErrInvalidParams.Code)
}

// 拆解按项目的视频平台写音色：输入里带平台的参考音频限制和每个角色的试音秒数，并拼入该平台的写法资料
func TestDramaBreakdownFollowsVideoPlatform(t *testing.T) {
	r := newDramaMemory()
	r.project.Ratio = "9:16"
	r.project.VideoModel = "vid"
	dramaAddEpisode(r, 1, "正文")
	asset := drama.Asset{Type: "prop", Name: "伞", Aliases: []string{}, Episodes: []int{1}, Description: "黑伞"}
	raw, _ := json.Marshal(drama.BreakdownOutput{Assets: []drama.Asset{asset}})
	e := &dramaTextStreamer{outputs: []string{string(raw)}}
	s, registry := dramaTestService(r, e, &dramaPipelineCanvas{})
	dramaTargetRegistry(registry)
	j, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "breakdown", Episodes: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"voice_seconds":5`, `"ref_audio":{"max_clips":3`, "# Seedance 2.0 提示词方言"} {
		if !strings.Contains(e.prompts[0], want) {
			t.Fatalf("拆解请求缺少 %s", want)
		}
	}
}
