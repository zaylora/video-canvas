package repository_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"video-canvas/internal/canvasdoc"
	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/utils"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/repository"
	"video-canvas/internal/router"
	"video-canvas/internal/service"
)

const canvasLoadDelay = 40 * time.Millisecond
const canvasLoadSecret = "canvas-collaboration-load-test-secret"

type canvasLoadClient struct {
	ctx      context.Context
	server   *httptest.Server
	token    string
	room     string
	base     string
	conn     *websocket.Conn
	revision uint64
	bytes    atomic.Int64
	labels   map[string]string
}

func TestCanvasCollaborationLoad(t *testing.T) {
	if os.Getenv("TEST_CANVAS_COLLABORATION_LOAD") != "1" || os.Getenv("TEST_REDIS_ADDR") == "" {
		t.Skip("需设置 TEST_CANVAS_COLLABORATION_LOAD=1、TEST_REDIS_ADDR 和 TEST_DATABASE_DSN")
	}
	clients, db, repo, project, owner := canvasLoadSetup(t, "long-prompt", nil)
	for _, client := range clients {
		client.join(t)
		defer client.disconnect()
		client.snapshot(t, project.CollaborationEpoch, 1000)
	}
	t.Logf("初始图谱 JSON %d 字节；首个连接的 HTTP 响应体及 WS 消息 %d 字节", len(project.PayloadJSON), clients[0].bytes.Load())
	propagation, withSampling := measureCanvasPropagation(t, clients, project.CollaborationEpoch)
	for _, scenario := range []string{"drag", "edge", "group", "prompt", "batch"} {
		stats := measureCanvasStructuralPropagation(t, db, clients, project.CollaborationEpoch, scenario)
		t.Logf("%s 提交至接收增量 p95=%s（%d 样本），单笔集群 WAL 增量 p95=%d 字节",
			scenario, canvasLoadP95(stats.delays), len(stats.delays), stats.walP95())
		if canvasLoadP95(stats.delays) > 300*time.Millisecond {
			t.Errorf("%s 协议耗时超出预算", scenario)
		}
	}
	outbound := make([]int64, len(clients))
	for index, client := range clients {
		outbound[index] = client.bytes.Load()
	}
	t.Logf("各连接累计 HTTP 响应体及 WS 消息字节（追赶前）：%v", outbound)
	catchup := measureCanvasCatchup(t, clients[9], repo, project, owner)
	t.Logf("1000 节点、10 人、模拟 80 ms RTT，提交到收到增量 p95=%s，加入采样等待后 p95=%s（各 %d 样本），100 批断线追赶 p95=%s（%d 样本）",
		canvasLoadP95(propagation), canvasLoadP95(withSampling), len(propagation), canvasLoadP95(catchup), len(catchup))
	if canvasLoadP95(propagation) > 300*time.Millisecond || canvasLoadP95(catchup) > 3*time.Second {
		t.Fatal("协作协议耗时超出预算")
	}
}

type canvasLoadStructuralStats struct {
	delays []time.Duration
	wal    []int64
}

func (stats *canvasLoadStructuralStats) walP95() int64 {
	slices.Sort(stats.wal)
	return stats.wal[(len(stats.wal)*95+99)/100-1]
}

func canvasLoadWALPosition(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var position string
	if err := db.Raw("SELECT pg_current_wal_insert_lsn()::text").Scan(&position).Error; err != nil {
		t.Fatal(err)
	}
	return position
}

func canvasLoadWALSince(t *testing.T, db *gorm.DB, position string) int64 {
	t.Helper()
	var walBytes int64
	if err := db.Raw("SELECT pg_wal_lsn_diff(pg_current_wal_insert_lsn(), ?::pg_lsn)::bigint", position).Scan(&walBytes).Error; err != nil {
		t.Fatal(err)
	}
	return walBytes
}

