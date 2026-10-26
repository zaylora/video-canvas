package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/ws"
	. "video-canvas/internal/service/agent"
)

type relayBroadcaster struct {
	mu   sync.Mutex
	msgs []struct {
		channel string
		msg     ws.Message
	}
}

func (b *relayBroadcaster) Publish(_ context.Context, channel string, msg ws.Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.msgs = append(b.msgs, struct {
		channel string
		msg     ws.Message
	}{channel, msg})
}

func goodEvent() RelayEvent {
	return RelayEvent{
		UserID: "42", SessionID: "7001", CanvasID: "9002", RunID: "8003", Seq: 5, Type: "message.done",
		Data: json.RawMessage(`{"text":"你好"}`), CreatedAt: "2026-10-25T08:00:00.000Z",
	}
}

// Agent 服务产生的事件走 Go 推给浏览器：频道是用户频道，内容和 Go 自己产生的事件一模一样（id 编码、run_id 可空）。
func TestEventRelay_PublishesToUserChannelInGoShape(t *testing.T) {
	bc := &relayBroadcaster{}
	relay := NewEventRelay(bc)
	temp := goodEvent()
	temp.Seq, temp.Type, temp.RunID = 0, "message.delta", "0" // 会话级、临时的事件：没有运行，序号 0
	if err := relay.Publish(context.Background(), []RelayEvent{goodEvent(), temp}); err != nil {
		t.Fatal(err)
	}
	if len(bc.msgs) != 2 {
		t.Fatalf("应推送 2 条：%d", len(bc.msgs))
	}
	m := bc.msgs[0]
	if m.channel != ws.UserChannel(42) || m.msg.Type != ws.TypeAgentEvent || m.msg.Channel != m.channel {
		t.Errorf("频道和消息类型不对：%+v", m)
	}
	b, _ := json.Marshal(m.msg.Data)
	var got map[string]any
	_ = json.Unmarshal(b, &got)
	want := map[string]any{
		"session_id": idcodec.Encode(7001), "canvas_id": idcodec.Encode(9002), "run_id": idcodec.Encode(8003),
		"seq": float64(5), "type": "message.done", "data": map[string]any{"text": "你好"}, "created_at": "2026-10-25T08:00:00Z",
	}
	for k, v := range want {
		gb, _ := json.Marshal(got[k])
		wb, _ := json.Marshal(v)
		if string(gb) != string(wb) {
			t.Errorf("字段 %s：期望 %s，实际 %s", k, wb, gb)
		}
	}
	b2, _ := json.Marshal(bc.msgs[1].msg.Data)
	if !strings.Contains(string(b2), `"run_id":null`) || !strings.Contains(string(b2), `"seq":0`) {
		t.Errorf("run_id 为 0 要变成 null，临时事件序号保持 0：%s", b2)
	}
}

func TestEventRelay_RejectsMalformedEventsAndPublishesNothing(t *testing.T) {
	cases := map[string]func(e *RelayEvent){
		"用户 id 不是正整数": func(e *RelayEvent) { e.UserID = "abc" },
		"用户 id 为 0":   func(e *RelayEvent) { e.UserID = "0" },
		"会话 id 缺失":    func(e *RelayEvent) { e.SessionID = "" },
		"画布 id 非法":    func(e *RelayEvent) { e.CanvasID = "-1" },
		"运行 id 非法":    func(e *RelayEvent) { e.RunID = "x" },
		"序号为负":        func(e *RelayEvent) { e.Seq = -1 },
		"类型为空":        func(e *RelayEvent) { e.Type = "" },
		"内容不是合法 JSON": func(e *RelayEvent) { e.Data = json.RawMessage(`{bad`) },
		"时间格式错误":      func(e *RelayEvent) { e.CreatedAt = "昨天" },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			bc := &relayBroadcaster{}
			bad := goodEvent()
			mut(&bad)
			err := NewEventRelay(bc).Publish(context.Background(), []RelayEvent{goodEvent(), bad})
			var e *errcode.Error
			if !errors.As(err, &e) || e.Code != errcode.ErrInvalidParams.Code {
				t.Fatalf("应报参数错误：%v", err)
			}
			if len(bc.msgs) != 0 {
				t.Errorf("整批校验通过才推送，否则一条都不推：%d", len(bc.msgs))
			}
		})
	}
}

func TestEventRelay_BatchSizeLimit(t *testing.T) {
	bc := &relayBroadcaster{}
	relay := NewEventRelay(bc)
	if err := relay.Publish(context.Background(), nil); err == nil {
		t.Error("空批次应报错")
	}
	big := make([]RelayEvent, MaxRelayBatch+1)
	for i := range big {
		big[i] = goodEvent()
	}
	if err := relay.Publish(context.Background(), big); err == nil {
		t.Errorf("超过 %d 条应报错", MaxRelayBatch)
	}
	if err := relay.Publish(context.Background(), big[:MaxRelayBatch]); err != nil {
		t.Errorf("正好 %d 条应通过：%v", MaxRelayBatch, err)
	}
	_ = time.Now
}
