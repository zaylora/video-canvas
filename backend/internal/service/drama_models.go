package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
)

// 分镜单段时长的合法范围与缺省，与前端 STORYBOARD_SECONDS 一致。
const (
	storyboardMinSeconds      = 4
	storyboardMaxSecondsLimit = 60
	storyboardDefaultSeconds  = 15
)

// checkAssetModel 校验短剧里指定的视频 / 图片模型：空串放行；非空必须是后台已启用且种类匹配的模型。
func (s *DramaService) checkAssetModel(ctx context.Context, key, kind, label string) error {
	if key == "" {
		return nil
	}
	invalid := errcode.ErrInvalidParams.WithMsg(label + "不可用，请重新选择")
	if s.registry == nil {
		return invalid
	}
	snap, err := s.registry.Snapshot(ctx, key)
	if errors.Is(err, provider.ErrModelUnavailable) {
		return invalid
	}
	if err != nil {
		return err
	}
	if snap == nil || snap.Model.Kind != kind {
		return invalid
	}
	return nil
}

// checkDramaModels 校验视频模型和三类资产的图片模型，nil 表示该项不改。
func (s *DramaService) checkDramaModels(ctx context.Context, video *string, images *model.DramaImageModels) error {
	if video != nil {
		if err := s.checkAssetModel(ctx, *video, modelcfg.KindVideo, "视频模型"); err != nil {
			return err
		}
	}
	if images == nil {
		return nil
	}
	for _, item := range []struct{ key, label string }{{images.Character, "人物图片模型"}, {images.Scene, "场景图片模型"}, {images.Prop, "物品图片模型"}} {
		if err := s.checkAssetModel(ctx, item.key, modelcfg.KindImage, item.label); err != nil {
			return err
		}
	}
	return nil
}

// dramaImageModels 解码项目保存的三类图片模型；坏数据按未指定处理。
func dramaImageModels(p *model.DramaProject) model.DramaImageModels {
	var out model.DramaImageModels
	if len(p.ImageModels) > 0 {
		_ = json.Unmarshal(p.ImageModels, &out)
	}
	return out
}

// storyboardSeconds 分镜单段时长上限：请求显式给了以请求为准；否则取项目视频模型的时长上限（夹在 4–60），
// 项目没配视频模型、模型已不可用或没有时长参数时回落 15。估价与提交共用，保证两边一致。
func (s *DramaService) storyboardSeconds(ctx context.Context, p *model.DramaProject, req *model.DramaJobReq) (int, error) {
	if req.MaxSeconds != 0 {
		return req.MaxSeconds, nil
	}
	if p.VideoModel == "" || s.registry == nil {
		return storyboardDefaultSeconds, nil
	}
	snap, err := s.registry.Snapshot(ctx, p.VideoModel)
	if errors.Is(err, provider.ErrModelUnavailable) {
		return storyboardDefaultSeconds, nil
	}
	if err != nil {
		return 0, err
	}
	if snap == nil {
		return storyboardDefaultSeconds, nil
	}
	maxSeconds, ok := modelMaxSeconds(snap.Model.Capabilities)
	if !ok {
		return storyboardDefaultSeconds, nil
	}
	return min(storyboardMaxSecondsLimit, max(storyboardMinSeconds, int(math.Floor(maxSeconds)))), nil
}

// modelMaxSeconds 读模型 duration 参数的上限：number 取 max，enum 取数字选项里最大的；没有时长参数返回 false。
func modelMaxSeconds(caps modelcfg.Capabilities) (float64, bool) {
	field, ok := caps.Params.Get("duration")
	if !ok {
		return 0, false
	}
	if field.Type == modelcfg.ParamNumber {
		if field.Max == nil {
			return 0, false
		}
		return float64(*field.Max), true
	}
	best, found := 0.0, false
	for _, option := range field.Options {
		n, ok := optionNumber(option)
		if ok && (!found || n > best) {
			best, found = n, true
		}
	}
	return best, found
}

// optionNumber 把枚举选项（字符串或数字）转成数字，无法转换返回 false。
func optionNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, !math.IsNaN(n) && !math.IsInf(n, 0)
	case int:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
	case fmt.Stringer:
		return optionNumber(n.String())
	}
	return 0, false
}