func measureCanvasStructuralPropagation(t *testing.T, db *gorm.DB, clients []*canvasLoadClient, epoch uint64, scenario string) canvasLoadStructuralStats {
	t.Helper()
	const rounds = 12
	stats := canvasLoadStructuralStats{delays: make([]time.Duration, 0, rounds*(len(clients)-1)), wal: make([]int64, 0, rounds)}
	for round := range rounds {
		sender := clients[round%len(clients)]
		batch := canvasLoadBatch(epoch, sender.revision, round, scenario)
		batch.Changes = canvasLoadStructuralChanges(scenario, round)
		body, err := json.Marshal(batch)
		if err != nil {
			t.Fatal(err)
		}
		walStart := canvasLoadWALPosition(t, db)
		start := time.Now()
		want := batch.BaseRevision + 1
		observed := observeCanvasLoadRevision(clients, sender, epoch, want, start)
		var accepted repository.CanvasCollaborationCommit
		if err := sender.request(http.MethodPost, sender.base+"/ops", body, &accepted); err != nil || accepted.Revision != want || len(accepted.Changes) != len(batch.Changes) {
			t.Fatalf("%s 操作未确认: %+v, %v", scenario, accepted, err)
		}
		for range clients {
			observation := <-observed
			if observation.err != nil {
				t.Fatal(observation.err)
			}
			if !observation.sender {
				stats.delays = append(stats.delays, observation.delay)
			}
		}
		stats.wal = append(stats.wal, canvasLoadWALSince(t, db, walStart))
	}
	return stats
}

type canvasLoadObservation struct {
	delay  time.Duration
	err    error
	sender bool
}

func observeCanvasLoadRevision(clients []*canvasLoadClient, sender *canvasLoadClient, epoch, want uint64, start time.Time) <-chan canvasLoadObservation {
	observed := make(chan canvasLoadObservation, len(clients))
	for _, client := range clients {
		go func() {
			err := client.readSignal(ws.TypeCanvasCollaboration)
			if err == nil {
				time.Sleep(canvasLoadDelay)
				err = client.replay(epoch, want)
			}
			observed <- canvasLoadObservation{time.Since(start), err, client == sender}
		}()
	}
	return observed
}

func canvasLoadStructuralChanges(scenario string, round int) []repository.CanvasCollaborationChange {
	id := fmt.Sprintf("node-%d", 800+round)
	changed := repository.CanvasCollaborationChange{Kind: "node", ID: id}
	switch scenario {
	case "drag":
		changed.Fields = map[string]json.RawMessage{"position": json.RawMessage(`{"x":7,"y":9}`)}
	case "edge":
		return []repository.CanvasCollaborationChange{{Kind: "edge", ID: "link-" + id, Action: "create",
			Value: fmt.Appendf(nil, `{"source":%q,"target":"node-900"}`, id)}}
	case "group":
		group := "group-" + id
		changed.Fields = map[string]json.RawMessage{"parentId": fmt.Appendf(nil, `%q`, group), "position": json.RawMessage(`{"x":10,"y":10}`)}
		return []repository.CanvasCollaborationChange{{Kind: "group", ID: group, Action: "create",
			Value: json.RawMessage(`{"position":{"x":30,"y":30},"width":300,"height":300,"data":{"label":"分组"}}`)}, changed}
	case "prompt":
		changed.Fields = map[string]json.RawMessage{"data.prompt": fmt.Appendf(nil, `%q`, strings.Repeat("多人协作镜头提示词", 300))}
	case "batch":
		changes := make([]repository.CanvasCollaborationChange, 50)
		for offset := range changes {
			changes[offset] = repository.CanvasCollaborationChange{Kind: "node", ID: fmt.Sprintf("node-%d", 100+round*len(changes)+offset),
				Fields: map[string]json.RawMessage{"position": json.RawMessage(`{"x":70,"y":90}`)}}
		}
		return changes
	}
	return []repository.CanvasCollaborationChange{changed}
}

func canvasLoadSetup(t *testing.T, layout string, sample *canvasdoc.Document) ([]*canvasLoadClient, *gorm.DB, *repository.CanvasCollaborationRepository, *model.CanvasProject, uint64) {
	t.Helper()
	return canvasLoadSetupFrom(t, layout, sample, 1, 0)
}

