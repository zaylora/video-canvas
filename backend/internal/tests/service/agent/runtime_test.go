package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	. "video-canvas/internal/service/agent"
)

// ---- 假的进程启动器 ----

type fakeProc struct {
	mu      sync.Mutex
	sent    [][]byte
	killed  bool
	done    chan ExitInfo
	sendErr error
}

func newFakeProc() *fakeProc { return &fakeProc{done: make(chan ExitInfo, 1)} }

func (p *fakeProc) Send(line []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sendErr != nil {
		return p.sendErr
	}
	p.sent = append(p.sent, append([]byte(nil), line...))
	return nil
}
func (p *fakeProc) Kill() {
	p.mu.Lock()
	p.killed = true
	p.mu.Unlock()
	select {
	case p.done <- ExitInfo{Code: -1}:
	default:
	}
}
func (p *fakeProc) Done() <-chan ExitInfo { return p.done }
func (p *fakeProc) lines() [][]byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([][]byte(nil), p.sent...)
}
func (p *fakeProc) wasKilled() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.killed }

type fakeLauncher struct {
	mu       sync.Mutex
	specs    []LaunchSpec
	procs    []*fakeProc
	err      error
	sendErr  error
	dirsSeen []string
}

func (l *fakeLauncher) Launch(_ context.Context, spec LaunchSpec) (Proc, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return nil, l.err
	}
	p := newFakeProc()
	p.sendErr = l.sendErr
	l.specs = append(l.specs, spec)
	l.procs = append(l.procs, p)
	l.dirsSeen = append(l.dirsSeen, spec.Dir)
	return p, nil
}
func (l *fakeLauncher) last() (LaunchSpec, *fakeProc) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.specs[len(l.specs)-1], l.procs[len(l.procs)-1]
}

type runtimeEnv struct {
	*bridgeEnv
	l    *fakeLauncher
	rt   *ProcessRuntime
	work string
}

func newRuntimeEnv(t *testing.T) *runtimeEnv {
	t.Helper()
	b := newBridgeEnv(t)
	l := &fakeLauncher{}
	work := t.TempDir()
	rt := NewProcessRuntime(RuntimeDeps{
		Repo: b.repo, Bridge: b.br, Canvas: NewAgentCanvasService(b.repo, b.bc), Agent: b.svc, Registry: b.reg, Launcher: l,
		Config: RuntimeConfig{NodePath: "/usr/bin/node", ScriptPath: "/opt/agent-runtime/src/main.mjs", BridgeURL: "http://127.0.0.1:47001", WorkRoot: work, CancelGrace: 80 * time.Millisecond},
	})
	return &runtimeEnv{bridgeEnv: b, l: l, rt: rt, work: work}
}

// startRun 创建一个 queued 的运行并交给运行时启动。
func (e *runtimeEnv) startRun(t *testing.T, mode string, selection ...string) (*model.AgentRun, map[string]any) {
	t.Helper()
	v := e.start(t, "把剧本拆成分镜")
	run := e.run(uint64(v.ID))
	in := model.AgentRunInput{Message: "把剧本拆成分镜", Mode: mode, Selection: selection, ModelKey: "claude"}
	if err := e.rt.Start(context.Background(), &run, in); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return &run, e.firstLine(t)
}

func (e *runtimeEnv) firstLine(t *testing.T) map[string]any {
	t.Helper()
	_, p := e.l.last()
	lines := p.lines()
	if len(lines) == 0 {
		t.Fatal("没有向进程发送启动参数")
	}
	var m map[string]any
	if err := json.Unmarshal(lines[0], &m); err != nil {
		t.Fatalf("启动参数不是 JSON: %v", err)
	}
	return m
}

