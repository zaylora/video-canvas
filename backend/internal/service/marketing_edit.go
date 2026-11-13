// 本文件是写完脚本后改出片素材与设置：换视频模型与分辨率、换模特图、换商品图、按新商品图重新识别外观锚点。
// 画幅、时长、平台、旁白语言和「有没有模特」决定了脚本怎么写，这里一律不让改，要改就再写一轮。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	"video-canvas/internal/marketing"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
)

// 决定分镜的视频参数：画幅（两种叫法）与时长。
var lockedParamKeys = [][]string{{"aspect_ratio", "ratio"}, {"duration"}}

// paramValue 取参数里第一个存在的键，统一成字符串比较（JSON 数字解出来是 float64，15 与 15.0 视为相同）。
func paramValue(params map[string]any, keys []string) (string, bool) {
	for _, k := range keys {
		if v, ok := params[k]; ok && v != nil {
			return fmt.Sprint(v), true
		}
	}
	return "", false
}

// checkLockedParams 确认画幅与时长没变：原来有的值，新参数里必须一样。
func checkLockedParams(old, next map[string]any) error {
	for _, keys := range lockedParamKeys {
		was, ok := paramValue(old, keys)
		if !ok {
			continue
		}
		if now, _ := paramValue(next, keys); now != was {
			return errcode.ErrInvalidParams.WithMsg("画幅和时长决定了分镜，不能在这里改；要换请再写一轮")
		}
	}
	return nil
}

// UpdateRoundSettings 改一轮的出片设置：视频模型、分辨率（画幅、时长不能变）、换模特图（原来有才能换，不能加也不能去掉）。
// 只影响之后的出片，已出的片不动。轮次写脚本中 68007；模型不满足 68004；素材无效 68002。
func (s *MarketingService) UpdateRoundSettings(ctx context.Context, userID, roundID uint64, req *model.UpdateMarketingRoundSettingsReq) (*model.MarketingRoundView, error) {
	round, err := s.repo.Round(ctx, userID, roundID, false)
	if mktNotFound(err) {
		return nil, errcode.ErrMarketingNotFound
	}
	if err != nil {
		return nil, err
	}
	st, err := settingsOf(round)
	if err != nil {
		return nil, err
	}
	params := map[string]any{}
	maps.Copy(params, req.Params)
	if err := checkLockedParams(st.Params, params); err != nil {
		return nil, err
	}
	modelID, err := s.nextModelAsset(ctx, userID, round, req.ModelAssetID)
	if err != nil {
		return nil, err
	}
	st.VideoModel, st.Params = req.VideoModel, params
	err = s.repo.WithTx(ctx, func(tx repository.MarketingTx) error {
		return s.saveRoundSettings(ctx, tx, userID, round, st, modelID)
	})
	if err != nil {
		return nil, err
	}
	return s.viewAndPush(ctx, userID, roundID)
}

// nextModelAsset 返回改完后的模特图：不换沿用原来的；要换时这一轮原来必须有模特图（不能加），新素材须是本人的图片。
func (s *MarketingService) nextModelAsset(ctx context.Context, userID uint64, round *model.MarketingRound, want *uint64) (*uint64, error) {
	if want == nil {
		return round.ModelAssetID, nil
	}
	if round.ModelAssetID == nil {
		return nil, errcode.ErrInvalidParams.WithMsg("这一轮没有模特图，加模特请再写一轮")
	}
	if err := s.checkAssetKind(ctx, userID, *want, "image"); err != nil {
		return nil, err
	}
	return want, nil
}

