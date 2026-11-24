package agent_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	. "video-canvas/internal/service/agent"
)

// composeCanvasPayload 是一张「三段视频 + 一个成片节点」的画布：视频自上而下排，
// 不写 composeOrder 时成片顺序就是 V1 → V2 → V3。
const composeCanvasPayload = `{
  "nodes": [
    {"id":"v1","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"video","label":"S1","src":"http://x/1.mp4","assetId":"1","status":"done"}},
    {"id":"v2","type":"canvas","position":{"x":0,"y":400},"data":{"kind":"video","label":"S2","src":"http://x/2.mp4","assetId":"2","status":"done"}},
    {"id":"v3","type":"canvas","position":{"x":0,"y":800},"data":{"kind":"video","label":"S3","src":"http://x/3.mp4","assetId":"3","status":"done"}},
    {"id":"c1","type":"canvas","position":{"x":700,"y":200},"data":{"kind":"compose","label":"成片"}}
  ],
  "edges": [
    {"id":"e1","source":"v1","target":"c1"},
    {"id":"e2","source":"v2","target":"c1"},
    {"id":"e3","source":"v3","target":"c1"}
  ],
  "viewport":{"x":0,"y":0,"zoom":1}
}`

// fakeCompose 是内存版成片合成：记下提交内容，按脚本回放任务状态。
type fakeCompose struct {
	mu        sync.Mutex
	submitted []*model.CanvasComposeReq
	active    *model.CanvasComposeResp // Active 的返回值；nil 表示这个节点没合成过
	steps     []string                 // Job 依次返回的状态，最后一个重复
	calls     int
	submitErr error
}

func (f *fakeCompose) Submit(_ context.Context, userID, canvasID uint64, req *model.CanvasComposeReq) (*model.CanvasComposeResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.submitErr != nil {
		return nil, f.submitErr
	}
	f.submitted = append(f.submitted, req)
	return &model.CanvasComposeResp{ID: idcodec.Encode(1), CanvasID: idcodec.Encode(canvasID), NodeID: req.NodeID,
		Mode: req.Mode, Status: model.ComposeJobQueued, SegmentCount: len(req.AssetIDs)}, nil
}

func (f *fakeCompose) Job(_ context.Context, userID, jobID uint64) (*model.CanvasComposeResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	status := model.ComposeJobSucceeded
	if len(f.steps) > 0 {
		at := f.calls - 1
		if at >= len(f.steps) {
			at = len(f.steps) - 1
		}
		status = f.steps[at]
	}
	return &model.CanvasComposeResp{ID: idcodec.Encode(jobID), NodeID: "c1", Status: status,
		SegmentCount: 3, DurationMS: map[string]int64{model.ComposeJobSucceeded: 14848, model.ComposeJobFailed: 0}[status]}, nil
}

func (f *fakeCompose) Active(context.Context, uint64, uint64, string, []string) (*model.CanvasComposeResp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.active, nil
}

// composeEnv 是带成片画布的桥环境：画布内容换成三段视频接一个成片节点。
func composeEnv(t *testing.T) *bridgeEnv {
	t.Helper()
	b := newBridgeEnv(t)
	b.repo.canvas.PayloadJSON = datatypes.JSON(composeCanvasPayload)
	return b
}

// composeOut 取工具返回的 JSON 内容。
func composeOut(t *testing.T, res *ToolResult) map[string]any {
	t.Helper()
	if res.IsError {
		t.Fatalf("工具报错：%s", res.Content)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.Content), &out); err != nil {
		t.Fatalf("结果不是 JSON：%s", res.Content)
	}
	return out
}

// TestToolCompose_SubmitsCurrentOrderAndWaits 触发合成时素材序列由画布现算，
// 顺序与界面一致；工具一直等到出片，返回状态、段数和时长。
func TestToolCompose_SubmitsCurrentOrderAndWaits(t *testing.T) {
	b := composeEnv(t)
	tok, _ := b.token(t, "all")
	res := b.tool(t, tok, "canvas_compose", map[string]any{"nodeId": "c1"})
	out := composeOut(t, res)

	if len(b.compose.submitted) != 1 {
		t.Fatalf("应提交一次合成，实际 %d 次", len(b.compose.submitted))
	}
	req := b.compose.submitted[0]
	if len(req.AssetIDs) != 3 || req.AssetIDs[0] != "1" || req.AssetIDs[1] != "2" || req.AssetIDs[2] != "3" {
		t.Errorf("素材序列应按画布位置从上到下：%v", req.AssetIDs)
	}
	if req.Mode != "compose" || req.NodeID != "c1" {
		t.Errorf("提交参数=%+v", req)
	}
	if out["status"] != model.ComposeJobSucceeded || out["segments"] != float64(3) || out["seconds"] != 14.848 {
		t.Errorf("结果=%v", out)
	}
	if got := toolEndSummary(res); got != "合成成片完成（3 段 14.8 秒）" {
		t.Errorf("摘要=%q", got)
	}
}

