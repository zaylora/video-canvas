package repository_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/canvasdoc"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/repository"
)

func TestCanvasCollaborationSampleTargets(t *testing.T) {
	const graph = `{"nodes":[{"type":"group","id":"group-first"},{"type":"canvas","id":"real-shot-a"},{"type":"canvas","id":"real-shot-b"}],"edges":[]}`
	targets, err := canvasLoadNodeIDs([]byte(graph), 2)
	if err != nil || !reflect.DeepEqual(targets, []string{"real-shot-a", "real-shot-b"}) {
		t.Fatalf("应使用普通节点的原始 ID: %v, %v", targets, err)
	}
	if _, err := canvasLoadNodeIDs([]byte(graph), 3); err == nil {
		t.Fatal("普通节点不足时应报告样本不合格")
	}
}

func TestCanvasCollaborationSampleDocument(t *testing.T) {
	input := fmt.Sprintf(`{"format":%q,"version":%d,"canvas":{"nodes":[{"id":"real-shot","type":"canvas","data":{"assetHash":%q}}]}}`,
		canvasdoc.Format, canvasdoc.CurrentVersion, strings.Repeat("c", 64))
	if _, err := decodeCanvasLoadSample(strings.NewReader(input)); err == nil {
		t.Fatal("未声明的媒体哈希应拒绝进入压测")
	}
	document, err := decodeCanvasLoadSample(strings.NewReader(strings.Replace(input, `"assetHash"`, `"label"`, 1)))
	if err != nil || len(document.Canvas.Nodes) != 1 || document.Canvas.Nodes[0]["id"] != "real-shot" {
		t.Fatalf("有效导出文档应保留原节点: %v", err)
	}
}

func TestCanvasCollaborationConcurrentLoad(t *testing.T) {
	if os.Getenv("TEST_CANVAS_COLLABORATION_LOAD") != "1" || os.Getenv("TEST_REDIS_ADDR") == "" {
		t.Skip("需设置 TEST_CANVAS_COLLABORATION_LOAD=1、TEST_REDIS_ADDR 和 TEST_DATABASE_DSN")
	}
	runCanvasCollaborationLoadMatrix(t, "")
}

func TestCanvasCollaborationSampleLoad(t *testing.T) {
	directory := os.Getenv("TEST_CANVAS_COLLABORATION_SAMPLE_DIR")
	if directory == "" {
		t.Skip("未设置 TEST_CANVAS_COLLABORATION_SAMPLE_DIR")
	}
	if os.Getenv("TEST_REDIS_ADDR") == "" || os.Getenv("TEST_DATABASE_DSN") == "" {
		t.Fatal("样本压测需设置隔离测试库 TEST_DATABASE_DSN 和 TEST_REDIS_ADDR")
	}
	runCanvasCollaborationLoadMatrix(t, directory)
}

func TestCanvasCollaborationSampleRoundTripLoad(t *testing.T) {
	if os.Getenv("TEST_CANVAS_COLLABORATION_ROUNDTRIP") != "1" {
		t.Skip("需设置 TEST_CANVAS_COLLABORATION_ROUNDTRIP=1、TEST_REDIS_ADDR 和 TEST_DATABASE_DSN")
	}
	if os.Getenv("TEST_REDIS_ADDR") == "" || os.Getenv("TEST_DATABASE_DSN") == "" {
		t.Fatal("往返压测需设置隔离测试库 TEST_DATABASE_DSN 和 TEST_REDIS_ADDR")
	}
	directory := t.TempDir()
	writeCanvasLoadSamples(t, directory)
	t.Setenv("TEST_CANVAS_COLLABORATION_SAMPLE_DIR", directory)
	t.Log("使用从合成图谱生成的画布导出文档；本测试验证导入与协作协议，不代表真实用户样本")
	runCanvasCollaborationLoadMatrix(t, directory)
}

