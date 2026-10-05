package ws_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	. "video-canvas/internal/pkg/ws"

	"github.com/gorilla/websocket"
)

// hubTestServer 起一个测试用 HTTP 服务：GET /ws?uid=N 直接升级并交给 Hub（省掉 ticket 环节）。
func hubTestServer(t *testing.T, h *Hub) *httptest.Server {
	t.Helper()
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid, _ := strconv.ParseUint(r.URL.Query().Get("uid"), 10, 64)
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		h.Serve(ws, uid)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// dialHub 以 uid 身份连上测试服务，并读掉第一条 hello 消息。
func dialHub(t *testing.T, srv *httptest.Server, uid uint64) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?uid=" + strconv.FormatUint(uid, 10)
	ws, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("建立 WebSocket 连接失败：%v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	m := readMsg(t, ws, 2*time.Second)
	if m.Type != TypeHello {
		t.Fatalf("第一条消息应为 hello，实际：%+v", m)
	}
	return ws
}

// inMsg 是测试里解析服务端消息用的结构。
type inMsg struct {
	Type    string          `json:"type"`
	Channel string          `json:"channel"`
	Data    json.RawMessage `json:"data"`
}

func readMsg(t *testing.T, ws *websocket.Conn, timeout time.Duration) inMsg {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(timeout))
	_, b, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("读取消息失败：%v", err)
	}
	var m inMsg
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("消息不是合法 JSON：%s", b)
	}
	return m
}

// expectNoMsg 断言在 d 时间内没有收到任何消息。
func expectNoMsg(t *testing.T, ws *websocket.Conn, d time.Duration) {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(d))
	_, b, err := ws.ReadMessage()
	if err == nil {
		t.Fatalf("不应收到消息，实际收到：%s", b)
	}
	if ne, ok := err.(interface{ Timeout() bool }); !ok || !ne.Timeout() {
		t.Fatalf("期望读超时，实际错误：%v", err)
	}
	// 读超时后 gorilla 的连接状态已损坏，之后不能再读；调用方不要在 expectNoMsg 后继续读
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待超时：%s", what)
}

func TestHub_Publish_同一用户多连接都能收到(t *testing.T) {
	h := NewHub()
	srv := hubTestServer(t, h)
	a := dialHub(t, srv, 1)
	b := dialHub(t, srv, 1)
	waitFor(t, "用户 1 有 2 个连接", func() bool { return h.UserConnCount(1) == 2 })

	h.Publish(context.Background(), UserChannel(1), Message{Type: TypeTaskUpdated, Data: map[string]any{"id": 7}})

	for name, ws := range map[string]*websocket.Conn{"a": a, "b": b} {
		m := readMsg(t, ws, 2*time.Second)
		if m.Type != TypeTaskUpdated || m.Channel != "user:1" {
			t.Fatalf("连接 %s 收到的消息不对：%+v", name, m)
		}
		if !strings.Contains(string(m.Data), `"id":7`) {
			t.Fatalf("连接 %s 的 data 不对：%s", name, m.Data)
		}
	}
}

func TestHub_Publish_只投递给订阅者(t *testing.T) {
	h := NewHub()
	srv := hubTestServer(t, h)
	u1 := dialHub(t, srv, 1)
	u2 := dialHub(t, srv, 2)
	waitFor(t, "两个用户都已注册", func() bool { return h.ConnCount() == 2 })

	h.Publish(context.Background(), UserChannel(1), Message{Type: TypeTaskUpdated, Data: 1})

	if m := readMsg(t, u1, 2*time.Second); m.Type != TypeTaskUpdated {
		t.Fatalf("用户 1 应收到消息：%+v", m)
	}
	expectNoMsg(t, u2, 200*time.Millisecond)
}

func TestHub_Publish_无人订阅的频道不阻塞(t *testing.T) {
	h := NewHub()
	done := make(chan struct{})
	go func() {
		h.Publish(context.Background(), "canvas:999", Message{Type: TypeTaskUpdated})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish 到无人订阅的频道阻塞了")
	}
}