func TestProcessRuntime_StartBuildsInputAndLaunches(t *testing.T) {
	e := newRuntimeEnv(t)
	run, in := e.startRun(t, "storyboard", "n_script")

	spec, _ := e.l.last()
	if spec.Name != "/usr/bin/node" || len(spec.Args) != 1 || spec.Args[0] != "/opt/agent-runtime/src/main.mjs" {
		t.Errorf("启动命令不对: %+v", spec)
	}
	if !strings.HasPrefix(spec.Dir, e.work) {
		t.Errorf("每个片段用 WorkRoot 下的独立临时目录: %q", spec.Dir)
	}
	if _, err := os.Stat(spec.Dir); err != nil {
		t.Errorf("临时目录应已创建: %v", err)
	}
	for _, kv := range spec.Env {
		if strings.Contains(kv, "sk-test") || strings.HasPrefix(kv, "APP_") || strings.HasPrefix(kv, "AWS_") {
			t.Errorf("子进程环境里不能有任何密钥或应用配置: %q", kv)
		}
	}
	if e.run(run.ID).Status != model.RunRunning {
		t.Errorf("启动后运行应是 running: %s", e.run(run.ID).Status)
	}

	if in["bridge_url"] != "http://127.0.0.1:47001" || in["mode"] != "start" {
		t.Errorf("in=%v", in)
	}
	tok, _ := in["token"].(string)
	if len(tok) < 32 {
		t.Fatalf("应带一次性令牌: %q", tok)
	}
	if _, err := e.br.ExecuteTool(context.Background(), tok, "tc", "plan_update", []byte(`{"steps":[]}`)); err != nil {
		t.Errorf("令牌应能用来调桥: %v", err)
	}
	m := in["model"].(map[string]any)
	if m["context_window"] != float64(200000) || m["max_tokens"] != float64(8192) || m["vision"] != true {
		t.Errorf("模型信息应取自快照: %v", m)
	}
	if sp, _ := in["system_prompt"].(string); !strings.Contains(sp, "画布 Agent") || !strings.Contains(sp, "分镜搭建") {
		t.Errorf("系统提示词应含基础内容和模式说明")
	}
	prompt := in["prompt"].(map[string]any)["text"].(string)
	for _, must := range []string{"把剧本拆成分镜", "<画布目录>", "n_script", "预算 50 积分"} {
		if !strings.Contains(prompt, must) {
			t.Errorf("用户消息应包含 %q:\n%s", must, prompt)
		}
	}
	if _, has := in["messages"]; has {
		t.Errorf("没有历史时不传 messages: %v", in["messages"])
	}
}

func TestProcessRuntime_AllowedToolsFollowMode(t *testing.T) {
	has := func(in map[string]any, tool string) bool {
		for _, v := range in["allowed_tools"].([]any) {
			if v == tool {
				return true
			}
		}
		return false
	}
	e := newRuntimeEnv(t)
	_, all := e.startRun(t, "all")
	if !has(all, "canvas_delete") || !has(all, "canvas_arrange") || !has(all, "canvas_get_state") {
		t.Errorf("全能创作应有全部工具: %v", all["allowed_tools"])
	}
	e2 := newRuntimeEnv(t)
	_, prompt := e2.startRun(t, "prompt")
	if has(prompt, "canvas_delete") || has(prompt, "canvas_arrange") || !has(prompt, "canvas_get_state") {
		t.Errorf("提示词优化不能用删除和排列，但要能读画布: %v", prompt["allowed_tools"])
	}
}

func TestProcessRuntime_StartPassesSavedHistory(t *testing.T) {
	e := newRuntimeEnv(t)
	v := e.start(t, "x")
	run := e.run(uint64(v.ID))
	if err := e.repo.UpdateSession(context.Background(), 1, run.SessionID, map[string]any{"session_jsonl": `[{"role":"user","content":"之前的话"}]`}); err != nil {
		t.Fatal(err)
	}
	if err := e.rt.Start(context.Background(), &run, model.AgentRunInput{Message: "继续", ModelKey: "claude"}); err != nil {
		t.Fatal(err)
	}
	msgs, _ := e.firstLine(t)["messages"].([]any)
	if len(msgs) != 1 || msgs[0].(map[string]any)["content"] != "之前的话" {
		t.Errorf("应把会话保存的历史交给进程: %v", msgs)
	}
}

