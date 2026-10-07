package agent_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/ws"
	. "video-canvas/internal/service/agent"
)

type agentEnv struct {
	repo *fakeAgentCanvasRepo
	bc   *fakeTaskBroadcaster
	rt   *fakeAgentRuntime
	gen  *fakeGenerator
	svc  *AgentService
	sess uint64
	now  time.Time
}

var defaultAgentModels = []model.AgentModelView{{Key: "claude", Name: "Claude", Vision: true}, {Key: "deepseek", Name: "DeepSeek"}}

func newAgentEnv(t *testing.T, models ...model.AgentModelView) *agentEnv {
	t.Helper()
	if models == nil {
		models = defaultAgentModels
	}
	repo := newFakeAgentCanvasRepo(agentBase)
	e := &agentEnv{repo: repo, bc: &fakeTaskBroadcaster{}, rt: &fakeAgentRuntime{}, gen: &fakeGenerator{}, now: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	e.svc = NewAgentService(AgentDeps{
		Repo: repo, Canvas: NewAgentCanvasService(repo, e.bc), Models: fakeAgentModels{list: models},
		Runtime: e.rt, Generator: e.gen, Broadcaster: e.bc, Now: func() time.Time { return e.now },
	})
	v, err := e.svc.CreateSession(context.Background(), 1, 7, &model.CreateAgentSessionReq{})
	if err != nil {
		t.Fatal(err)
	}
	e.sess = uint64(v.ID)
	return e
}

func wantAgentCode(t *testing.T, err error, want *errcode.Error) {
	t.Helper()
	var ec *errcode.Error
	if !errors.As(err, &ec) || ec.Code != want.Code {
		t.Fatalf("应返回 %d（%s），实际 %v", want.Code, want.Msg, err)
	}
}

func (e *agentEnv) start(t *testing.T, msg string) *AgentRunView {
	t.Helper()
	v, err := e.svc.StartRun(context.Background(), 1, e.sess, &model.StartAgentRunReq{Message: msg})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	return v
}

// setStatus 直接改假仓储里运行的状态，模拟 runtime 推进了运行。
func (e *agentEnv) setStatus(runID uint64, status string) {
	e.repo.mu.Lock()
	defer e.repo.mu.Unlock()
	e.repo.runs[runID].Status = status
}

func (e *agentEnv) run(runID uint64) model.AgentRun {
	e.repo.mu.Lock()
	defer e.repo.mu.Unlock()
	return *e.repo.runs[runID]
}

func TestAgentSessions(t *testing.T) {
	ctx := context.Background()

	t.Run("新建补默认值，列表只含自己画布上的会话", func(t *testing.T) {
		e := newAgentEnv(t)
		list, err := e.svc.ListSessions(ctx, 1, 7)
		if err != nil || len(list) != 1 || list[0].Title != "新对话" || list[0].Mode != model.AgentModeAll {
			t.Fatalf("list=%+v err=%v", list, err)
		}
		if _, err := e.svc.ListSessions(ctx, 2, 7); err == nil {
			t.Error("别人的画布应返回不存在")
		} else {
			wantAgentCode(t, err, errcode.ErrCanvasNotFound)
		}
	})

	t.Run("每个画布最多 50 个会话", func(t *testing.T) {
		e := newAgentEnv(t)
		for i := 1; i < 50; i++ {
			if _, err := e.svc.CreateSession(ctx, 1, 7, &model.CreateAgentSessionReq{}); err != nil {
				t.Fatal(err)
			}
		}
		_, err := e.svc.CreateSession(ctx, 1, 7, &model.CreateAgentSessionReq{})
		wantAgentCode(t, err, errcode.ErrAgentSessionLimit)
	})

	t.Run("重命名：去空白、截断、不能清空、别人的不存在", func(t *testing.T) {
		e := newAgentEnv(t)
		v, err := e.svc.RenameSession(ctx, 1, e.sess, "  雨夜   便利店  ")
		if err != nil || v.Title != "雨夜 便利店" {
			t.Fatalf("v=%+v err=%v", v, err)
		}
		long, _ := e.svc.RenameSession(ctx, 1, e.sess, strings.Repeat("长", 80))
		if len([]rune(long.Title)) != 40 {
			t.Errorf("应截到 40 字: %d", len([]rune(long.Title)))
		}
		_, err = e.svc.RenameSession(ctx, 1, e.sess, "   ")
		wantAgentCode(t, err, errcode.ErrInvalidParams)
		_, err = e.svc.RenameSession(ctx, 2, e.sess, "x")
		wantAgentCode(t, err, errcode.ErrAgentSessionMissing)
	})

	t.Run("会话占着画布时不能删，结束后可以删", func(t *testing.T) {
		e := newAgentEnv(t)
		run := e.start(t, "拆分镜")
		wantAgentCode(t, e.svc.DeleteSession(ctx, 1, e.sess), errcode.ErrAgentRunActive)
		e.setStatus(uint64(run.ID), model.RunSucceeded)
		if err := e.svc.DeleteSession(ctx, 1, e.sess); err != nil {
			t.Fatal(err)
		}
		wantAgentCode(t, e.svc.DeleteSession(ctx, 1, e.sess), errcode.ErrAgentSessionMissing)
	})

	t.Run("事件回放：要是自己的会话，按 after 增量，限制条数", func(t *testing.T) {
		e := newAgentEnv(t)
		e.start(t, "拆分镜")
		all, err := e.svc.Events(ctx, 1, e.sess, 0, 0)
		if err != nil || len(all) != 2 || all[0].Type != "message.user" || all[1].Type != "run.status" {
			t.Fatalf("events=%+v err=%v", all, err)
		}
		if all[0].RunID == nil {
			t.Error("运行内的事件应带 run_id")
		}
		part, _ := e.svc.Events(ctx, 1, e.sess, 1, 10)
		if len(part) != 1 || part[0].Seq != 2 {
			t.Errorf("after=1 应只剩第 2 条: %+v", part)
		}
		_, err = e.svc.Events(ctx, 2, e.sess, 0, 10)
		wantAgentCode(t, err, errcode.ErrAgentSessionMissing)
	})
}

func TestAgentStartRun(t *testing.T) {
	ctx := context.Background()

	t.Run("成功：运行排队、交给 runtime、记事件并推送、首条消息当标题", func(t *testing.T) {
		e := newAgentEnv(t)
		v := e.start(t, "把剧本拆成分镜")
		if v.Status != model.RunQueued || v.BudgetCredits != 50 || v.MaxSteps != 40 {
			t.Fatalf("run=%+v", v)
		}
		if len(e.rt.started) != 1 {
			t.Error("应交给 runtime")
		}
		if got := e.repo.eventTypes(); len(got) != 2 {
			t.Errorf("events=%v", got)
		}
		if len(e.bc.msgs) != 2 || e.bc.msgs[0].Type != ws.TypeAgentEvent || e.bc.channels[0] != "user:1" {
			t.Errorf("事件应推到 user:1: %+v %v", e.bc.msgs, e.bc.channels)
		}
		if ev, ok := e.bc.msgs[0].Data.(*AgentEventView); !ok || ev.CanvasID != idcodec.ID(7) || ev.Seq != 1 {
			t.Errorf("推送的事件应带编码后的 canvas_id 供前端过滤: %+v", e.bc.msgs[0].Data)
		}
		list, _ := e.svc.ListSessions(ctx, 1, 7)
		if list[0].Title != "把剧本拆成分镜" || list[0].ModelKey != "claude" {
			t.Errorf("会话=%+v", list[0])
		}
	})

	t.Run("预算、模式、模型按请求；预算 0 合法", func(t *testing.T) {
		e := newAgentEnv(t)
		zero := 0
		v, err := e.svc.StartRun(ctx, 1, e.sess, &model.StartAgentRunReq{Message: "x", Mode: "script", BudgetCredits: &zero, AgentModelKey: "deepseek"})
		if err != nil || v.BudgetCredits != 0 || v.Mode != "script" {
			t.Fatalf("v=%+v err=%v", v, err)
		}
	})

	t.Run("没有已发布的 Agent 模型：60002", func(t *testing.T) {
		e := newAgentEnv(t, []model.AgentModelView{}...)
		e.svc = NewAgentService(AgentDeps{Repo: e.repo, Canvas: NewAgentCanvasService(e.repo, e.bc), Models: fakeAgentModels{}, Runtime: e.rt})
		_, err := e.svc.StartRun(ctx, 1, e.sess, &model.StartAgentRunReq{Message: "x"})
		wantAgentCode(t, err, errcode.ErrAgentModelNA)
		if len(e.rt.started) != 0 {
			t.Error("不能启动运行")
		}
	})

	t.Run("指定的模型不在清单里：60002", func(t *testing.T) {
		e := newAgentEnv(t)
		_, err := e.svc.StartRun(ctx, 1, e.sess, &model.StartAgentRunReq{Message: "x", AgentModelKey: "gpt-x"})
		wantAgentCode(t, err, errcode.ErrAgentModelNA)
	})

	t.Run("画布上已有运行中的：60001", func(t *testing.T) {
		e := newAgentEnv(t)
		e.start(t, "第一条")
		_, err := e.svc.StartRun(ctx, 1, e.sess, &model.StartAgentRunReq{Message: "第二条"})
		wantAgentCode(t, err, errcode.ErrAgentRunActive)
	})

	t.Run("消息为空白、别人的会话", func(t *testing.T) {
		e := newAgentEnv(t)
		_, err := e.svc.StartRun(ctx, 1, e.sess, &model.StartAgentRunReq{Message: "  \n "})
		wantAgentCode(t, err, errcode.ErrInvalidParams)
		_, err = e.svc.StartRun(ctx, 2, e.sess, &model.StartAgentRunReq{Message: "x"})
		wantAgentCode(t, err, errcode.ErrAgentSessionMissing)
	})

	t.Run("runtime 启动失败：60005，运行标为失败，画布空出来", func(t *testing.T) {
		e := newAgentEnv(t)
		e.rt.startErr = errAgentBoom
		_, err := e.svc.StartRun(ctx, 1, e.sess, &model.StartAgentRunReq{Message: "x"})
		wantAgentCode(t, err, errcode.ErrAgentUnavailable)
		e.rt.startErr = nil
		if _, err := e.svc.StartRun(ctx, 1, e.sess, &model.StartAgentRunReq{Message: "再试一次"}); err != nil {
			t.Fatalf("失败的运行不应占着画布: %v", err)
		}
	})
}

func TestAgentInterjectCancelResume(t *testing.T) {
	ctx := context.Background()

	t.Run("插话：只有运行中才行，runtime 故障 60005", func(t *testing.T) {
		e := newAgentEnv(t)
		run := e.start(t, "x")
		id := uint64(run.ID)
		wantAgentCode(t, e.svc.Interject(ctx, 1, id, "补充"), errcode.ErrAgentState) // queued
		e.setStatus(id, model.RunRunning)
		if err := e.svc.Interject(ctx, 1, id, "再暖一点"); err != nil || len(e.rt.interjected) != 1 {
			t.Fatalf("err=%v", err)
		}
		wantAgentCode(t, e.svc.Interject(ctx, 1, id, "  "), errcode.ErrInvalidParams)
		wantAgentCode(t, e.svc.Interject(ctx, 2, id, "x"), errcode.ErrAgentRunMissing)
		e.rt.interjectErr = errAgentBoom
		wantAgentCode(t, e.svc.Interject(ctx, 1, id, "x"), errcode.ErrAgentUnavailable)
	})

	t.Run("停止：运行置为 canceled，待处理的审批失效，通知 runtime", func(t *testing.T) {
		e := newAgentEnv(t)
		run := e.start(t, "x")
		id := uint64(run.ID)
		e.setStatus(id, model.RunRunning)
		ap, err := e.svc.CreateApproval(ctx, ptr(e.run(id)), "tc", model.ApprovalDelete, DeletePayload{NodeIDs: []string{"n_script"}}, 0)
		if err != nil {
			t.Fatal(err)
		}
		v, err := e.svc.Cancel(ctx, 1, id)
		if err != nil || v.Status != model.RunCanceled {
			t.Fatalf("v=%+v err=%v", v, err)
		}
		if got, _ := e.repo.GetApproval(ctx, 1, uint64(ap.ID)); got.Status != model.ApprovalExpired {
			t.Errorf("审批应失效: %s", got.Status)
		}
		if len(e.rt.canceled) != 1 {
			t.Error("应通知 runtime")
		}
		_, err = e.svc.Cancel(ctx, 1, id)
		wantAgentCode(t, err, errcode.ErrAgentState)
		if _, err := e.svc.StartRun(ctx, 1, e.sess, &model.StartAgentRunReq{Message: "y"}); err != nil {
			t.Errorf("停止后画布应空出来: %v", err)
		}
	})

	t.Run("继续：中断可直接继续；预算用尽必须追加；步数用尽放宽上限", func(t *testing.T) {
		e := newAgentEnv(t)
		run := e.start(t, "x")
		id := uint64(run.ID)

		e.setStatus(id, model.RunBudgetExhausted)
		_, err := e.svc.Resume(ctx, 1, id, 0)
		wantAgentCode(t, err, errcode.ErrInvalidParams)
		v, err := e.svc.Resume(ctx, 1, id, 20)
		if err != nil || v.BudgetCredits != 70 || v.Status != model.RunQueued {
			t.Fatalf("v=%+v err=%v", v, err)
		}

		e.setStatus(id, model.RunStepLimit)
		v, err = e.svc.Resume(ctx, 1, id, 0)
		if err != nil || v.MaxSteps != 80 {
			t.Fatalf("v=%+v err=%v", v, err)
		}

		e.setStatus(id, model.RunInterrupted)
		if _, err := e.svc.Resume(ctx, 1, id, 0); err != nil {
			t.Fatal(err)
		}
		if len(e.rt.resumed) != 3 {
			t.Errorf("每次继续都应通知 runtime: %d", len(e.rt.resumed))
		}

		e.setStatus(id, model.RunSucceeded)
		_, err = e.svc.Resume(ctx, 1, id, 0)
		wantAgentCode(t, err, errcode.ErrAgentState)
	})

	t.Run("继续时画布已被别的运行占用：60001", func(t *testing.T) {
		e := newAgentEnv(t)
		old := e.start(t, "第一条")
		e.setStatus(uint64(old.ID), model.RunInterrupted)
		e.start(t, "第二条") // 占住画布
		_, err := e.svc.Resume(ctx, 1, uint64(old.ID), 0)
		wantAgentCode(t, err, errcode.ErrAgentRunActive)
	})
}

func ptr[T any](v T) *T { return &v }