// saveRoundSettings 在事务里锁项目和轮次、确认不在写脚本、按新的参考图张数校验视频模型，写回设置与模特图。
func (s *MarketingService) saveRoundSettings(ctx context.Context, tx repository.MarketingTx, userID uint64, round *model.MarketingRound,
	st model.MarketingSettings, modelID *uint64) error {
	p, err := tx.Project(ctx, userID, round.ProjectID, true)
	if mktNotFound(err) {
		return errcode.ErrMarketingNotFound
	}
	if err != nil {
		return err
	}
	cur, err := tx.Round(ctx, userID, round.ID, true)
	if err != nil {
		return err
	}
	if cur.Status == model.MarketingRoundScripting {
		return errcode.ErrMarketingRoundBusy
	}
	images := productCount(p)
	if modelID != nil {
		images++
	}
	if _, err := s.checkVideoModel(ctx, st.VideoModel, st.Params, images, cur.RefVideoAssetID != nil); err != nil {
		return err
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if err := tx.UpdateRound(ctx, cur.ID, map[string]any{"settings": raw, "model_asset_id": modelID}); err != nil {
		return err
	}
	return s.syncProjectModel(ctx, tx, userID, p, cur.ID, modelID)
}

// syncProjectModel 改的是最新一轮的模特图时，项目的默认模特图跟着换（下一轮没传模特时沿用它）。
func (s *MarketingService) syncProjectModel(ctx context.Context, tx repository.MarketingTx, userID uint64, p *model.MarketingProject, roundID uint64, modelID *uint64) error {
	if modelID == nil || (p.ModelAssetID != nil && *p.ModelAssetID == *modelID) {
		return nil
	}
	rounds, err := tx.ProjectRounds(ctx, userID, p.ID)
	if err != nil {
		return err
	}
	if len(rounds) == 0 || rounds[0].ID != roundID {
		return nil
	}
	return tx.UpdateProject(ctx, p.ID, map[string]any{"model_asset_id": *modelID})
}

// productAssetsField 校验新的商品图（1–4 张、本人图片、不重复），返回要写入项目的列值。
func (s *MarketingService) productAssetsField(ctx context.Context, userID uint64, ids []uint64) ([]byte, error) {
	if _, err := s.checkAssets(ctx, userID, ids, nil, nil, modelcfg.Capabilities{}); err != nil {
		return nil, err
	}
	return json.Marshal(ids)
}

// checkProductSwap 在已锁项目的事务里确认可以换商品图：没有轮次在写脚本（事实卡和脚本正读这些图），
// 且每一轮的视频模型都收得下新的参考图张数（模型已下线的轮次跳过，出片时再报）。
func (s *MarketingService) checkProductSwap(ctx context.Context, tx repository.MarketingTx, userID, projectID uint64, count int) error {
	rounds, err := tx.ProjectRounds(ctx, userID, projectID)
	if err != nil {
		return err
	}
	for i := range rounds {
		r := &rounds[i]
		if r.Status == model.MarketingRoundScripting {
			return errcode.ErrMarketingRoundBusy
		}
		st, err := settingsOf(r)
		if err != nil {
			return err
		}
		snap, err := s.models.Snapshot(ctx, st.VideoModel)
		if err != nil {
			continue
		}
		need := count
		if r.ModelAssetID != nil {
			need++
		}
		if image := snap.Model.Capabilities.Refs.Image; image.On && image.Max < need {
			return errcode.ErrMarketingVideoModel.WithMsg(fmt.Sprintf("第 %d 轮的视频模型最多支持 %d 张参考图，换图后需要 %d 张", r.No, image.Max, need))
		}
	}
	return nil
}

// RefreshFacts 按项目当前的商品图重新识别外观锚点：在最新一轮上重跑事实卡环节，按文本模型计费；
// 跑完只替换外观锚点，商品名、卖点、禁用说法、引导下单保留（见 mergeRefreshedFacts）。
// 还没有事实卡时请走重试；最新一轮写脚本中 68007；积分不足 40001。
func (s *MarketingService) RefreshFacts(ctx context.Context, userID, projectID uint64) (*model.MarketingRoundView, error) {
	var roundID uint64
	err := s.repo.WithTx(ctx, func(tx repository.MarketingTx) error {
		p, err := tx.Project(ctx, userID, projectID, true)
		if mktNotFound(err) {
			return errcode.ErrMarketingNotFound
		}
		if err != nil {
			return err
		}
		if len(p.Product) == 0 {
			return errcode.ErrInvalidParams.WithMsg("还没有商品事实卡，请先重试事实卡环节")
		}
		rounds, err := tx.ProjectRounds(ctx, userID, projectID)
		if err != nil {
			return err
		}
		if len(rounds) == 0 {
			return errcode.ErrMarketingNotFound
		}
		st, err := settingsOf(&rounds[0])
		if err != nil {
			return err
		}
		roundID = rounds[0].ID
		return s.requeue(ctx, tx, userID, roundID, marketing.StepFacts, st)
	})
	if err != nil {
		return nil, err
	}
	return s.viewAndPush(ctx, userID, roundID)
}

// mergeRefreshedFacts 项目已有事实卡时（重新识别），只取新结果的外观锚点，其余保留用户已有的内容。
func mergeRefreshedFacts(existing []byte, fresh marketing.ProductFacts) (marketing.ProductFacts, error) {
	if len(existing) == 0 {
		return fresh, nil
	}
	var cur marketing.ProductFacts
	if err := json.Unmarshal(existing, &cur); err != nil {
		return fresh, fmt.Errorf("解码事实卡失败：%w", err)
	}
	cur.Anchors = fresh.Anchors
	return cur, nil
}
