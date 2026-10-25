package agent_test

import (
	"errors"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	. "video-canvas/internal/service/agent"
)

// agentEnv 是服务层测试的基础环境：假仓储（画布、改动、几条运行）、广播器、生成执行器，和只剩平台侧能力的 AgentService。
// 会话、运行、审批在独立的 Agent 服务里，Go 这边不再有对应的业务，所以这里没有会话和运行时。
type agentEnv struct {
	repo *fakeAgentCanvasRepo
	bc   *fakeTaskBroadcaster
	gen  *fakeGenerator
	svc  *AgentService
	now  time.Time
}

var defaultAgentModels = []model.AgentModelView{{Key: "claude", Name: "Claude", Vision: true}, {Key: "deepseek", Name: "DeepSeek"}}

func newAgentEnv(t *testing.T, models ...model.AgentModelView) *agentEnv {
	t.Helper()
	return newAgentEnvOpts(t, nil, models)
}

// newAgentEnvWith 在默认环境上让 mutate 调整 AgentDeps，用来注入测试要的可选依赖。
func newAgentEnvWith(t *testing.T, mutate func(*AgentDeps)) *agentEnv {
	t.Helper()
	return newAgentEnvOpts(t, mutate, nil)
}

func newAgentEnvOpts(t *testing.T, mutate func(*AgentDeps), models []model.AgentModelView) *agentEnv {
	t.Helper()
	if models == nil {
		models = defaultAgentModels
	}
	repo := newFakeAgentCanvasRepo(agentBase)
	e := &agentEnv{repo: repo, bc: &fakeTaskBroadcaster{}, gen: &fakeGenerator{}, now: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	deps := AgentDeps{
		Repo: repo, Canvas: NewAgentCanvasService(repo, e.bc), Models: fakeAgentModels{list: models},
		Generator: e.gen, Now: func() time.Time { return e.now },
	}
	if mutate != nil {
		mutate(&deps)
	}
	e.svc = NewAgentService(deps)
	return e
}

func wantAgentCode(t *testing.T, err error, want *errcode.Error) {
	t.Helper()
	var ec *errcode.Error
	if !errors.As(err, &ec) || ec.Code != want.Code {
		t.Fatalf("应返回 %d（%s），实际 %v", want.Code, want.Msg, err)
	}
}

// runningRun 在假仓储里放一条运行中的运行并返回它：画布工具、改动记录、撤销都按运行 id 工作。
func (e *agentEnv) runningRun(t *testing.T) *model.AgentRun {
	t.Helper()
	e.repo.mu.Lock()
	defer e.repo.mu.Unlock()
	run := &model.AgentRun{ID: uint64(len(e.repo.runs)) + 101, SessionID: 5, CanvasID: 7, UserID: 1, Status: model.RunRunning, Mode: model.AgentModeAll, MaxSteps: 40, CreatedAt: e.now}
	e.repo.runs[run.ID] = run
	c := *run
	return &c
}
