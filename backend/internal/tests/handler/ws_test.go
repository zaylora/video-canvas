package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	. "video-canvas/internal/handler"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"video-canvas/internal/middleware"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/ws"
)

// wsTestEngine 组装和真实路由一致的结构：ticket 在“登录”分组下（用测试中间件模拟 JWT），ws 不鉴权。
func wsTestEngine(h *WSHandler, userID uint) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/api/v1")
	v1.GET("/ws", h.Connect)
	auth := v1.Group("", func(c *gin.Context) {
		c.Set(middleware.CtxUserIDKey, userID)
		c.Next()
	})
	auth.POST("/ws/ticket", h.IssueTicket)
	return r
}

type wsBody struct {
	Code int `json:"code"`
	Data struct {
		Ticket    string `json:"ticket"`
		ExpiresIn int    `json:"expires_in"`
	} `json:"data"`
}

func wsPostTicket(t *testing.T, base string) wsBody {
	t.Helper()
	resp, err := http.Post(base+"/api/v1/ws/ticket", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var b wsBody
	if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
		t.Fatal(err)
	}
	return b
}

func wsURL(base, ticket string) string {
	return "ws" + strings.TrimPrefix(base, "http") + "/api/v1/ws?ticket=" + ticket
}

func TestWSHandler_IssueTicket(t *testing.T) {
	store := ws.NewMemoryTicketStore()
	h := NewWSHandler(store, ws.NewHub(), nil)
	r := wsTestEngine(h, 7)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/ws/ticket", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d：%s", w.Code, w.Body.String())
	}
	var b wsBody
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	if b.Code != 0 || b.Data.Ticket == "" || b.Data.ExpiresIn != 30 {
		t.Fatalf("响应不符合预期：%s", w.Body.String())
	}
	// ticket 应绑定到当前登录用户
	uid, ok := store.Consume(context.Background(), b.Data.Ticket)
	if !ok || uid != 7 {
		t.Fatalf("ticket 应绑定用户 7，实际 uid=%d ok=%v", uid, ok)
	}
}

func TestWSHandler_Connect_ticket无效(t *testing.T) {
	store := ws.NewMemoryTicketStore()
	h := NewWSHandler(store, ws.NewHub(), nil)
	r := wsTestEngine(h, 1)

	used, _ := store.Issue(context.Background(), 1)
	_, _ = store.Consume(context.Background(), used)

	tests := []struct {
		name string
		url  string
	}{
		{"没有 ticket 参数", "/api/v1/ws"},
		{"ticket 为空", "/api/v1/ws?ticket="},
		{"ticket 不存在", "/api/v1/ws?ticket=nope"},
		{"ticket 已被使用过", "/api/v1/ws?ticket=" + used},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tt.url, nil))
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("期望 401，实际 %d：%s", w.Code, w.Body.String())
			}
			var b wsBody
			_ = json.Unmarshal(w.Body.Bytes(), &b)
			if b.Code != errcode.ErrWSTicketInvalid.Code {
				t.Fatalf("期望错误码 %d，实际 %d", errcode.ErrWSTicketInvalid.Code, b.Code)
			}
		})
	}
}

func TestWSHandler_Connect_成功建连且ticket一次性(t *testing.T) {
	hub := ws.NewHub()
	defer hub.Close()
	h := NewWSHandler(ws.NewMemoryTicketStore(), hub, nil)
	srv := httptest.NewServer(wsTestEngine(h, 5))
	defer srv.Close()

	tk := wsPostTicket(t, srv.URL).Data.Ticket
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, tk), nil)
	if err != nil {
		t.Fatalf("建连失败：%v", err)
	}
	defer conn.Close()

	// 首条消息是 hello，且连接已按 ticket 里的用户订阅了 user:5
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, b, err := conn.ReadMessage()
	if err != nil || !strings.Contains(string(b), `"type":"hello"`) || !strings.Contains(string(b), "server_time") {
		t.Fatalf("应先收到 hello，实际 %s err=%v", b, err)
	}
	hub.Publish(context.Background(), ws.UserChannel(5), ws.Message{Type: ws.TypeTaskUpdated, Data: "ok"})
	_, b, err = conn.ReadMessage()
	if err != nil || !strings.Contains(string(b), `"channel":"user:5"`) {
		t.Fatalf("应收到 user:5 的推送，实际 %s err=%v", b, err)
	}

	// 同一个 ticket 再连一次必须失败
	_, resp, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, tk), nil)
	if err == nil {
		t.Fatal("ticket 已被消费，再次建连应失败")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("期望 401，实际 %+v", resp)
	}
}

func TestWSHandler_Connect_Origin校验(t *testing.T) {
	hub := ws.NewHub()
	defer hub.Close()
	h := NewWSHandler(ws.NewMemoryTicketStore(), hub, []string{"http://localhost:5173"})
	srv := httptest.NewServer(wsTestEngine(h, 1))
	defer srv.Close()

	tests := []struct {
		name   string
		origin string // 空表示不带 Origin 头
		wantOK bool
	}{
		{"非浏览器客户端不带 Origin", "", true},
		{"白名单内的 Origin", "http://localhost:5173", true},
		{"陌生站点被拒绝", "http://evil.example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tk := wsPostTicket(t, srv.URL).Data.Ticket
			hdr := http.Header{}
			if tt.origin != "" {
				hdr.Set("Origin", tt.origin)
			}
			conn, resp, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, tk), hdr)
			if tt.wantOK {
				if err != nil {
					t.Fatalf("应允许建连：%v", err)
				}
				_ = conn.Close()
				return
			}
			if err == nil {
				_ = conn.Close()
				t.Fatal("应拒绝建连")
			}
			if resp == nil || resp.StatusCode != http.StatusForbidden {
				t.Fatalf("期望 403，实际 %+v", resp)
			}
		})
	}
}