func TestProcessRuntime_StartFailures(t *testing.T) {
	ctx := context.Background()

	t.Run("模型不可用：不启动进程，运行保持 queued", func(t *testing.T) {
		e := newRuntimeEnv(t)
		v := e.start(t, "x")
		run := e.run(uint64(v.ID))
		e.reg.snapEr = provider.ErrModelUnavailable
		err := e.rt.Start(ctx, &run, model.AgentRunInput{ModelKey: "claude"})
		wantAgentCode(t, err, errcode.ErrAgentModelNA)
		if len(e.l.specs) != 0 || e.run(run.ID).Status != model.RunQueued {
			t.Errorf("specs=%d status=%s", len(e.l.specs), e.run(run.ID).Status)
		}
	})

	t.Run("进程启动失败：返回错误，令牌被收回", func(t *testing.T) {
		e := newRuntimeEnv(t)
		v := e.start(t, "x")
		run := e.run(uint64(v.ID))
		e.l.err = errors.New("exec: node not found")
		if err := e.rt.Start(ctx, &run, model.AgentRunInput{ModelKey: "claude"}); err == nil {
			t.Fatal("应返回错误")
		}
		if e.br.TokenCount() != 0 {
			t.Errorf("启动失败不能留下有效令牌: %d", e.br.TokenCount())
		}
		if entries, _ := os.ReadDir(e.work); len(entries) != 0 {
			t.Errorf("启动失败要清掉临时目录: %v", entries)
		}
	})

	t.Run("发送启动参数失败：杀掉进程，收回令牌", func(t *testing.T) {
		e := newRuntimeEnv(t)
		e.l.sendErr = errors.New("broken pipe")
		v := e.start(t, "x")
		run := e.run(uint64(v.ID))
		if err := e.rt.Start(ctx, &run, model.AgentRunInput{ModelKey: "claude"}); err == nil {
			t.Fatal("应返回错误")
		}
		_, p := e.l.last()
		if !p.wasKilled() || e.br.TokenCount() != 0 {
			t.Errorf("killed=%v tokens=%d", p.wasKilled(), e.br.TokenCount())
		}
	})

	t.Run("同一运行不能同时有两个进程", func(t *testing.T) {
		e := newRuntimeEnv(t)
		run, _ := e.startRun(t, "all")
		if err := e.rt.Start(ctx, run, model.AgentRunInput{ModelKey: "claude"}); err == nil {
			t.Error("应拒绝")
		}
	})
}

func TestProcessRuntime_InterjectAndCancel(t *testing.T) {
	ctx := context.Background()

	t.Run("插话：给进程发 steer", func(t *testing.T) {
		e := newRuntimeEnv(t)
		run, _ := e.startRun(t, "all")
		if err := e.rt.Interject(ctx, run.ID, "再暖一点"); err != nil {
			t.Fatal(err)
		}
		_, p := e.l.last()
		lines := p.lines()
		var m map[string]string
		_ = json.Unmarshal(lines[len(lines)-1], &m)
		if m["type"] != "steer" || m["text"] != "再暖一点" {
			t.Errorf("m=%v", m)
		}
		if err := e.rt.Interject(ctx, 99999, "x"); err == nil {
			t.Error("没有这个进程应返回错误")
		}
	})

	t.Run("取消：先发 abort，进程没退出才在宽限期后强杀", func(t *testing.T) {
		e := newRuntimeEnv(t)
		run, _ := e.startRun(t, "all")
		if err := e.rt.Cancel(ctx, run.ID); err != nil {
			t.Fatal(err)
		}
		_, p := e.l.last()
		lines := p.lines()
		if !strings.Contains(string(lines[len(lines)-1]), `"abort"`) {
			t.Errorf("应先发 abort: %s", lines[len(lines)-1])
		}
		deadline := time.Now().Add(2 * time.Second)
		for !p.wasKilled() && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if !p.wasKilled() {
			t.Error("宽限期过了进程还在，应强杀")
		}
	})

	t.Run("取消：进程在宽限期内自己退出了，不强杀", func(t *testing.T) {
		e := newRuntimeEnv(t)
		run, _ := e.startRun(t, "all")
		_ = e.rt.Cancel(ctx, run.ID)
		_, p := e.l.last()
		p.done <- ExitInfo{Code: 0}
		time.Sleep(250 * time.Millisecond)
		if p.wasKilled() {
			t.Error("已经退出的进程不该再杀")
		}
	})

	t.Run("取消一个不存在的运行不是错误", func(t *testing.T) {
		e := newRuntimeEnv(t)
		if err := e.rt.Cancel(ctx, 424242); err != nil {
			t.Errorf("err=%v", err)
		}
	})
}

