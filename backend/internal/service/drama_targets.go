package service

import (
	"context"
	"slices"

	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
)

// checkTargetModels 请求里带的图片 / 视频模型必须是当前用户可用的同类模型，否则直接拒绝，不建任务。
func (s *DramaService) checkTargetModels(ctx context.Context, req *model.DramaJobReq) error {
	for _, target := range []struct{ key, kind, label string }{
		{req.ImageModel, modelcfg.KindImage, "图片"},
		{req.VideoModel, modelcfg.KindVideo, "视频"},
	} {
		if target.key == "" {
			continue
		}
		info, err := s.findModel(ctx, target.kind, target.key)
		if err != nil {
			return err
		}
		if info == nil {
			return errcode.ErrInvalidParams.WithMsg("所选" + target.label + "模型不可用，请重新选择")
		}
	}
	return nil
}

// findModel 在可用模型里按 key 找一个指定种类的模型；找不到返回 nil。
func (s *DramaService) findModel(ctx context.Context, kind, key string) (*provider.ModelInfo, error) {
	if s.registry == nil || key == "" {
		return nil, nil
	}
	models, err := s.registry.ListModels(ctx, kind)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(models, func(m provider.ModelInfo) bool { return m.Key == key })
	if i < 0 {
		return nil, nil
	}
	return &models[i], nil
}

// targetModelFields 是请求要写回项目的模型字段；和项目现值相同的不写。
func targetModelFields(p *model.DramaProject, req *model.DramaJobReq) map[string]any {
	fields := map[string]any{}
	if req.ImageModel != "" && req.ImageModel != p.ImageModel {
		fields["image_model"] = req.ImageModel
	}
	if req.VideoModel != "" && req.VideoModel != p.VideoModel {
		fields["video_model"] = req.VideoModel
	}
	return fields
}

// withTargetModels 返回套上请求所选模型的项目副本，估价时用，不改原项目。
func withTargetModels(p *model.DramaProject, req *model.DramaJobReq) *model.DramaProject {
	out := *p
	if req.ImageModel != "" {
		out.ImageModel = req.ImageModel
	}
	if req.VideoModel != "" {
		out.VideoModel = req.VideoModel
	}
	return &out
}

// dramaTargets 把项目记下的模型 key 解析成提示词选写法要用的厂商、显示名和上游模型名。
// 模型后来下线或不可用时那一项留空，提示词按通用写法，不让整步失败。
func (s *DramaService) dramaTargets(ctx context.Context, p *model.DramaProject) drama.Targets {
	return drama.Targets{
		Image: s.targetModel(ctx, modelcfg.KindImage, p.ImageModel),
		Video: s.targetModel(ctx, modelcfg.KindVideo, p.VideoModel),
	}
}

func (s *DramaService) targetModel(ctx context.Context, kind, key string) drama.TargetModel {
	info, err := s.findModel(ctx, kind, key)
	if err != nil || info == nil {
		return drama.TargetModel{}
	}
	upstream := ""
	if snap, err := s.registry.Snapshot(ctx, key); err == nil && snap != nil {
		upstream = snap.Model.UpstreamModel
	}
	if kind == modelcfg.KindVideo {
		return drama.NewVideoTarget(info.Key, info.Label, info.Vendor, upstream)
	}
	return drama.TargetModel{Key: info.Key, Label: info.Label, Vendor: info.Vendor, Upstream: upstream}
}

// referenceChars 是这一步拼进提示词的参考技能字数，报价时计入输入长度。
func (s *DramaService) referenceChars(ctx context.Context, p *model.DramaProject, step string) int {
	return drama.ReferenceChars(step, s.dramaTargets(ctx, p))
}
