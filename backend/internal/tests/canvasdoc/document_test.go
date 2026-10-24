package canvasdoc_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"video-canvas/internal/canvasdoc"
)

var (
	h1 = strings.Repeat("a", 64)
	h2 = strings.Repeat("b", 64)
	h3 = strings.Repeat("c", 64)
)

// docJSON 生成一份带两个素材、一张角色卡的合法文档 JSON，nodes 由调用方给出。
func docJSON(nodes string) string {
	return `{
  "format": "video-canvas/canvas",
  "version": 1,
  "exportedAt": "2026-10-24T08:00:00Z",
  "appVersion": "0.4.16",
  "canvas": {"title": "雨夜短片", "nodes": ` + nodes + `, "edges": []},
  "assets": {
    "` + h1 + `": {"kind":"image","mimeType":"image/png","byteSize":100,"width":1,"height":1,"durationMs":0,"fileName":"a.png"},
    "` + h2 + `": {"kind":"image","mimeType":"image/png","byteSize":200,"width":1,"height":1,"durationMs":0,"fileName":"b.png"}
  },
  "characters": {"c1": {"name":"林夏","description":"","avatar":"` + h2 + `","extras":[],"voice":null}}
}`
}

// nodeWithData 返回只含一个节点、data 为给定 JSON 的 nodes 数组。
func nodeWithData(data string) string {
	return `[{"id":"n1","type":"canvas","position":{"x":1,"y":2},"data":` + data + `}]`
}

