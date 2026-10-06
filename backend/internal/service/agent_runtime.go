package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/agentprompts"
	"video-canvas/internal/canvasgraph"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
)

// waitExitLimit 是续跑前最多等上一个进程退出多久。进程收尾只是回传历史和报告结束，正常是毫秒级。
const waitExitLimit = 15 * time.Second

// RuntimeConfig 是运行时的配置。
type RuntimeConfig struct {
	NodePath    string        // node 可执行文件
	ScriptPath  string        // agent-runtime/src/main.mjs 的路径
	BridgeURL   string        // 桥的地址，如 http://127.0.0.1:47001，Node 用它回调 Go
	WorkRoot    string        // 每个片段的临时目录建在这里，空表示系统临时目录
	CancelGrace time.Duration // 发 abort 之后等进程自己退出多久，超时强杀；0 取 5 秒
}

// RuntimeDeps 是创建 ProcessRuntime 需要的依赖。
type RuntimeDeps struct {
	Repo     AgentRepo           // 会话、运行
	Bridge   *AgentBridge        // 令牌、状态
	Canvas   *AgentCanvasService // 画布目录
	Agent    *AgentService       // 事件
	Registry provider.Registry   // 模型快照
	Launcher Launcher            // 进程启动器
	Config   RuntimeConfig       // 配置
}

// segment 是一个运行中的进程（一个运行片段）。
type segment struct {
	proc   Proc
	token  string
	dir    string
	exited chan struct{} // 进程退出并清理完后关闭（续跑要等它：它退出才意味着历史已保存）
	done   chan struct{} // 退出后的状态修正（标为中断等）也做完后关闭（服务退出要等它，之后才能关库）
}

// ProcessRuntime 是 AgentRuntime 的真正实现：每个运行片段起一个 Node 进程，跑完即退出。
// 片段指一次开始、一次审批后的续跑、或一次「继续」。进程里没有任何密钥，只有一个一次性的桥令牌。
type ProcessRuntime struct {
	d    RuntimeDeps
	mu   sync.Mutex
	segs map[uint64]*segment
}

// NewProcessRuntime 创建运行时。
func NewProcessRuntime(d RuntimeDeps) *ProcessRuntime {
	if d.Config.CancelGrace <= 0 {
		d.Config.CancelGrace = 5 * time.Second
	}
	return &ProcessRuntime{d: d, segs: map[uint64]*segment{}}
}

// Start 启动一轮运行的第一个片段。
func (r *ProcessRuntime) Start(ctx context.Context, run *model.AgentRun, in model.AgentRunInput) error {
	input, err := r.baseInput(ctx, run, in)
	if err != nil {
		return err
	}
	// 用户消息后面附上画布目录和运行参数，明确标成数据，不是指令
	cat, err := r.d.Canvas.Catalog(ctx, run, canvasgraph.CatalogOptions{Priority: in.Selection})
	if err != nil {
		return err
	}
	catJSON, err := json.Marshal(cat)
	if err != nil {
		return err
	}
	text := agentprompts.User(in.Message, string(catJSON), agentprompts.RunInfo{BudgetCredits: run.BudgetCredits, SpentCredits: run.SpentCredits, MaxSteps: run.MaxSteps})
	input["mode"], input["prompt"] = "start", map[string]any{"text": text}
	return r.launch(ctx, run, in, input, true)
}

// Resume 启动续跑的片段：审批后把真实结果补回工具调用再继续；或用户点「继续」从中断的地方接着走。
func (r *ProcessRuntime) Resume(ctx context.Context, run *model.AgentRun, info ResumeInfo) error {
	// 先等上一个片段的进程退出：用户点批准时它可能还在收尾（回传最终的对话历史、报告结束）。
	// 它退出才意味着历史已保存，这时再读历史，续跑才能找到那条等待中的工具结果；也避免同一个运行出现两个进程
	if err := r.waitExit(ctx, run.ID); err != nil {
		return err
	}
	// 续跑没有新的用户消息，模型和模式取自会话和运行
	sess, err := r.d.Repo.GetSession(ctx, run.UserID, run.SessionID)
	if err != nil {
		return sessionErr(err)
	}
	in := model.AgentRunInput{ModelKey: sess.ModelKey, Mode: firstNonEmpty(run.Mode, sess.Mode, model.AgentModeAll)}
	input, err := r.baseInput(ctx, run, in)
	if err != nil {
		return err
	}
	switch info.Reason {
	case "approval":
		// Decide 已经把运行放回 running，这里不再改状态
		input["mode"], input["tool_result"] = "continue", map[string]any{"tool_call_id": info.ToolCallID, "content": info.Content}
		return r.launch(ctx, run, in, input, false)
	case "resume":
		input["mode"] = "resume"
		return r.launch(ctx, run, in, input, true)
	}
	return fmt.Errorf("未知的续跑原因 %q", info.Reason)
}

