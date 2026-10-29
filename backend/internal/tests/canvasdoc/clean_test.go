package canvasdoc_test

import (
	"errors"
	"strings"
	"testing"

	"video-canvas/internal/canvasdoc"
)

// Build 产出的文档就是合法导入文件，扫描必须放行，否则所有正常导出的画布都导不回来。
func TestCheckDocumentClean_AcceptsBuiltDocument(t *testing.T) {
	doc, _ := mustBuild(t, exportInput())
	if err := canvasdoc.CheckDocumentClean(doc); err != nil {
		t.Fatalf("Build 产出的文档应当通过扫描: %v", err)
	}
}

// 手改文件把原环境字段藏进任意层级（含 params、scene、连线）都要拒绝，返回 ErrInvalid 并点名字段。
func TestCheckDocumentClean_RejectsSmuggledFields(t *testing.T) {
	cases := []struct {
		name string
		node string
		edge string
		key  string
	}{
		{"顶层 assetId", `{"id":"n1","type":"canvas","data":{"assetId":"42"}}`, "", "assetId"},
		{"顶层 src", `{"id":"n1","type":"canvas","data":{"src":"/files/u9/a.png"}}`, "", "src"},
		{"嵌套未知对象里的 assetId", `{"id":"n1","type":"canvas","data":{"x-extra":{"deep":[{"assetId":"42"}]}}}`, "", "assetId"},
		{"params 里的 taskId", `{"id":"n1","type":"canvas","data":{"params":{"opt":{"taskId":"7"}}}}`, "", "taskId"},
		{"scene 里的 characterId", `{"id":"n1","type":"canvas","data":{"scene":{"characters":[{"characterId":"abc"}]}}}`, "", "characterId"},
		{"pendingTaskIds", `{"id":"n1","type":"canvas","data":{"pendingTaskIds":["1"]}}`, "", "pendingTaskIds"},
		{"voiceTaskId", `{"id":"n1","type":"canvas","data":{"voiceTaskId":"1"}}`, "", "voiceTaskId"},
		{"errorTaskRef", `{"id":"n1","type":"canvas","data":{"errorTaskRef":"r"}}`, "", "errorTaskRef"},
		{"activeOutputId", `{"id":"n1","type":"canvas","data":{"activeOutputId":"1"}}`, "", "activeOutputId"},
		{"labelCache", `{"id":"n1","type":"canvas","data":{"labelCache":{"name":"x"}}}`, "", "labelCache"},
		{"节点顶层 src", `{"id":"n1","type":"canvas","src":"/files/x","data":{}}`, "", "src"},
		{"连线里的 assetId", `{"id":"n1","type":"canvas","data":{}}`, `{"id":"e1","source":"n1","target":"n1","data":{"assetId":"42"}}`, "assetId"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			edges := "[]"
			if tc.edge != "" {
				edges = "[" + tc.edge + "]"
			}
			raw := `{"format":"video-canvas/canvas","version":1,"canvas":{"title":"t","nodes":[` + tc.node + `],"edges":` + edges + `}}`
			doc := mustParseDoc(t, raw)
			err := canvasdoc.CheckDocumentClean(doc)
			if !errors.Is(err, canvasdoc.ErrInvalid) {
				t.Fatalf("应当返回 ErrInvalid，实际 %v", err)
			}
			if !strings.Contains(err.Error(), tc.key) {
				t.Errorf("错误信息应当点名字段 %q: %v", tc.key, err)
			}
		})
	}
}

// url 只是访问地址、不是原环境身份引用，且 Restore 会重建它出现的已知位置，所以扫描放过它。
func TestCheckDocumentClean_IgnoresURL(t *testing.T) {
	raw := `{"format":"video-canvas/canvas","version":1,"canvas":{"title":"t","nodes":[
		{"id":"n1","type":"canvas","data":{"x-extra":{"url":"https://example.com/a.png"}}}],"edges":[]}}`
	if err := canvasdoc.CheckDocumentClean(mustParseDoc(t, raw)); err != nil {
		t.Fatalf("url 不应被拒绝: %v", err)
	}
}

func TestCheckDocumentClean_NilDocument(t *testing.T) {
	if err := canvasdoc.CheckDocumentClean(nil); !errors.Is(err, canvasdoc.ErrInvalid) {
		t.Fatalf("nil 文档应当返回 ErrInvalid，实际 %v", err)
	}
}
