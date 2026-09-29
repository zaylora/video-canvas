package ws

import (
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

func (s *MemoryTicketStore) SetNow(fn func() time.Time) { s.now = fn }

func (s *MemoryTicketStore) Size() int { return s.size() }

// Done 返回连接的关闭信号。
func (c *client) Done() <-chan struct{} { return c.done }

// Pending 返回发送队列里尚未写出的消息数。
func (c *client) Pending() int { return len(c.send) }

// Drain 取走发送队列里的一条消息。
func (c *client) Drain() { <-c.send }