func writeCanvasLoadSamples(t *testing.T, directory string) {
	t.Helper()
	for _, layout := range []string{"spaced", "grouped", "long-prompt", "media"} {
		var asset *model.Asset
		assets := map[string]canvasdoc.AssetEntry{}
		if layout == "media" {
			asset = &model.Asset{ID: 987654, Kind: "image", StorageKey: "synthetic.png", MimeType: "image/png", ByteSize: 1024}
			assets["987654"] = canvasdoc.AssetEntry{Hash: strings.Repeat("a", 64), Info: canvasdoc.AssetInfo{
				Kind: "image", MimeType: "image/png", ByteSize: 1024, FileName: "synthetic.png",
			}}
		}
		document, report, err := canvasdoc.Build(canvasdoc.ExportInput{
			Title: "合成协作容量样本", Payload: canvasLoadGraph(t, layout, asset), Assets: assets,
			AppVersion: "capacity-test", Now: time.Date(2026, time.November, 27, 0, 0, 0, 0, time.UTC),
		})
		if err != nil || report.SkippedAssets != 0 {
			t.Fatalf("导出 %s 合成样本失败: report=%+v err=%v", layout, report, err)
		}
		raw, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, layout+".canvas.json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func runCanvasCollaborationLoadMatrix(t *testing.T, directory string) {
	t.Helper()
	for _, layout := range []string{"spaced", "grouped", "long-prompt", "media"} {
		t.Run(layout, func(t *testing.T) {
			var sample *canvasdoc.Document
			wantNodes := 1000
			if directory != "" {
				sample = readCanvasLoadSample(t, directory, layout)
				wantNodes = len(sample.Canvas.Nodes)
			} else if layout == "grouped" {
				wantNodes = 1010
			}
			clients, db, _, project, _ := canvasLoadSetup(t, layout, sample)
			targets, err := canvasLoadNodeIDs(project.PayloadJSON, len(clients))
			if err != nil {
				t.Fatal(err)
			}
			for _, client := range clients {
				client.join(t)
				defer client.disconnect()
			}
			measureCanvasLoadSnapshots(t, clients, project.CollaborationEpoch, wantNodes)
			for _, contention := range []string{"dispersed", "hotspot"} {
				t.Run(contention, func(t *testing.T) {
					labels := measureConcurrentCanvasLoad(t, db, clients, targets, project.CollaborationEpoch, contention)
					confirmed := clients[0].snapshot(t, project.CollaborationEpoch, wantNodes)
					if err := verifyCanvasLoadSnapshotLabels(confirmed, labels); err != nil {
						t.Fatal(err)
					}
				})
			}
		})
	}
}

func measureCanvasLoadSnapshots(t *testing.T, clients []*canvasLoadClient, epoch uint64, nodes int) {
	t.Helper()
	delays := make([]time.Duration, 0, len(clients))
	var graphBytes int
	var responseBytes int64
	for _, client := range clients {
		before := client.bytes.Load()
		started := time.Now()
		payload := client.snapshot(t, epoch, nodes)
		delays = append(delays, time.Since(started))
		graphBytes = max(graphBytes, len(payload))
		responseBytes = max(responseBytes, client.bytes.Load()-before)
	}
	t.Logf("授权快照读取并解码 p95=%s（%d 样本），最大图谱 %d 字节，最大 HTTP 响应体 %d 字节；各连接首轮 HTTP 响应体和 WS 消息字节 %v",
		canvasLoadP95(delays), len(delays), graphBytes, responseBytes, canvasLoadOutbound(clients, nil))
}

func canvasLoadOutbound(clients []*canvasLoadClient, baseline []int64) []int64 {
	outbound := make([]int64, len(clients))
	for index, client := range clients {
		outbound[index] = client.bytes.Load()
		if baseline != nil {
			outbound[index] -= baseline[index]
		}
	}
	return outbound
}

func measureConcurrentCanvasLoad(t *testing.T, db *gorm.DB, clients []*canvasLoadClient, targets []string, epoch uint64, contention string) map[string]string {
	t.Helper()
	const rounds = 5
	stats := canvasLoadStructuralStats{delays: make([]time.Duration, 0, rounds*len(clients)), wal: make([]int64, 0, rounds)}
	var submissions, signals, replays []time.Duration
	expected := make(map[string]string)
	outboundStart := canvasLoadOutbound(clients, nil)
	for _, client := range clients {
		client.labels = make(map[string]string)
	}
	for round := range rounds {
		baseline := clients[0].revision
		advance := len(clients)
		if contention == "hotspot" {
			advance = 1
		}
		walStart := canvasLoadWALPosition(t, db)
		start := time.Now()
		observed := replayConcurrentCanvasWrites(clients, epoch, baseline, advance, start)
		commits := 0
		for _, observation := range concurrentCanvasWrites(t, clients, targets, epoch, baseline, round, contention) {
			if observation.err != nil {
				t.Fatal(observation.err)
			}
			if err := recordCanvasLoadLabels(expected, observation.receipt.Changes); err != nil {
				t.Fatal(err)
			}
			commits += len(observation.receipt.Changes)
			submissions = append(submissions, observation.delay)
		}
		if commits != advance {
			t.Fatalf("%s 的确认数不符", contention)
		}
		for range clients {
			result := <-observed
			if result.err != nil {
				t.Fatal(result.err)
			}
			stats.delays = append(stats.delays, result.delay)
			signals = append(signals, result.signals)
			replays = append(replays, result.replay)
		}
		stats.wal = append(stats.wal, canvasLoadWALSince(t, db, walStart)/int64(commits))
	}
	for index, client := range clients {
		if !maps.Equal(client.labels, expected) {
			t.Fatalf("第 %d 个会话应用补发后的节点标签未收敛", index+1)
		}
		client.labels = nil
	}
	p95 := canvasLoadP95(stats.delays)
	t.Logf("阶段 P95：提交 HTTP=%s，提交返回后读完已到达或待到达的通知=%s，HTTP 补发及应用=%s",
		canvasLoadP95(submissions), canvasLoadP95(signals), canvasLoadP95(replays))
	t.Logf("十会话同时写入至全部版本补齐 P95=%s（%d 样本，模拟 80 ms RTT），每轮 WAL 增量按确认批次折算 P95=%d 字节，各连接 HTTP 响应体和 WS 增量字节 %v",
		p95, len(stats.delays), stats.walP95(), canvasLoadOutbound(clients, outboundStart))
	if p95 > 300*time.Millisecond {
		t.Fatalf("十会话并发的 %s 场景协议耗时超出预算", contention)
	}
	return expected
}

func recordCanvasLoadLabels(labels map[string]string, changes []repository.CanvasCollaborationChange) error {
	for _, change := range changes {
		var value string
		if change.Kind != "node" || json.Unmarshal(change.Fields["data.label"], &value) != nil || value == "" {
			return fmt.Errorf("节点 %q 缺少确认的标签写入", change.ID)
		}
		labels[change.ID] = value
	}
	return nil
}

type concurrentCanvasResult struct {
	receipt repository.CanvasCollaborationCommit
	delay   time.Duration
	err     error
}

func concurrentCanvasWrites(t *testing.T, clients []*canvasLoadClient, targets []string, epoch, baseline uint64, round int, contention string) []concurrentCanvasResult {
	t.Helper()
	results := make(chan concurrentCanvasResult, len(clients))
	for index, client := range clients {
		attempt := round*len(clients) + index
		batch := canvasLoadBatch(epoch, baseline, attempt, contention)
		batch.Changes[0].ID = targets[attempt%len(targets)]
		if contention == "hotspot" {
			batch.Changes[0].ID = targets[len(targets)-1]
		}
		body, err := json.Marshal(batch)
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			var result concurrentCanvasResult
			started := time.Now()
			result.err = client.request(http.MethodPost, client.base+"/ops", body, &result.receipt)
			result.delay = time.Since(started)
			if result.err == nil {
				result.err = verifyCanvasLoadReceipt(result.receipt, batch, contention)
			}
			results <- result
		}()
	}
	collected := make([]concurrentCanvasResult, 0, len(clients))
	for range clients {
		collected = append(collected, <-results)
	}
	return collected
}

