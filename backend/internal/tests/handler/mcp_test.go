package handler_test

import (
	"testing"

	"video-canvas/internal/handler/mcp"
	"video-canvas/internal/model"
	agentsvc "video-canvas/internal/service/agent"
)

// TestMCPToolFaceFullyClassified 保证每个已登记的工具都被显式分类：要么开放并标好权限档位，要么写明不开放的原因。
// 以后加了新工具却忘了分类，这条会红——否则新能力会无声无息地出现在外部接入里。
func TestMCPToolFaceFullyClassified(t *testing.T) {
	names := agentsvc.AllToolNames()
	if len(names) == 0 {
		t.Fatal("工具登记表是空的，测试没有意义")
	}
	for _, name := range names {
		scope, open := mcp.ScopeFor(name)
		_, denied := mcp.DenialReason(name)
		switch {
		case open && denied:
			t.Errorf("%s 同时出现在开放表和排除表里，二选一", name)
		case !open && !denied:
			t.Errorf("%s 既没开放也没写排除原因：新增工具必须显式分类", name)
		case open && !model.MCPScopeKnown(scope):
			t.Errorf("%s 的权限档位 %q 不认识", name, scope)
		}
	}
}

// TestMCPOpenToolsCannotBypassApproval 保证开放出去的工具不会卡在审批上，也不会绕过不该绕过的审批。
//
// 判据有两条：需要审批的工具要么不开放（删除、后台运营），要么在 externalNoApproval 里显式登记
// 「外部接入直连执行、靠权限档与余额兜底」（生成）。漏登记的后果是工具一调就报错，用户看到的是「用不了」。
func TestMCPOpenToolsCannotBypassApproval(t *testing.T) {
	for _, name := range agentsvc.AllToolNames() {
		if _, open := mcp.ScopeFor(name); !open {
			continue
		}
		if agentsvc.ToolExcludedFromPlatform(name) && !agentsvc.ToolRunsWithoutApproval(name) {
			t.Errorf("%s 需要审批才能执行，不能对外接入开放", name)
		}
		if name == "canvas_delete" {
			t.Errorf("%s 会删除画布内容，不可逆，不能对外接入开放", name)
		}
	}
	// 生成是这条规则唯一的例外，写死一遍：它必须恰好落在 canvas:generate 上，且必须登记为直连执行。
	if scope, open := mcp.ScopeFor("generate_media"); !open || scope != model.MCPScopeCanvasGenerate {
		t.Fatalf("generate_media 应开放且需要 %s，实际 scope=%q open=%v", model.MCPScopeCanvasGenerate, scope, open)
	}
	if !agentsvc.ToolRunsWithoutApproval("generate_media") {
		t.Fatal("generate_media 没登记为外部直连执行：调用时会被审批拦住")
	}
}

// TestMCPDeniedKeepsOperatorTools 保证后台运营类工具永远不出现在开放表里。
func TestMCPDeniedKeepsOperatorTools(t *testing.T) {
	for _, name := range agentsvc.AllToolNames() {
		if len(name) > 6 && name[:6] == "admin_" {
			if _, open := mcp.ScopeFor(name); open {
				t.Errorf("%s 是后台运营能力，不能对外接入开放", name)
			}
		}
	}
}