func canvasLoadSetupFrom(t *testing.T, layout string, sample *canvasdoc.Document, revision, epoch uint64) ([]*canvasLoadClient, *gorm.DB, *repository.CanvasCollaborationRepository, *model.CanvasProject, uint64) {
	t.Helper()
	db, repo, owner, _, _, _ := newCollaborationFixture(t)
	if err := db.AutoMigrate(&model.CanvasCollaborationComment{}); err != nil {
		t.Fatal(err)
	}
	project := &model.CanvasProject{
		BaseModel: model.BaseModel{ID: 1<<60 + rand.Uint64N(1<<60)},
		UserID:    owner, Revision: revision, CollaborationEpoch: epoch, Title: "协议容量测试", PayloadJSON: canvasLoadPayload(t, db, owner, layout, sample),
	}
	if err := db.Create(project).Error; err != nil {
		t.Fatal(err)
	}
	project, err := repo.SetActive(t.Context(), owner, project.ID, revision, true)
	if err != nil {
		t.Fatal(err)
	}
	shares := repository.NewCanvasShareRepository(db)
	room := ws.CanvasCollaborationChannel(idcodec.Encode(project.ID))
	var servers [2]*httptest.Server
	for index := range servers {
		name := fmt.Sprintf("canvas-load-%d-%d", project.ID, index)
		upstream := redis.NewClient(&redis.Options{Addr: os.Getenv("TEST_REDIS_ADDR"), ClientName: name})
		t.Cleanup(func() { _ = upstream.Close() })
		servers[index] = canvasLoadServer(t, db, repo, shares, upstream, project.ID, room)
		waitCanvasLoadRedis(t, upstream, name)
	}
	clients := make([]*canvasLoadClient, 10)
	for index := range clients {
		actor := owner
		if index > 0 {
			user := model.User{Username: fmt.Sprintf("load-member-%d", index), Email: fmt.Sprintf("load-%d@example.test", index), Password: "unused", Status: model.UserStatusActive}
			if err := db.Create(&user).Error; err != nil {
				t.Fatal(err)
			}
			actor = user.ID
			if err := shares.GrantRole(t.Context(), owner, project.ID, actor, model.CanvasRoleEditor); err != nil {
				t.Fatal(err)
			}
		}
		token, _, err := utils.GenerateToken(uint(actor), "", 1, canvasLoadSecret, "test", 1)
		if err != nil {
			t.Fatal(err)
		}
		clients[index] = &canvasLoadClient{ctx: t.Context(), server: servers[index%2], token: token, room: room,
			base: "/api/v1/canvas/" + idcodec.Encode(project.ID) + "/collaboration", revision: project.Revision}
	}
	return clients, db, repo, project, owner
}

func canvasLoadPayload(t *testing.T, db *gorm.DB, owner uint64, layout string, sample *canvasdoc.Document) []byte {
	t.Helper()
	if sample != nil {
		return restoreCanvasLoadSample(t, db, owner, sample)
	}
	var asset *model.Asset
	if layout == "media" {
		asset = seedCanvasLoadAsset(t, db, owner)
	}
	return canvasLoadGraph(t, layout, asset)
}

func seedCanvasLoadAsset(t *testing.T, db *gorm.DB, owner uint64) *model.Asset {
	t.Helper()
	if err := db.AutoMigrate(&model.Asset{}); err != nil {
		t.Fatal(err)
	}
	asset := &model.Asset{UserID: owner, Kind: "image", Source: "upload", StorageKey: "capacity/fixture.png", MimeType: "image/png"}
	if err := db.Create(asset).Error; err != nil {
		t.Fatal(err)
	}
	return asset
}