func TestHub_Ping回Pong(t *testing.T) {
	h := NewHub()
	srv := hubTestServer(t, h)
	ws := dialHub(t, srv, 1)

	if err := ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"ping"}`)); err != nil {
		t.Fatal(err)
	}
	m := readMsg(t, ws, 2*time.Second)
	if m.Type != TypePong {
		t.Fatalf("期望 pong，实际：%+v", m)
	}
}

func TestHub_订阅授权(t *testing.T) {
	// allowed 只放行 canvas:34，且只对用户 1
	allow := func(_ context.Context, uid uint64, ch string) bool { return uid == 1 && ch == "canvas:34" }

	tests := []struct {
		name     string
		hubOpts  []Option
		uid      uint64
		send     string
		wantType string
		wantCode string // error 消息的 data.code
		wantSubs int    // 之后 canvas:34 的订阅数
	}{
		{"默认拒绝手动订阅", nil, 1, `{"type":"subscribe","channel":"canvas:34"}`, TypeError, "forbidden", 0},
		{"授权函数放行", []Option{WithAuthorizer(allow)}, 1, `{"type":"subscribe","channel":"canvas:34"}`, TypeSubscribed, "", 1},
		{"授权函数拒绝其他用户", []Option{WithAuthorizer(allow)}, 2, `{"type":"subscribe","channel":"canvas:34"}`, TypeError, "forbidden", 0},
		{"授权函数拒绝其他频道", []Option{WithAuthorizer(allow)}, 1, `{"type":"subscribe","channel":"canvas:35"}`, TypeError, "forbidden", 0},
		{"不能订阅别人的用户频道", []Option{WithAuthorizer(allow)}, 1, `{"type":"subscribe","channel":"user:2"}`, TypeError, "forbidden", 0},
		{"订阅自己的用户频道无需授权", nil, 1, `{"type":"subscribe","channel":"user:1"}`, TypeSubscribed, "", 0},
		{"空频道名", nil, 1, `{"type":"subscribe","channel":""}`, TypeError, "bad_request", 0},
		{"超长频道名", nil, 1, `{"type":"subscribe","channel":"` + strings.Repeat("a", 200) + `"}`, TypeError, "bad_request", 0},
		{"非 JSON", nil, 1, `not json`, TypeError, "bad_request", 0},
		{"未知消息类型", nil, 1, `{"type":"whatever"}`, TypeError, "bad_request", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHub(tt.hubOpts...)
			srv := hubTestServer(t, h)
			ws := dialHub(t, srv, tt.uid)
			if err := ws.WriteMessage(websocket.TextMessage, []byte(tt.send)); err != nil {
				t.Fatal(err)
			}
			m := readMsg(t, ws, 2*time.Second)
			if m.Type != tt.wantType {
				t.Fatalf("期望消息类型 %s，实际：%+v", tt.wantType, m)
			}
			if tt.wantCode != "" {
				var d map[string]string
				_ = json.Unmarshal(m.Data, &d)
				if d["code"] != tt.wantCode {
					t.Fatalf("期望错误码 %s，实际：%s", tt.wantCode, m.Data)
				}
			}
			if got := h.SubscriberCount("canvas:34"); got != tt.wantSubs {
				t.Fatalf("canvas:34 订阅数期望 %d，实际 %d", tt.wantSubs, got)
			}
		})
	}
}

func TestHub_订阅后能收到画布频道消息_取消后收不到(t *testing.T) {
	h := NewHub(WithAuthorizer(func(context.Context, uint64, string) bool { return true }))
	srv := hubTestServer(t, h)
	ws := dialHub(t, srv, 1)

	_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"subscribe","channel":"canvas:34"}`))
	if m := readMsg(t, ws, 2*time.Second); m.Type != TypeSubscribed || m.Channel != "canvas:34" {
		t.Fatalf("订阅应成功：%+v", m)
	}
	h.Publish(context.Background(), CanvasChannel(34), Message{Type: "canvas.event", Data: "x"})
	if m := readMsg(t, ws, 2*time.Second); m.Type != "canvas.event" {
		t.Fatalf("应收到画布频道消息：%+v", m)
	}

	_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"unsubscribe","channel":"canvas:34"}`))
	if m := readMsg(t, ws, 2*time.Second); m.Type != TypeUnsubscribed {
		t.Fatalf("取消订阅应成功：%+v", m)
	}
	if n := h.SubscriberCount("canvas:34"); n != 0 {
		t.Fatalf("取消订阅后订阅数应为 0，实际 %d", n)
	}

	// 不能取消自己的用户频道
	_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"unsubscribe","channel":"user:1"}`))
	if m := readMsg(t, ws, 2*time.Second); m.Type != TypeError {
		t.Fatalf("取消用户频道应被拒绝：%+v", m)
	}
	if n := h.UserConnCount(1); n != 1 {
		t.Fatalf("用户频道订阅应保留，实际 %d", n)
	}
}

