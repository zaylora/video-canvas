package agent_test

import (
	"strings"
	"testing"

	"video-canvas/internal/model"
	. "video-canvas/internal/service/agent"
)

// newAssetToolEnv 装一个既能看图、又能把素材落成节点的桥：素材 1 是 owner(=1) 的 PNG，2 是 JPEG，6 是视频。
func newAssetToolEnv(t *testing.T, mode string, vision bool) (*bridgeEnv, string) {
	t.Helper()
	b := newBridgeEnv(t)
	b.repo.canvas.PayloadJSON = []byte(inspectCanvas)
	b.reg.snap.Model.Capabilities.Vision = vision
	assets := &fakeAssets{owner: 1, files: map[uint64]*model.Asset{
		1: {ID: 1, Kind: "image", MimeType: "image/png", ByteSize: 3, Width: 800, Height: 600, FileName: "参考.png", StorageKey: "u1/a.png"},
		2: {ID: 2, Kind: "image", MimeType: "image/jpeg", ByteSize: 3, StorageKey: "u1/b.jpg"},
		6: {ID: 6, Kind: "video", MimeType: "video/mp4", ByteSize: 3, StorageKey: "u1/v.mp4"},
	}, data: map[uint64][]byte{1: []byte("ABC"), 2: []byte("XYZ")}}
	b.br = NewAgentBridge(BridgeDeps{
		Repo: b.repo, Canvas: NewAgentCanvasService(b.repo, b.bc), Agent: b.svc, Billing: NewAgentBilling(b.bill),
		Registry: b.reg, Secrets: fakeSecrets{"channel:ch1": "sk-test"}, Streamer: b.str, Assets: assets, AssetLinks: fakeLinks{},
	})
	tok, _ := b.token(t, mode)
	return b, tok
}

func TestApplyOps_CreateImageNodeFromAttachment(t *testing.T) {
	b, tok := newAssetToolEnv(t, "all", true)
	res := b.tool(t, tok, "canvas_apply_ops", map[string]any{"ops": []map[string]any{
		{"op": "create_node", "tempId": "r", "kind": "image", "label": "林夏参考", "assetId": "1"},
	}})
	if res.IsError {
		t.Fatalf("res=%+v", res)
	}
	payload := string(b.repo.canvas.PayloadJSON)
	for _, want := range []string{`"assetId":"1"`, `"src":"/files/u1/a.png"`, `"status":"done"`, `"uploaded":true`, `"林夏参考"`} {
		if !strings.Contains(payload, want) {
			t.Errorf("画布里要有 %s: %s", want, payload)
		}
	}
}

func TestApplyOps_AssetIdMustBeOwnImage(t *testing.T) {
	b, tok := newAssetToolEnv(t, "all", true)
	for name, c := range map[string]struct {
		id   string
		want string
	}{
		"素材不存在":  {"99", "不存在或已删除"},
		"不是数字":   {"abc", "无效"},
		"不是图片格式": {"6", "不支持"},
	} {
		t.Run(name, func(t *testing.T) {
			res := b.tool(t, tok, "canvas_apply_ops", map[string]any{"ops": []map[string]any{
				{"op": "create_node", "kind": "image", "assetId": c.id},
			}})
			if !res.IsError || !strings.Contains(res.Content, c.want) {
				t.Errorf("res=%+v", res)
			}
		})
	}
}

func TestApplyOps_AssetIdNotAllowedInPromptMode(t *testing.T) {
	b, tok := newAssetToolEnv(t, "prompt", true)
	res := b.tool(t, tok, "canvas_apply_ops", map[string]any{"ops": []map[string]any{
		{"op": "create_node", "kind": "image", "assetId": "1"},
	}})
	if !res.IsError {
		t.Errorf("提示词优化模式不能新建节点: %+v", res)
	}
}

func TestInspectImage_ByAssetIds(t *testing.T) {
	b, tok := newAssetToolEnv(t, "all", true)
	res := b.tool(t, tok, "canvas_inspect_image", map[string]any{"assetIds": []string{"1", "2"}})
	if res.IsError || len(res.Images) != 2 || res.Images[0].Data != "QUJD" || res.Images[1].MimeType != "image/jpeg" {
		t.Fatalf("res=%+v", res)
	}
	if !strings.Contains(res.Content, "asset:1") || !strings.Contains(res.Content, "asset:2") {
		t.Errorf("文字里说明每张图是哪个素材: %s", res.Content)
	}
}

func TestInspectImage_NodesAndAssetsTogether(t *testing.T) {
	b, tok := newAssetToolEnv(t, "all", true)
	res := b.tool(t, tok, "canvas_inspect_image", map[string]any{"nodeIds": []string{"a"}, "assetIds": []string{"2"}})
	if res.IsError || len(res.Images) != 2 {
		t.Fatalf("res=%+v", res)
	}
	if !strings.Contains(res.Content, "节点 a") || !strings.Contains(res.Content, "asset:2") {
		t.Errorf("先节点后附件，各自说明: %s", res.Content)
	}
}

func TestInspectImage_AssetIdsLimitsAndErrors(t *testing.T) {
	b, tok := newAssetToolEnv(t, "all", true)
	for name, c := range map[string]struct {
		args map[string]any
		want string
	}{
		"一个都没给":  {map[string]any{}, "1 到 4"},
		"合计超过 4": {map[string]any{"nodeIds": []string{"a", "b", "big"}, "assetIds": []string{"1", "2"}}, "1 到 4"},
		"素材不存在":  {map[string]any{"assetIds": []string{"99"}}, "不存在或已删除"},
		"视频不能当图": {map[string]any{"assetIds": []string{"6"}}, "不支持"},
	} {
		t.Run(name, func(t *testing.T) {
			res := b.tool(t, tok, "canvas_inspect_image", c.args)
			if !res.IsError || !strings.Contains(res.Content, c.want) {
				t.Errorf("res=%+v", res)
			}
		})
	}
	// 模型不能看图时，按附件看也不行
	nb, ntok := newAssetToolEnv(t, "all", false)
	res := nb.tool(t, ntok, "canvas_inspect_image", map[string]any{"assetIds": []string{"1"}})
	if !res.IsError || !strings.Contains(res.Content, "不能看图") {
		t.Errorf("res=%+v", res)
	}
}