func canvasLoadGraph(t *testing.T, layout string, asset *model.Asset) []byte {
	t.Helper()
	nodes, edges := make([]map[string]any, 0, 1010), make([]map[string]any, 0)
	for index := range 1000 {
		nodes = append(nodes, canvasLoadNode(layout, index, asset))
	}
	if layout == "grouped" {
		for group := range 10 {
			nodes = append(nodes, map[string]any{
				"id": fmt.Sprintf("group-%d", group), "type": "group", "position": map[string]int{"x": group % 5 * 2600, "y": group / 5 * 2600},
				"width": 2450, "height": 2450, "data": map[string]any{"label": "分组"},
			})
		}
		for index := range 1000 {
			for _, step := range []int{1, 2, 10} {
				if index/100 == (index+step)/100 {
					edges = append(edges, map[string]any{"id": fmt.Sprintf("edge-%d-%d", index, step), "source": fmt.Sprintf("node-%d", index), "target": fmt.Sprintf("node-%d", index+step)})
				}
			}
		}
	}
	graph, err := json.Marshal(map[string]any{"nodes": nodes, "edges": edges})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("初始图谱包含 %d 个节点记录、%d 条连线，大小 %d 字节", len(nodes), len(edges), len(graph))
	return graph
}

func canvasLoadNode(layout string, index int, asset *model.Asset) map[string]any {
	data := map[string]any{"kind": "image", "prompt": "镜头输入"}
	node := map[string]any{
		"id": fmt.Sprintf("node-%d", index), "type": "canvas", "position": map[string]int{"x": index % 50 * 200, "y": index / 50 * 300}, "data": data,
	}
	switch layout {
	case "long-prompt":
		data["prompt"] = strings.Repeat("镜头输入", 100)
	case "grouped":
		node["parentId"] = fmt.Sprintf("group-%d", index/100)
		node["position"] = map[string]int{"x": index % 10 * 220, "y": index % 100 / 10 * 220}
	case "media":
		if index%2 == 0 {
			data["assetId"], data["src"], data["status"], data["mediaType"] = strconv.FormatUint(asset.ID, 10), "/files/"+asset.StorageKey, "done", "image"
		}
	}
	return node
}

func canvasLoadServer(t *testing.T, db *gorm.DB, repo *repository.CanvasCollaborationRepository, shares *repository.CanvasShareRepository, upstream *redis.Client, canvas uint64, room string) *httptest.Server {
	t.Helper()
	authorize := func(ctx context.Context, user uint64, channel string) bool {
		return channel == room && repo.CanJoin(ctx, user, canvas)
	}
	readers := func(ctx context.Context, users []uint64, channel string) map[uint64]bool {
		if channel != room {
			return nil
		}
		return repo.RoomReaders(ctx, users, canvas)
	}
	hub := ws.NewHub(ws.WithAuthorizer(authorize), ws.WithRoomReaders(readers))
	t.Cleanup(hub.Close)
	canEdit := func(ctx context.Context, user uint64, channel string) bool {
		return channel == room && repo.CanEditRoom(ctx, user, canvas)
	}
	bus := ws.NewCanvasCollaborationBus(hub, upstream, canEdit).WithPresenceUsername(func(ctx context.Context, id uint64) string {
		var user model.User
		if err := db.WithContext(ctx).Select("username").First(&user, id).Error; err != nil {
			return ""
		}
		return user.Username
	})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); bus.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	engine := router.New("test", canvasLoadSecret, router.Handlers{
		Health:              &handler.HealthHandler{},
		CanvasProject:       handler.NewCanvasProjectHandler(service.NewCanvasProjectService(repository.NewCanvasProjectRepository(db))),
		Me:                  handler.NewMeHandler(service.NewMeService(service.MeDeps{Repo: repository.NewUserRepository(db)})),
		WS:                  handler.NewWSHandler(ws.NewTicketStore(upstream), hub, nil),
		CanvasCollaboration: handler.NewCanvasCollaborationHandler(service.NewCanvasCollaborationService(repo, shares, nil, true).WithRoomBus(bus).WithUsernameDirectory(repository.NewUserRepository(db))),
		UserState: func(ctx context.Context, id uint64) (*middleware.UserState, error) {
			if !repo.CanJoin(ctx, id, canvas) {
				return nil, repository.ErrNotFound
			}
			return &middleware.UserState{Status: model.UserStatusActive, Role: "user", TokenVersion: 1}, nil
		},
	})
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	return server
}

