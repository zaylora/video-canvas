package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// DramaSegmentAdapter 用项目文本模型把片段原稿转写成目标平台写法。
type DramaSegmentAdapter interface {
	// AdaptSegment 返回通过校验的转写稿和应收积分（只算钱不扣）；积分不够、模型不可用、超时或两次都不合格返回业务错误。
	AdaptSegment(ctx context.Context, p *model.DramaProject, format string, in drama.AdaptInput) (string, int, error)
}

// SetAdapter 在组装完成后注入转写实现：转写依赖短剧服务的文本模型调用，而短剧服务又依赖片段服务。
func (s *DramaSegmentService) SetAdapter(a DramaSegmentAdapter) { s.adapter = a }

// Adapt 把片段原稿（Seedance 写法）转写成 format 写法并按格式存下，返回最新片段视图。
func (s *DramaSegmentService) Adapt(ctx context.Context, userID, sid uint64, req *model.AdaptDramaSegmentReq) (*model.DramaSegmentView, error) {
	// 1. 只有通用和 H3 需要转写；Seedance 就是原稿本身
	if !drama.AdaptFormatSupported(req.Format) {
		return nil, errcode.ErrDramaAdaptFormat
	}
	// 2. 读片段和项目（不加锁：接下来要调模型，不能在事务里做网络 I/O）；
	//    revision 必须是用户眼前那一版，确保转写的就是屏幕上的原稿
	row, err := s.repo.Segment(ctx, userID, sid, false)
	if err != nil {
		return nil, segmentError(err)
	}
	ep, err := s.repo.SegmentEpisode(ctx, userID, row.EpisodeID)
	if err != nil {
		return nil, segmentError(err)
	}
	p, err := s.repo.Project(ctx, userID, ep.DramaID, false)
	if err != nil {
		return nil, segmentError(err)
	}
	if row.Revision != req.Revision {
		return nil, errcode.ErrCanvasConflict
	}
	if strings.TrimSpace(row.Prompt) == "" {
		return nil, errcode.ErrDramaAdaptEmpty
	}
	if s.adapter == nil {
		return nil, errcode.ErrDramaNoTextModel
	}
	// 3. 同一片段同一格式同时只转一次：重复点击或多标签页并发时直接告诉用户正在转写，不重复花模型调用。
	//    锁在进程内，多实例部署时最坏是各转一次，结果后写覆盖，不会损坏数据
	key := adaptKey(sid, req.Format)
	if _, busy := s.adapting.LoadOrStore(key, struct{}{}); busy {
		return nil, errcode.ErrDramaAdaptBusy
	}
	defer s.adapting.Delete(key)
	source := row.Prompt
	out, credits, err := s.adapter.AdaptSegment(ctx, p, req.Format, drama.AdaptInput{Source: source, Seconds: row.Seconds, Ratio: p.Ratio})
	if err != nil {
		return nil, err
	}
	// 4. 写回时重新按项目→片段加锁；不再比较 revision：转写期间用户可能又改了原稿（自动保存会推高 revision），
	//    这时照样存下结果，指纹记的是转写时的原稿，前端据此显示「已过期」，不白白丢掉一次转写；
	//    扣费和写回同一个事务：要么转写稿存下并扣费，要么都不发生
	err = s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		_, lockedEp, locked, err := lockSegment(ctx, tx, userID, sid)
		if err != nil {
			return err
		}
		all, err := segmentAdaptations(locked)
		if err != nil {
			return err
		}
		all[req.Format] = model.DramaSegmentAdaptation{Prompt: out, SourceHash: drama.SourceHash(source), UpdatedAt: time.Now()}
		if locked.Adaptations, err = json.Marshal(all); err != nil {
			return err
		}
		if err := tx.SaveSegment(ctx, locked, locked.Revision); err != nil {
			return err
		}
		note := fmt.Sprintf("片场转写 · 第 %d 集片段 %02d · %s", lockedEp.No, locked.Seq+1, adaptFormatLabel(req.Format))
		return chargeAdapt(ctx, tx, userID, sid, credits, note)
	})
	if err != nil {
		return nil, segmentError(err)
	}
	// 5. 推送完整视图：转写可能耗时几十秒，用户离开片段再回来也能看到结果
	return s.changedView(ctx, userID, sid)
}

// editAdaptation 手改一份已有的转写稿：标记 edited，指纹保持不变（过期与否仍按原稿判断）。
func editAdaptation(row *model.DramaSegment, edit *model.DramaSegmentAdaptationEdit) error {
	all, err := segmentAdaptations(row)
	if err != nil {
		return err
	}
	current, ok := all[edit.Format]
	if !ok {
		return errcode.ErrDramaAdaptMissing
	}
	current.Prompt, current.Edited, current.UpdatedAt = edit.Prompt, true, time.Now()
	all[edit.Format] = current
	row.Adaptations, err = json.Marshal(all)
	return err
}

// segmentAdaptations 解出片段的转写稿；空列（旧数据）当作没有转写稿，返回非 nil 的空表。
func segmentAdaptations(row *model.DramaSegment) (map[string]model.DramaSegmentAdaptation, error) {
	all := map[string]model.DramaSegmentAdaptation{}
	if len(row.Adaptations) == 0 {
		return all, nil
	}
	if err := json.Unmarshal(row.Adaptations, &all); err != nil {
		return nil, err
	}
	if all == nil {
		all = map[string]model.DramaSegmentAdaptation{}
	}
	return all, nil
}

// adaptKey 是进程内转写锁的键：片段 + 格式。
func adaptKey(sid uint64, format string) string {
	return strconv.FormatUint(sid, 10) + ":" + format
}

// chargeAdapt 在账户锁内扣转写费：实扣封顶在可用积分（余额减冻结），不扣成负数、不动进行中任务冻结的积分；
// 和 Agent 对话一样成对写一条冻结流水和一条结算流水（金额相同、冻结额不变），后台对账等式保持成立。
// 应收为 0（免费模型）或账户不存在时不写流水。
func chargeAdapt(ctx context.Context, tx repository.DramaTx, userID, sid uint64, credits int, note string) error {
	if credits <= 0 {
		return nil
	}
	credit, err := tx.LockCredit(ctx, userID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	charged := min(credits, max(credit.Balance-credit.Frozen, 0))
	if charged == 0 {
		return nil
	}
	for _, typ := range []string{model.LedgerFreeze, model.LedgerSettle} {
		if _, err := tx.InsertLedger(ctx, &model.CreditLedger{UserID: userID, DramaSegmentID: &sid, Type: typ, Amount: charged, Note: note}); err != nil {
			return err
		}
	}
	return tx.AddCredit(ctx, userID, -charged, 0)
}

// adaptFormatLabel 是流水备注里的格式名。
func adaptFormatLabel(format string) string {
	if format == drama.PromptFormatH3 {
		return "H3"
	}
	return "通用"
}