// waitFor 轮询直到条件成立（异步测试用轮询加超时，不用固定 sleep）。
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待超时：%s", what)
}

func TestProcessRuntime_ExitHandling(t *testing.T) {
	t.Run("进程异常退出而运行还在 running：标为中断，收回令牌，清临时目录", func(t *testing.T) {
		e := newRuntimeEnv(t)
		run, in := e.startRun(t, "all")
		spec, p := e.l.last()
		p.done <- ExitInfo{Code: 1, StderrTail: "boom"}
		waitFor(t, "运行变为 interrupted", func() bool { return e.run(run.ID).Status == model.RunInterrupted })
		waitFor(t, "令牌被收回", func() bool { return e.br.TokenCount() == 0 })
		waitFor(t, "临时目录被清掉", func() bool { _, err := os.Stat(spec.Dir); return os.IsNotExist(err) })
		if _, err := e.br.ExecuteTool(context.Background(), in["token"].(string), "tc", "plan_update", []byte(`{}`)); !errors.Is(err, ErrBridgeToken) {
			t.Errorf("退出后令牌应失效: %v", err)
		}
		found := false
		for _, ty := range e.persistedTypes() {
			found = found || ty == "run.status"
		}
		if !found {
			t.Error("应记 run.status 事件")
		}
	})

	t.Run("正常结束（运行已不是 running）：不改状态", func(t *testing.T) {
		e := newRuntimeEnv(t)
		run, _ := e.startRun(t, "all")
		e.setStatus(run.ID, model.RunSucceeded)
		_, p := e.l.last()
		p.done <- ExitInfo{Code: 0}
		waitFor(t, "令牌被收回", func() bool { return e.br.TokenCount() == 0 })
		if e.run(run.ID).Status != model.RunSucceeded {
			t.Errorf("status=%s", e.run(run.ID).Status)
		}
	})

	t.Run("旧进程已报告结束（令牌已收回）才退出，而运行已被续跑放回 running：不能判为崩溃", func(t *testing.T) {
		e := newRuntimeEnv(t)
		run, in := e.startRun(t, "all")
		_ = e.br.Finish(context.Background(), in["token"].(string), "paused", "") // 旧进程因等审批而停下，报告了 paused
		e.setStatus(run.ID, model.RunRunning)                                     // 用户批准，Decide 把运行放回 running
		_, p := e.l.last()
		p.done <- ExitInfo{Code: 0}
		waitFor(t, "进程清理完成", func() bool { return e.br.TokenCount() == 0 })
		time.Sleep(100 * time.Millisecond)
		if e.run(run.ID).Status != model.RunRunning {
			t.Errorf("正常结束的旧进程不能把续跑中的运行标为中断: %s", e.run(run.ID).Status)
		}
	})

	t.Run("停在等审批：进程退出是正常的，状态不变", func(t *testing.T) {
		e := newRuntimeEnv(t)
		run, _ := e.startRun(t, "all")
		e.setStatus(run.ID, model.RunWaitingApproval)
		_, p := e.l.last()
		p.done <- ExitInfo{Code: 0}
		waitFor(t, "令牌被收回", func() bool { return e.br.TokenCount() == 0 })
		if e.run(run.ID).Status != model.RunWaitingApproval {
			t.Errorf("status=%s", e.run(run.ID).Status)
		}
	})

	t.Run("退出后可以再启动同一个运行的下一个片段", func(t *testing.T) {
		e := newRuntimeEnv(t)
		run, _ := e.startRun(t, "all")
		e.setStatus(run.ID, model.RunWaitingApproval)
		_, p := e.l.last()
		p.done <- ExitInfo{Code: 0}
		waitFor(t, "令牌被收回", func() bool { return e.br.TokenCount() == 0 })
		e.setStatus(run.ID, model.RunRunning) // 用户批准后 Decide 把运行放回 running
		if err := e.rt.Resume(context.Background(), run, ResumeInfo{Reason: "approval", ToolCallID: "tc1", Content: "ok"}); err != nil {
			t.Fatalf("续跑: %v", err)
		}
	})
}

