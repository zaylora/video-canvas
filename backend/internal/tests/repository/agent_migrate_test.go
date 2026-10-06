package repository_test

import (
	"strings"
	"testing"

	"video-canvas/internal/model"
)

// TestAgentTablesMigrateWithActiveRunIndex 验证 Agent 的 5 张表能迁移成功，
// 并且「每个画布同时只能有一个活跃运行」的部分唯一索引真的生效：
// 第二个活跃运行写不进去，终态的运行不占位。
func TestAgentTablesMigrateWithActiveRunIndex(t *testing.T) {
	db := isolatedDB(t, &model.AgentSession{}, &model.AgentRun{}, &model.AgentEvent{}, &model.AgentMutation{}, &model.AgentApproval{})

	// 索引定义里不能残留标签转义用的反斜杠，否则条件写错了也可能碰巧通过下面的行为测试。
	var def string
	if err := db.Raw("SELECT indexdef FROM pg_indexes WHERE indexname = 'uk_agent_runs_active' AND schemaname = current_schema()").Scan(&def).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(def, "'queued'") || !strings.Contains(def, "'waiting_input'") || strings.Contains(def, `\`) {
		t.Fatalf("部分唯一索引定义不对: %s", def)
	}

	mk := func(canvas uint64, status string) *model.AgentRun {
		return &model.AgentRun{SessionID: 1, CanvasID: canvas, UserID: 1, Status: status, Mode: "all", MaxSteps: 40}
	}
	if err := db.Create(mk(7, model.RunRunning)).Error; err != nil {
		t.Fatalf("第一个活跃运行应能写入: %v", err)
	}
	if err := db.Create(mk(7, model.RunWaitingApproval)).Error; err == nil {
		t.Fatal("同一画布的第二个活跃运行必须被唯一索引拒绝")
	}
	if err := db.Create(mk(7, model.RunSucceeded)).Error; err != nil {
		t.Fatalf("终态运行不应占位: %v", err)
	}
	if err := db.Create(mk(8, model.RunQueued)).Error; err != nil {
		t.Fatalf("不同画布互不影响: %v", err)
	}
}
