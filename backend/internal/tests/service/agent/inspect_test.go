package agent_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	. "video-canvas/internal/service/agent"
)

// fakeAssets 是内存里的素材库：只有 owner 能打开。
type fakeAssets struct {
	files map[uint64]*model.Asset
	data  map[uint64][]byte
	owner uint64
}

func (f *fakeAssets) Get(_ context.Context, _, id uint64) (*model.Asset, error) {
	if a := f.files[id]; a != nil {
		return a, nil
	}
	return nil, provider.ErrAssetNotFound
}

func (f *fakeAssets) Open(_ context.Context, userID, id uint64) (*provider.AssetFile, error) {
	a := f.files[id]
	if a == nil || userID != f.owner {
		return nil, provider.ErrAssetNotFound
	}
	return &provider.AssetFile{Asset: a, Body: io.NopCloser(bytes.NewReader(f.data[id]))}, nil
}

const inspectCanvas = `{"nodes":[
 {"id":"a","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"image","label":"角色","assetId":"1","src":"/f/1"}},
 {"id":"b","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"image","label":"场景","assetId":"2","src":"/f/2"}},
 {"id":"big","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"image","label":"大图","assetId":"3","src":"/f/3"}},
 {"id":"gif","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"image","label":"表情包","assetId":"4","src":"/f/4"}},
 {"id":"gone","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"image","label":"没了","assetId":"9","src":"/f/9"}},
 {"id":"txt","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"script","label":"剧本"}}
],"edges":[]}`

func newInspectEnv(t *testing.T, vision bool) (*bridgeEnv, string) {
	t.Helper()
	b := newBridgeEnv(t)
	b.repo.canvas.PayloadJSON = []byte(inspectCanvas)
	b.reg.snap.Model.Capabilities.Vision = vision
	assets := &fakeAssets{owner: 1, files: map[uint64]*model.Asset{
		1: {ID: 1, Kind: "image", MimeType: "image/png", ByteSize: 3},
		2: {ID: 2, Kind: "image", MimeType: "image/jpeg", ByteSize: 3},
		3: {ID: 3, Kind: "image", MimeType: "image/png", ByteSize: 6 << 20},
		4: {ID: 4, Kind: "image", MimeType: "image/svg+xml", ByteSize: 3},
	}, data: map[uint64][]byte{1: []byte("ABC"), 2: []byte("XYZ")}}
	b.br = NewAgentBridge(BridgeDeps{
		Repo: b.repo, Canvas: NewAgentCanvasService(b.repo, b.bc), Agent: b.svc, Billing: NewAgentBilling(b.bill),
		Registry: b.reg, Secrets: fakeSecrets{"channel:ch1": "sk-test"}, Streamer: b.str, Assets: assets,
	})
	tok, _ := b.token(t, "prompt")
	return b, tok
}

func TestAgentBridge_InspectImage(t *testing.T) {
	t.Run("按顺序随结果交回图片（base64），标题写在文字里；所有任务模式都能用", func(t *testing.T) {
		b, tok := newInspectEnv(t, true)
		res := b.tool(t, tok, "canvas_inspect_image", map[string]any{"nodeIds": []string{"a", "b"}})
		if res.IsError || len(res.Images) != 2 || res.Images[0].Data != "QUJD" || res.Images[0].MimeType != "image/png" || res.Images[1].MimeType != "image/jpeg" {
			t.Fatalf("res=%+v", res)
		}
		if !strings.Contains(res.Content, "角色") || !strings.Contains(res.Content, "场景") {
			t.Errorf("文字里说明每张图是哪个节点: %s", res.Content)
		}
		if !hasTool(AgentToolsForMode("storyboard"), "canvas_inspect_image") {
			t.Error("看图是只读工具，各模式都能用")
		}
	})

	t.Run("模型不能看图：工具错误；也不会把工具声明给它", func(t *testing.T) {
		b, tok := newInspectEnv(t, false)
		res := b.tool(t, tok, "canvas_inspect_image", map[string]any{"nodeIds": []string{"a"}})
		if !res.IsError || len(res.Images) != 0 || !strings.Contains(res.Content, "不能看图") {
			t.Errorf("res=%+v", res)
		}
		if hasTool(AgentToolsFor("all", false), "canvas_inspect_image") || !hasTool(AgentToolsFor("all", true), "canvas_inspect_image") {
			t.Error("只有支持看图的模型才拿到这个工具")
		}
	})

	t.Run("不合格的请求都是写给模型的工具错误，不带图片", func(t *testing.T) {
		b, tok := newInspectEnv(t, true)
		cases := []struct {
			want string
			ids  []string
		}{
			{"不是图片节点", []string{"txt"}},
			{"不存在", []string{"ghost"}},
			{"超过 5 MB", []string{"big"}},
			{"不支持查看", []string{"gif"}},
			{"已删除", []string{"gone"}},
			{"1 到 4", nil},
			{"1 到 4", []string{"a", "b", "a", "b", "a"}},
		}
		for _, c := range cases {
			res := b.tool(t, tok, "canvas_inspect_image", map[string]any{"nodeIds": c.ids})
			if !res.IsError || len(res.Images) != 0 || !strings.Contains(res.Content, c.want) {
				t.Errorf("%v 应是含 %q 的工具错误: %+v", c.ids, c.want, res)
			}
		}
	})
}