func TestProcessRuntime_Resume(t *testing.T) {
	ctx := context.Background()

	t.Run("审批后续跑：mode=continue，带上工具结果，不再改运行状态", func(t *testing.T) {
		e := newRuntimeEnv(t)
		run := e.runningRun(t) // Decide 已经把运行放回 running
		err := e.rt.Resume(ctx, run, ResumeInfo{Reason: "approval", ToolCallID: "tc-del", Content: "用户已批准，已删除 1 个节点。"})
		if err != nil {
			t.Fatal(err)
		}
		in := e.firstLine(t)
		tr := in["tool_result"].(map[string]any)
		if in["mode"] != "continue" || tr["tool_call_id"] != "tc-del" || tr["content"] != "用户已批准，已删除 1 个节点。" {
			t.Errorf("in=%v", in)
		}
		if _, has := in["prompt"]; has {
			t.Error("续跑不带新的用户消息")
		}
		if e.run(run.ID).Status != model.RunRunning {
			t.Errorf("status=%s", e.run(run.ID).Status)
		}
	})

	t.Run("点继续：mode=resume，运行从 queued 变 running", func(t *testing.T) {
		e := newRuntimeEnv(t)
		v := e.start(t, "x")
		run := e.run(uint64(v.ID))
		if err := e.rt.Resume(ctx, &run, ResumeInfo{Reason: "resume"}); err != nil {
			t.Fatal(err)
		}
		if e.firstLine(t)["mode"] != "resume" || e.run(run.ID).Status != model.RunRunning {
			t.Errorf("mode=%v status=%s", e.firstLine(t)["mode"], e.run(run.ID).Status)
		}
	})

	t.Run("上一个片段的进程还没退出：等它退出再启动（它退出才意味着最终历史已保存）", func(t *testing.T) {
		e := newRuntimeEnv(t)
		run, _ := e.startRun(t, "all")
		e.setStatus(run.ID, model.RunRunning) // 用户已经批准：Decide 把运行放回 running，而旧进程还在做收尾
		_, old := e.l.last()

		errc := make(chan error, 1)
		go func() {
			errc <- e.rt.Resume(ctx, run, ResumeInfo{Reason: "approval", ToolCallID: "tc1", Content: "ok"})
		}()
		time.Sleep(150 * time.Millisecond)
		e.l.mu.Lock()
		launchedEarly := len(e.l.specs) > 1
		e.l.mu.Unlock()
		if launchedEarly {
			t.Fatal("旧进程还没退出就启动了新进程：新进程会读到没保存完的历史")
		}
		old.done <- ExitInfo{Code: 0} // 旧进程收尾完毕、退出
		select {
		case err := <-errc:
			if err != nil {
				t.Fatalf("旧进程退出后应能续跑: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("旧进程退出后续跑应继续")
		}
		e.l.mu.Lock()
		defer e.l.mu.Unlock()
		if len(e.l.specs) != 2 {
			t.Errorf("应启动第二个进程: %d", len(e.l.specs))
		}
	})

	t.Run("旧进程迟迟不退出：超时返回错误，不会无限等", func(t *testing.T) {
		e := newRuntimeEnv(t)
		run, _ := e.startRun(t, "all")
		e.setStatus(run.ID, model.RunRunning)
		short, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
		if err := e.rt.Resume(short, run, ResumeInfo{Reason: "approval", ToolCallID: "tc1", Content: "ok"}); err == nil {
			t.Error("等不到旧进程退出应返回错误")
		}
	})

	t.Run("未知的续跑原因", func(t *testing.T) {
		e := newRuntimeEnv(t)
		run := e.runningRun(t)
		if err := e.rt.Resume(ctx, run, ResumeInfo{Reason: "whatever"}); err == nil {
			t.Error("应返回错误")
		}
	})
}

func TestProcessRuntime_ShutdownKillsEverything(t *testing.T) {
	e := newRuntimeEnv(t)
	e.startRun(t, "all")
	_, p := e.l.last()
	e.rt.Shutdown()
	if !p.wasKilled() {
		t.Error("服务退出时要杀掉所有运行进程，不能留下孤儿进程")
	}
}
