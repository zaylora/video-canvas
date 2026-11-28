package repository_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/repository"
)

func TestRealCLICollaborationConcurrent(t *testing.T) {
	if os.Getenv("TEST_CLI_COLLABORATION_LOAD") != "1" || os.Getenv("TEST_REDIS_ADDR") == "" {
		t.Skip("需先构建 cli/dist/cli.js 并设置 TEST_CLI_COLLABORATION_LOAD=1、TEST_REDIS_ADDR 和 TEST_DATABASE_DSN")
	}
	cli, err := filepath.Abs("../../../../cli/dist/cli.js")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cli); err != nil {
		t.Fatalf("先运行 cd cli && npm run build: %v", err)
	}
	for _, writers := range []int{4, 8} {
		t.Run(strconv.Itoa(writers), func(t *testing.T) { verifyRealCLIWriters(t, cli, writers) })
	}
	t.Run("presence", func(t *testing.T) { verifyRealCLIPresence(t, cli) })
	t.Run("comments", func(t *testing.T) { verifyRealCLIComments(t, cli) })
	t.Run("rename", func(t *testing.T) { verifyRealCLIRename(t, cli) })
	t.Run("progress", func(t *testing.T) { verifyRealCLIProgress(t, cli, false) })
	t.Run("progress-large-version", func(t *testing.T) { verifyRealCLIProgress(t, cli, true) })
	t.Run("same-storyboard", func(t *testing.T) { verifyRealCLIStoryboard(t, cli) })
	t.Run("lost-response-failover", func(t *testing.T) { verifyRealCLIFailover(t, cli) })
}

func verifyRealCLIFailover(t *testing.T, bin string) {
	t.Helper()
	clients, _, repo, project, owner := canvasLoadSetup(t, "loose", nil)
	primary, secondary := clients[0].server, clients[1].server
	proxy, promoted, posts := newCanvasFailoverProxy(t, primary.URL, secondary.URL)
	client := newRealCLI(t, bin, &canvasLoadClient{server: proxy, token: clients[0].token})
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	code := idcodec.Encode(project.ID)
	planPath := prepareRealCLIPlan(ctx, t, client, code, 0)
	data, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	var plan struct{ OpID string }
	if err := json.Unmarshal(data, &plan); err != nil || plan.OpID == "" {
		t.Fatalf("计划应有操作编号: %v", err)
	}
	output, err := client.command(ctx, "canvas", "apply", code, "--plan", planPath, "--json").CombinedOutput()
	if err == nil || !bytes.Contains(output, []byte(`"reason":"result_unknown"`)) || posts.Load() != 1 {
		t.Fatalf("响应丢失应报告未知结果: %v %s", err, output)
	}
	primary.Close()
	promoted.Store(true)
	output, err = client.command(ctx, "canvas", "resume", code, "--run", plan.OpID, "--json").CombinedOutput()
	if err != nil || !bytes.Contains(output, []byte(`"result":"applied"`)) || posts.Load() != 1 {
		t.Fatalf("切换实例后应读取已确认的收据且不重新提交: %v %s", err, output)
	}
	replay, err := repo.Replay(ctx, owner, project.ID, project.CollaborationEpoch, project.Revision, 10)
	if err != nil || len(replay.Events) != 1 || replay.Cursor != project.Revision+1 || replay.Events[0].OpID != plan.OpID {
		t.Fatalf("恢复后应只存在一次持久化提交: %+v %v", replay, err)
	}
}