func TestNewOriginChecker(t *testing.T) {
	allowed := []string{"http://localhost:5173", "https://App.Example.com/"}

	tests := []struct {
		name    string
		allowed []string
		origin  string
		host    string
		want    bool
	}{
		{"Origin 为空放行（非浏览器）", allowed, "", "api.example.com", true},
		{"同源放行", nil, "https://api.example.com", "api.example.com", true},
		{"同源带端口放行", nil, "http://127.0.0.1:8080", "127.0.0.1:8080", true},
		{"同源忽略大小写", nil, "https://API.example.com", "api.example.com", true},
		{"白名单精确命中", allowed, "http://localhost:5173", "api.example.com", true},
		{"白名单忽略大小写与末尾斜杠", allowed, "https://app.example.com", "api.example.com", true},
		{"白名单为空且不同源拒绝", nil, "http://localhost:5173", "api.example.com", false},
		{"端口不同不算命中", allowed, "http://localhost:3000", "api.example.com", false},
		{"协议不同不算命中", allowed, "https://localhost:5173", "api.example.com", false},
		{"前缀相似的域名拒绝", allowed, "http://localhost:5173.evil.com", "api.example.com", false},
		{"子域名拒绝", allowed, "https://evil.app.example.com", "api.example.com", false},
		{"Origin 是 null 拒绝", allowed, "null", "api.example.com", false},
		{"Origin 无法解析拒绝", allowed, "://bad", "api.example.com", false},
		{"通配符不被支持", []string{"*"}, "http://evil.example.com", "api.example.com", false},
		{"白名单里的空字符串被忽略", []string{"", "  "}, "http://evil.example.com", "api.example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/v1/ws", nil)
			r.Host = tt.host
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			if got := NewOriginChecker(tt.allowed)(r); got != tt.want {
				t.Fatalf("期望 %v，实际 %v", tt.want, got)
			}
		})
	}
}

// 验收条件 7：WebSocket 连接可以保持 > 10 分钟不断（已绕开 60s WriteTimeout）。
//
// 生产环境 http.Server 配了 ReadTimeout=10s / WriteTimeout=60s，普通 HTTP 响应超过这个时间就会被切断。
// gorilla/websocket 的 Upgrade 会劫持底层 TCP 连接并清掉 http.Server 设置的读写 deadline，
// 所以 WebSocket 不受这两个超时影响。真等 60 秒太慢，这里把两个超时缩到 1 秒，
// 然后让连接保持 3.5 秒（超过超时值的 3 倍），期间持续双向收发：
// 如果 deadline 没被清掉，连接会在 1 秒左右被切断，收发就会失败，测试随之失败。
// 这个测试锁定的是“Upgrade 之后不受 http.Server 超时影响”这个行为，
// 以后如果有人换 WebSocket 库、或在 Upgrade 之后又给连接设了 deadline，这里会报警。
func TestWSHandler_Connect_连接保持时间超过Server读写超时(t *testing.T) {
	hub := ws.NewHub()
	defer hub.Close()
	h := NewWSHandler(ws.NewMemoryTicketStore(), hub, nil)

	const serverTimeout = time.Second
	srv := httptest.NewUnstartedServer(wsTestEngine(h, 1))
	srv.Config.ReadTimeout = serverTimeout
	srv.Config.WriteTimeout = serverTimeout
	srv.Start()
	defer srv.Close()

	tk := wsPostTicket(t, srv.URL).Data.Ticket
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, tk), nil)
	if err != nil {
		t.Fatalf("建连失败：%v", err)
	}
	defer conn.Close()

	begin := time.Now()
	readType := func() string {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, b, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("连接被切断或读取失败（已保持 %v）：%v", time.Since(begin), err)
		}
		var m struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(b, &m)
		return m.Type
	}

	if got := readType(); got != ws.TypeHello {
		t.Fatalf("应先收到 hello，实际 %s", got)
	}

	rounds := 0
	for time.Since(begin) < 3*serverTimeout+500*time.Millisecond {
		// 客户端 → 服务端：ping，期望收到 pong（服务端读没有被 ReadTimeout 切断）
		if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"ping"}`)); err != nil {
			t.Fatalf("客户端写失败（已保持 %v）：%v", time.Since(begin), err)
		}
		// 服务端 → 客户端：推一条任务消息（服务端写没有被 WriteTimeout 切断）
		hub.Publish(context.Background(), ws.UserChannel(1), ws.Message{Type: ws.TypeTaskUpdated, Data: rounds})

		got := map[string]bool{}
		for i := 0; i < 2; i++ {
			got[readType()] = true
		}
		if !got[ws.TypePong] || !got[ws.TypeTaskUpdated] {
			t.Fatalf("第 %d 轮期望同时收到 pong 和 task.updated，实际 %v", rounds, got)
		}
		rounds++
		time.Sleep(250 * time.Millisecond)
	}
	if elapsed := time.Since(begin); elapsed < 3*serverTimeout {
		t.Fatalf("测试保持时间不足：%v", elapsed)
	}
	if rounds < 5 {
		t.Fatalf("收发轮数过少：%d", rounds)
	}
	if n := hub.UserConnCount(1); n != 1 {
		t.Fatalf("连接应仍在 Hub 中，实际 %d", n)
	}
}
