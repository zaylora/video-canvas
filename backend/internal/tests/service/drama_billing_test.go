package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/service"
)

// 积分流水必须满足 freeze = settle + refund，且失败任务整笔退回、不扣费。
func TestDramaLedgerConservation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outputs []string
		status  string
		settled int
	}{
		{"成功只结算实际费用", []string{`{"script_md":"剧本"}`}, "succeeded", 5},
		{"校验两次失败整笔退回", []string{`{bad`, `{bad`}, "failed", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newDramaMemory()
			dramaAddEpisode(r, 1, "")
			s, _ := dramaTestService(r, &dramaTextExecutor{outputs: tc.outputs}, nil)
			j, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts", Episodes: []int{1}})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); err != nil {
				t.Fatal(err)
			}
			sum := map[string]int{}
			for _, e := range r.ledger {
				sum[e.Type] += e.Amount
			}
			if sum[model.LedgerFreeze] != sum[model.LedgerSettle]+sum[model.LedgerRefund] {
				t.Fatalf("流水不守恒: %v", sum)
			}
			if sum[model.LedgerSettle] != tc.settled || r.jobs[0].Status != tc.status || r.credit.Frozen != 0 {
				t.Fatalf("sum=%v job=%+v credit=%+v", sum, r.jobs[0], r.credit)
			}
			// 重复投递终态任务不得再次结算或调用模型。
			if err := s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); err != nil {
				t.Fatal(err)
			}
			if r.credit.Balance != 1000-tc.settled {
				t.Fatalf("重复投递改变了余额: %+v", r.credit)
			}
		})
	}
}

// asset_prompts 失败后手动重试，必须仍只处理第一次拆解新建的资产，而不是整张画布。
func TestDramaAssetPromptsRetryKeepsNewAssetList(t *testing.T) {
	r := newDramaMemory()
	r.project.Ratio = "9:16"
	dramaAddEpisode(r, 1, "正文")
	asset := drama.Asset{Type: "character", Name: "林川", Aliases: []string{}, Episodes: []int{1}, Description: "人物"}
	raw, _ := json.Marshal(drama.BreakdownOutput{Assets: []drama.Asset{asset}})
	good, _ := json.Marshal(drama.AssetPromptsOutput{Prompts: []drama.AssetPrompt{{NodeID: "new", Prompt: "角色正向描述"}}})
	e := &dramaTextExecutor{outputs: []string{string(raw), `{bad`, `{bad`, string(good)}}
	c := &dramaPipelineCanvas{created: []service.DramaCanvasAsset{{NodeID: "new", Asset: asset}}}
	s, _ := dramaTestService(r, e, c)
	ctx := context.Background()

	j, err := s.CreateJob(ctx, 7, 1, &model.DramaJobReq{Step: "breakdown", Episodes: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.HandleDrama(ctx, 7, 1, uint64(j.ID)); err != nil {
		t.Fatal(err)
	}
	// 自动接续的 asset_prompts 两次输出非法 -> 67008、不扣费。
	if err = s.HandleDrama(ctx, 7, 1, r.jobs[1].ID); err != nil {
		t.Fatal(err)
	}
	if r.jobs[1].Status != "failed" || r.jobs[1].ErrorCode != 67008 || r.jobs[1].Credits != 0 {
		t.Fatalf("job=%+v", r.jobs[1])
	}
	retry, err := s.CreateJob(ctx, 7, 1, &model.DramaJobReq{Step: "asset_prompts", Episodes: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.HandleDrama(ctx, 7, 1, uint64(retry.ID)); err != nil {
		t.Fatal(err)
	}
	last := e.prompts[len(e.prompts)-1]
	if r.jobs[2].Status != "succeeded" || c.prompted != 1 || !strings.Contains(last, `"node_id":"new"`) {
		t.Fatalf("job=%+v prompted=%d prompt=%s", r.jobs[2], c.prompted, last)
	}
	// 失败的一次不扣费，成功的 breakdown 与重试各扣一次。
	if r.credit.Balance != 1000-2*5 || r.credit.Frozen != 0 {
		t.Fatalf("credit=%+v", r.credit)
	}
}
