package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
	agentsvc "video-canvas/internal/service/agent"
)

// verifyDramaAgent 将短剧工具接到与 HTTP/worker 相同的真实服务，验证审批后读写、锁定和归属。
func verifyDramaAgent(ctx context.Context, e env, id string, canvasID uint64) {
	repo := repository.NewAgentRepository(e.db)
	canvas := agentsvc.NewAgentCanvasService(repo, nil)
	bridge := agentsvc.NewAgentBridge(agentsvc.BridgeDeps{Repo: repo, Agent: agentsvc.NewAgentService(agentsvc.AgentDeps{Repo: repo, Canvas: canvas}), Canvas: canvas, Drama: e.drama, DramaSegments: e.segments, DramaApprovalKey: []byte("drama-smoke-approval-secret-0123456789")})
	rc := agentsvc.RunContext{RunID: 901, SessionID: 902, UserID: 1, CanvasID: canvasID, Mode: model.AgentModeDrama}
	must(bridge.CheckCanvasMode(ctx, 1, canvasID, model.AgentModeDrama))
	for _, scope := range []string{"overview", "episode", "segments"} {
		result, err := bridge.ExecutePlatformTool(ctx, rc, "read", "drama_read", []byte(`{"scope":"`+scope+`","episode_no":1}`))
		must(err)
		require(!result.IsError, result.Content)
	}
	other := rc
	other.UserID = 2
	denied, err := bridge.ExecutePlatformTool(ctx, other, "read-other", "drama_read", []byte(`{"scope":"overview"}`))
	must(err)
	require(denied.IsError, "短剧工具泄露他人数据")
	locked, err := bridge.ProposeApproval(ctx, rc, "drama_update_episode", []byte(`{"episode_no":1,"script_md":"锁定后不能改"}`))
	must(err)
	require(locked.IsError && strings.Contains(locked.Content, "锁定"), fmt.Sprint(locked))
	dramaID, ok := idcodec.Decode(id)
	require(ok, "短剧编码错误")
	segments, err := e.segments.List(ctx, 1, dramaID, 1)
	must(err)
	args, err := json.Marshal(map[string]any{"segment_id": segments[0].ID, "prompt": "助手审批后更新的片段"})
	must(err)
	for i, change := range []struct{ kind, args string }{
		{"drama_update_outline", `{"outline_md":"助手审批后更新的大纲"}`},
		{"drama_update_episode", `{"episode_no":1,"title":"助手审批后更新的标题"}`},
		{"drama_update_segment", string(args)},
		{"drama_run_job", `{"step":"compose","episodes":[1]}`},
	} {
		approveSmokeDrama(ctx, bridge, rc, uint64(910+i), change.kind, []byte(change.args))
	}
	awaitReady(e.url, id)
	view, err := e.drama.Get(ctx, 1, dramaID)
	must(err)
	require(view.OutlineMD == "助手审批后更新的大纲" && view.Episodes[0].Title == "助手审批后更新的标题", fmt.Sprint(view))
	segments, err = e.segments.List(ctx, 1, dramaID, 1)
	must(err)
	require(segments[0].Prompt == "助手审批后更新的片段", fmt.Sprint(segments))
	bridge.EndPlatformRun(rc.RunID)
}

// approveSmokeDrama 执行真实审批三阶段，并重放批准内容证明不会重复改动或提交任务。
func approveSmokeDrama(ctx context.Context, bridge *agentsvc.AgentBridge, rc agentsvc.RunContext, approvalID uint64, kind string, args []byte) {
	proposal, err := bridge.ProposeApproval(ctx, rc, kind, args)
	must(err)
	require(!proposal.IsError && proposal.Terminate, fmt.Sprint(proposal))
	var payload map[string]any
	must(json.Unmarshal(proposal.Payload, &payload))
	require(payload["before"] != "" && payload["after"] != "", string(proposal.Payload))
	if kind == "drama_run_job" {
		require(proposal.QuoteCredits == 0 && payload["estimate"].(float64) == 0, fmt.Sprint(proposal))
	}
	outcome, err := bridge.ResolveApproval(ctx, rc, kind, proposal.Payload, model.DecideAgentApprovalReq{Decision: "approve"})
	must(err)
	for range 2 {
		result, err := bridge.ExecuteApproval(ctx, rc, kind, approvalID, "smoke-write", *outcome)
		must(err)
		require(result.Status == agentsvc.ExecExecuted, result.OutcomeText)
	}
}