// waitExit 等运行当前的进程退出；没有进程立即返回。等不到（ctx 取消或超过上限）返回错误。
func (r *ProcessRuntime) waitExit(ctx context.Context, runID uint64) error {
	r.mu.Lock()
	seg := r.segs[runID]
	r.mu.Unlock()
	if seg == nil {
		return nil
	}
	select {
	case <-seg.exited:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("等待上一个运行进程退出被中断：%w", ctx.Err())
	case <-time.After(waitExitLimit):
		return errors.New("上一个运行进程长时间没有退出")
	}
}

// baseInput 组装启动参数里与启动方式无关的部分：模型、系统提示词、可用工具、历史。
func (r *ProcessRuntime) baseInput(ctx context.Context, run *model.AgentRun, in model.AgentRunInput) (map[string]any, error) {
	snap, _, err := r.d.Bridge.resolveModel(ctx, in.ModelKey) // 顺便确认模型和渠道 Key 都可用，不可用就不要白起一个进程
	if err != nil {
		return nil, err
	}
	caps := snap.Model.Capabilities
	if caps.Context == nil {
		return nil, errcode.ErrAgentModelNA
	}
	mode := firstNonEmpty(in.Mode, run.Mode, model.AgentModeAll)
	input := map[string]any{
		"bridge_url":    r.d.Config.BridgeURL,
		"model":         map[string]any{"name": snap.Model.Label, "context_window": caps.Context.Window, "max_tokens": caps.Context.Output, "vision": caps.Vision},
		"system_prompt": agentprompts.System(mode),
		"allowed_tools": AgentToolsForMode(mode),
	}
	state, err := r.d.Bridge.LoadState(ctx, run.UserID, run.SessionID)
	if err != nil {
		return nil, err
	}
	if state != "" {
		input["messages"] = json.RawMessage(state)
	}
	return input, nil
}

// launch 启动进程并把启动参数写进它的标准输入。markRunning 为真时先把运行从 queued 标为 running。
// 任何一步失败都要把已经做的清理掉：令牌、临时目录、进程。
func (r *ProcessRuntime) launch(ctx context.Context, run *model.AgentRun, in model.AgentRunInput, input map[string]any, markRunning bool) error {
	// 1. 同一个运行同时只能有一个进程：先占位，避免并发启动两个
	r.mu.Lock()
	if _, busy := r.segs[run.ID]; busy {
		r.mu.Unlock()
		return errors.New("这个运行已经有一个进程在跑")
	}
	seg := &segment{exited: make(chan struct{}), done: make(chan struct{})}
	r.segs[run.ID] = seg
	r.mu.Unlock()
	release := func() { r.mu.Lock(); delete(r.segs, run.ID); r.mu.Unlock() }

	if markRunning {
		if err := r.d.Bridge.MarkRunning(ctx, run); err != nil {
			release()
			return err
		}
	}
	// 2. 令牌、临时目录、最小的环境：子进程拿不到服务进程的任何配置和密钥
	seg.token = r.d.Bridge.IssueToken(run, in)
	input["token"] = seg.token
	dir, err := os.MkdirTemp(r.d.Config.WorkRoot, "agent-run-*")
	if err != nil {
		r.d.Bridge.RevokeToken(seg.token)
		release()
		return err
	}
	seg.dir = dir
	cleanup := func() { r.d.Bridge.RevokeToken(seg.token); _ = os.RemoveAll(dir); release() } // 目录已经没了也不要紧
	// 3. 启动并发送启动参数
	proc, err := r.d.Launcher.Launch(ctx, LaunchSpec{
		Name: r.d.Config.NodePath, Args: []string{r.d.Config.ScriptPath}, Dir: dir,
		Env: []string{"PATH=/usr/bin:/bin", "HOME=" + dir, "TMPDIR=" + dir, "NODE_ENV=production", "NODE_OPTIONS=--max-old-space-size=512"},
	})
	if err != nil {
		cleanup()
		return err
	}
	seg.proc = proc
	line, err := json.Marshal(input)
	if err == nil {
		err = proc.Send(line)
	}
	if err != nil {
		proc.Kill()
		cleanup()
		return err
	}
	//nolint:gosec // G118: 进程比发起它的请求活得久，退出后的清理和状态修正不能随请求取消
	go r.watch(run, seg)
	return nil
}