func TestHub_单连接订阅数有上限(t *testing.T) {
	h := NewHub(WithAuthorizer(func(context.Context, uint64, string) bool { return true }))
	srv := hubTestServer(t, h)
	ws := dialHub(t, srv, 1)

	var last inMsg
	// 已自动订阅 1 个 user 频道，再订 MaxSubsPerConnection 个必然有拒绝
	for i := 0; i < MaxSubsPerConnection; i++ {
		_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"subscribe","channel":"canvas:`+strconv.Itoa(i)+`"}`))
		last = readMsg(t, ws, 2*time.Second)
	}
	if last.Type != TypeError {
		t.Fatalf("超过订阅上限后应返回错误：%+v", last)
	}
}

// 队列满时应断开慢连接：这里不启动写 goroutine，让队列一直堆积，最直接地锁定“满了就踢”的行为。
func TestHub_队列满踢掉慢连接(t *testing.T) {
	h := NewHub(WithSendQueueSize(2))
	slow := h.NewClient(nil, 1)
	fast := h.NewClient(nil, 1)
	if !h.Register(slow) || !h.Register(fast) {
		t.Fatal("注册失败")
	}
	t.Cleanup(func() { h.WaitGroup().Add(-2) }) // register 里 Add 过，测试里没有 Serve 来 Done

	for i := 0; i < 2; i++ {
		h.Publish(context.Background(), UserChannel(1), Message{Type: TypeTaskUpdated, Data: i})
	}
	select {
	case <-slow.Done():
		t.Fatal("队列未满时不应踢连接")
	default:
	}
	// fast 一直有人消费
	for fast.Pending() > 0 {
		fast.Drain()
	}

	h.Publish(context.Background(), UserChannel(1), Message{Type: TypeTaskUpdated, Data: 3}) // slow 已满
	select {
	case <-slow.Done():
	default:
		t.Fatal("队列满的连接应被踢掉")
	}
	select {
	case <-fast.Done():
		t.Fatal("队列有空位的连接不应被踢")
	default:
	}
}

// 端到端：客户端只连不读，服务端持续推大消息，最终这条连接会被服务端断开，且 Hub 里不再有它。
func TestHub_慢客户端被断开并注销(t *testing.T) {
	h := NewHub(WithSendQueueSize(4), WithWriteWait(300*time.Millisecond))
	srv := hubTestServer(t, h)
	slow := dialHub(t, srv, 1)
	waitFor(t, "连接已注册", func() bool { return h.ConnCount() == 1 })

	payload := strings.Repeat("x", 32*1024)
	deadline := time.Now().Add(5 * time.Second)
	for h.ConnCount() > 0 && time.Now().Before(deadline) {
		h.Publish(context.Background(), UserChannel(1), Message{Type: TypeTaskUpdated, Data: payload})
		time.Sleep(time.Millisecond)
	}
	if n := h.ConnCount(); n != 0 {
		t.Fatalf("慢连接应被断开，Hub 里还剩 %d 个连接", n)
	}
	if n := h.UserConnCount(1); n != 0 {
		t.Fatalf("慢连接应从频道注销，还剩 %d", n)
	}
	// 客户端此时继续读，最终会读到连接错误（服务端已关闭）
	_ = slow.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		if _, _, err := slow.ReadMessage(); err != nil {
			break
		}
	}
}

func TestHub_读消息超过上限会断开(t *testing.T) {
	h := NewHub(WithReadLimit(64))
	srv := hubTestServer(t, h)
	ws := dialHub(t, srv, 1)

	_ = ws.WriteMessage(websocket.TextMessage, []byte(strings.Repeat("a", 1024)))
	waitFor(t, "超限连接被断开", func() bool { return h.ConnCount() == 0 })
}

func TestHub_客户端不回应pong会被断开(t *testing.T) {
	// 心跳缩短：每 50ms ping，200ms 内没有任何回应就断开
	h := NewHub(WithHeartbeat(50*time.Millisecond, 200*time.Millisecond))
	srv := hubTestServer(t, h)
	ws := dialHub(t, srv, 1)
	// 不读连接：gorilla 客户端只有在读取时才会处理 ping 并自动回 pong，所以这里等同于“死连接”
	_ = ws
	waitFor(t, "死连接被断开", func() bool { return h.ConnCount() == 0 })
}

func TestHub_客户端正常读取则心跳保活(t *testing.T) {
	h := NewHub(WithHeartbeat(50*time.Millisecond, 200*time.Millisecond))
	srv := hubTestServer(t, h)
	ws := dialHub(t, srv, 1)

	// 后台持续读，gorilla 会自动回 pong；跨过 pongWait 若干倍后连接仍应存在
	readErr := make(chan error, 1)
	go func() {
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				readErr <- err
				return
			}
		}
	}()
	time.Sleep(800 * time.Millisecond)
	if n := h.ConnCount(); n != 1 {
		t.Fatalf("正常回 pong 的连接不应被断开，Hub 连接数 %d", n)
	}
	select {
	case err := <-readErr:
		t.Fatalf("客户端连接不应断开：%v", err)
	default:
	}
}

func TestHub_客户端断开后注销且无goroutine泄漏(t *testing.T) {
	runtime.GC()
	base := runtime.NumGoroutine()

	h := NewHub()
	srv := hubTestServer(t, h)
	conns := make([]*websocket.Conn, 0, 5)
	for i := 0; i < 5; i++ {
		conns = append(conns, dialHub(t, srv, uint64(i%2+1)))
	}
	waitFor(t, "5 个连接已注册", func() bool { return h.ConnCount() == 5 })

	for _, c := range conns {
		_ = c.Close()
	}
	waitFor(t, "连接全部注销", func() bool { return h.ConnCount() == 0 })
	if n := h.SubscriberCount(UserChannel(1)) + h.SubscriberCount(UserChannel(2)); n != 0 {
		t.Fatalf("频道里不应残留订阅，实际 %d", n)
	}

	// Hub 自己的 goroutine（每连接的读写）必须全部退出：Shutdown 等 WaitGroup，能返回就说明没泄漏
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown 未能在超时内等到所有 goroutine 退出：%v", err)
	}

	// 再关掉测试服务，整体 goroutine 数应回到基线附近（留一点余量给 runtime / http 连接池的收尾）
	srv.Close()
	waitFor(t, "goroutine 数回到基线", func() bool { return runtime.NumGoroutine() <= base+3 })
}

func TestHub_Shutdown关闭所有连接并拒绝新连接(t *testing.T) {
	h := NewHub()
	srv := hubTestServer(t, h)
	ws := dialHub(t, srv, 1)
	waitFor(t, "连接已注册", func() bool { return h.ConnCount() == 1 })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown 失败：%v", err)
	}
	if n := h.ConnCount(); n != 0 {
		t.Fatalf("Shutdown 后应没有连接，实际 %d", n)
	}

	// 客户端会收到 close 帧（GoingAway）
	_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := ws.ReadMessage()
	if !websocket.IsCloseError(err, websocket.CloseGoingAway) {
		t.Fatalf("期望收到 GoingAway 关闭帧，实际：%v", err)
	}

	// 关闭后新连接会被拒绝（Serve 里直接关掉连接）
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?uid=1"
	nws, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err == nil {
		defer nws.Close()
		_ = nws.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, _, err := nws.ReadMessage(); err == nil {
			t.Fatal("Hub 关闭后不应再向新连接发送消息")
		}
	}

	// 重复 Shutdown / Close 是安全的
	if err := h.Shutdown(ctx); err != nil {
		t.Fatalf("重复 Shutdown 应成功：%v", err)
	}
	h.Close()
}

func TestHub_并发Publish与连接进出(t *testing.T) {
	// 主要给 -race 用：并发发布、订阅、断开不应触发数据竞争或死锁
	h := NewHub(WithAuthorizer(func(context.Context, uint64, string) bool { return true }))
	srv := hubTestServer(t, h)

	stop := make(chan struct{})
	pubDone := make(chan struct{})
	go func() {
		defer close(pubDone)
		for {
			select {
			case <-stop:
				return
			default:
				h.Publish(context.Background(), UserChannel(1), Message{Type: TypeTaskUpdated, Data: 1})
				h.Publish(context.Background(), CanvasChannel(1), Message{Type: TypeTaskUpdated, Data: 1})
			}
		}
	}()

	for i := 0; i < 10; i++ {
		url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?uid=1"
		ws, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"subscribe","channel":"canvas:1"}`))
		_ = ws.Close()
	}
	close(stop)
	<-pubDone
	waitFor(t, "连接全部注销", func() bool { return h.ConnCount() == 0 })
}

