package ws

import (
	"context"
	"encoding/json"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"video-canvas/internal/pkg/logger"
)

// 连接参数默认值，可用 Option 覆盖（测试里会缩短）。
const (
	defaultSendQueueSize  = 64               // 每个连接的发送队列长度，满了就断开慢连接
	defaultPingInterval   = 25 * time.Second // 服务端 ping 间隔
	defaultPongWait       = 60 * time.Second // 超过这个时间没收到任何 pong/消息就断开
	defaultWriteWait      = 10 * time.Second // 单次写的超时
	defaultReadLimit      = 4 * 1024         // 单条客户端消息上限（4KB）
	defaultCloseTimeout   = 5 * time.Second  // Close() 等待所有连接退出的时间
	maxChannelNameLen     = 128              // 频道名长度上限，防止客户端塞超长字符串
	maxSubsPerConnection  = 32               // 单连接可订阅的频道数上限（含自动订阅的 user 频道）
	closeFrameWriteWait   = time.Second      // 优雅关闭时写 close 帧的超时
	errCodeBadRequest     = "bad_request"
	errCodeForbidden      = "forbidden"
	errCodeTooManyChannel = "too_many_channels"
)

// pongBytes 是固定的 pong 应答，避免每次都序列化。
var pongBytes = []byte(`{"type":"pong"}`)

// Authorizer 判断某用户能否订阅某个频道（如 canvas:34）。返回 false 表示拒绝。
// 用户自己的 user:{id} 频道在建连时自动订阅，不经过 Authorizer。
type Authorizer func(ctx context.Context, userID uint64, channel string) bool

// denyAllAuthorizer 是默认授权函数：MVP 不开放任何手动订阅，
// 协作功能上线时再注入“校验画布访问权限”的实现，避免用户订阅到别人的频道。
func denyAllAuthorizer(context.Context, uint64, string) bool { return false }

// Option 是 Hub 的可选配置。
type Option func(*Hub)

// WithAuthorizer 注入订阅授权函数；不注入时默认拒绝所有手动订阅。
func WithAuthorizer(a Authorizer) Option {
	return func(h *Hub) {
		if a != nil {
			h.authorizer = a
		}
	}
}

// WithSendQueueSize 设置每个连接的发送队列长度（默认 64）。
func WithSendQueueSize(n int) Option {
	return func(h *Hub) {
		if n > 0 {
			h.sendQueueSize = n
		}
	}
}

// WithHeartbeat 设置服务端 ping 间隔和 pong 等待时间（默认 25s / 60s）。
func WithHeartbeat(pingInterval, pongWait time.Duration) Option {
	return func(h *Hub) {
		if pingInterval > 0 {
			h.pingInterval = pingInterval
		}
		if pongWait > 0 {
			h.pongWait = pongWait
		}
	}
}

// WithWriteWait 设置单次写的超时（默认 10s）。
func WithWriteWait(d time.Duration) Option {
	return func(h *Hub) {
		if d > 0 {
			h.writeWait = d
		}
	}
}

// WithReadLimit 设置单条客户端消息的大小上限（默认 4KB）。
func WithReadLimit(n int64) Option {
	return func(h *Hub) {
		if n > 0 {
			h.readLimit = n
		}
	}
}

// Hub 管理所有 WebSocket 连接并实现 Broadcaster。
// 模型：频道 → 连接集合。建连时自动订阅 user:{id}，同一用户的多个标签页各自是一个连接。
type Hub struct {
	authorizer    Authorizer
	sendQueueSize int
	pingInterval  time.Duration
	pongWait      time.Duration
	writeWait     time.Duration
	readLimit     int64

	mu       sync.RWMutex
	clients  map[*client]struct{}
	channels map[string]map[*client]struct{}
	closed   bool
	wg       sync.WaitGroup // 每个 Serve 调用（含它的读写 goroutine）算一个
}

var _ Broadcaster = (*Hub)(nil)

