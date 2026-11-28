package repository_test

import (
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

	"video-canvas/internal/pkg/idcodec"
)

func TestCanvasMixedRealClients(t *testing.T) {
	verifyCanvasMixedRealClients(t, 1, 0, 30*time.Minute)
}

func TestCanvasMixedLargeVersions(t *testing.T) {
	const safeLimit = 1<<53 - 1
	verifyCanvasMixedRealClients(t, safeLimit-2, safeLimit+1, 25*time.Second)
}

func verifyCanvasMixedRealClients(t *testing.T, revision, epoch uint64, duration time.Duration) {
	t.Helper()
	chrome := os.Getenv("TEST_CANVAS_CHROME")
	if os.Getenv("TEST_CANVAS_MIXED_LOAD") != "1" || chrome == "" || os.Getenv("TEST_REDIS_ADDR") == "" {
		t.Skip("需要 TEST_CANVAS_MIXED_LOAD=1、TEST_CANVAS_CHROME、TEST_REDIS_ADDR、TEST_DATABASE_DSN 及已构建的 CLI 和网页")
	}
	const root = "../../../.."
	web := filepath.Join(root, "web/dist")
	cli := filepath.Join(root, "cli/dist/cli.js")
	for _, artifact := range []string{filepath.Join(web, "index.html"), cli} {
		if _, err := os.Stat(artifact); err != nil {
			t.Fatal(err)
		}
	}
	layout := canvasMixedLayout(t)
	clients, db, repo, project, owner := canvasLoadSetupFrom(t, layout, nil, revision, epoch)
	code := idcodec.Encode(project.ID)
	if os.Getenv("TEST_CANVAS_MIXED_SMOKE") == "1" {
		duration = 25 * time.Second
	}
	ctx, cancel := context.WithTimeout(t.Context(), duration+2*time.Minute)
	defer cancel()
	fronts := [2]*httptest.Server{mixedCanvasFrontend(t, web, clients[0].server.URL), mixedCanvasFrontend(t, web, clients[1].server.URL)}
	driver := startMixedBrowsers(ctx, t, chrome, filepath.Join(root, "scripts/canvas-mixed-browser.mjs"), code, duration, fronts, clients[4:8])
	writers := make([]realCLI, 4)
	for index := range writers {
		writers[index] = newRealCLI(t, cli, clients[index])
	}
	select {
	case <-driver.ready:
	case err := <-driver.result:
		t.Fatalf("浏览器未就绪: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	walStart := canvasLoadWALPosition(t, db)
	start := time.Now()
	var contention atomic.Int64
	results := launchMixedCLI(ctx, writers, code, start.Add(duration), &contention)
	rounds := collectMixedEdits(ctx, t, results, len(writers), driver)
	editingTime := time.Since(start).Round(time.Second)
	walBytes := canvasLoadWALSince(t, db, walStart)
	confirmed, _, err := repo.Snapshot(ctx, owner, project.ID)
	if err != nil {
		t.Fatalf("混合负载未落到权威图谱: %v", err)
	}
	if epoch > 0 && confirmed.Revision <= 1<<53 || confirmed.Revision <= project.Revision {
		t.Fatalf("确认版本未推进到预期范围: %d", confirmed.Revision)
	}
	verifyMixedConvergence(ctx, t, driver, code, writers, rounds, confirmed.Revision, confirmed.PayloadJSON)
	if contention.Load() == 0 {
		t.Error("未观测到热点字段冲突")
	}
	t.Logf("4 CLI + 4 Chrome，图谱 %s，实际编辑时长 %s，热点冲突 %d，最终确认版本 %d，PostgreSQL 实例 WAL 增量 %d B",
		layout, editingTime, contention.Load(), confirmed.Revision, walBytes)
}

func canvasMixedLayout(t *testing.T) string {
	t.Helper()
	layout := os.Getenv("TEST_CANVAS_MIXED_LAYOUT")
	switch layout {
	case "":
		return "long-prompt"
	case "sparse", "grouped", "long-prompt", "media":
		return layout
	default:
		t.Fatalf("未知 TEST_CANVAS_MIXED_LAYOUT %q", layout)
		return ""
	}
}

func mixedCanvasFrontend(t *testing.T, web, backend string) *httptest.Server {
	t.Helper()
	base, err := url.Parse(backend)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(base)
	static := http.FileServer(http.Dir(web))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/api/") {
			if !strings.HasSuffix(req.URL.Path, "/ws") {
				time.Sleep(canvasLoadDelay)
			}
			proxy.ServeHTTP(w, req)
			return
		}
		if strings.HasPrefix(req.URL.Path, "/canvas/") {
			http.ServeFile(w, req, filepath.Join(web, "index.html"))
			return
		}
		static.ServeHTTP(w, req)
	}))
	t.Cleanup(server.Close)
	return server
}

