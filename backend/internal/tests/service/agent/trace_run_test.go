package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/agent/prompts"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	. "video-canvas/internal/service/agent"
)

// ---- StartRun：开跑时记下版本快照、用例标签、轨迹开关 ----

func newTracedService(e *agentEnv, rec *TraceRecorder) *AgentService {
	return NewAgentService(AgentDeps{
		Repo: e.repo, Canvas: NewAgentCanvasService(e.repo, e.bc), Models: fakeAgentModels{list: defaultAgentModels},
		Runtime: e.rt, Generator: e.gen, Broadcaster: e.bc, Now: func() time.Time { return e.now }, Trace: rec,
	})
}

func TestStartRun_RecordsVersionSnapshot(t *testing.T) {
	ctx := context.Background()
	e := newAgentEnv(t)
	te := newTraceEnv(t, nil)
	svc := newTracedService(e, te.rec)

	budget := 30
	v, err := svc.StartRun(ctx, 1, e.sess, &model.StartAgentRunReq{Message: "  拆分镜  ", Mode: model.AgentModeStoryboard, Selection: []string{"n_script"},
		BudgetCredits: &budget, HarnessCase: "  storyboard-9x16 "})
	if err != nil {
		t.Fatal(err)
	}
	run := e.repo.runs[uint64(v.ID)]
	if run.PromptVersion != prompts.Version || run.AgentModelKey == "" || run.HarnessCase != "storyboard-9x16" || !run.TraceEnabled {
		t.Errorf("版本快照 / 标签 / 轨迹开关不对: %+v", run)
	}
	var in map[string]any
	if err := json.Unmarshal(run.InputJSON, &in); err != nil {
		t.Fatalf("input_json 不是 JSON: %v (%s)", err, run.InputJSON)
	}
	if in["message"] != "拆分镜" || in["mode"] != "storyboard" || in["budget_credits"].(float64) != 30 || in["model_key"] == "" || len(in["selection"].([]any)) != 1 {
		t.Errorf("input_json 应含 message / mode / selection / model_key / budget_credits: %v", in)
	}
}

func TestStartRun_TraceDisabledAndNoHarness(t *testing.T) {
	ctx := context.Background()
	e := newAgentEnv(t)
	te := newTraceEnv(t, func(o *TraceOptions) { o.Enabled = false })
	svc := newTracedService(e, te.rec)
	v, err := svc.StartRun(ctx, 1, e.sess, &model.StartAgentRunReq{Message: "你好"})
	if err != nil {
		t.Fatal(err)
	}
	run := e.repo.runs[uint64(v.ID)]
	if run.TraceEnabled || run.HarnessCase != "" {
		t.Errorf("轨迹关闭时 trace_enabled 为 false，没有标签就是真实用户: %+v", run)
	}
	if run.PromptVersion != prompts.Version {
		t.Errorf("版本快照与轨迹开关无关，照样记: %d", run.PromptVersion)
	}
}

func TestStartRun_HarnessCaseLength(t *testing.T) {
	ctx := context.Background()
	e := newAgentEnv(t)
	ok := strings.Repeat("用", 128)
	if _, err := e.svc.StartRun(ctx, 1, e.sess, &model.StartAgentRunReq{Message: "a", HarnessCase: ok}); err != nil {
		t.Fatalf("128 个字符是允许的上限: %v", err)
	}
	for id := range e.repo.runs {
		e.setStatus(id, model.RunSucceeded)
	}
	_, err := e.svc.StartRun(ctx, 1, e.sess, &model.StartAgentRunReq{Message: "b", HarnessCase: ok + "多"})
	wantAgentCode(t, err, errcode.ErrInvalidParams)
	for _, r := range e.repo.runs {
		if len([]rune(r.HarnessCase)) > 128 {
			t.Error("超长的标签不能落库")
		}
	}
}

// ---- 运行时：片段开始 / 结束与轨迹 ----

