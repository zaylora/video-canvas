// Package realtime 是用户级 WebSocket 推送：连接管理（Hub）、一次性 ticket、广播接口。
package ws

import (
	"context"
	"fmt"
)

// 消息类型。
const (
	TypeHello       = "hello"
	TypeTaskUpdated = "task.updated"

	// 以下是连接控制类消息（订阅协议与心跳）。
	TypePing         = "ping"         // 客户端 → 服务端：应用层心跳
	TypePong         = "pong"         // 服务端 → 客户端：心跳应答
	TypeSubscribe    = "subscribe"    // 客户端 → 服务端：订阅频道
	TypeUnsubscribe  = "unsubscribe"  // 客户端 → 服务端：取消订阅
	TypeSubscribed   = "subscribed"   // 服务端 → 客户端：订阅成功
	TypeUnsubscribed = "unsubscribed" // 服务端 → 客户端：取消订阅成功
	TypeError        = "error"        // 服务端 → 客户端：请求被拒绝或格式错误，data 为 {code, msg}
)

// Message 是服务端 → 客户端的推送消息。
type Message struct {
	Type    string `json:"type"`
	Channel string `json:"channel,omitempty"`
	Data    any    `json:"data"`
}

// Broadcaster 把消息发布到某个频道（如 user:12）。MVP 是进程内 Hub 实现，
// 多实例时换成 Redis pub/sub 实现，业务代码不用改。Publish 不返回错误：推送可以丢，客户端会对账。
type Broadcaster interface {
	Publish(ctx context.Context, channel string, msg Message)
}

// UserChannel 是用户级频道名。
func UserChannel(userID uint64) string { return fmt.Sprintf("user:%d", userID) }

// CanvasChannel 是画布频道名（协作预留，MVP 不订阅）。
func CanvasChannel(canvasID uint64) string { return fmt.Sprintf("canvas:%d", canvasID) }

// NopBroadcaster 什么都不做，用于测试和未启用推送的场景。
type NopBroadcaster struct{}

func (NopBroadcaster) Publish(context.Context, string, Message) {}