func newCanvasFailoverProxy(t *testing.T, primary, secondary string) (*httptest.Server, *atomic.Bool, *atomic.Int64) {
	t.Helper()
	servers := make([]*httputil.ReverseProxy, 0, 2)
	for _, backend := range []string{primary, secondary} {
		base, err := url.Parse(backend)
		if err != nil {
			t.Fatal(err)
		}
		servers = append(servers, httputil.NewSingleHostReverseProxy(base))
	}
	var promoted atomic.Bool
	var posts atomic.Int64
	servers[0].ModifyResponse = func(resp *http.Response) error {
		if resp.StatusCode == http.StatusOK && resp.Request.Method == http.MethodPost && strings.HasSuffix(resp.Request.URL.Path, "/collaboration/ops") {
			return fmt.Errorf("测试注入的已提交响应丢失")
		}
		return nil
	}
	servers[0].ErrorHandler = func(writer http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(writer, "上游响应中断", http.StatusBadGateway)
	}
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/collaboration/ops") {
			posts.Add(1)
		}
		if promoted.Load() {
			servers[1].ServeHTTP(writer, request)
			return
		}
		servers[0].ServeHTTP(writer, request)
	}))
	t.Cleanup(proxy.Close)
	return proxy, &promoted, &posts
}

func verifyRealCLIRename(t *testing.T, bin string) {
	t.Helper()
	clients, _, _, project, _ := canvasLoadSetup(t, "loose", nil)
	observer := clients[1]
	observer.join(t)
	defer observer.disconnect()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	code := idcodec.Encode(project.ID)
	cli := newRealCLI(t, bin, clients[0])
	output, err := cli.command(ctx, "canvas", "rename", code, "--title", "确认后的分镜", "--json").CombinedOutput()
	if err != nil || !bytes.Contains(output, []byte(`"title":"确认后的分镜"`)) {
		t.Fatalf("CLI 重命名失败: %v %s", err, output)
	}
	if err := observer.readSignal("canvas.collaboration"); err != nil {
		t.Fatalf("跨实例未收到重命名通知: %v", err)
	}
	var replay repository.CanvasCollaborationReplay
	path := fmt.Sprintf("%s/events?epoch=%d&after=%d", observer.base, project.CollaborationEpoch, project.Revision)
	if err := observer.request(http.MethodGet, path, nil, &replay); err != nil || replay.Title != "确认后的分镜" || replay.Cursor != project.Revision+1 {
		t.Fatalf("跨实例未回放重命名: %+v %v", replay, err)
	}
}

