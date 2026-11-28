package repository_test

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"gorm.io/gorm"

	"video-canvas/internal/canvasdoc"
	"video-canvas/internal/model"
)

func readCanvasLoadSample(t *testing.T, directory, layout string) *canvasdoc.Document {
	t.Helper()
	file, err := os.Open(filepath.Join(directory, layout+".canvas.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	document, err := decodeCanvasLoadSample(file)
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func decodeCanvasLoadSample(reader io.Reader) (*canvasdoc.Document, error) {
	limits := canvasdoc.DefaultLimits()
	raw, err := io.ReadAll(io.LimitReader(reader, limits.MaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取压测画布: %w", err)
	}
	if int64(len(raw)) > limits.MaxBytes {
		return nil, fmt.Errorf("压测画布超出 %d 字节导入上限", limits.MaxBytes)
	}
	document, err := canvasdoc.Parse(raw)
	if err != nil {
		return nil, err
	}
	if err := document.Validate(limits); err != nil {
		return nil, fmt.Errorf("压测画布导出文件校验失败: %w", err)
	}
	return document, nil
}

func restoreCanvasLoadSample(t *testing.T, db *gorm.DB, owner uint64, sample *canvasdoc.Document) []byte {
	t.Helper()
	assets := seedCanvasLoadSampleAssets(t, db, owner, sample.Assets)
	payload, report, err := canvasdoc.Restore(canvasdoc.RestoreInput{Doc: sample, Assets: assets})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.MissingAssets) > 0 || report.EdgesDropped > 0 {
		t.Fatalf("压测样本还原时丢失 %d 处媒体引用、%d 条连线", len(report.MissingAssets), report.EdgesDropped)
	}
	t.Logf("导出文档包含 %d 个节点记录、%d 条连线、%d 个素材哈希，还原图谱为 %d 字节",
		len(sample.Canvas.Nodes), len(sample.Canvas.Edges), len(assets), len(payload))
	return payload
}

func seedCanvasLoadSampleAssets(t *testing.T, db *gorm.DB, owner uint64, entries map[string]canvasdoc.AssetInfo) map[string]canvasdoc.RestoredAsset {
	t.Helper()
	if err := db.AutoMigrate(&model.Asset{}); err != nil {
		t.Fatal(err)
	}
	assets := make(map[string]canvasdoc.RestoredAsset, len(entries))
	for hash, entry := range entries {
		asset := model.Asset{UserID: owner, Kind: entry.Kind, MimeType: entry.MimeType, ByteSize: entry.ByteSize,
			Width: entry.Width, Height: entry.Height, DurationMs: entry.DurationMs, Source: "upload", StorageKey: "capacity/" + hash}
		if err := db.Create(&asset).Error; err != nil {
			t.Fatal(err)
		}
		assets[hash] = canvasdoc.RestoredAsset{ID: strconv.FormatUint(asset.ID, 10), URL: "/files/" + asset.StorageKey}
	}
	return assets
}

func canvasLoadNodeIDs(payload []byte, minimum int) ([]string, error) {
	var graph struct {
		Nodes []struct{ ID, Type string }
	}
	if err := json.Unmarshal(payload, &graph); err != nil {
		return nil, fmt.Errorf("读取确认图谱: %w", err)
	}
	ids := make([]string, 0, len(graph.Nodes))
	for _, node := range graph.Nodes {
		if node.Type == "canvas" && node.ID != "" {
			ids = append(ids, node.ID)
		}
	}
	if len(ids) < minimum {
		return nil, fmt.Errorf("图谱只有 %d 个普通节点，压测至少需要 %d 个", len(ids), minimum)
	}
	return ids, nil
}

func TestCanvasCollaborationLoadVerifySnapshotLabels(t *testing.T) {
	const graph = `{"nodes":[{"type":"canvas","id":"changed","data":{"label":"saved"}},{"type":"group","id":"group","data":{"label":"ignored"}}]}`
	for _, outcome := range []struct {
		name   string
		labels map[string]string
		valid  bool
	}{
		{name: "确认快照包含提交的字段", labels: map[string]string{"changed": "saved"}, valid: true},
		{name: "版本正确但内容过旧", labels: map[string]string{"changed": "expected"}},
		{name: "提交目标缺失", labels: map[string]string{"missing": ""}},
		{name: "不能以组节点充当普通节点", labels: map[string]string{"group": "ignored"}},
	} {
		t.Run(outcome.name, func(t *testing.T) {
			if err := verifyCanvasLoadSnapshotLabels([]byte(graph), outcome.labels); (err == nil) != outcome.valid {
				t.Fatalf("快照字段校验结果错误: %v", err)
			}
		})
	}
}

func verifyCanvasLoadSnapshotLabels(payload []byte, expected map[string]string) error {
	var graph struct {
		Nodes []struct {
			ID, Type string
			Data     struct{ Label string }
		}
	}
	if err := json.Unmarshal(payload, &graph); err != nil {
		return fmt.Errorf("读取确认快照: %w", err)
	}
	found := 0
	for _, node := range graph.Nodes {
		label, target := expected[node.ID]
		if node.Type != "canvas" || !target {
			continue
		}
		if node.Data.Label != label {
			return fmt.Errorf("确认快照中的节点 %q 标签未收敛", node.ID)
		}
		found++
	}
	if found != len(expected) {
		return fmt.Errorf("确认快照只包含 %d/%d 个提交目标", found, len(expected))
	}
	return nil
}