type mixedBrowserProcess struct {
	ready         <-chan struct{}
	editingDone   <-chan struct{}
	result        <-chan error
	finalFilePath string
}

func startMixedBrowsers(ctx context.Context, t *testing.T, chrome, script, code string, duration time.Duration, fronts [2]*httptest.Server, clients []*canvasLoadClient) mixedBrowserProcess {
	t.Helper()
	dir := t.TempDir()
	config := struct {
		Chrome   string              `json:"chrome"`
		Dir      string              `json:"dir"`
		Code     string              `json:"code"`
		Duration int64               `json:"durationMs"`
		Sessions []map[string]string `json:"sessions"`
	}{Chrome: chrome, Dir: dir, Code: code, Duration: duration.Milliseconds()}
	for index, client := range clients {
		config.Sessions = append(config.Sessions, map[string]string{"base": fronts[index%2].URL, "token": client.token})
	}
	input, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "node", script)
	cmd.Stdin = strings.NewReader(string(input))
	ready, editingDone, result := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		out, err := cmd.CombinedOutput()
		if err != nil {
			result <- fmt.Errorf("Chrome 协作验收: %w\n%s", err, out)
		} else {
			t.Logf("%s", out)
			result <- nil
		}
	}()
	go waitMixedBrowserReady(ctx, filepath.Join(dir, "ready"), ready)
	go waitMixedBrowserReady(ctx, filepath.Join(dir, "editing-done"), editingDone)
	return mixedBrowserProcess{ready: ready, editingDone: editingDone, result: result, finalFilePath: filepath.Join(dir, "final.json")}
}

func waitMixedBrowserReady(ctx context.Context, path string, ready chan<- struct{}) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			close(ready)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type mixedCLIResult struct {
	index  int
	rounds int
	err    error
}

func launchMixedCLI(ctx context.Context, writers []realCLI, code string, deadline time.Time, contention *atomic.Int64) <-chan mixedCLIResult {
	results := make(chan mixedCLIResult, len(writers))
	for index, writer := range writers {
		go func() {
			ticker := time.NewTicker(3 * time.Second)
			defer ticker.Stop()
			round := 0
			for ; time.Now().Before(deadline); round++ {
				if err := mixedCLIEdit(ctx, writer, code, index, round); err != nil {
					results <- mixedCLIResult{index: index, err: err}
					return
				}
				if round%5 == 1 {
					if err := mixedCLIHotspot(ctx, writer, code, index, round, contention); err != nil {
						results <- mixedCLIResult{index: index, err: err}
						return
					}
				}
				select {
				case <-ctx.Done():
					results <- mixedCLIResult{index: index, err: ctx.Err()}
					return
				case <-ticker.C:
				}
			}
			results <- mixedCLIResult{index: index, rounds: round}
		}()
	}
	return results
}