// TestToolCompose_SkipsSegmentsWithoutVideo 还没出片的段不提交，段号如实回来，
// 模型才知道缺哪一段。
func TestToolCompose_SkipsSegmentsWithoutVideo(t *testing.T) {
	b := composeEnv(t)
	b.repo.canvas.PayloadJSON = datatypes.JSON(`{
	  "nodes": [
	    {"id":"v1","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"video","label":"S1","src":"http://x/1.mp4","assetId":"1"}},
	    {"id":"v2","type":"canvas","position":{"x":0,"y":400},"data":{"kind":"video","label":"S2","status":"running"}},
	    {"id":"c1","type":"canvas","position":{"x":700,"y":0},"data":{"kind":"compose","label":"成片"}}
	  ],
	  "edges":[{"id":"e1","source":"v1","target":"c1"},{"id":"e2","source":"v2","target":"c1"}]
	}`)
	tok, _ := b.token(t, "all")
	out := composeOut(t, b.tool(t, tok, "canvas_compose", map[string]any{"nodeId": "c1"}))

	if got := b.compose.submitted[0].AssetIDs; len(got) != 1 || got[0] != "1" {
		t.Errorf("只应提交已出片的那一段：%v", got)
	}
	skipped, _ := out["skipped"].([]any)
	if len(skipped) != 1 || skipped[0] != float64(1) {
		t.Errorf("没出片的段号要从 0 起如实回报：%v", out["skipped"])
	}
}

// TestToolCompose_ReusesFreshResult 同样素材、同样顺序已经有成片时不重复合成。
func TestToolCompose_ReusesFreshResult(t *testing.T) {
	b := composeEnv(t)
	stale := false
	b.compose.active = &model.CanvasComposeResp{ID: idcodec.Encode(3), NodeID: "c1", Status: model.ComposeJobSucceeded,
		SegmentCount: 3, DurationMS: 9000, Stale: &stale}
	tok, _ := b.token(t, "all")
	out := composeOut(t, b.tool(t, tok, "canvas_compose", map[string]any{"nodeId": "c1"}))

	if len(b.compose.submitted) != 0 {
		t.Errorf("成片还是最新的，不该再提交：%+v", b.compose.submitted)
	}
	if out["reused"] != true || out["status"] != model.ComposeJobSucceeded {
		t.Errorf("结果=%v", out)
	}
}

// TestToolCompose_CheckDoesNotSubmit action=check 只回报状态，不产生新任务。
func TestToolCompose_CheckDoesNotSubmit(t *testing.T) {
	b := composeEnv(t)
	b.compose.active = &model.CanvasComposeResp{ID: idcodec.Encode(4), NodeID: "c1", Status: model.ComposeJobRunning, SegmentCount: 3}
	tok, _ := b.token(t, "all")
	out := composeOut(t, b.tool(t, tok, "canvas_compose", map[string]any{"nodeId": "c1", "action": "check"}))

	if len(b.compose.submitted) != 0 {
		t.Errorf("check 不该提交：%+v", b.compose.submitted)
	}
	if out["status"] != model.ComposeJobRunning {
		t.Errorf("结果=%v", out)
	}
}

// TestToolCompose_RejectsNonHost 不能承载成片的节点（如视频节点）直接报错，不提交。
func TestToolCompose_RejectsNonHost(t *testing.T) {
	b := composeEnv(t)
	tok, _ := b.token(t, "all")
	res := b.tool(t, tok, "canvas_compose", map[string]any{"nodeId": "v1"})
	if !res.IsError {
		t.Fatalf("视频节点不能当承载成片的节点：%s", res.Content)
	}
	if len(b.compose.submitted) != 0 {
		t.Error("校验失败不该提交任务")
	}
}