// watch 等进程退出，然后清理；进程没有报告结束就没了、而运行还是 running，标为中断，用户可以点「继续」。
// 报告过结束的进程（包括因等审批而 paused 的）退出是正常的，不能碰运行状态：运行可能已经被续跑放回 running。
func (r *ProcessRuntime) watch(run *model.AgentRun, seg *segment) {
	defer close(seg.done)
	exit := <-seg.proc.Done()
	// 令牌已被 Finish 收回，说明进程正常报告过结束；还在说明它没报告就没了（崩溃、被杀）。
	// 必须在收回令牌之前判断
	reported := !r.d.Bridge.TokenActive(seg.token)
	r.d.Bridge.RevokeToken(seg.token)
	_ = os.RemoveAll(seg.dir) // 清不掉只是留下一个临时目录，不影响运行
	r.mu.Lock()
	if r.segs[run.ID] == seg {
		delete(r.segs, run.ID)
	}
	r.mu.Unlock()
	close(seg.exited)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cur, err := r.d.Repo.GetRun(ctx, run.UserID, run.ID)
	if err != nil {
		logger.Error("进程退出后读取运行失败", zap.Error(err), zap.Uint64("run_id", run.ID))
		return
	}
	if reported || cur.Status != model.RunRunning {
		if exit.Code != 0 {
			logger.Warn("Agent 进程非零退出，运行状态已由别处处理", zap.Uint64("run_id", run.ID), zap.Int("code", exit.Code), zap.String("status", cur.Status))
		}
		return
	}
	if _, err := r.d.Repo.UpdateRunIf(ctx, run.ID, []string{model.RunRunning}, map[string]any{"status": model.RunInterrupted}); err != nil {
		return
	}
	logger.Error("Agent 进程异常退出，运行已标为中断", zap.Uint64("run_id", run.ID), zap.Int("code", exit.Code), zap.String("stderr", exit.StderrTail))
	r.d.Agent.emit(ctx, run.UserID, run.SessionID, run.ID, run.CanvasID, "run.status", map[string]any{"status": model.RunInterrupted, "error": "运行进程异常退出"})
}

// segment 取运行当前的进程。
func (r *ProcessRuntime) segment(runID uint64) *segment {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s := r.segs[runID]; s != nil && s.proc != nil {
		return s
	}
	return nil
}

// Interject 运行中插话：给进程发一条 steer，pi 在当前工具批结束后注入。
func (r *ProcessRuntime) Interject(_ context.Context, runID uint64, message string) error {
	seg := r.segment(runID)
	if seg == nil {
		return errors.New("这个运行没有进程在跑")
	}
	line, _ := json.Marshal(map[string]string{"type": "steer", "text": message}) // 只含字符串，序列化不会失败
	return seg.proc.Send(line)
}

// Cancel 中止运行：先发 abort 让 pi 自己中止（连接断开，桥据此结算已产生的用量），
// 宽限期内进程没退出就强杀。没有进程不是错误。
func (r *ProcessRuntime) Cancel(_ context.Context, runID uint64) error {
	seg := r.segment(runID)
	if seg == nil {
		return nil
	}
	_ = seg.proc.Send([]byte(`{"type":"abort"}`)) // 进程可能已经在退出，写失败不要紧，下面的强杀兜底
	go func() {
		select {
		case <-seg.exited:
		case <-time.After(r.d.Config.CancelGrace):
			seg.proc.Kill()
		}
	}()
	return nil
}

// Shutdown 服务退出时杀掉所有运行进程，不留孤儿进程；运行会被标为中断，用户可以之后继续。
func (r *ProcessRuntime) Shutdown() {
	r.mu.Lock()
	segs := make([]*segment, 0, len(r.segs))
	for _, s := range r.segs {
		if s.proc != nil {
			segs = append(segs, s)
		}
	}
	r.mu.Unlock()
	for _, s := range segs {
		s.proc.Kill()
	}
	for _, s := range segs {
		select {
		case <-s.done: // 等状态修正也做完，调用方随后才能关库
		case <-time.After(5 * time.Second):
		}
	}
}
