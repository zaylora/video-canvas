package agent_test

import (
	"context"
	"strings"
	"testing"

	"video-canvas/internal/model"
	. "video-canvas/internal/service/agent"
)

// 运行状态给用户看的文字（标题和「可以做什么」）由后端随 run.status 事件下发，前端不再写死：改文案、加状态只改这一处。
func TestRunStatusText(t *testing.T) {
	want := map[string][2]string{
		"failed":           {"运行失败", "可以换个说法重试"},
		"timeout":          {"运行超时", "可以继续，或换个说法重试"},
		"interrupted":      {"运行被中断", "服务重启或运行进程退出了，可以从断点继续"},
		"budget_exhausted": {"本轮预算用完了", "追加预算后可以继续"},
		"step_limit":       {"达到本轮步数上限", "继续的话会再给一轮步数"},
		"canceled":         {"已停止", "没做完的计划还留着，可以接着让它做"},
		"expired":          {"等待超时", "你太久没有回应，这一轮已经结束"},
	}
	for status, w := range want {
		if title, hint := RunStatusText(status); title != w[0] || hint != w[1] {
			t.Errorf("%s: (%q, %q)，应为 (%q, %q)", status, title, hint, w[0], w[1])
		}
	}
	for _, status := range []string{"queued", "running", "succeeded", "waiting_approval", "waiting_input", "nope"} {
		if title, hint := RunStatusText(status); title != "" || hint != "" {
			t.Errorf("%s 不需要提示用户，不该有文字: (%q, %q)", status, title, hint)
		}
	}
}

func TestRunStatusEvents_CarryText(t *testing.T) {
	ctx := context.Background()

	t.Run("停止", func(t *testing.T) {
		e := newAgentEnv(t)
		run := e.runningRun(t)
		if _, err := e.svc.Cancel(ctx, 1, run.ID); err != nil {
			t.Fatal(err)
		}
		ev := e.repo.lastEvent("run.status")
		if ev["status"] != "canceled" || ev["title"] != "已停止" || !strings.Contains(ev["hint"].(string), "没做完的计划") {
			t.Errorf("canceled 事件要带标题和提示: %v", ev)
		}
	})

	t.Run("runtime 启动失败", func(t *testing.T) {
		e := newAgentEnv(t)
		e.rt.startErr = errAgentBoom
		_, _ = e.svc.StartRun(ctx, 1, e.sess, &model.StartAgentRunReq{Message: "x"})
		ev := e.repo.lastEvent("run.status")
		if ev["status"] != "failed" || ev["title"] != "运行失败" || ev["error"] == "" {
			t.Errorf("failed 事件要带标题和提示，原因照旧在 error 里: %v", ev)
		}
	})

	t.Run("不需要提示的状态不带文字", func(t *testing.T) {
		e := newAgentEnv(t)
		e.start(t, "拆分镜")
		ev := e.repo.lastEvent("run.status")
		if ev["status"] != "queued" {
			t.Fatalf("ev=%v", ev)
		}
		if _, has := ev["title"]; has {
			t.Errorf("queued 不需要提示用户: %v", ev)
		}
	})

	t.Run("步数用尽", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, run := b.token(t, "all")
		b.repo.mu.Lock()
		b.repo.runs[run.ID].Steps = run.MaxSteps
		b.repo.mu.Unlock()
		b.tool(t, tok, "canvas_get_state", map[string]any{})
		ev := b.repo.lastEvent("run.status")
		if ev["status"] != "step_limit" || ev["title"] != "达到本轮步数上限" {
			t.Errorf("step_limit 事件要带标题和提示: %v", ev)
		}
	})

	t.Run("预算用尽", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, run := b.token(t, "all")
		b.repo.mu.Lock()
		b.repo.runs[run.ID].SpentCredits = 50
		b.repo.mu.Unlock()
		_ = b.br.ProxyModel(ctx, tok, []byte(`{"model":"x","messages":[{"role":"user","content":"x"}]}`), func([]byte) {})
		ev := b.repo.lastEvent("run.status")
		if ev["status"] != "budget_exhausted" || ev["title"] != "本轮预算用完了" {
			t.Errorf("budget_exhausted 事件要带标题和提示: %v", ev)
		}
	})

	t.Run("Agent 报告出错", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, _ := b.token(t, "all")
		if err := b.br.Finish(ctx, tok, "error", "上游挂了"); err != nil {
			t.Fatal(err)
		}
		ev := b.repo.lastEvent("run.status")
		if ev["status"] != "failed" || ev["title"] != "运行失败" || ev["error"] != "上游挂了" {
			t.Errorf("failed 事件要带标题和提示: %v", ev)
		}
	})

	t.Run("正常完成不带文字", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, _ := b.token(t, "all")
		if err := b.br.Finish(ctx, tok, "done", ""); err != nil {
			t.Fatal(err)
		}
		ev := b.repo.lastEvent("run.status")
		if ev["status"] != "succeeded" {
			t.Fatalf("ev=%v", ev)
		}
		if _, has := ev["title"]; has {
			t.Errorf("succeeded 不需要提示用户: %v", ev)
		}
	})

	t.Run("进程异常退出", func(t *testing.T) {
		e := newRuntimeEnv(t)
		run, _ := e.startRun(t, "all")
		_, p := e.l.last()
		p.done <- ExitInfo{Code: 1, StderrTail: "boom"}
		waitFor(t, "运行变为 interrupted", func() bool { return e.run(run.ID).Status == model.RunInterrupted })
		ev := e.repo.lastEvent("run.status")
		if ev["status"] != "interrupted" || ev["title"] != "运行被中断" || ev["error"] != "运行进程异常退出" {
			t.Errorf("interrupted 事件要带标题和提示: %v", ev)
		}
	})
}