// NewHub 创建 Hub。
func NewHub(opts ...Option) *Hub {
	h := &Hub{
		authorizer:    denyAllAuthorizer,
		sendQueueSize: defaultSendQueueSize,
		pingInterval:  defaultPingInterval,
		pongWait:      defaultPongWait,
		writeWait:     defaultWriteWait,
		readLimit:     defaultReadLimit,
		clients:       make(map[*client]struct{}),
		channels:      make(map[string]map[*client]struct{}),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// client 是一条 WebSocket 连接在 Hub 里的状态。
type client struct {
	hub    *Hub
	userID uint64
	ws     *websocket.Conn // 单元测试里可能为 nil（只测队列逻辑）
	send   chan []byte     // 有界发送队列；只有写 goroutine 消费，不会被 close
	done   chan struct{}   // 关闭信号，stop() 时 close
	once   sync.Once
	subs   map[string]struct{} // 已订阅的频道，受 hub.mu 保护
}

func (h *Hub) newClient(ws *websocket.Conn, userID uint64) *client {
	return &client{
		hub:    h,
		userID: userID,
		ws:     ws,
		send:   make(chan []byte, h.sendQueueSize),
		done:   make(chan struct{}),
		subs:   make(map[string]struct{}),
	}
}

// stop 通知写 goroutine 退出（写 goroutine 会尽力发一个 close 帧再关闭连接），可重复调用。
func (c *client) stop() {
	c.once.Do(func() { close(c.done) })
}

// kill 立即断开：先发停止信号，再直接关底层连接，让阻塞中的读写马上返回。
// 用于慢连接踢除、读循环退出这类不需要体面告别的场景。
func (c *client) kill() {
	c.stop()
	if c.ws != nil {
		_ = c.ws.Close()
	}
}

// enqueue 非阻塞地把消息放进发送队列；队列满或连接已关闭返回 false。
func (c *client) enqueue(b []byte) bool {
	select {
	case <-c.done:
		return false
	default:
	}
	select {
	case c.send <- b:
		return true
	default:
		return false
	}
}

// enqueueOrKick 入队，队列满时断开该慢连接（客户端重连后会通过对账接口补齐状态）。
func (c *client) enqueueOrKick(b []byte) {
	if c.enqueue(b) {
		return
	}
	select {
	case <-c.done: // 已经关了，无需再踢
		return
	default:
	}
	logger.Warn("WebSocket 发送队列已满，断开慢连接", zap.Uint64("user_id", c.userID))
	c.kill()
}

// register 把连接加入 Hub 并自动订阅 user 频道；Hub 已关闭时返回 false。
func (h *Hub) register(c *client) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	h.wg.Add(1)
	h.clients[c] = struct{}{}
	h.subscribeLocked(c, UserChannel(c.userID))
	return true
}

// unregister 把连接从所有频道移除。
func (h *Hub) unregister(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, c)
	for ch := range c.subs {
		if set := h.channels[ch]; set != nil {
			delete(set, c)
			if len(set) == 0 {
				delete(h.channels, ch)
			}
		}
	}
	c.subs = make(map[string]struct{})
}

func (h *Hub) subscribeLocked(c *client, channel string) {
	set := h.channels[channel]
	if set == nil {
		set = make(map[*client]struct{})
		h.channels[channel] = set
	}
	set[c] = struct{}{}
	c.subs[channel] = struct{}{}
}

// Publish 把消息投递给订阅了该频道的所有连接。
// 全程不阻塞：发送队列满的连接直接被踢，没有订阅者时立即返回。
func (h *Hub) Publish(_ context.Context, channel string, msg Message) {
	if msg.Channel == "" {
		msg.Channel = channel
	}
	b, err := json.Marshal(msg)
	if err != nil {
		logger.Warn("WebSocket 消息序列化失败，已丢弃", zap.String("channel", channel), zap.Error(err))
		return
	}

	// 先在读锁下拷贝订阅者，再在锁外入队/踢除，避免 kill 里的关连接操作占着锁
	h.mu.RLock()
	set := h.channels[channel]
	if len(set) == 0 {
		h.mu.RUnlock()
		return
	}
	targets := make([]*client, 0, len(set))
	for c := range set {
		targets = append(targets, c)
	}
	h.mu.RUnlock()

	for _, c := range targets {
		c.enqueueOrKick(b)
	}
}

