package ws_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	. "video-canvas/internal/pkg/ws"
)

func TestCanvasCollaborationNoticeRechecksAuthorization(t *testing.T) {
	var authorized atomic.Bool
	authorized.Store(true)
	const canvas = "7ca75e"
	channel := CanvasCollaborationChannel(canvas)
	hub := NewHub(WithAuthorizer(func(_ context.Context, userID uint64, requested string) bool {
		return userID == 8 && requested == channel && authorized.Load()
	}))
	t.Cleanup(hub.Close)
	conn := dialHub(t, hubTestServer(t, hub), 8)
	if err := conn.WriteJSON(Message{Type: TypeSubscribe, Channel: channel}); err != nil {
		t.Fatal(err)
	}
	if got := readMsg(t, conn, time.Second); got.Type != TypeSubscribed {
		t.Fatal(got)
	}
	bus := NewCanvasCollaborationBus(hub, nil)
	bus.Notice(t.Context(), CanvasCollaborationNotice{CanvasID: canvas, Epoch: 1, Revision: 2})
	if got := readMsg(t, conn, time.Second); got.Type != TypeCanvasCollaboration || got.Channel != channel {
		t.Fatal(got)
	}
	bus.Notice(t.Context(), CanvasCollaborationNotice{CanvasID: canvas, Epoch: 1, TransferPending: true})
	warning := readMsg(t, conn, time.Second)
	var data CanvasCollaborationNotice
	if warning.Type != TypeCanvasCollaboration || json.Unmarshal(warning.Data, &data) != nil || !data.TransferPending || data.Epoch != 1 {
		t.Fatalf("在线成员应先收到当前协作代数的转移提醒: %+v", warning)
	}
	authorized.Store(false)
	bus.Notice(t.Context(), CanvasCollaborationNotice{CanvasID: canvas, Epoch: 1, Revision: 3})
	if got := readMsg(t, conn, time.Second); got.Type != TypeUnsubscribed || got.Channel != channel {
		t.Fatalf("撤权后应退出房间且不交付新版本: %+v", got)
	}
}

func TestCanvasCollaborationSlowReceiver(t *testing.T) {
	const canvas = "slow-receiver"
	room := CanvasCollaborationChannel(canvas)
	hub := NewHub(WithSendQueueSize(2), WithAuthorizer(func(_ context.Context, _ uint64, channel string) bool {
		return channel == room
	}))
	slow, fast := hub.NewClient(nil, 1), hub.NewClient(nil, 2)
	if !hub.Register(slow) || !hub.Register(fast) {
		t.Fatal("连接未注册")
	}
	t.Cleanup(func() {
		hub.WaitGroup().Add(-2)
		hub.Close()
	})
	for _, connection := range []interface {
		Subscribe(context.Context, string)
		Drain()
	}{slow, fast} {
		connection.Subscribe(t.Context(), room)
		connection.Drain()
	}
	bus := NewCanvasCollaborationBus(hub, nil)
	bus.Notice(t.Context(), CanvasCollaborationNotice{CanvasID: canvas, Epoch: 1, Revision: 2})
	fast.Drain()
	if stats := bus.RoomStats(); stats.Rooms != 1 || stats.Connections != 2 || stats.MaxQueued != 1 {
		t.Fatalf("协作房间应报告两个连接及慢连接的积压: %+v", stats)
	}
	for range 4 {
		bus.Notice(t.Context(), CanvasCollaborationNotice{CanvasID: canvas, Epoch: 1,
			Progress: &CanvasGenerationProgress{NodeID: "image", RunRevision: 1, Percent: 30}})
		if fast.Pending() != 1 || slow.Pending() != 2 {
			t.Fatalf("慢连接的临时进度应合并到一个队列位置: %d, %d", fast.Pending(), slow.Pending())
		}
		fast.Drain()
	}
	for revision := uint64(3); revision < 5; revision++ {
		bus.Notice(t.Context(), CanvasCollaborationNotice{CanvasID: canvas, Epoch: 1, Revision: revision})
		if fast.Pending() != 1 {
			t.Fatal("慢连接不应影响其他连接处理确认版本")
		}
		fast.Drain()
	}
	select {
	case <-slow.Done():
	default:
		t.Fatal("确认版本堆满队列后应断开慢连接，以便重连追赶")
	}
	select {
	case <-fast.Done():
		t.Fatal("正常连接应继续保持连接")
	default:
	}
	if stats := bus.RoomStats(); stats.Rooms != 1 || stats.Connections != 1 || stats.MaxQueued != 0 {
		t.Fatalf("被断开的连接不应再计入房间: %+v", stats)
	}
}