func verifyRealCLIStoryboard(t *testing.T, bin string) {
	t.Helper()
	clients, _, repo, project, owner := canvasLoadSetup(t, "long-prompt", nil)
	const writers = 8
	shots := make([]map[string]string, writers)
	for index := range shots {
		shots[index] = map[string]string{"id": fmt.Sprintf("shot-%d", index), "dialogue": "待编辑"}
	}
	value, err := json.Marshal(map[string]any{"position": map[string]int{"x": 0, "y": 0}, "data": map[string]any{
		"kind": "storyboard", "label": "共享分镜表", "storyboard": map[string]any{"shots": shots},
	}})
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := repo.Commit(t.Context(), owner, project.ID, repository.CanvasCollaborationBatch{
		OpID: "create-load-board", Epoch: project.CollaborationEpoch, BaseRevision: project.Revision,
		Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: "board", Action: "create", Value: value}},
	})
	if err != nil || len(baseline.Changes) != 1 {
		t.Fatalf("创建分镜表: %+v %v", baseline, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	code := idcodec.Encode(project.ID)
	observed := startRealCLIWatch(ctx, t, newRealCLI(t, bin, clients[9]), code, baseline.Revision+writers)
	launchRealCLIStoryboardWriters(ctx, t, bin, clients[:writers], code, baseline)
	if err := <-observed; err != nil {
		t.Fatal(err)
	}
	confirmed, _, err := repo.Snapshot(ctx, owner, project.ID)
	if err != nil || confirmed.Revision != baseline.Revision+writers {
		t.Fatalf("分镜表应收敛到预期版本: %v", err)
	}
	for index := range writers {
		if !bytes.Contains(confirmed.PayloadJSON, fmt.Appendf(nil, `"dialogue":"并行对白 %d"`, index)) {
			t.Errorf("缺少第 %d 个进程的对白", index)
		}
	}
}

func launchRealCLIStoryboardWriters(ctx context.Context, t *testing.T, bin string, clients []*canvasLoadClient, code string, baseline *repository.CanvasCollaborationCommit) {
	t.Helper()
	results := make(chan error, len(clients))
	for index, actor := range clients {
		client := newRealCLI(t, bin, actor)
		file := filepath.Join(client.home, "dialogue.json")
		if err := os.WriteFile(file, fmt.Appendf(nil, `{"dialogue":"并行对白 %d"}`, index), 0o600); err != nil {
			t.Fatal(err)
		}
		go func() {
			output, err := client.command(ctx, "canvas", "storyboard", "shot", "set", code, "board", fmt.Sprintf("shot-%d", index),
				"--epoch", strconv.FormatUint(baseline.Epoch, 10), "--revision", strconv.FormatUint(baseline.Revision, 10), "--input", file, "--json").CombinedOutput()
			if err != nil {
				err = fmt.Errorf("CLI %d 修改对白: %w %s", index, err, output)
			}
			results <- err
		}()
	}
	for range clients {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
}

func verifyRealCLIProgress(t *testing.T, bin string, largeVersion bool) {
	t.Helper()
	clients, db, _, project, _ := canvasLoadSetup(t, "long-prompt", nil)
	if largeVersion {
		project.CollaborationEpoch = 9_007_199_254_740_993
		project.Revision = project.CollaborationEpoch + 2
		if err := db.Model(project).Select("CollaborationEpoch", "Revision").Updates(project).Error; err != nil {
			t.Fatal(err)
		}
	}
	status := fmt.Sprintf(`{"status":"running","generationRevision":%d}`, project.Revision)
	if err := db.Model(&model.CanvasCollaborationRecord{}).Where("canvas_id = ? AND kind = 'node' AND record_id = 'node-1'", project.ID).
		Update("body", gorm.Expr("jsonb_set(body, '{data}', body->'data' || CAST(? AS jsonb))", status)).Error; err != nil {
		t.Fatal(err)
	}
	observer := clients[1]
	observer.join(t)
	defer observer.disconnect()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	reader := newRealCLI(t, bin, clients[2])
	cmd := reader.command(ctx, "canvas", "watch", idcodec.Encode(project.ID), "--jsonl", "--interval", "60")
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	if !scanner.Scan() || !bytes.Contains(scanner.Bytes(), []byte(`"type":"snapshot"`)) {
		t.Fatalf("CLI 未输出初始快照: %v", scanner.Err())
	}
	if err := observer.readSignal(ws.TypeCanvasAttendance); err != nil {
		t.Fatalf("未收到 CLI 在线心跳: %v", err)
	}
	publishTestProgress(ctx, t, project)
	for scanner.Scan() {
		line := scanner.Bytes()
		if bytes.Contains(line, []byte(`"type":"generation-progress"`)) && bytes.Contains(line, []byte(`"nodeId":"node-1"`)) && bytes.Contains(line, []byte(`"percent":63`)) &&
			bytes.Contains(line, fmt.Appendf(nil, `"runRevision":"%d"`, project.Revision)) {
			return
		}
	}
	t.Fatalf("CLI 未输出公开任务进度: %v", scanner.Err())
}

func publishTestProgress(ctx context.Context, t *testing.T, project *model.CanvasProject) {
	t.Helper()
	upstream := redis.NewClient(&redis.Options{Addr: os.Getenv("TEST_REDIS_ADDR")})
	defer upstream.Close()
	notice := ws.CanvasCollaborationNotice{CanvasID: idcodec.Encode(project.ID), Epoch: project.CollaborationEpoch,
		Progress: &ws.CanvasGenerationProgress{NodeID: "node-1", RunRevision: project.Revision, Percent: 63}}
	encoded, err := json.Marshal(notice)
	if err != nil {
		t.Fatal(err)
	}
	if err := upstream.Publish(ctx, "video-canvas:collaboration:notice", encoded).Err(); err != nil {
		t.Fatal(err)
	}
}

func verifyRealCLIComments(t *testing.T, bin string) {
	t.Helper()
	clients, _, _, project, _ := canvasLoadSetup(t, "long-prompt", nil)
	writer := newRealCLI(t, bin, clients[1])
	reader := newRealCLI(t, bin, clients[2])
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	code := idcodec.Encode(project.ID)
	epoch := strconv.FormatUint(project.CollaborationEpoch, 10)
	verifyCommentRoomNotice(t, clients, epoch)
	if output, err := writer.command(ctx, "canvas", "comment", "add", code, "--node", "node-1", "--text", "确认继续制作", "--json").CombinedOutput(); err != nil {
		t.Fatalf("CLI 写入评论: %v %s", err, output)
	}
	output, err := reader.command(ctx, "canvas", "watch", code, "--once", "--jsonl", "--epoch", epoch, "--comments-since", "0").CombinedOutput()
	if err != nil {
		t.Fatalf("CLI 评论回放失败: %v %s", err, output)
	}
	if !bytes.Contains(output, []byte(`"type":"comment"`)) || !bytes.Contains(output, []byte(`"type":"comment-cursor"`)) ||
		!bytes.Contains(output, []byte(`"authorUsername":"load-member-1"`)) || !bytes.Contains(output, []byte(`"body":"确认继续制作"`)) {
		t.Fatal("另一实例的 CLI 应能回放具名评论及评论游标")
	}
}

func verifyCommentRoomNotice(t *testing.T, clients []*canvasLoadClient, epoch string) {
	t.Helper()
	observer := clients[2]
	observer.join(t)
	defer observer.disconnect()
	body := fmt.Appendf(nil, `{"epoch":%s,"nodeId":"node-1","opId":"comment-notice","body":"正文只通过回放读取"}`, epoch)
	var response json.RawMessage
	if err := clients[1].request(http.MethodPost, clients[1].base+"/comments", body, &response); err != nil {
		t.Fatal(err)
	}
	if err := observer.readSignal("canvas.comment_changed"); err != nil {
		t.Fatalf("跨实例评论唤醒失败: %v", err)
	}
}

func verifyRealCLIPresence(t *testing.T, bin string) {
	t.Helper()
	clients, _, _, project, _ := canvasLoadSetup(t, "long-prompt", nil)
	sender := clients[1]
	sender.join(t)
	defer sender.disconnect()
	cli := newRealCLI(t, bin, clients[2])
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	code := idcodec.Encode(project.ID)
	result := make(chan error, 1)
	go func() {
		result <- verifyCLIFollow(ctx, cli, code)
	}()
	if err := sender.readSignal(ws.TypeCanvasAttendance); err != nil {
		t.Fatalf("未收到跨实例 CLI 心跳: %v", err)
	}
	state := map[string]any{"epoch": project.CollaborationEpoch, "selection": []string{"node-1"},
		"view": map[string]any{"x": 20, "y": 30, "zoom": 1.2}}
	if err := sender.conn.WriteJSON(ws.Message{Type: ws.TypeCanvasPresence, Channel: sender.room, Data: state}); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func verifyCLIFollow(ctx context.Context, cli realCLI, code string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := cli.command(ctx, "canvas", "watch", code, "--follow", "load-member-1", "--jsonl", "--interval", "60")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var event struct {
			Type string `json:"type"`
			Data struct {
				Viewpoint struct {
					ActorUsername string                 `json:"actorUsername"`
					Selection     []string               `json:"selection"`
					View          struct{ Zoom float64 } `json:"view"`
				} `json:"viewpoint"`
			} `json:"data"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) == nil && event.Type == "follow" && event.Data.Viewpoint.ActorUsername == "load-member-1" &&
			event.Data.Viewpoint.View.Zoom == 1.2 && len(event.Data.Viewpoint.Selection) == 1 && event.Data.Viewpoint.Selection[0] == "node-1" {
			cancel()
			_ = cmd.Wait()
			return nil
		}
	}
	err = cmd.Wait()
	return fmt.Errorf("CLI 未输出授权视角: %w %w %s", scanner.Err(), err, stderr.String())
}

func verifyRealCLIWriters(t *testing.T, cli string, writers int) {
	t.Helper()
	clients, _, repo, project, owner := canvasLoadSetup(t, "long-prompt", nil)
	code := idcodec.Encode(project.ID)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	observer := newRealCLI(t, cli, clients[9])
	escaped := startRealCLIWatch(ctx, t, observer, code, project.Revision+uint64(writers))
	results := make(chan error, writers)
	release := make(chan struct{})
	for index := range writers {
		client := newRealCLI(t, cli, clients[index])
		plan := prepareRealCLIPlan(ctx, t, client, code, index)
		go func() {
			<-release
			output, err := client.command(ctx, "canvas", "apply", code, "--plan", plan, "--json").CombinedOutput()
			if err != nil {
				err = fmt.Errorf("CLI %d apply: %w %s", index, err, output)
			}
			results <- err
		}()
	}
	start := time.Now()
	close(release)
	for range writers {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
	if err := <-escaped; err != nil {
		t.Fatal(err)
	}
	confirmed, _, err := repo.Snapshot(ctx, owner, project.ID)
	if err != nil || confirmed.Revision != project.Revision+uint64(writers) {
		t.Fatalf("所有进程的独立编辑都应存在: %v", err)
	}
	for index := range writers {
		if !bytes.Contains(confirmed.PayloadJSON, fmt.Appendf(nil, `"prompt":"CLI 镜头 %d"`, index)) {
			t.Errorf("缺少进程 %d 的编辑", index)
		}
	}
	t.Logf("%d 个写入进程与 1 个监听进程在双实例收敛，耗时 %s", writers, time.Since(start).Round(time.Millisecond))
}

type realCLI struct {
	bin  string
	home string
}

func newRealCLI(t *testing.T, cli string, client *canvasLoadClient) realCLI {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, "video-canvas")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	creds, err := json.Marshal(map[string]string{"base": client.server.URL + "/api/v1", "token": client.token, "role": "user"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "credentials.json"), creds, 0o600); err != nil {
		t.Fatal(err)
	}
	return realCLI{bin: cli, home: home}
}

func (cli realCLI) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "node", append([]string{cli.bin}, args...)...)
	cmd.Env = append(os.Environ(), "XDG_CONFIG_HOME="+cli.home, "VC_NO_UPDATE_CHECK=1", "VC_API_BASE=")
	return cmd
}

func prepareRealCLIPlan(ctx context.Context, t *testing.T, cli realCLI, code string, index int) string {
	t.Helper()
	file, plan := filepath.Join(cli.home, "edit.json"), filepath.Join(cli.home, "plan.json")
	input := fmt.Appendf(nil, `{"changes":[{"kind":"node","id":"node-%d","fields":{"data.prompt":"CLI 镜头 %d"}}]}`, index, index)
	if err := os.WriteFile(file, input, 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := cli.command(ctx, "canvas", "plan", code, "--input", file, "-o", plan, "--json").CombinedOutput(); err != nil {
		t.Fatalf("CLI plan: %v %s", err, output)
	}
	return plan
}

func startRealCLIWatch(ctx context.Context, t *testing.T, cli realCLI, code string, target uint64) <-chan error {
	t.Helper()
	watchCtx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	cmd := cli.command(watchCtx, "canvas", "watch", code, "--jsonl", "--interval", "60")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	if !scanner.Scan() {
		cancel()
		_ = cmd.Wait()
		t.Fatalf("CLI 监听没有返回初始快照: %v %s", scanner.Err(), stderr.String())
	}
	result := make(chan error, 1)
	go observeRealCLIWatch(scanner, target, cmd, cancel, &stderr, result)
	return result
}

func observeRealCLIWatch(scanner *bufio.Scanner, target uint64, cmd *exec.Cmd, cancel context.CancelFunc, stderr *bytes.Buffer, result chan<- error) {
	for scanner.Scan() {
		var event struct {
			Type     string `json:"type"`
			Revision string `json:"revision"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) == nil && event.Type == "change" && event.Revision == strconv.FormatUint(target, 10) {
			cancel()
			_ = cmd.Wait()
			result <- nil
			return
		}
	}
	cancel()
	err := cmd.Wait()
	result <- fmt.Errorf("CLI 未从 WebSocket 通知追到版本 %d: %w %w %s", target, scanner.Err(), err, stderr.String())
}