// Serve 接管一条已完成 Upgrade 的连接，直到连接断开才返回（返回时读写 goroutine 都已退出）。
// 调用方（handler）之后不能再对这条连接做任何 HTTP 写入。
func (h *Hub) Serve(ws *websocket.Conn, userID uint64) {
	c := h.newClient(ws, userID)

	// 1. 先把 hello 放进队列再注册：保证 hello 一定是客户端收到的第一条消息，
	//    不会被注册后立刻发布的 task.updated 抢在前面
	hello, _ := json.Marshal(Message{
		Type: TypeHello,
		Data: map[string]any{"server_time": time.Now().Format(time.RFC3339)},
	})
	c.send <- hello

	// 2. 注册；Hub 已关闭（正在优雅退出）时直接拒绝这条连接
	if !h.register(c) {
		_ = ws.Close()
		return
	}
	defer h.wg.Done()
	logger.Debug("WebSocket 连接建立", zap.Uint64("user_id", userID))

	// 3. 写 goroutine 独立运行；当前 goroutine 跑读循环，读循环退出即连接结束
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		c.writeLoop()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	c.readLoop(ctx)
	cancel()

	// 4. 收尾：先断开连接并等写 goroutine 退出，再注销，确保不泄漏 goroutine
	c.kill()
	<-writerDone
	h.unregister(c)
	logger.Debug("WebSocket 连接关闭", zap.Uint64("user_id", userID))
}

// writeLoop 是唯一写连接的 goroutine：消费发送队列，并按间隔发 ping。
func (c *client) writeLoop() {
	ticker := time.NewTicker(c.hub.pingInterval)
	defer ticker.Stop()
	defer c.ws.Close()

	for {
		select {
		case b := <-c.send:
			_ = c.ws.SetWriteDeadline(time.Now().Add(c.hub.writeWait))
			if err := c.ws.WriteMessage(websocket.TextMessage, b); err != nil {
				c.kill()
				return
			}
		case <-ticker.C:
			_ = c.ws.SetWriteDeadline(time.Now().Add(c.hub.writeWait))
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.kill()
				return
			}
		case <-c.done:
			// 优雅关闭：尽力告诉客户端“服务端正在关闭”，失败也无所谓
			_ = c.ws.WriteControl(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutting down"),
				time.Now().Add(closeFrameWriteWait))
			return
		}
	}
}

// clientMessage 是客户端 → 服务端的消息。
type clientMessage struct {
	Type    string `json:"type"`
	Channel string `json:"channel"`
}

// readLoop 读客户端消息：处理 ping / subscribe / unsubscribe，同时负责读超时（心跳）。
func (c *client) readLoop(ctx context.Context) {
	h := c.hub
	c.ws.SetReadLimit(h.readLimit)
	_ = c.ws.SetReadDeadline(time.Now().Add(h.pongWait))
	// 浏览器收到 ping 会自动回 pong；收到 pong 就把读截止时间往后推
	c.ws.SetPongHandler(func(string) error {
		return c.ws.SetReadDeadline(time.Now().Add(h.pongWait))
	})

	for {
		mt, data, err := c.ws.ReadMessage()
		if err != nil {
			return // 超时、对端关闭、消息超限都会走到这里
		}
		// 收到任何消息都说明连接是活的
		_ = c.ws.SetReadDeadline(time.Now().Add(h.pongWait))
		if mt != websocket.TextMessage {
			continue
		}
		c.handleClientMessage(ctx, data)
	}
}

// handleClientMessage 处理一条客户端文本消息。
func (c *client) handleClientMessage(ctx context.Context, data []byte) {
	var m clientMessage
	if err := json.Unmarshal(data, &m); err != nil {
		c.reply(Message{Type: TypeError, Data: errData(errCodeBadRequest, "消息不是合法的 JSON")})
		return
	}
	switch m.Type {
	case TypePing:
		c.enqueueOrKick(pongBytes)
	case TypeSubscribe:
		c.handleSubscribe(ctx, m.Channel)
	case TypeUnsubscribe:
		c.handleUnsubscribe(m.Channel)
	default:
		c.reply(Message{Type: TypeError, Data: errData(errCodeBadRequest, "不支持的消息类型")})
	}
}

// handleSubscribe 处理订阅请求：必须通过 Authorizer 才能订阅（默认拒绝）。
func (c *client) handleSubscribe(ctx context.Context, channel string) {
	if !validChannel(channel) {
		c.reply(Message{Type: TypeError, Channel: channel, Data: errData(errCodeBadRequest, "频道名不合法")})
		return
	}
	h := c.hub

	// 自己的 user 频道建连时已自动订阅，重复订阅直接确认，不需要授权
	// 其他 user:{别人} 频道走 Authorizer，默认会被拒绝，防止窃听别人的任务推送
	if channel != UserChannel(c.userID) && !h.authorizer(ctx, c.userID, channel) {
		c.reply(Message{Type: TypeError, Channel: channel, Data: errData(errCodeForbidden, "无权订阅该频道")})
		return
	}

	h.mu.Lock()
	if _, ok := c.subs[channel]; !ok {
		if len(c.subs) >= maxSubsPerConnection {
			h.mu.Unlock()
			c.reply(Message{Type: TypeError, Channel: channel, Data: errData(errCodeTooManyChannel, "订阅的频道过多")})
			return
		}
		// 连接已被注销（正在关闭）时不能再加订阅，否则会在 channels 里留下悬挂引用
		if _, alive := h.clients[c]; !alive {
			h.mu.Unlock()
			return
		}
		h.subscribeLocked(c, channel)
	}
	h.mu.Unlock()
	c.reply(Message{Type: TypeSubscribed, Channel: channel})
}