func waitCanvasLoadRedis(t *testing.T, upstream *redis.Client, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		clients, err := upstream.ClientList(ctx).Result()
		if err == nil {
			for line := range strings.SplitSeq(clients, "\n") {
				fields := strings.Fields(line)
				if slices.Contains(fields, "name="+name) && slices.Contains(fields, "sub=2") {
					return
				}
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("实例 %s 的通知和在线状态订阅未就绪", name)
		case <-ticker.C:
		}
	}
}

func (client *canvasLoadClient) request(method, path string, body []byte, result any) error {
	time.Sleep(canvasLoadDelay)
	ctx, cancel := context.WithTimeout(client.ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, client.server.URL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+client.token)
	req.Header.Set("Content-Type", "application/json")
	response, err := client.server.Client().Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	time.Sleep(canvasLoadDelay)
	if response.StatusCode != http.StatusOK {
		details, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return fmt.Errorf("响应 %d: %s", response.StatusCode, details)
	}
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	client.bytes.Add(int64(len(payload)))
	return json.Unmarshal(payload, &struct {
		Data any `json:"data"`
	}{Data: result})
}

func (client *canvasLoadClient) join(t *testing.T) {
	t.Helper()
	var ticket handler.WSTicketResp
	if err := client.request(http.MethodPost, "/api/v1/ws/ticket", nil, &ticket); err != nil {
		t.Fatal(err)
	}
	address := "ws" + strings.TrimPrefix(client.server.URL, "http") + "/api/v1/ws?ticket=" + ticket.Ticket
	conn, _, err := websocket.DefaultDialer.DialContext(client.ctx, address, nil)
	if err != nil {
		t.Fatal(err)
	}
	client.conn = conn
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.WriteJSON(ws.Message{Type: ws.TypeSubscribe, Channel: client.room}); err != nil {
		t.Fatal(err)
	}
	if err := client.readSignal(ws.TypeHello); err != nil {
		t.Fatal(err)
	}
	if err := client.readSignal(ws.TypeSubscribed); err != nil {
		t.Fatal(err)
	}
}

func (client *canvasLoadClient) disconnect() {
	if client.conn != nil {
		_ = client.conn.Close()
	}
}

func (client *canvasLoadClient) readSignal(kind string) error {
	if err := client.conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return err
	}
	_, payload, err := client.conn.ReadMessage()
	if err != nil {
		return err
	}
	client.bytes.Add(int64(len(payload)))
	var event ws.Message
	if err := json.Unmarshal(payload, &event); err != nil {
		return err
	}
	if event.Type != kind || (kind != ws.TypeHello && event.Channel != client.room) {
		return fmt.Errorf("预期收到 %s，得到 %+v", kind, event)
	}
	return nil
}

func (client *canvasLoadClient) snapshot(t *testing.T, epoch uint64, nodes int) []byte {
	t.Helper()
	var snapshot service.CanvasCollaborationSnapshot
	if err := client.request(http.MethodGet, client.base, nil, &snapshot); err != nil {
		t.Fatal(err)
	}
	var graph struct{ Nodes []json.RawMessage }
	err := json.Unmarshal(snapshot.Canvas.PayloadJSON, &graph)
	if err != nil || snapshot.Epoch != epoch || snapshot.Canvas.Revision != client.revision || len(graph.Nodes) != nodes {
		t.Fatal("未收到完整的初始图谱")
	}
	return snapshot.Canvas.PayloadJSON
}

func (client *canvasLoadClient) replay(epoch, want uint64) error {
	for client.revision < want {
		if err := client.replayPage(epoch, want); err != nil {
			return err
		}
	}
	return nil
}

