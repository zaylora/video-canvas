package agent

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/ws"
)

// MaxRelayBatch 是一次推送最多带多少条事件：文字增量会成批发来，但不该无上限。
const MaxRelayBatch = 200

// RelayEvent 是独立的 Agent 服务（agent.runtime=durable）发来的一条事件。id 是十进制字符串（雪花 id 超过 2^53），
// 转成对外的编码串由 Go 做：编码密钥只在 Go 手里，浏览器看到的 id 和 Go 自己产生的事件一致。
type RelayEvent struct {
	UserID    string          `json:"user_id"`
	SessionID string          `json:"session_id"`
	CanvasID  string          `json:"canvas_id"`
	RunID     string          `json:"run_id"` // 会话级事件为空或 "0"
	Seq       int64           `json:"seq"`    // 0 表示临时事件（文字增量），不参与回放
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	CreatedAt string          `json:"created_at"` // RFC3339
}

// EventRelay 把 Agent 服务产生的事件推给浏览器的 WebSocket。事件已经由 Agent 服务落库，
// 这里只做校验、编码和发布；推送可以丢，前端按 seq 走 HTTP 回放对账。
type EventRelay struct {
	bc ws.Broadcaster
}

// NewEventRelay 创建中继；bc 为 nil 时什么都不推（校验照常）。
func NewEventRelay(bc ws.Broadcaster) *EventRelay {
	if bc == nil {
		bc = ws.NopBroadcaster{}
	}
	return &EventRelay{bc: bc}
}

// relayItem 是校验、转换后的一条待发布消息。
type relayItem struct {
	channel string
	view    *AgentEventView
}

// Publish 校验整批事件，全部通过才逐条发布（一条不合格整批拒绝，不会推出半批）。
func (r *EventRelay) Publish(ctx context.Context, events []RelayEvent) error {
	if len(events) == 0 || len(events) > MaxRelayBatch {
		return errcode.ErrInvalidParams.WithMsg("events 必须是 1 到 " + strconv.Itoa(MaxRelayBatch) + " 条")
	}
	items := make([]relayItem, 0, len(events))
	for i, e := range events {
		it, err := convertRelayEvent(e)
		if err != nil {
			return errcode.ErrInvalidParams.WithMsg("第 " + strconv.Itoa(i+1) + " 条事件" + err.Error())
		}
		items = append(items, it)
	}
	for _, it := range items {
		r.bc.Publish(ctx, it.channel, ws.Message{Type: ws.TypeAgentEvent, Channel: it.channel, Data: it.view})
	}
	return nil
}

type relayErr string

func (e relayErr) Error() string { return string(e) }

func convertRelayEvent(e RelayEvent) (relayItem, error) {
	userID, ok := positiveID(e.UserID)
	if !ok {
		return relayItem{}, relayErr("的 user_id 必须是正整数")
	}
	sessionID, ok := positiveID(e.SessionID)
	if !ok {
		return relayItem{}, relayErr("的 session_id 必须是正整数")
	}
	canvasID, ok := positiveID(e.CanvasID)
	if !ok {
		return relayItem{}, relayErr("的 canvas_id 必须是正整数")
	}
	var runID *idcodec.ID
	if e.RunID != "" && e.RunID != "0" {
		id, ok := positiveID(e.RunID)
		if !ok {
			return relayItem{}, relayErr("的 run_id 必须是正整数")
		}
		v := idcodec.ID(id)
		runID = &v
	}
	if e.Seq < 0 {
		return relayItem{}, relayErr("的 seq 不能为负")
	}
	if e.Type == "" || len(e.Type) > 64 {
		return relayItem{}, relayErr("的 type 必须是 1 到 64 个字符")
	}
	if !json.Valid(e.Data) {
		return relayItem{}, relayErr("的 data 必须是合法的 JSON")
	}
	at, err := time.Parse(time.RFC3339Nano, e.CreatedAt)
	if err != nil {
		return relayItem{}, relayErr("的 created_at 必须是 RFC3339 时间")
	}
	return relayItem{
		channel: ws.UserChannel(userID),
		view: &AgentEventView{
			SessionID: idcodec.ID(sessionID), CanvasID: idcodec.ID(canvasID), RunID: runID,
			Seq: e.Seq, Type: e.Type, Data: e.Data, CreatedAt: at,
		},
	}, nil
}

func positiveID(s string) (uint64, bool) {
	id, err := strconv.ParseUint(s, 10, 64)
	return id, err == nil && id > 0
}