func TestCanvasCollaborationQueuedMessagesAfterRejoin(t *testing.T) {
	const canvas = "queued-revocation"
	room := CanvasCollaborationChannel(canvas)
	var authorized atomic.Bool
	authorized.Store(true)
	hub := NewHub(WithAuthorizer(func(_ context.Context, _ uint64, channel string) bool {
		return channel == room && authorized.Load()
	}))
	connection := hub.NewClient(nil, 1)
	if !hub.Register(connection) {
		t.Fatal("连接未注册")
	}
	t.Cleanup(func() {
		hub.WaitGroup().Done()
		hub.Close()
	})
	connection.Subscribe(t.Context(), room)
	connection.Drain()
	bus := NewCanvasCollaborationBus(hub, nil)
	bus.Notice(t.Context(), CanvasCollaborationNotice{CanvasID: canvas, Epoch: 1, Revision: 2})
	bus.Notice(t.Context(), CanvasCollaborationNotice{CanvasID: canvas, Epoch: 1,
		Progress: &CanvasGenerationProgress{NodeID: "previous-node", RunRevision: 1, Percent: 35}})
	hub.Publish(t.Context(), UserChannel(1), Message{Type: TypeTaskUpdated})
	if connection.Pending() != 3 {
		t.Fatalf("撤权前应有三个待发送消息，实际 %d", connection.Pending())
	}
	authorized.Store(false)
	bus.Notice(t.Context(), CanvasCollaborationNotice{CanvasID: canvas, Epoch: 1})
	if connection.Pending() != 2 {
		t.Fatal("撤权后应立即清除旧房间消息，只保留退订确认和用户频道消息")
	}
	authorized.Store(true)
	connection.Subscribe(t.Context(), room)
	bus.Notice(t.Context(), CanvasCollaborationNotice{CanvasID: canvas, Epoch: 1, Revision: 3})
	for _, want := range []string{TypeTaskUpdated, TypeUnsubscribed, TypeSubscribed, TypeCanvasCollaboration} {
		if got := connection.DrainDeliverable(); got != want {
			t.Fatalf("预期写出 %s，实际 %s", want, got)
		}
	}
	if connection.Pending() != 0 {
		t.Fatal("已消费全部队列消息")
	}
}

func TestCanvasCollaborationQueueByteBudget(t *testing.T) {
	const canvas = "queue-budget"
	const budget = 320
	room := CanvasCollaborationChannel(canvas)
	hub := NewHub(WithSendQueueSize(8), WithSendQueueBytes(budget), WithAuthorizer(func(_ context.Context, _ uint64, channel string) bool {
		return channel == room
	}))
	connection := hub.NewClient(nil, 1)
	if !hub.Register(connection) {
		t.Fatal("连接未注册")
	}
	t.Cleanup(func() {
		hub.WaitGroup().Done()
		hub.Close()
	})
	connection.Subscribe(t.Context(), room)
	connection.Drain()
	bus := NewCanvasCollaborationBus(hub, nil)
	bus.Notice(t.Context(), CanvasCollaborationNotice{CanvasID: canvas, Epoch: 1,
		Progress: &CanvasGenerationProgress{NodeID: "image", RunRevision: 1, Percent: 25}})
	if stats := bus.RoomStats(); stats.MaxQueued != 1 || stats.MaxQueuedBytes <= 0 || stats.MaxQueuedBytes > budget {
		t.Fatalf("应记录临时消息的字节积压: %+v", stats)
	}
	reliable := Message{Type: TypeTaskUpdated, Data: strings.Repeat("x", 190)}
	hub.Publish(t.Context(), UserChannel(1), reliable)
	if stats := bus.RoomStats(); stats.MaxQueued != 1 || stats.MaxQueuedBytes < 190 || stats.MaxQueuedBytes > budget {
		t.Fatalf("可靠消息应淘汰临时进度并遵守字节预算: %+v", stats)
	}
	if got := connection.DrainDeliverable(); got != TypeTaskUpdated {
		t.Fatalf("实际写出了 %s", got)
	}
	if stats := bus.RoomStats(); stats.MaxQueuedBytes != 0 {
		t.Fatalf("已取出的消息应释放字节预算: %+v", stats)
	}
	hub.Publish(t.Context(), UserChannel(1), reliable)
	hub.Publish(t.Context(), UserChannel(1), reliable)
	select {
	case <-connection.Done():
	default:
		t.Fatal("可靠消息超出字节预算时应断开连接")
	}
}