func collectMixedEdits(ctx context.Context, t *testing.T, results <-chan mixedCLIResult, writers int, browser mixedBrowserProcess) []int {
	t.Helper()
	rounds := make([]int, writers)
	for range writers {
		select {
		case result := <-results:
			if result.err != nil || result.rounds == 0 {
				t.Fatalf("CLI %d 编辑未完成: %v", result.index, result.err)
			}
			rounds[result.index] = result.rounds
		case err := <-browser.result:
			t.Fatalf("浏览器提前结束: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	select {
	case <-browser.editingDone:
		return rounds
	case err := <-browser.result:
		t.Fatalf("浏览器在收敛前退出: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	return nil
}

func verifyMixedConvergence(ctx context.Context, t *testing.T, browser mixedBrowserProcess, code string, writers []realCLI, rounds []int, revision uint64, payload []byte) {
	t.Helper()
	var graph struct {
		Nodes []struct {
			ID   string                 `json:"id"`
			Data struct{ Label string } `json:"data"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(payload, &graph); err != nil {
		t.Fatal(err)
	}
	labels := make(map[string]string, len(graph.Nodes))
	for _, node := range graph.Nodes {
		labels[node.ID] = node.Data.Label
	}
	finalLabels := make(map[string]string, len(writers)*2)
	for index, writer := range writers {
		last := rounds[index] - 1
		id := fmt.Sprintf("node-%d", index+8)
		finalLabels[id] = mixedCLILabel(index, last)
		assertMixedBatch(t, labels, index, last-last%5)
		assertMixedCLIRevision(ctx, t, writer, code, revision)
	}
	for index := range writers {
		id := fmt.Sprintf("node-%d", index)
		finalLabels[id] = labels[id]
	}
	for id, label := range finalLabels {
		if label == "" || labels[id] != label {
			t.Fatalf("节点 %s 的最终名称为 %q，预期 %q", id, labels[id], label)
		}
	}
	data, err := json.Marshal(finalLabels)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(browser.finalFilePath+".tmp", data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(browser.finalFilePath+".tmp", browser.finalFilePath); err != nil {
		t.Fatal(err)
	}
	if err := <-browser.result; err != nil {
		t.Fatal(err)
	}
	t.Logf("CLI 各提交 %v 轮，所有 CLI 读取了同一确认版本，四个网页精确匹配最终节点名称", rounds)
}

func assertMixedBatch(t *testing.T, labels map[string]string, index, round int) {
	t.Helper()
	for _, node := range mixedCLIBatchNodes(index) {
		id := fmt.Sprintf("node-%d", node)
		if want := mixedCLILabel(index, round); labels[id] != want {
			t.Fatalf("CLI %d 末次批量提交的节点 %s 为 %q，预期 %q", index, id, labels[id], want)
		}
	}
}

func assertMixedCLIRevision(ctx context.Context, t *testing.T, writer realCLI, code string, revision uint64) {
	t.Helper()
	output, err := writer.command(ctx, "canvas", "status", code, "--json").CombinedOutput()
	if err != nil {
		t.Fatalf("CLI 读取最终版本: %v %s", err, output)
	}
	var result struct {
		Data struct{ Revision string } `json:"data"`
	}
	if err := json.Unmarshal(output, &result); err != nil || result.Data.Revision != strconv.FormatUint(revision, 10) {
		t.Fatalf("CLI 最终版本不匹配，预期 %d: %v %s", revision, err, output)
	}
}

func mixedCLILabel(index, round int) string {
	return fmt.Sprintf("混合负载 %d/%d", index, round)
}

func mixedCLIBatchNodes(index int) []int {
	nodes := make([]int, 15)
	for offset := range nodes {
		nodes[offset] = 100 + 16*index + offset
	}
	return nodes
}

func mixedCLIEdit(ctx context.Context, cli realCLI, code string, index, round int) error {
	nodes := []int{index + 8}
	if round%5 == 0 {
		nodes = append(nodes, mixedCLIBatchNodes(index)...)
	}
	return applyMixedCLIPlan(ctx, cli, code, fmt.Sprintf("edit-%d", round), mixedCLILabel(index, round), nodes)
}

func mixedCLIHotspot(ctx context.Context, cli realCLI, code string, index, round int, contention *atomic.Int64) error {
	err := applyMixedCLIPlan(ctx, cli, code, fmt.Sprintf("hot-%d", round), fmt.Sprintf("热点 %d/%d", index, round), []int{32})
	if err != nil && strings.Contains(err.Error(), `"reason":"conflict"`) {
		contention.Add(1)
		return nil
	}
	return err
}

func applyMixedCLIPlan(ctx context.Context, cli realCLI, code, suffix, label string, nodes []int) error {
	file := filepath.Join(cli.home, "mixed-edit.json")
	plan := filepath.Join(cli.home, "mixed-plan-"+suffix+".json")
	changes := make([]map[string]any, 0, len(nodes))
	for _, node := range nodes {
		changes = append(changes, map[string]any{"kind": "node", "id": fmt.Sprintf("node-%d", node), "fields": map[string]string{"data.label": label}})
	}
	input, err := json.Marshal(map[string]any{"changes": changes})
	if err != nil {
		return err
	}
	if err := os.WriteFile(file, input, 0o600); err != nil {
		return err
	}
	for _, args := range [][]string{{"canvas", "plan", code, "--input", file, "-o", plan, "--json"}, {"canvas", "apply", code, "--plan", plan, "--json"}} {
		out, err := cli.command(ctx, args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("CLI %s %s: %w %s", suffix, args[1], err, out)
		}
	}
	return nil
}