func TestHub_DisconnectUser只断开目标用户的全部连接(t *testing.T) {
	h := NewHub()
	srv := hubTestServer(t, h)
	a1 := dialHub(t, srv, 7)
	a2 := dialHub(t, srv, 7) // 同一用户的第二个标签页
	b := dialHub(t, srv, 8)
	waitFor(t, "三条连接注册", func() bool { return h.UserConnCount(7) == 2 && h.UserConnCount(8) == 1 })

	if n := h.DisconnectUser(7); n != 2 {
		t.Fatalf("应断开 2 条连接，实际 %d", n)
	}
	// 目标用户的连接被服务端关闭：读到错误，并最终从 Hub 注销
	for _, c := range []*websocket.Conn{a1, a2} {
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				break
			}
		}
	}
	waitFor(t, "目标用户连接注销", func() bool { return h.UserConnCount(7) == 0 })

	// 其他用户不受影响，仍能收到推送
	h.Publish(context.Background(), UserChannel(8), Message{Type: TypeTaskUpdated, Channel: UserChannel(8)})
	if m := readMsg(t, b, 2*time.Second); m.Type != TypeTaskUpdated {
		t.Fatalf("用户 8 应仍能收到推送：%+v", m)
	}
	// 没有连接的用户返回 0，不报错
	if n := h.DisconnectUser(999); n != 0 {
		t.Fatalf("无连接应返回 0，实际 %d", n)
	}
}

func TestHub_DisconnectUser与连接进出并发安全(t *testing.T) {
	h := NewHub()
	srv := hubTestServer(t, h)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 50 {
			h.DisconnectUser(5)
			time.Sleep(time.Millisecond)
		}
	}()
	for range 10 {
		url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?uid=5"
		if c, _, err := websocket.DefaultDialer.Dial(url, nil); err == nil {
			_ = c.Close()
		}
	}
	<-done
	h.DisconnectUser(5)
	waitFor(t, "全部注销", func() bool { return h.UserConnCount(5) == 0 })
}