// handleUnsubscribe 取消订阅。自己的 user 频道不允许取消，否则会收不到任务推送。
func (c *client) handleUnsubscribe(channel string) {
	if !validChannel(channel) {
		c.reply(Message{Type: TypeError, Channel: channel, Data: errData(errCodeBadRequest, "频道名不合法")})
		return
	}
	if channel == UserChannel(c.userID) {
		c.reply(Message{Type: TypeError, Channel: channel, Data: errData(errCodeForbidden, "不能取消自己的用户频道")})
		return
	}
	h := c.hub
	h.mu.Lock()
	if _, ok := c.subs[channel]; ok {
		delete(c.subs, channel)
		if set := h.channels[channel]; set != nil {
			delete(set, c)
			if len(set) == 0 {
				delete(h.channels, channel)
			}
		}
	}
	h.mu.Unlock()
	c.reply(Message{Type: TypeUnsubscribed, Channel: channel})
}

// reply 给当前连接回一条控制消息。
func (c *client) reply(m Message) {
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	c.enqueueOrKick(b)
}

func errData(code, msg string) map[string]string {
	return map[string]string{"code": code, "msg": msg}
}

// validChannel 校验客户端传来的频道名：非空、长度受限、合法 UTF-8。
func validChannel(ch string) bool {
	return ch != "" && len(ch) <= maxChannelNameLen && utf8.ValidString(ch)
}

// ConnCount 返回当前连接总数。
func (h *Hub) ConnCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// UserConnCount 返回某用户当前的连接数（多标签页）。
func (h *Hub) UserConnCount(userID uint64) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.channels[UserChannel(userID)])
}

// SubscriberCount 返回某频道当前的订阅连接数。
func (h *Hub) SubscriberCount(channel string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.channels[channel])
}

// DisconnectUser 立即断开某用户的全部连接（多标签页都断），返回被断开的连接数；用户没有连接时返回 0。
// 用于封禁账号：已建立的连接不会再做鉴权，必须主动踢掉，否则被封用户还能继续收到推送。
// 先在读锁内收集连接，释放锁后再 kill：kill 会关底层连接，读循环随之退出并走 unregister（要写锁），不能在持锁时调用。
// 被踢的连接由 Serve 的收尾流程完成注销；客户端重连会在鉴权（ticket 签发走 RequireActive）处被拒绝。
func (h *Hub) DisconnectUser(userID uint64) int {
	h.mu.RLock()
	set := h.channels[UserChannel(userID)]
	targets := make([]*client, 0, len(set))
	for c := range set {
		targets = append(targets, c)
	}
	h.mu.RUnlock()

	for _, c := range targets {
		c.kill()
	}
	return len(targets)
}

// Shutdown 优雅关闭：拒绝新连接，通知所有连接关闭，并等待它们的 goroutine 全部退出。
// ctx 超时后会强制断开剩余连接并返回 ctx.Err()。可重复调用。
func (h *Hub) Shutdown(ctx context.Context) error {
	h.mu.Lock()
	h.closed = true
	all := make([]*client, 0, len(h.clients))
	for c := range h.clients {
		all = append(all, c)
	}
	h.mu.Unlock()

	for _, c := range all {
		c.stop()
	}

	// 写 goroutine 收到 stop 后会关闭底层连接，读循环随之返回
	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		for _, c := range all {
			c.kill()
		}
		return ctx.Err()
	}
}

// Close 是带默认超时（5s）的 Shutdown，适合在 defer 或退出流程里直接调用。
func (h *Hub) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), defaultCloseTimeout)
	defer cancel()
	if err := h.Shutdown(ctx); err != nil {
		logger.Warn("WebSocket Hub 关闭超时，已强制断开剩余连接", zap.Error(err))
	}
}