func TestCanvasCollaborationCoalescesProgressAndPrioritizesConfirmedVersion(t *testing.T) {
	const canvas = "latest-progress"
	room := CanvasCollaborationChannel(canvas)
	hub := NewHub(WithSendQueueSize(8), WithAuthorizer(func(_ context.Context, _ uint64, channel string) bool {
		return channel == room
	}))
	connection := hub.NewClient(nil, 1)
	if !hub.Register(connection) {
		t.Fatal("连接未注册")
	}
	t.Cleanup(func() {
		hub.WaitGroup().Done()
		hub.Close()
	})
	connection.Subscribe(t.Context(), room)
	connection.Drain()
	bus := NewCanvasCollaborationBus(hub, nil)
	for _, percent := range []int{10, 20, 50, 75, 85} {
		bus.Notice(t.Context(), CanvasCollaborationNotice{CanvasID: canvas, Epoch: 1,
			Progress: &CanvasGenerationProgress{NodeID: "image", RunRevision: 1, Percent: percent}})
	}
	bus.Notice(t.Context(), CanvasCollaborationNotice{CanvasID: canvas, Epoch: 1,
		Progress: &CanvasGenerationProgress{NodeID: "video", RunRevision: 1, Percent: 31}})
	bus.Notice(t.Context(), CanvasCollaborationNotice{CanvasID: canvas, Epoch: 1, Revision: 2})
	if got := connection.DrainDeliverable(); got != TypeCanvasCollaboration {
		t.Fatalf("确认版本应优先发出，实际为 %s", got)
	}
	for _, want := range []CanvasGenerationProgress{{NodeID: "image", RunRevision: 1, Percent: 85}, {NodeID: "video", RunRevision: 1, Percent: 31}} {
		var message struct {
			Type string
			Data CanvasCollaborationNotice
		}
		if err := json.Unmarshal(connection.DrainPayload(), &message); err != nil || message.Type != TypeCanvasProgress ||
			message.Data.Progress == nil || *message.Data.Progress != want {
			t.Fatalf("应发送对应节点的最新进度 %+v，得到 %+v (%v)", want, message.Data.Progress, err)
		}
	}
	if connection.Pending() != 0 {
		t.Fatal("同一节点不应保留过期的进度消息")
	}
}

