package canvasgraph_test

import (
	"strings"
	"testing"

	"video-canvas/internal/agent/canvasgraph"
)

// applyWithAssets 在 opts() 的基础上带上已解析好的素材再应用操作。
func applyWithAssets(t *testing.T, raw string, assets map[string]canvasgraph.AssetRef) (*canvasgraph.Result, error) {
	t.Helper()
	o := opts()
	o.Assets = assets
	return canvasgraph.Apply(mustParse(t, basePayload), mustOps(t, raw), o)
}

var attachmentAsset = map[string]canvasgraph.AssetRef{
	"101": {ID: "101", URL: "/files/u1/ref.png", FileName: "参考.png", Width: 800, Height: 600},
}

func TestCreateNode_WithAssetIdBuildsFinishedUploadedImage(t *testing.T) {
	res, err := applyWithAssets(t, `[{"op":"create_node","tempId":"r","kind":"image","label":"林夏参考","assetId":"101"}]`, attachmentAsset)
	if err != nil {
		t.Fatal(err)
	}
	n := node(t, res.Graph, res.IDMap["r"])
	d := n.Data()
	for k, want := range map[string]any{
		"kind": "image", "status": "done", "src": "/files/u1/ref.png", "assetId": "101",
		"mediaType": "image", "uploaded": true, "fileName": "参考.png", "label": "林夏参考",
	} {
		if d[k] != want {
			t.Errorf("data[%s]=%v, want %v", k, d[k], want)
		}
	}
	if got, _ := d["mediaAspect"].(float64); got < 1.33 || got > 1.34 {
		t.Errorf("mediaAspect 按素材宽高算: %v", d["mediaAspect"])
	}
}

func TestCreateNode_WithAssetIdCanBeConnectedInTheSameBatch(t *testing.T) {
	res, err := applyWithAssets(t, `[
		{"op":"create_node","tempId":"r","kind":"image","assetId":"101"},
		{"op":"create_node","tempId":"v","kind":"video","label":"镜头"},
		{"op":"connect","source":"r","target":"v"}
	]`, attachmentAsset)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Graph.Edges) != 1 {
		t.Errorf("参考图应连到视频节点: %v", res.Graph.Edges)
	}
}

func TestCreateNode_WithAssetIdRejects(t *testing.T) {
	for name, c := range map[string]struct {
		ops  string
		want string
	}{
		"不是图片节点":  {`[{"op":"create_node","kind":"video","assetId":"101"}]`, "只能用在图片节点"},
		"素材没有解析到": {`[{"op":"create_node","kind":"image","assetId":"999"}]`, "素材 \"999\" 不可用"},
		"同时给了提示词": {`[{"op":"create_node","kind":"image","assetId":"101","prompt":"重画"}]`, "已有素材"},
		"同时给了模型":  {`[{"op":"create_node","kind":"image","assetId":"101","model":"m1"}]`, "已有素材"},
		"同时给了参数":  {`[{"op":"create_node","kind":"image","assetId":"101","params":{"a":1}}]`, "已有素材"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := applyWithAssets(t, c.ops, attachmentAsset)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("want %q, got %v", c.want, err)
			}
		})
	}
}

func TestUpdateNode_CannotCarryAssetId(t *testing.T) {
	// 产物字段仍然只能由系统写：update_node 带 assetId 在应用时被拒
	_, err := applyWithAssets(t, `[{"op":"update_node","id":"n_script","assetId":"101"}]`, attachmentAsset)
	if err == nil || !strings.Contains(err.Error(), "只能用在新建图片节点上") {
		t.Errorf("update_node 不能带 assetId: %v", err)
	}
}
