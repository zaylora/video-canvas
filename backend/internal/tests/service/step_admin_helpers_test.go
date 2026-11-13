package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	. "video-canvas/internal/service"
	"video-canvas/internal/service/stepfake"
	"video-canvas/internal/stepkit"
)

const (
	stepActor   = uint64(9)
	stepKeyName = "test.step"
	stepGood    = `{"title":"晨雾","shots":[{"text":"开场"}]}`
	stepBad     = `{"title":"禁用词","shots":[{"text":"开场"}]}`
	stepSchema  = `{"type":"object","required":["title","shots"],"properties":{"title":{"type":"string"},"shots":{"type":"array","minItems":1,"items":{"type":"object","required":["text"],"properties":{"text":{"type":"string"}}}}}}`
)

// stepEnv 是环节后台服务的测试环境：内存仓储、技能库、模型清单和脚本化的模型调用。
type stepEnv struct {
	svc    *StepAdminService
	repo   *stepfake.Repo
	lib    *stepfake.Library
	models *stepfake.Models
	caller *stepfake.Caller
	audit  *stepfake.Audit
	reg    *stepkit.Registry
	now    time.Time
	stats  *stubStats
}

// stubStats 是固定的失败率统计。
type stubStats struct{ rates map[string]float64 }

func (s *stubStats) FailRate7d(_ context.Context, key string) (float64, error) {
	return s.rates[key], nil
}

// newStepStep 返回测试环节：标题含「禁用」是硬错误；带一个样例 s1。
func newStepStep(key string) stepkit.Step {
	return stepkit.Step{
		Key: key, Title: "测试环节", Studio: "test", Duty: "写镜头。", Media: stepkit.MediaNone,
		Inputs:  []stepkit.Var{{Name: "product", Type: "string", Desc: "商品名"}},
		Schema:  json.RawMessage(stepSchema),
		Rules:   stepkit.Rules{Hard: []string{"标题不能含禁用词"}},
		Builtin: "内置方法",
		Samples: []stepkit.Sample{{Key: "s1", Name: "样例一", Build: func(context.Context) (stepkit.Input, error) {
			return stepkit.Input{Vars: map[string]any{"product": "香水"}}, nil
		}}},
		Validate: func(out json.RawMessage, _ stepkit.Input) (stepkit.Flags, error) {
			if strings.Contains(string(out), "禁用") {
				return nil, stepkit.Hard("标题含禁用词")
			}
			return nil, nil
		},
	}
}

func newStepEnv(t *testing.T) *stepEnv {
	t.Helper()
	e := &stepEnv{repo: stepfake.NewRepo(), lib: stepfake.NewLibrary(), models: stepfake.NewModels(), caller: &stepfake.Caller{},
		audit: &stepfake.Audit{}, reg: stepkit.NewRegistry(), now: time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC), stats: &stubStats{rates: map[string]float64{}}}
	e.reg.MustRegister(newStepStep(stepKeyName))
	e.repo.Credits[stepActor] = &model.UserCredit{UserID: stepActor, Balance: 100}
	e.repo.Actors[stepActor] = "admin-nine"
	e.lib.Add("tvc", &stepfake.Skill{ID: 7, Title: "TVC 导演", Enabled: true, Active: 1, Versions: map[int]string{1: "旧方法"}})
	e.repo.SkillNames[7] = "tvc"
	e.svc = NewStepAdminService(StepAdminDeps{Registry: e.reg, Repo: e.repo, Skills: e.lib, Models: e.models, Credits: stepfake.Policy{Initial: 50},
		Audit: e.audit, Stats: e.stats, Caller: e.caller, Now: func() time.Time { return e.now }})
	return e
}

func builtinTrialReq() *model.CreateStepTrialReq {
	return &model.CreateStepTrialReq{Candidate: model.StepCandidateReq{Source: "builtin"}, SampleKey: "s1", TextModel: "text-a"}
}

func skillTrialReq(name string, version int) *model.CreateStepTrialReq {
	return &model.CreateStepTrialReq{Candidate: model.StepCandidateReq{Source: "skill", SkillName: name, Version: version}, SampleKey: "s1", TextModel: "text-a"}
}

// codeOf 取错误里的业务码，不是业务错误返回 0。
func stepCodeOf(err error) int {
	var e *errcode.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return 0
}

func wantStepCode(t *testing.T, err error, code int) {
	t.Helper()
	if stepCodeOf(err) != code {
		t.Fatalf("应返回业务码 %d，实际 %v", code, err)
	}
}

// trialID 把试跑视图里的十六进制 id 原样返回（接口里它就是字符串）。
func (e *stepEnv) create(t *testing.T, req *model.CreateStepTrialReq) *model.StepTrialList {
	t.Helper()
	list, err := e.svc.CreateTrials(context.Background(), stepActor, stepKeyName, req)
	if err != nil {
		t.Fatalf("发起试跑失败：%v", err)
	}
	return list
}

// runTrial 发起一条试跑并立刻执行，按 replies 预设模型回复，返回执行后的试跑行。
func (e *stepEnv) runTrial(t *testing.T, req *model.CreateStepTrialReq, replies ...stepfake.Reply) *model.StepTrial {
	t.Helper()
	e.caller.Replies, e.caller.Calls = replies, 0
	list := e.create(t, req)
	e.handleAll(t)
	_ = list
	var last *model.StepTrial
	for _, id := range e.repo.Enqueued {
		last = e.repo.Trials[id]
	}
	return last
}

// handleAll 执行全部已入队的试跑（模拟 River worker）。
func (e *stepEnv) handleAll(t *testing.T) {
	t.Helper()
	for _, id := range append([]uint64(nil), e.repo.Enqueued...) {
		if err := e.svc.HandleStepTrial(context.Background(), id); err != nil {
			t.Fatalf("执行试跑 %d 失败：%v", id, err)
		}
	}
}

// okReply 是一次通过校验的回复。
func okReply() stepfake.Reply {
	return stepfake.Reply{Text: stepGood, Usage: llmgateway.Usage{InputTokens: 100, OutputTokens: 20}}
}

// passedTrialFor 为候选造一条已通过、刚结束的试跑，返回对外的 id，用来给 SetBinding 当依据。
func (e *stepEnv) passedTrialFor(t *testing.T, req *model.CreateStepTrialReq) string {
	t.Helper()
	trial := e.runTrial(t, req, okReply())
	if trial.Status != model.StepTrialSucceeded || !trial.Passed {
		t.Fatalf("试跑应通过：%+v", trial)
	}
	return encodeTrialID(trial.ID)
}

// encodeTrialID 把试跑主键编码成接口里的十六进制 id。
func encodeTrialID(id uint64) string { return idcodec.Encode(id) }
