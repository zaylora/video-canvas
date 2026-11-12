// 本文件是营销脚本发到画布。
package service

import (
	"context"
	"encoding/json"
	"fmt"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/marketing"
	"video-canvas/internal/marketing/format"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider/modelcfg"

	"go.uber.org/zap"
)

// ToCanvas 把脚本发到新画布：素材节点（商品图、模特图、参考视频）、最新成片节点、每镜一个未生成的视频节点并连线。
// 不自动生成。失败返回 68011。
func (s *MarketingService) ToCanvas(ctx context.Context, userID, id uint64) (*model.MarketingCanvasResult, error) {
	c, err := s.loadScript(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	snap, err := s.videoSnapshot(ctx, c.st.VideoModel)
	videoModel, caps := "", modelcfg.Capabilities{}
	if err == nil {
		videoModel, caps = snap.Model.Key, snap.Model.Capabilities
	}
	spec, err := s.canvasSpec(ctx, c, videoModel, caps)
	if err != nil {
		return nil, err
	}
	payload, err := canvasgraph.BuildMarketing(spec, canvasgraph.Options{})
	if err != nil {
		logger.Warn("生成营销画布内容失败", zap.Uint64("script_id", id), zap.Error(err))
		return nil, errcode.ErrMarketingCanvasFailed
	}
	title := canvasTitle(factsOf(c.project).Name, c.project.Title, c.sc)
	cv, err := s.canvases.Create(ctx, userID, &model.CreateCanvasProjectReq{Title: title, PayloadJSON: payload})
	if err != nil {
		logger.Warn("营销脚本发到画布失败", zap.Uint64("script_id", id), zap.Error(err))
		return nil, errcode.ErrMarketingCanvasFailed
	}
	return &model.MarketingCanvasResult{CanvasID: hexID(cv.ID)}, nil
}

// canvasTitle 是新画布标题：商品名 · 角度（跟拍写「跟拍」），不超过 40 字。
func canvasTitle(factName, projectTitle string, sc *model.MarketingScript) string {
	name := factName
	if name == "" {
		name = projectTitle
	}
	angle := marketing.AngleName(sc.Angle)
	if angle == "" {
		angle = sc.Angle
	}
	return clipRunes(name+" · "+angle, 40)
}

// canvasSpec 组装发到画布的内容：素材视图、最新成片、每镜的单独提示词。
func (s *MarketingService) canvasSpec(ctx context.Context, c *scriptCtx, videoModel string, caps modelcfg.Capabilities) (canvasgraph.MarketingSpec, error) {
	spec := canvasgraph.MarketingSpec{Model: videoModel, Params: map[string]any{}}
	if v, ok := c.st.Params["aspect_ratio"]; ok {
		spec.Params["aspect_ratio"] = v
	}
	if err := s.canvasMedia(ctx, c, &spec); err != nil {
		return spec, err
	}
	in, err := s.formatInput(c, c.st, caps)
	if err != nil {
		return spec, err
	}
	f := format.For(caps.PromptFormat)
	for i := range in.Script.Shots {
		prompt, err := f.RenderShot(in, i)
		if err != nil {
			if prompt, err = format.For("").RenderShot(in, i); err != nil {
				return spec, err
			}
		}
		spec.Shots = append(spec.Shots, canvasgraph.MarketingShot{Label: fmt.Sprintf("镜头 %d", i+1), Prompt: prompt})
	}
	return spec, nil
}

// canvasMedia 读出发到画布的素材节点：商品图、模特图、参考视频和最新成片。
func (s *MarketingService) canvasMedia(ctx context.Context, c *scriptCtx, spec *canvasgraph.MarketingSpec) error {
	var ids []uint64
	if err := json.Unmarshal(c.project.ProductAssetIDs, &ids); err != nil {
		return err
	}
	hasModel := c.round.ModelAssetID != nil
	if hasModel {
		ids = append(ids, *c.round.ModelAssetID)
	}
	for i, aid := range ids {
		a, err := s.assetViewOrNil(ctx, c.sc.UserID, aid)
		if err != nil {
			return err
		}
		if a == nil {
			continue
		}
		label := fmt.Sprintf("商品图 %d", i+1)
		if hasModel && i == len(ids)-1 {
			label = "模特图"
		}
		spec.Images = append(spec.Images, mediaOf(a, label))
	}
	if c.round.RefVideoAssetID != nil {
		a, err := s.assetViewOrNil(ctx, c.sc.UserID, *c.round.RefVideoAssetID)
		if err != nil {
			return err
		}
		if a != nil {
			m := mediaOf(a, "参考视频")
			spec.RefVideo = &m
		}
	}
	final, err := s.latestFinal(ctx, c.sc)
	spec.Final = final
	return err
}

// mediaOf 把素材视图转成画布节点要的素材描述。
func mediaOf(a *model.AssetView, label string) canvasgraph.MarketingMedia {
	return canvasgraph.MarketingMedia{AssetID: assetIDString(a.ID), URL: a.URL, FileName: a.FileName, Width: a.Width, Height: a.Height, Label: label}
}

// latestFinal 取脚本最新一条成功的成片（视频素材）；没有返回 nil。
func (s *MarketingService) latestFinal(ctx context.Context, sc *model.MarketingScript) (*canvasgraph.MarketingMedia, error) {
	tasks, err := s.repo.ScriptTasks(ctx, sc.UserID, []string{marketingNode(sc.ID)})
	if err != nil {
		return nil, err
	}
	for i := len(tasks) - 1; i >= 0; i-- {
		if tasks[i].Status != model.TaskSucceeded {
			continue
		}
		var outputs []model.TaskOutput
		if err := json.Unmarshal(tasks[i].OutputJSON, &outputs); err != nil {
			return nil, err
		}
		for _, o := range outputs {
			if o.MediaType != model.KindVideo || o.AssetID == 0 {
				continue
			}
			a, err := s.assetViewOrNil(ctx, sc.UserID, o.AssetID)
			if err != nil {
				return nil, err
			}
			if a != nil {
				m := mediaOf(a, "成片")
				return &m, nil
			}
		}
	}
	return nil, nil //nolint:nilnil // 没有成片是正常结果
}