func (client *canvasLoadClient) replayPage(epoch, want uint64) error {
	start := client.revision
	var result repository.CanvasCollaborationReplay
	if err := client.request(http.MethodGet, fmt.Sprintf("%s/events?epoch=%d&after=%d", client.base, epoch, start), nil, &result); err != nil {
		return err
	}
	if result.SnapshotRequired || result.Cursor <= start || result.Cursor > want || uint64(len(result.Events)) != result.Cursor-start {
		return fmt.Errorf("补发未追平: start=%d cursor=%d want=%d events=%d", start, result.Cursor, want, len(result.Events))
	}
	for _, event := range result.Events {
		if event.Epoch != epoch || event.Revision != client.revision+1 || len(event.Changes) == 0 {
			return fmt.Errorf("缺少连续变更: %d", event.Revision)
		}
		if client.labels != nil {
			if err := recordCanvasLoadLabels(client.labels, event.Changes); err != nil {
				return err
			}
		}
		client.revision = event.Revision
	}
	return nil
}

func measureCanvasPropagation(t *testing.T, clients []*canvasLoadClient, epoch uint64) ([]time.Duration, []time.Duration) {
	t.Helper()
	type result struct {
		observedAt time.Time
		err        error
		sender     bool
	}
	samples := make([]time.Duration, 0, 180)
	withSampling := make([]time.Duration, 0, 180)
	for round := range 20 {
		sender := clients[round%len(clients)]
		want := sender.revision + 1
		start := time.Now()
		received := make(chan result, len(clients))
		for _, client := range clients {
			go func() {
				err := client.readSignal(ws.TypeCanvasCollaboration)
				if err == nil {
					time.Sleep(canvasLoadDelay)
					err = client.replay(epoch, want)
				}
				received <- result{time.Now(), err, client == sender}
			}()
		}
		time.Sleep(100 * time.Millisecond)
		batch := canvasLoadBatch(epoch, want-1, round, "live")
		body, err := json.Marshal(batch)
		if err != nil {
			t.Fatal(err)
		}
		var accepted repository.CanvasCollaborationCommit
		sent := time.Now()
		if err := sender.request(http.MethodPost, sender.base+"/ops", body, &accepted); err != nil || accepted.Revision != want {
			t.Fatalf("写入未确认: %+v, %v", accepted, err)
		}
		for range clients {
			observed := <-received
			if observed.err != nil {
				t.Fatal(observed.err)
			}
			if !observed.sender {
				samples = append(samples, observed.observedAt.Sub(sent))
				withSampling = append(withSampling, observed.observedAt.Sub(start))
			}
		}
	}
	return samples, withSampling
}

func measureCanvasCatchup(t *testing.T, client *canvasLoadClient, repo *repository.CanvasCollaborationRepository, project *model.CanvasProject, owner uint64) []time.Duration {
	t.Helper()
	samples := make([]time.Duration, 0, 5)
	for round := range 5 {
		client.disconnect()
		startRevision := client.revision
		for offset := range 100 {
			batch := canvasLoadBatch(project.CollaborationEpoch, startRevision+uint64(offset), round*100+offset, "offline")
			confirmed, err := repo.Commit(t.Context(), owner, project.ID, batch)
			if err != nil || confirmed.Revision != batch.BaseRevision+1 {
				t.Fatalf("断线期间的提交失败: %+v %v", confirmed, err)
			}
		}
		start := time.Now()
		client.join(t)
		if err := client.replay(project.CollaborationEpoch, startRevision+100); err != nil {
			t.Fatal(err)
		}
		samples = append(samples, time.Since(start))
	}
	return samples
}

func canvasLoadBatch(epoch, revision uint64, index int, mode string) repository.CanvasCollaborationBatch {
	change := repository.CanvasCollaborationChange{Kind: "node", ID: fmt.Sprintf("node-%d", index), Fields: map[string]json.RawMessage{
		"data.label": json.RawMessage(fmt.Sprintf(`"%s-%d"`, mode, index)),
	}}
	return repository.CanvasCollaborationBatch{Epoch: epoch, BaseRevision: revision, OpID: fmt.Sprintf("%s-%d", mode, index), Changes: []repository.CanvasCollaborationChange{change}}
}

func canvasLoadP95(samples []time.Duration) time.Duration {
	slices.Sort(samples)
	return samples[(len(samples)*95+99)/100-1]
}
