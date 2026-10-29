package service_test

import (
	"context"
	"testing"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
)

// 短剧文本步骤只用后台的 Agent 模型；只配了文本模型时等同于没有可用模型。
func TestDramaIgnoresTextModels(t *testing.T) {
	r := newDramaMemory()
	dramaAddEpisode(r, 1, "")
	s, registry := dramaTestService(r, &dramaTextStreamer{}, nil)
	registry.models = []provider.ModelInfo{{Key: "text", Kind: "text"}}
	_, err := s.Estimate(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts"})
	dramaCheckCode(t, err, 67007)
}

// 模型请求发往快照里的渠道，带解密后的渠道 Key、上游模型名和接口格式。
func TestDramaCallsAgentChannel(t *testing.T) {
	r := newDramaMemory()
	dramaAddEpisode(r, 1, "")
	e := &dramaTextStreamer{outputs: []string{`{"script_md":"剧本"}`}}
	s, _ := dramaTestService(r, e, nil)
	j, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts", Episodes: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); err != nil {
		t.Fatal(err)
	}
	if len(e.targets) != 1 {
		t.Fatalf("calls=%d", len(e.targets))
	}
	got := e.targets[0]
	if got.ChannelKey != "chan" || got.BaseURL != "https://llm.example" || got.APIKey != "sk-test" || got.UpstreamModel != "upstream-chat" || got.Format != "openai" {
		t.Fatalf("target=%+v", got)
	}
}

// Agent 模型只按 Token 计费：成功后按上游回报的实际用量结算，多冻结的退回。
func TestDramaSettlesAgentModelByTokenUsage(t *testing.T) {
	r := newDramaMemory()
	dramaAddEpisode(r, 1, "")
	e := &dramaTextStreamer{outputs: []string{`{"script_md":"剧本"}`}, usage: llmgateway.Usage{InputTokens: 1000, OutputTokens: 500}}
	s, registry := dramaTestService(r, e, nil)
	registry.snap.Model.Pricing = modelcfg.Pricing{Billing: modelcfg.BillingToken, Token: &modelcfg.TokenPrice{In: 1000, Out: 4000}}
	j, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts", Episodes: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	if r.credit.Frozen <= 3 {
		t.Fatalf("冻结额应按上限预估: %+v", r.credit)
	}
	if err := s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); err != nil {
		t.Fatal(err)
	}
	if r.jobs[0].Status != "succeeded" || r.credit.Balance != 1000-3 || r.credit.Frozen != 0 {
		t.Fatalf("job=%+v credit=%+v", r.jobs[0], r.credit)
	}
}

// 渠道 Key 读不到时任务失败并整笔退回，不请求上游。
func TestDramaFailsWithoutChannelKey(t *testing.T) {
	r := newDramaMemory()
	dramaAddEpisode(r, 1, "")
	e := &dramaTextStreamer{outputs: []string{`{"script_md":"剧本"}`}}
	s, registry := dramaTestService(r, e, nil)
	registry.snap.Channel.Key = "other"
	j, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts", Episodes: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); err != nil {
		t.Fatal(err)
	}
	if e.calls != 0 || r.jobs[0].Status != "failed" || r.credit.Balance != 1000 || r.credit.Frozen != 0 {
		t.Fatalf("calls=%d job=%+v credit=%+v", e.calls, r.jobs[0], r.credit)
	}
}

// 首页创建短剧时选的 Agent 模型：存进项目，研读任务就用它；不选则跟随后台第一个（存空串）。
func TestDramaCreateKeepsChosenAgentModel(t *testing.T) {
	for _, tc := range []struct{ name, chosen, want string }{
		{"选了模型", "pro", "pro"},
		{"没选跟随默认", "", "text"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newDramaMemory()
			s, registry := dramaTestService(r, &dramaTextStreamer{}, nil)
			registry.models = []provider.ModelInfo{{Key: "text", Kind: "agent"}, {Key: "pro", Kind: "agent"}}
			_, err := s.Create(context.Background(), 7, &model.CreateDramaReq{Text: "第一章\n正文", StyleKey: "custom", StyleCustom: "写实", TextModel: tc.chosen})
			if err != nil {
				t.Fatal(err)
			}
			if r.project.TextModel != tc.chosen {
				t.Fatalf("项目里的模型=%q want %q", r.project.TextModel, tc.chosen)
			}
			if len(registry.requested) == 0 || registry.requested[0] != tc.want {
				t.Fatalf("研读任务应先用 %q，实际请求 %v", tc.want, registry.requested)
			}
		})
	}
}
