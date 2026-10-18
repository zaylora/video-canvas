package agent

import "video-canvas/internal/model"

// runStatusTexts 是需要在消息流里提示用户的运行状态：标题，以及一句「接下来可以做什么」。
// 文字由后端随 run.status 事件下发，前端不再写死：改文案、加状态只改这里。
var runStatusTexts = map[string]struct{ title, hint string }{
	model.RunFailed:          {"运行失败", "可以换个说法重试"},
	model.RunTimeout:         {"运行超时", "可以继续，或换个说法重试"},
	model.RunInterrupted:     {"运行被中断", "服务重启或运行进程退出了，可以从断点继续"},
	model.RunBudgetExhausted: {"本轮预算用完了", "追加预算后可以继续"},
	model.RunStepLimit:       {"达到本轮步数上限", "继续的话会再给一轮步数"},
	model.RunCanceled:        {"已停止", "没做完的计划还留着，可以接着让它做"},
	"expired":                {"等待超时", "你太久没有回应，这一轮已经结束"},
}

// RunStatusText 返回运行状态给用户看的标题和提示；不需要提示用户的状态（排队、运行中、完成等）返回空串。
func RunStatusText(status string) (title, hint string) {
	t := runStatusTexts[status]
	return t.title, t.hint
}

// statusEvent 构造 run.status 事件的内容：需要提示用户的状态带上 title 和 hint，extra 是成对的键和值（error、resumed 等）。
func statusEvent(status string, extra ...any) map[string]any {
	ev := map[string]any{"status": status}
	if title, hint := RunStatusText(status); title != "" {
		ev["title"], ev["hint"] = title, hint
	}
	for i := 0; i+1 < len(extra); i += 2 {
		ev[extra[i].(string)] = extra[i+1]
	}
	return ev
}