func newTracedRuntime(t *testing.T) (*runtimeEnv, *traceEnv) {
	t.Helper()
	e := newRuntimeEnv(t)
	te := newTraceEnv(t, nil)
	e.rt = NewProcessRuntime(RuntimeDeps{
		Repo: e.repo, Bridge: e.br, Canvas: NewAgentCanvasService(e.repo, e.bc), Agent: e.svc, Registry: e.reg, Launcher: e.l, Trace: te.rec,
		Config: RuntimeConfig{NodePath: "/usr/bin/node", ScriptPath: "/opt/agent-runtime/src/main.mjs", BridgeURL: "http://127.0.0.1:47001", WorkRoot: e.work, CancelGrace: 80 * time.Millisecond},
	})
	return e, te
}

// startTraced 起一个开了轨迹的运行并交给运行时启动，返回运行和启动参数里的令牌。
func (e *runtimeEnv) startTraced(t *testing.T, te *traceEnv) (*model.AgentRun, string) {
	t.Helper()
	v := e.start(t, "把剧本拆成分镜")
	run := e.run(uint64(v.ID))
	run.TraceEnabled = true
	run.CreatedAt = te.run().CreatedAt
	in := model.AgentRunInput{Message: "把剧本拆成分镜", Mode: "all", ModelKey: "claude"}
	if err := e.rt.Start(context.Background(), &run, in); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return &run, e.firstLine(t)["token"].(string)
}

func keyFor(runID uint64, seg string) string {
	return "agent-traces/2026/10/20/" + itoa64(int64(runID)) + "/" + seg + ".jsonl.gz"
}