func TestCanvasCollaborationCrossInstance(t *testing.T) {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("未设置 TEST_REDIS_ADDR")
	}
	redisClient := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = redisClient.Close() })
	const canvas = "7ca75e"
	room := CanvasCollaborationChannel(canvas)
	var editorAccess atomic.Bool
	editorAccess.Store(true)
	authorize := func(_ context.Context, user uint64, channel string) bool {
		return channel == room && (user == 8 || (user == 9 && editorAccess.Load()))
	}
	senderHub, receiverHub := NewHub(WithAuthorizer(authorize)), NewHub(WithAuthorizer(authorize))
	t.Cleanup(senderHub.Close)
	t.Cleanup(receiverHub.Close)
	senderBus := NewCanvasCollaborationBus(senderHub, redisClient, authorize).
		WithPresenceUsername(func(context.Context, uint64) string { return "小林" })
	receiverBus := NewCanvasCollaborationBus(receiverHub, redisClient, authorize)
	for _, bus := range []*CanvasCollaborationBus{senderBus, receiverBus} {
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() { defer close(done); bus.Run(ctx) }()
		t.Cleanup(func() { cancel(); <-done })
	}
	waitCanvasBusSubscribers(t, redisClient)
	sender := dialHub(t, hubTestServer(t, senderHub), 8)
	receiver := dialHub(t, hubTestServer(t, receiverHub), 9)
	for index, connection := range []interface{ WriteJSON(any) error }{sender, receiver} {
		if err := connection.WriteJSON(Message{Type: TypeSubscribe, Channel: room, Data: map[string]bool{"resumed": index == 1}}); err != nil {
			t.Fatal(err)
		}
	}
	if readMsg(t, sender, time.Second).Type != TypeSubscribed || readMsg(t, receiver, time.Second).Type != TypeSubscribed {
		t.Fatal("两个连接均应进入房间")
	}
	if stats := senderBus.RoomStats(); stats.Joins != 1 || stats.Resumed != 0 {
		t.Fatalf("首次加入应单独计数: %+v", stats)
	}
	if err := receiver.WriteJSON(Message{Type: TypeSubscribe, Channel: room, Data: map[string]bool{"resumed": true}}); err != nil {
		t.Fatal(err)
	}
	if readMsg(t, receiver, time.Second).Type != TypeSubscribed {
		t.Fatal("应该幂等确认重复订阅")
	}
	if stats := receiverBus.RoomStats(); stats.Joins != 1 || stats.Resumed != 1 {
		t.Fatalf("重连及同一连接的重复订阅应正确去重: %+v", stats)
	}
	if err := sender.WriteJSON(Message{Type: TypeCanvasPresence, Channel: room, Data: map[string]any{"epoch": 1}}); err != nil {
		t.Fatal(err)
	}
	if event := readMsg(t, receiver, time.Second); event.Type != TypeCanvasPresence || event.Channel != room ||
		!strings.Contains(string(event.Data), `"actorUsername":"小林"`) {
		t.Fatalf("临时状态未跨实例到达: %+v", event)
	}
	senderBus.Notice(t.Context(), CanvasCollaborationNotice{CanvasID: canvas, Epoch: 1, Revision: 2})
	if event := readMsg(t, receiver, time.Second); event.Type != TypeCanvasCollaboration || event.Channel != room {
		t.Fatalf("内容版本通知未跨实例到达: %+v", event)
	}
	senderBus.Notice(t.Context(), CanvasCollaborationNotice{CanvasID: canvas, Epoch: 1,
		Progress: &CanvasGenerationProgress{NodeID: "shot", RunRevision: 2, Percent: 47}})
	progress := readMsg(t, receiver, time.Second)
	if progress.Type != TypeCanvasProgress || progress.Channel != room || strings.Contains(string(progress.Data), "taskId") {
		t.Fatalf("两实例只分发公开的节点进度: %+v", progress)
	}
	editorAccess.Store(false)
	revokedAt := time.Now()
	senderBus.Notice(t.Context(), CanvasCollaborationNotice{CanvasID: canvas})
	if event := readMsg(t, receiver, time.Second); event.Type != TypeUnsubscribed || event.Channel != room {
		t.Fatalf("撤权后另一实例应取消房间订阅: %+v", event)
	}
	t.Logf("另一实例在线连接收到取消订阅耗时 %s", time.Since(revokedAt))
}

func waitCanvasBusSubscribers(t *testing.T, client *redis.Client) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	const topic = "video-canvas:collaboration:notice"
	for {
		count, err := client.PubSubNumSub(ctx, topic).Result()
		if err == nil && count[topic] >= 2 {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("协作 Redis 订阅未就绪: %v", err)
		}
	}
}

func TestCanvasPresenceScopesAndIdentifiesSender(t *testing.T) {
	const canvas = "7ca75e"
	channel := CanvasCollaborationChannel(canvas)
	hub := NewHub(WithAuthorizer(func(_ context.Context, _ uint64, requested string) bool {
		return requested == channel
	}))
	t.Cleanup(hub.Close)
	NewCanvasCollaborationBus(hub, nil, func(_ context.Context, user uint64, _ string) bool { return user == 8 }).
		WithPresenceUsername(func(_ context.Context, user uint64) string {
			if user == 8 {
				return "小林"
			}
			return ""
		})
	server := hubTestServer(t, hub)
	sender := dialHub(t, server, 8)
	receiver := dialHub(t, server, 9)
	for _, connection := range []interface{ WriteJSON(any) error }{sender, receiver} {
		if err := connection.WriteJSON(Message{Type: TypeSubscribe, Channel: channel}); err != nil {
			t.Fatal(err)
		}
	}
	if readMsg(t, sender, time.Second).Type != TypeSubscribed || readMsg(t, receiver, time.Second).Type != TypeSubscribed {
		t.Fatal("房间未订阅")
	}
	if err := receiver.WriteJSON(Message{Type: TypeCanvasPresence, Channel: channel, Data: map[string]any{"epoch": 1}}); err != nil {
		t.Fatal(err)
	}
	if err := sender.WriteJSON(Message{Type: TypeCanvasPresence, Channel: channel, Data: map[string]any{
		"epoch": 1, "cursor": map[string]int{"x": 10, "y": 20}, "actorId": "forged", "actorUsername": "forged",
		"editing": "node-first", "draft": "private draft", "view": map[string]float64{"x": 20, "y": 30, "zoom": 1.2},
	}}); err != nil {
		t.Fatal(err)
	}
	message := readMsg(t, receiver, time.Second)
	if message.Type != TypeCanvasPresence || message.Channel != channel {
		t.Fatalf("应收到该房间的临时状态: %+v", message)
	}
	var data map[string]any
	if json.Unmarshal(message.Data, &data) != nil || data["actorId"] == "forged" || data["actorId"] == nil || data["actorUsername"] != "小林" {
		t.Fatalf("临时消息的参与者由服务端确定: %+v", message)
	}
	if data["editing"] != "node-first" || data["draft"] != nil {
		t.Fatalf("编辑提示只转发节点 ID: %+v", message)
	}
	if view, ok := data["view"].(map[string]any); !ok || view["x"] != float64(20) || view["zoom"] != 1.2 {
		t.Fatalf("视角应包含世界坐标与缩放: %+v", message)
	}
}