func verifyCanvasLoadReceipt(receipt repository.CanvasCollaborationCommit, batch repository.CanvasCollaborationBatch, contention string) error {
	if receipt.OpID != batch.OpID || receipt.Epoch != batch.Epoch {
		return fmt.Errorf("提交 %q 的收据与请求不符", batch.OpID)
	}
	requested := batch.Changes[0]
	if len(receipt.Changes) == 1 {
		confirmed := receipt.Changes[0]
		if confirmed.ID == requested.ID && bytes.Equal(confirmed.Fields["data.label"], requested.Fields["data.label"]) {
			return nil
		}
	}
	if len(receipt.Changes) == 0 && contention == "hotspot" && len(receipt.Conflicts) == 1 {
		conflict := receipt.Conflicts[0]
		if conflict.ID == requested.ID && conflict.Field == "data.label" {
			return nil
		}
	}
	return fmt.Errorf("提交 %q 缺少匹配的确认或字段冲突", batch.OpID)
}

type concurrentCanvasReplay struct {
	delay   time.Duration
	signals time.Duration
	replay  time.Duration
	err     error
}

func replayConcurrentCanvasWrites(clients []*canvasLoadClient, epoch, baseline uint64, commits int, start time.Time) <-chan concurrentCanvasReplay {
	observed := make(chan concurrentCanvasReplay, len(clients))
	for _, client := range clients {
		go func() {
			started := time.Now()
			for range commits {
				if err := client.readSignal(ws.TypeCanvasCollaboration); err != nil {
					observed <- concurrentCanvasReplay{err: err}
					return
				}
			}
			replayStart := time.Now()
			err := client.replay(epoch, baseline+uint64(commits))
			finished := time.Now()
			observed <- concurrentCanvasReplay{delay: finished.Sub(start), signals: replayStart.Sub(started), replay: finished.Sub(replayStart), err: err}
		}()
	}
	return observed
}
