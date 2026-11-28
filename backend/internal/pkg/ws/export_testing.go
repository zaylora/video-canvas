package ws

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// 本文件只为 internal/tests 下的外部测试包暴露包内符号，业务代码不要引用。

const (
	MaxSubsPerConnection = maxSubsPerConnection
	TicketKeyPrefix      = ticketKeyPrefix
)

func (h *Hub) NewClient(ws *websocket.Conn, userID uint64) *client { return h.newClient(ws, userID) }

func (h *Hub) Register(c *client) bool { return h.register(c) }

func (h *Hub) WaitGroup() *sync.WaitGroup { return &h.wg }

func WithSendQueueBytes(limit int) Option {
	return func(h *Hub) { h.sendQueueBytes = limit }
}

func (s *MemoryTicketStore) SetNow(fn func() time.Time) { s.now = fn }

func (s *MemoryTicketStore) Size() int { return s.size() }

// Done 返回连接的关闭信号。
func (c *client) Done() <-chan struct{} { return c.done }

// Pending 返回发送队列里尚未写出的消息数。
func (c *client) Pending() int {
	count, _ := c.send.stats()
	return count
}

// Drain 取走发送队列里的一条消息。
func (c *client) Drain() { c.send.pop() }

// DrainDeliverable 读取当前队列中下一条可以写出的消息类型。
func (c *client) DrainDeliverable() string {
	var message Message
	_ = json.Unmarshal(c.DrainPayload(), &message)
	return message.Type
}

// DrainPayload 读取当前队列中下一条可以写出的消息。
func (c *client) DrainPayload() []byte {
	for {
		queued, available := c.send.pop()
		if !available {
			return nil
		}
		if queued.deliverable() {
			return queued.payload
		}
	}
}

// Subscribe 通过正常订阅入口将无写循环的测试连接加入频道。
func (c *client) Subscribe(ctx context.Context, channel string) {
	c.handleSubscribe(ctx, channel, false)
}