func TestCanvasPresenceViewerMayAnnounceOnlyAttendance(t *testing.T) {
	room := CanvasCollaborationChannel("attendance")
	hub := NewHub(WithAuthorizer(func(_ context.Context, _ uint64, channel string) bool { return channel == room }))
	t.Cleanup(hub.Close)
	NewCanvasCollaborationBus(hub, nil, func(_ context.Context, _ uint64, _ string) bool { return false }).
		WithPresenceUsername(func(_ context.Context, _ uint64) string { return "小林" })
	receiver := hub.NewClient(nil, 9)
	if !hub.Register(receiver) {
		t.Fatal("接收者未注册")
	}
	t.Cleanup(hub.WaitGroup().Done)
	receiver.Subscribe(t.Context(), room)
	receiver.Drain()
	sender := dialHub(t, hubTestServer(t, hub), 8)
	if err := sender.WriteJSON(Message{Type: TypeSubscribe, Channel: room}); err != nil {
		t.Fatal(err)
	}
	if readMsg(t, sender, time.Second).Type != TypeSubscribed {
		t.Fatal("查看者未进入房间")
	}
	if err := sender.WriteJSON(Message{Type: TypeCanvasAttendance, Channel: room, Data: map[string]any{
		"epoch": 1, "actorUsername": "forged",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := sender.WriteJSON(Message{Type: TypePing}); err != nil {
		t.Fatal(err)
	}
	if readMsg(t, sender, time.Second).Type != TypePong || receiver.DrainDeliverable() != TypeCanvasAttendance {
		t.Fatal("查看者的纯在线心跳应投递给队友")
	}
}

func TestCanvasPresenceDiscardsAuthorizationStartedBeforeDowngrade(t *testing.T) {
	const canvas = "presence-downgrade"
	room := CanvasCollaborationChannel(canvas)
	hub := NewHub(WithAuthorizer(func(_ context.Context, _ uint64, channel string) bool { return channel == room }))
	t.Cleanup(hub.Close)
	receiver := hub.NewClient(nil, 9)
	if !hub.Register(receiver) {
		t.Fatal("接收者未注册")
	}
	t.Cleanup(hub.WaitGroup().Done)
	receiver.Subscribe(t.Context(), room)
	receiver.Drain()
	started, release := make(chan struct{}), make(chan struct{})
	bus := NewCanvasCollaborationBus(hub, nil, func(_ context.Context, user uint64, channel string) bool {
		close(started)
		<-release
		return user == 8 && channel == room
	})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	sender := dialHub(t, hubTestServer(t, hub), 8)
	if err := sender.WriteJSON(Message{Type: TypeSubscribe, Channel: room}); err != nil {
		t.Fatal(err)
	}
	if readMsg(t, sender, time.Second).Type != TypeSubscribed {
		t.Fatal("编辑者未进入房间")
	}
	if err := sender.WriteJSON(Message{Type: TypeCanvasPresence, Channel: room, Data: map[string]any{"epoch": 1}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("在线状态未开始鉴权")
	}
	bus.Notice(t.Context(), CanvasCollaborationNotice{CanvasID: canvas})
	if readMsg(t, sender, time.Second).Type != TypeCanvasCollaboration || receiver.DrainDeliverable() != TypeCanvasCollaboration {
		t.Fatal("连接应继续接收权限变更通知")
	}
	close(release)
	if err := sender.WriteJSON(Message{Type: TypePing}); err != nil {
		t.Fatal(err)
	}
	if readMsg(t, sender, time.Second).Type != TypePong {
		t.Fatal("未确认发送连接已处理在线状态")
	}
	if got := receiver.DrainDeliverable(); got != "" {
		t.Fatalf("权限变更前开始的鉴权不应继续分发在线状态，收到 %s", got)
	}
}
