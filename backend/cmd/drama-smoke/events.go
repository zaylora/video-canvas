package main

import (
	"context"
	"encoding/json"
	"sync"

	"video-canvas/internal/pkg/ws"
)

// smokeEvents 记录真实业务发送到用户频道的事件，验证task.updated与drama.segment同时存在。
type smokeEvents struct {
	mu       sync.Mutex
	messages []ws.Message
}

func (e *smokeEvents) Publish(_ context.Context, channel string, msg ws.Message) {
	require(channel == "user:1", "事件发往错误用户")
	e.mu.Lock()
	defer e.mu.Unlock()
	e.messages = append(e.messages, msg)
}

// verify 在并发worker完成后检查采用结果与首帧事件内容，失败时输出原始事件数据。
func (e *smokeEvents) verify(sid string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var taskSeen, videoSeen, keyframeSeen bool
	for _, msg := range e.messages {
		raw, err := json.Marshal(msg.Data)
		must(err)
		var body map[string]any
		must(json.Unmarshal(raw, &body))
		if msg.Type == ws.TypeTaskUpdated && body["node_id"] == "seg:"+sid && body["status"] == "succeeded" {
			taskSeen = true
		}
		if msg.Type != ws.TypeDramaSegment {
			continue
		}
		segment := body["segment"].(map[string]any)
		if segment["id"] != sid {
			continue
		}
		require(body["episode_no"].(float64) == 1 && body["drama_id"] != "", string(raw))
		videoSeen = videoSeen || (segment["status"] == "done" && segment["chosen_task_id"] != "")
		keyframeSeen = keyframeSeen || segment["keyframe"] != nil
	}
	require(taskSeen && videoSeen && keyframeSeen, "task.updated或drama.segment事件缺失")
}
