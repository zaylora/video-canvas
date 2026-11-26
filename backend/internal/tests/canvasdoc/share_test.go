package canvasdoc_test

import (
	"encoding/json"
	"strings"
	"testing"

	"video-canvas/internal/canvasdoc"
)

func TestProjectSharedCanvas_RemovesPrivateStateAndRewritesMedia(t *testing.T) {
	raw := []byte(`{"title":"board","chatSessions":[{"id":"private"}],"nodes":[{"id":"n1","type":"canvas","position":{"x":4,"y":8},"data":{"assetId":"123","src":"/files/u1/a.png","taskId":"987","pendingTaskIds":["988"],"params":{"voiceTaskId":"989"},"text":"prompt"}}],"edges":[],"viewport":{"x":1,"y":2,"zoom":1.5}}`)
	got, err := canvasdoc.ProjectShared(raw, func(id string) string { return "/api/v1/canvas/shared/media/" + id })
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(got, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["chatSessions"]; ok {
		t.Fatal("共享 payload 不应包含私有会话")
	}
	node := payload["nodes"].([]any)[0].(map[string]any)
	data := node["data"].(map[string]any)
	if data["src"] != "/api/v1/canvas/shared/media/123" || data["text"] != "prompt" {
		t.Fatalf("媒体地址应代理化且创作内容应保留: %#v", data)
	}
	for _, key := range []string{"taskId", "pendingTaskIds"} {
		if _, ok := data[key]; ok {
			t.Errorf("共享 payload 不应包含 %s", key)
		}
	}
	params := data["params"].(map[string]any)
	if _, ok := params["voiceTaskId"]; ok {
		t.Error("嵌套任务引用也应剔除")
	}
	if payload["viewport"].(map[string]any)["zoom"] != 1.5 {
		t.Error("画布布局应保留")
	}
}

func TestProjectSharedCanvas_RejectsInvalidPayload(t *testing.T) {
	if _, err := canvasdoc.ProjectShared([]byte(`[]`), func(string) string { return "" }); err == nil {
		t.Fatal("非法画布结构应返回错误")
	}
}

func TestProjectSharedCanvasLegacyCharacter(t *testing.T) {
	raw := []byte(`{"nodes":[{"id":"role","type":"canvas","position":{"x":20,"y":30},"data":{"kind":"character","label":"旧角色","characterId":"143","labelCache":{"name":" 程雪 ","coverAssetId":"456"},"assetId":"12","src":"/files/current"}}],"edges":[{"id":"link","source":"role","target":"video"}]}`)
	got, err := canvasdoc.ProjectShared(raw, func(id string) string { return "/reader/" + id })
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"name":"程雪"`, `"look":"基础形象"`, `"extras":[]`, `"voice":null`, `"src":"/reader/12"`, `"position":{"x":20,"y":30}`, `"source":"role"`} {
		if !strings.Contains(string(got), expected) {
			t.Errorf("缺少 %s: %s", expected, got)
		}
	}
	for _, private := range []string{"characterId", "labelCache", "456", "/files/"} {
		if strings.Contains(string(got), private) {
			t.Errorf("残留旧引用 %q: %s", private, got)
		}
	}
}

func TestProjectSharedCanvasTextWithDirectoryName(t *testing.T) {
	raw := []byte(`{"nodes":[{"type":"canvas","data":{"kind":"script","text":"说明 /files/ 目录的用途"}}]}`)
	got, err := canvasdoc.ProjectShared(raw, func(string) string { return "" })
	if err != nil || !strings.Contains(string(got), "说明 /files/ 目录的用途") {
		t.Fatal(string(got), err)
	}
}

func TestProjectSharedCanvasNamedParamAsset(t *testing.T) {
	raw := []byte(`{"nodes":[{"id":"n","type":"canvas","data":{"params":{"mask":"23"},"paramAssets":{"mask":{"url":"/files/private"}}}}]}`)
	got, err := canvasdoc.ProjectShared(raw, func(id string) string { return "/reader/" + id })
	if err != nil || !strings.Contains(string(got), `"url":"/reader/23"`) {
		t.Fatalf("命名素材参数未映射: %s %v", got, err)
	}
}

func TestProjectSharedCanvasMixedGraph(t *testing.T) {
	raw := []byte(`{
		"nodes":[
			{"id":"group","type":"group","position":{"x":10,"y":15},"width":720,"height":550,"data":{"label":"Group","color":"blue"}},
			{"id":"image","type":"canvas","parentId":"group","position":{"x":30,"y":40},"data":{"kind":"image","assetId":"21","src":"/files/current.png","activeOutputId":"21","outputs":[{"id":"20","assetId":"20","src":"/files/earlier.png","taskId":"9"}],"params":{"images":["16"]},"paramAssets":{"16":{"url":"/files/reference.png","mediaType":"image"}}}},
			{"id":"board","type":"canvas","data":{"kind":"storyboard","storyboard":{"shots":[{"id":"shot1","imageNodeId":"image","videoNodeId":"video","duration":3.5},{"id":"shot2","image":{"assetId":"23","src":"/files/legacy.png"}}]}}},
			{"id":"video","type":"canvas","data":{"kind":"video","assetId":"24","src":"/files/clip.mp4","mediaType":"video","frameAspect":0.5625}},
			{"id":"audio","type":"canvas","data":{"kind":"audio","assetId":"25","src":"/files/audio.mp3","mediaType":"audio"}},
			{"id":"character","type":"canvas","data":{"kind":"character","character":{"name":"Lead","voice":{"assetId":"26","src":"/files/voice.mp3"}}}},
			{"id":"director","type":"canvas","data":{"kind":"director","scene":{"version":1,"duration":6,"activeCameraId":"cam","cameras":[{"id":"cam"}],"thumb":{"assetId":"22","url":"/files/thumb.png"}}}}
		],
		"edges":[{"id":"edge","source":"board","target":"image","sourceHandle":null,"targetHandle":"image"}],
		"viewport":{"x":20,"y":30,"zoom":0.8}
	}`)
	got, err := canvasdoc.ProjectShared(raw, func(id string) string { return "/reader/" + id })
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`"color":"blue"`, `"height":550`, `"width":720`, `"parentId":"group"`, `"position":{"x":30,"y":40}`, `"frameAspect":0.5625`,
		`"imageNodeId":"image"`, `"videoNodeId":"video"`, `"duration":3.5`, `"activeCameraId":"cam"`,
		`"sourceHandle":null,"target":"image","targetHandle":"image"`, `"viewport":{"x":20,"y":30,"zoom":0.8}`,
		`"src":"/reader/20"`, `"src":"/reader/21"`, `"url":"/reader/16"`, `"url":"/reader/22"`,
		`"src":"/reader/23"`, `"src":"/reader/24"`, `"src":"/reader/25"`, `"src":"/reader/26"`,
	} {
		if !strings.Contains(string(got), fragment) {
			t.Errorf("共享投影缺少 %s", fragment)
		}
	}
	if strings.Contains(string(got), `/files/`) || strings.Contains(string(got), `"taskId"`) {
		t.Fatal("共享投影泄漏原始素材地址或任务编号")
	}
}