func TestParse(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr error
	}{
		{"合法文档", docJSON(`[]`), nil},
		{"缺 format", `{"version":1,"canvas":{"title":"t","nodes":[],"edges":[]}}`, canvasdoc.ErrInvalid},
		{"format 写错", `{"format":"other/thing","version":1}`, canvasdoc.ErrInvalid},
		{"format 不是字符串", `{"format":1,"version":1}`, canvasdoc.ErrInvalid},
		{"顶层是数组", `[]`, canvasdoc.ErrInvalid},
		{"顶层是字符串", `"video-canvas/canvas"`, canvasdoc.ErrInvalid},
		{"空内容", ``, canvasdoc.ErrInvalid},
		{"不是 JSON", `not json`, canvasdoc.ErrInvalid},
		{"JSON 之后还有内容", docJSON(`[]`) + `{}`, canvasdoc.ErrInvalid},
		{"缺 version", `{"format":"video-canvas/canvas"}`, canvasdoc.ErrInvalid},
		{"version 为 0", `{"format":"video-canvas/canvas","version":0}`, canvasdoc.ErrInvalid},
		{"nodes 里混入字符串", docJSON(`["n1"]`), canvasdoc.ErrInvalid},
		{"version 高于当前", `{"format":"video-canvas/canvas","version":2}`, canvasdoc.ErrTooNew},
		{"高版本且正文结构不同", `{"format":"video-canvas/canvas","version":2,"canvas":"换了结构"}`, canvasdoc.ErrTooNew},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := canvasdoc.Parse([]byte(tc.raw))
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("不应报错: %v", err)
				}
				if doc.Format != canvasdoc.Format || doc.Version != canvasdoc.CurrentVersion {
					t.Fatalf("format/version 不对: %q %d", doc.Format, doc.Version)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("want %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestParseKeepsNumbers(t *testing.T) {
	data := `{"kind":"image","outputs":[{"assetHash":"` + h1 + `","createdAt":1761300000000}]}`
	doc, err := canvasdoc.Parse([]byte(docJSON(nodeWithData(data))))
	if err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"createdAt":1761300000000`) {
		t.Fatalf("createdAt 没有原样往返: %s", out)
	}
	if strings.Contains(string(out), "e+") {
		t.Fatalf("出现科学计数法: %s", out)
	}
}

func TestParseNormalizesNullMaps(t *testing.T) {
	raw := `{"format":"video-canvas/canvas","version":1,"canvas":{"title":"t","nodes":[],"edges":[]},"assets":null,"characters":null}`
	doc, err := canvasdoc.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Assets == nil || doc.Characters == nil {
		t.Fatalf("null 应规整成空 map: %v %v", doc.Assets, doc.Characters)
	}
	raw = `{"format":"video-canvas/canvas","version":1,"canvas":{"title":"t"}}`
	doc, err = canvasdoc.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Assets == nil || doc.Characters == nil {
		t.Fatalf("缺省应规整成空 map: %v %v", doc.Assets, doc.Characters)
	}
}

func TestParseCharacterVoice(t *testing.T) {
	raw := strings.Replace(docJSON(`[]`), `"voice":null`, `"voice":"`+h1+`"`, 1)
	doc, err := canvasdoc.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if v := doc.Characters["c1"].Voice; v == nil || *v != h1 {
		t.Fatalf("voice 解析不对: %v", v)
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr error
	}{
		{"合法文档", docJSON(`[]`), nil},
		{"空画布且 assets 为空", `{"format":"video-canvas/canvas","version":1,"canvas":{"title":"t","nodes":[],"edges":[]},"assets":{},"characters":{}}`, nil},
		{"节点引用都存在", docJSON(nodeWithData(`{"kind":"image","assetHash":"` + h1 + `"}`)), nil},

		{"assets 键为大写 hex", strings.Replace(docJSON(`[]`), h1, strings.Repeat("A", 64), 1), canvasdoc.ErrInvalid},
		{"assets 键只有 63 位", strings.Replace(docJSON(`[]`), h1, strings.Repeat("a", 63), 1), canvasdoc.ErrInvalid},
		{"assets 键含非 hex", strings.Replace(docJSON(`[]`), h1, strings.Repeat("a", 63)+"g", 1), canvasdoc.ErrInvalid},
		{"assets byteSize 为负", strings.Replace(docJSON(`[]`), `"byteSize":100`, `"byteSize":-1`, 1), canvasdoc.ErrInvalid},

		{"data.assetHash 悬空", docJSON(nodeWithData(`{"assetHash":"` + h3 + `"}`)), canvasdoc.ErrInvalid},
		{"outputs[].assetHash 悬空", docJSON(nodeWithData(`{"outputs":[{"assetHash":"` + h1 + `"},{"assetHash":"` + h3 + `"}]}`)), canvasdoc.ErrInvalid},
		{"params.images 悬空", docJSON(nodeWithData(`{"params":{"images":["` + h1 + `","` + h3 + `"]}}`)), canvasdoc.ErrInvalid},
		{"params.videos 悬空", docJSON(nodeWithData(`{"params":{"videos":["` + h3 + `"]}}`)), canvasdoc.ErrInvalid},
		{"params.audios 悬空", docJSON(nodeWithData(`{"params":{"audios":["` + h3 + `"]}}`)), canvasdoc.ErrInvalid},
		{"paramAssets 键悬空", docJSON(nodeWithData(`{"paramAssets":{"` + h3 + `":{"fileName":"x.png","mediaType":"image"}}}`)), canvasdoc.ErrInvalid},
		{"scene.thumbHash 悬空", docJSON(nodeWithData(`{"scene":{"thumbHash":"` + h3 + `"}}`)), canvasdoc.ErrInvalid},
		{"params 与 paramAssets 与 scene 都存在则通过", docJSON(nodeWithData(`{"params":{"ratio":"16:9","images":["` + h1 + `"]},"paramAssets":{"` + h1 + `":{"fileName":"a.png"}},"scene":{"thumbHash":"` + h2 + `"},"outputs":[{"assetHash":"` + h2 + `","createdAt":1761300000000}]}`)), nil},

		{"角色卡 avatar 缺失", strings.Replace(docJSON(`[]`), `"avatar":"`+h2+`",`, ``, 1), canvasdoc.ErrInvalid},
		{"角色卡 avatar 不在 assets", strings.Replace(docJSON(`[]`), `"avatar":"`+h2+`"`, `"avatar":"`+h3+`"`, 1), canvasdoc.ErrInvalid},
		{"角色卡 extras 不在 assets", strings.Replace(docJSON(`[]`), `"extras":[]`, `"extras":["`+h3+`"]`, 1), canvasdoc.ErrInvalid},
		{"角色卡 voice 不在 assets", strings.Replace(docJSON(`[]`), `"voice":null`, `"voice":"`+h3+`"`, 1), canvasdoc.ErrInvalid},
		{"角色卡 extras 与 voice 都存在则通过", strings.Replace(strings.Replace(docJSON(`[]`), `"extras":[]`, `"extras":["`+h1+`"]`, 1), `"voice":null`, `"voice":"`+h1+`"`, 1), nil},

		{"characterRef 指向不存在的角色卡", docJSON(nodeWithData(`{"kind":"character","characterRef":"c9"}`)), canvasdoc.ErrInvalid},
		{"characterRef 指向存在的角色卡", docJSON(nodeWithData(`{"kind":"character","characterRef":"c1"}`)), nil},
		{"节点是 null", docJSON(`[null]`), canvasdoc.ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := canvasdoc.Parse([]byte(tc.raw))
			if err != nil {
				t.Fatalf("Parse 不应报错: %v", err)
			}
			err = doc.Validate(canvasdoc.DefaultLimits())
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("不应报错: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("want %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestValidateLimits(t *testing.T) {
	nodes := `[{"id":"n1","type":"canvas","data":{}},{"id":"n2","type":"canvas","data":{}},{"id":"n3","type":"canvas","data":{}}]`
	cases := []struct {
		name    string
		raw     string
		limits  canvasdoc.Limits
		wantErr error
	}{
		{"节点数刚好等于上限", docJSON(nodes), canvasdoc.Limits{MaxNodes: 3, MaxAssets: 10, MaxAssetBytes: 1000}, nil},
		{"节点数超上限", docJSON(nodes), canvasdoc.Limits{MaxNodes: 2, MaxAssets: 10, MaxAssetBytes: 1000}, canvasdoc.ErrLimit},
		{"素材数超上限", docJSON(`[]`), canvasdoc.Limits{MaxNodes: 10, MaxAssets: 1, MaxAssetBytes: 1000}, canvasdoc.ErrLimit},
		{"素材总字节刚好等于上限", docJSON(`[]`), canvasdoc.Limits{MaxNodes: 10, MaxAssets: 10, MaxAssetBytes: 300}, nil},
		{"素材总字节超上限", docJSON(`[]`), canvasdoc.Limits{MaxNodes: 10, MaxAssets: 10, MaxAssetBytes: 299}, canvasdoc.ErrLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := canvasdoc.Parse([]byte(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			err = doc.Validate(tc.limits)
			if tc.wantErr == nil && err != nil {
				t.Fatalf("不应报错: %v", err)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("want %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestDefaultLimits(t *testing.T) {
	l := canvasdoc.DefaultLimits()
	want := canvasdoc.Limits{MaxBytes: 10 * 1024 * 1024, MaxNodes: 2000, MaxAssets: 200, MaxAssetBytes: 1 << 30}
	if l != want {
		t.Fatalf("want %+v, got %+v", want, l)
	}
}

func TestValidateErrorMessageIsReadable(t *testing.T) {
	doc, err := canvasdoc.Parse([]byte(docJSON(nodeWithData(`{"assetHash":"` + h3 + `"}`))))
	if err != nil {
		t.Fatal(err)
	}
	err = doc.Validate(canvasdoc.DefaultLimits())
	if err == nil || !strings.Contains(err.Error(), "n1") {
		t.Fatalf("错误信息应带上节点 id 方便用户定位: %v", err)
	}
}