func TestProcessRuntime_TracesSegment(t *testing.T) {
	t.Run("正常结束：片段原文上传，索引写好，运行不标不完整", func(t *testing.T) {
		e, te := newTracedRuntime(t)
		run, tok := e.startTraced(t, te)
		if _, err := e.br.ExecuteTool(context.Background(), tok, "tc1", "plan_update", json.RawMessage(`{"steps":[]}`)); err != nil {
			t.Fatal(err)
		}
		if err := e.br.Finish(context.Background(), tok, "done", ""); err != nil {
			t.Fatal(err)
		}
		_, p := e.l.last()
		p.done <- ExitInfo{Code: 0}
		waitFor(t, "原文上传", func() bool { return len(te.stores.st.keys()) == 1 })
		waitFor(t, "运行字段更新", func() bool { te.repo.mu.Lock(); defer te.repo.mu.Unlock(); return len(te.repo.updates) == 1 })
		if te.repo.updates[0].Incomplete {
			t.Error("正常结束不该标不完整")
		}
		lines := te.stores.st.lines(t, keyFor(run.ID, "1"))
		sys := lines[0]
		if sys["kind"] != "system" || !strings.Contains(sys["system_prompt"].(string), "画布") || len(sys["tools"].([]any)) == 0 {
			t.Errorf("system 行应取自交给 Node 的启动参数: %v", sys["kind"])
		}
		if te.repo.updates[0].ToolsHash != sys["tools_hash"] {
			t.Errorf("run.tools_hash 应与 system 行一致: %q vs %v", te.repo.updates[0].ToolsHash, sys["tools_hash"])
		}
		segs := te.repo.spansOf(model.TraceSpanSegment)
		if len(segs) != 1 || segs[0].Name != "start" || segs[0].Status != "ok" {
			t.Errorf("片段索引: %+v", segs)
		}
		if len(kindLines(lines, "execute_tool")) != 1 {
			t.Error("片段里的工具调用应在原文里")
		}
	})

	t.Run("进程没报告结束就没了：片段标 aborted，运行标不完整，已写的部分照样上传", func(t *testing.T) {
		e, te := newTracedRuntime(t)
		run, tok := e.startTraced(t, te)
		if _, err := e.br.ExecuteTool(context.Background(), tok, "tc1", "plan_update", json.RawMessage(`{"steps":[]}`)); err != nil {
			t.Fatal(err)
		}
		_, p := e.l.last()
		p.done <- ExitInfo{Code: 139, StderrTail: "boom"}
		waitFor(t, "运行变为 interrupted", func() bool { return e.run(run.ID).Status == model.RunInterrupted })
		waitFor(t, "运行字段更新", func() bool { te.repo.mu.Lock(); defer te.repo.mu.Unlock(); return len(te.repo.updates) == 1 })
		if !te.repo.updates[0].Incomplete {
			t.Error("崩溃应标不完整")
		}
		segs := te.repo.spansOf(model.TraceSpanSegment)
		if len(segs) != 1 || segs[0].Status != "aborted" || segs[0].ErrorCode != "crashed" {
			t.Errorf("片段索引: %+v", segs)
		}
		last := te.stores.st.lines(t, keyFor(run.ID, "1"))
		if tail := last[len(last)-1]["stderr_tail"]; tail != "boom" {
			t.Errorf("stderr 末尾应进片段行: %v", tail)
		}
	})

	t.Run("用户停止（运行已是 canceled）：进程没来得及报告也不算崩溃", func(t *testing.T) {
		e, te := newTracedRuntime(t)
		run, _ := e.startTraced(t, te)
		e.setStatus(run.ID, model.RunCanceled)
		_, p := e.l.last()
		p.done <- ExitInfo{Code: -1}
		waitFor(t, "运行字段更新", func() bool { te.repo.mu.Lock(); defer te.repo.mu.Unlock(); return len(te.repo.updates) == 1 })
		if te.repo.updates[0].Incomplete {
			t.Error("用户主动停止不该标轨迹不完整")
		}
	})

	t.Run("续跑是第二个片段：启动方式 approval，序号接着数", func(t *testing.T) {
		e, te := newTracedRuntime(t)
		run, tok := e.startTraced(t, te)
		_ = e.br.Finish(context.Background(), tok, "paused", "")
		e.setStatus(run.ID, model.RunWaitingApproval)
		_, p := e.l.last()
		p.done <- ExitInfo{Code: 0}
		waitFor(t, "第一个片段上传", func() bool { return len(te.stores.st.keys()) == 1 })
		e.setStatus(run.ID, model.RunRunning)
		cur := e.run(run.ID)
		cur.TraceEnabled = true
		cur.CreatedAt = te.run().CreatedAt
		if err := e.rt.Resume(context.Background(), &cur, ResumeInfo{Reason: "approval", ToolCallID: "tc1", Content: "ok"}); err != nil {
			t.Fatal(err)
		}
		_, p2 := e.l.last()
		p2.done <- ExitInfo{Code: 0}
		waitFor(t, "第二个片段上传", func() bool { return len(te.stores.st.keys()) == 2 })
		segs := te.repo.spansOf(model.TraceSpanSegment)
		if len(segs) != 2 || segs[1].Name != "approval" || segs[1].SegmentSeq != 2 {
			t.Errorf("第二个片段: %+v", segs)
		}
	})

	t.Run("进程起不来：不留轨迹", func(t *testing.T) {
		e, te := newTracedRuntime(t)
		e.l.err = errors.New("没有 node")
		v := e.start(t, "你好")
		run := e.run(uint64(v.ID))
		run.TraceEnabled = true
		if err := e.rt.Start(context.Background(), &run, model.AgentRunInput{Message: "你好", Mode: "all", ModelKey: "claude"}); err == nil {
			t.Fatal("启动失败应返回错误")
		}
		if len(te.tempFiles(t)) != 0 || len(te.repo.spans) != 0 || len(te.stores.st.keys()) != 0 {
			t.Error("没真正启动的片段不留任何轨迹痕迹")
		}
	})

	t.Run("运行没开轨迹：运行时照常，不产生任何轨迹", func(t *testing.T) {
		e, te := newTracedRuntime(t)
		v := e.start(t, "你好")
		run := e.run(uint64(v.ID)) // TraceEnabled 为 false
		if err := e.rt.Start(context.Background(), &run, model.AgentRunInput{Message: "你好", Mode: "all", ModelKey: "claude"}); err != nil {
			t.Fatal(err)
		}
		_, p := e.l.last()
		p.done <- ExitInfo{Code: 0}
		waitFor(t, "令牌被收回", func() bool { return e.br.TokenCount() == 0 })
		if len(te.stores.st.keys()) != 0 || len(te.repo.spans) != 0 {
			t.Error("trace_enabled=false 的运行不记轨迹")
		}
	})
}
