package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"gorm.io/gorm"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

func agentDB(t *testing.T) *gorm.DB {
	return isolatedDB(t, &model.CanvasProject{}, &model.AgentSession{}, &model.AgentRun{}, &model.AgentEvent{}, &model.AgentMutation{}, &model.AgentApproval{})
}

func newCanvas(t *testing.T, db *gorm.DB, userID uint64) *model.CanvasProject {
	t.Helper()
	c := &model.CanvasProject{UserID: userID, Title: "画布", PayloadJSON: []byte(`{"nodes":[]}`), Revision: 1}
	if err := db.Create(c).Error; err != nil {
		t.Fatal(err)
	}
	return c
}

func newSession(t *testing.T, r *AgentRepository, userID, canvasID uint64) *model.AgentSession {
	t.Helper()
	s := &model.AgentSession{UserID: userID, CanvasID: canvasID, Title: "新对话", Mode: model.AgentModeAll}
	if err := r.CreateSession(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	return s
}

func newRun(t *testing.T, r *AgentRepository, s *model.AgentSession, status string) *model.AgentRun {
	t.Helper()
	run := &model.AgentRun{SessionID: s.ID, CanvasID: s.CanvasID, UserID: s.UserID, Status: status, Mode: "all", MaxSteps: 40, BudgetCredits: 50}
	if err := r.CreateRun(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	return run
}

func TestAgentSessions(t *testing.T) {
	ctx := context.Background()
	db := agentDB(t)
	r := NewAgentRepository(db)
	s1 := newSession(t, r, 1, 10)
	s2 := newSession(t, r, 1, 10)
	newSession(t, r, 1, 11) // 别的画布
	newSession(t, r, 2, 10) // 别的用户

	t.Run("列表只含本人本画布，最近更新的在前，不带大字段", func(t *testing.T) {
		if err := r.UpdateSession(ctx, 1, s1.ID, map[string]any{"session_jsonl": "大段会话", "title": "改名了"}); err != nil {
			t.Fatal(err)
		}
		got, err := r.ListSessions(ctx, 1, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0].ID != s1.ID || got[1].ID != s2.ID {
			t.Fatalf("应为 [s1, s2]（s1 刚更新）: %+v", got)
		}
		if got[0].SessionJSONL != "" {
			t.Errorf("列表不应带 session_jsonl")
		}
		if n, _ := r.CountSessions(ctx, 1, 10); n != 2 {
			t.Errorf("CountSessions=%d", n)
		}
	})

	t.Run("别人的会话统一返回不存在", func(t *testing.T) {
		if _, err := r.GetSession(ctx, 2, s1.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("GetSession 应 ErrNotFound: %v", err)
		}
		if err := r.UpdateSession(ctx, 2, s1.ID, map[string]any{"title": "x"}); !errors.Is(err, ErrNotFound) {
			t.Errorf("UpdateSession 应 ErrNotFound: %v", err)
		}
		if err := r.DeleteSession(ctx, 2, s1.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("DeleteSession 应 ErrNotFound: %v", err)
		}
	})

	t.Run("软删除后查不到也不计数", func(t *testing.T) {
		if err := r.DeleteSession(ctx, 1, s2.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := r.GetSession(ctx, 1, s2.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("已删除应 ErrNotFound: %v", err)
		}
		if n, _ := r.CountSessions(ctx, 1, 10); n != 1 {
			t.Errorf("CountSessions=%d", n)
		}
	})
}

func TestAgentRuns(t *testing.T) {
	ctx := context.Background()
	db := agentDB(t)
	r := NewAgentRepository(db)
	s := newSession(t, r, 1, 10)

	run := newRun(t, r, s, model.RunRunning)

	t.Run("同一画布第二个活跃运行返回 ErrDuplicate", func(t *testing.T) {
		dup := &model.AgentRun{SessionID: s.ID, CanvasID: 10, UserID: 1, Status: model.RunQueued, MaxSteps: 40}
		if err := r.CreateRun(ctx, dup); !errors.Is(err, ErrDuplicate) {
			t.Fatalf("应 ErrDuplicate: %v", err)
		}
		active, err := r.ActiveRun(ctx, 10)
		if err != nil || active.ID != run.ID {
			t.Fatalf("ActiveRun=%+v err=%v", active, err)
		}
		if _, err := r.ActiveRun(ctx, 99); !errors.Is(err, ErrNotFound) {
			t.Errorf("没有活跃运行应 ErrNotFound: %v", err)
		}
	})

	t.Run("状态迁移是 CAS：只有当前状态在 from 里才成功", func(t *testing.T) {
		got, err := r.UpdateRunIf(ctx, run.ID, []string{model.RunRunning}, map[string]any{"status": model.RunWaitingApproval})
		if err != nil || got.Status != model.RunWaitingApproval {
			t.Fatalf("got=%+v err=%v", got, err)
		}
		// 状态已变，用旧状态再迁移应冲突。
		_, err = r.UpdateRunIf(ctx, run.ID, []string{model.RunRunning}, map[string]any{"status": model.RunSucceeded})
		if !errors.Is(err, ErrAgentStateConflict) {
			t.Errorf("应 ErrAgentStateConflict: %v", err)
		}
		if _, err := r.UpdateRunIf(ctx, 9999, []string{model.RunRunning}, map[string]any{"status": "x"}); !errors.Is(err, ErrAgentStateConflict) {
			t.Errorf("不存在的运行也是冲突: %v", err)
		}
	})

	t.Run("别人的运行查不到", func(t *testing.T) {
		if _, err := r.GetRun(ctx, 2, run.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("应 ErrNotFound: %v", err)
		}
	})

	t.Run("累加用量", func(t *testing.T) {
		if err := r.AddRunUsage(ctx, run.ID, 2, 5); err != nil {
			t.Fatal(err)
		}
		if err := r.AddRunUsage(ctx, run.ID, 1, 3); err != nil {
			t.Fatal(err)
		}
		got, _ := r.GetRun(ctx, 1, run.ID)
		if got.Steps != 3 || got.SpentCredits != 8 {
			t.Errorf("steps=%d spent=%d", got.Steps, got.SpentCredits)
		}
		if err := r.AddRunUsage(ctx, 9999, 1, 1); !errors.Is(err, ErrNotFound) {
			t.Errorf("不存在应 ErrNotFound: %v", err)
		}
	})

	t.Run("运行结束后画布空出来，可以开下一个", func(t *testing.T) {
		if _, err := r.UpdateRunIf(ctx, run.ID, []string{model.RunWaitingApproval}, map[string]any{"status": model.RunSucceeded}); err != nil {
			t.Fatal(err)
		}
		newRun(t, r, s, model.RunQueued)
	})
}

func TestAgentEventsSeqIsAtomic(t *testing.T) {
	ctx := context.Background()
	db := agentDB(t)
	r := NewAgentRepository(db)
	s := newSession(t, r, 1, 10)

	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := r.AppendEvent(ctx, s.ID, 1, "message.delta", nil); err != nil {
				t.Errorf("AppendEvent: %v", err)
			}
		}()
	}
	wg.Wait()

	evs, err := r.ListEvents(ctx, s.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != n {
		t.Fatalf("应有 %d 条事件，实际 %d", n, len(evs))
	}
	for i, e := range evs {
		if e.Seq != int64(i+1) {
			t.Fatalf("并发追加后序号必须连续无重复：第 %d 条 seq=%d", i, e.Seq)
		}
	}
	if string(evs[0].PayloadJSON) != "{}" {
		t.Errorf("空 payload 应补成 {}: %s", evs[0].PayloadJSON)
	}

	t.Run("按 after 增量读取并限制条数", func(t *testing.T) {
		got, _ := r.ListEvents(ctx, s.ID, 15, 3)
		if len(got) != 3 || got[0].Seq != 16 || got[2].Seq != 18 {
			t.Errorf("after=15 limit=3 应为 16..18: %+v", got)
		}
	})
	t.Run("会话的 last_seq 同步", func(t *testing.T) {
		got, _ := r.GetSession(ctx, 1, s.ID)
		if got.LastSeq != n {
			t.Errorf("last_seq=%d", got.LastSeq)
		}
	})
	t.Run("会话不存在返回 ErrNotFound", func(t *testing.T) {
		if _, err := r.AppendEvent(ctx, 9999, 1, "x", nil); !errors.Is(err, ErrNotFound) {
			t.Errorf("应 ErrNotFound: %v", err)
		}
	})
}

func TestCommitCanvasMutation(t *testing.T) {
	ctx := context.Background()
	db := agentDB(t)
	r := NewAgentRepository(db)
	canvas := newCanvas(t, db, 1)
	s := newSession(t, r, 1, canvas.ID)
	run := newRun(t, r, s, model.RunRunning)

	commit := func(base uint64, payload string, kind string, undoRun uint64) (*model.AgentMutation, error) {
		m := &model.AgentMutation{RunID: run.ID, Kind: kind, ToolCallID: "tc1", ChangesJSON: []byte(`[{"id":"n1"}]`)}
		err := r.CommitCanvasMutation(ctx, CommitInput{UserID: 1, CanvasID: canvas.ID, BaseRevision: base, Payload: []byte(payload), Mutation: m, UndoRunID: undoRun})
		return m, err
	}
	reload := func() *model.CanvasProject {
		var c model.CanvasProject
		db.First(&c, canvas.ID)
		return &c
	}

	t.Run("成功：画布 revision+1，改动按序记录", func(t *testing.T) {
		m1, err := commit(1, `{"nodes":[{"id":"a"}]}`, model.MutationApplyOps, 0)
		if err != nil {
			t.Fatal(err)
		}
		if m1.Seq != 1 || m1.RevisionBefore != 1 || m1.RevisionAfter != 2 {
			t.Errorf("m1=%+v", m1)
		}
		m2, err := commit(2, `{"nodes":[{"id":"a"},{"id":"b"}]}`, model.MutationArrange, 0)
		if err != nil {
			t.Fatal(err)
		}
		if m2.Seq != 2 {
			t.Errorf("同一运行内序号应递增: %d", m2.Seq)
		}
		c := reload()
		if c.Revision != 3 || !jsonEqual(c.PayloadJSON, `{"nodes":[{"id":"a"},{"id":"b"}]}`) {
			t.Errorf("画布=%+v %s", c, c.PayloadJSON)
		}
	})

	t.Run("revision 不一致：返回冲突，画布和日志都不变", func(t *testing.T) {
		before := reload()
		var cntBefore int64
		db.Model(&model.AgentMutation{}).Count(&cntBefore)

		_, err := commit(1, `{"nodes":["脏数据"]}`, model.MutationApplyOps, 0)
		if !errors.Is(err, ErrRevisionConflict) {
			t.Fatalf("应 ErrRevisionConflict: %v", err)
		}
		after := reload()
		if after.Revision != before.Revision || string(after.PayloadJSON) != string(before.PayloadJSON) {
			t.Errorf("冲突时画布不能被改: %+v", after)
		}
		var cntAfter int64
		db.Model(&model.AgentMutation{}).Count(&cntAfter)
		if cntAfter != cntBefore {
			t.Errorf("冲突时不能留下改动日志: %d → %d", cntBefore, cntAfter)
		}
	})

	t.Run("画布不属于该用户：返回不存在", func(t *testing.T) {
		m := &model.AgentMutation{RunID: run.ID, Kind: model.MutationApplyOps}
		err := r.CommitCanvasMutation(ctx, CommitInput{UserID: 2, CanvasID: canvas.ID, BaseRevision: 3, Payload: []byte(`{}`), Mutation: m})
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("应 ErrNotFound: %v", err)
		}
	})

	t.Run("撤销：同一事务里把原改动标记为已撤销，撤销自己不被标记", func(t *testing.T) {
		undo, err := commit(3, `{"nodes":[]}`, model.MutationUndo, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		muts, _ := r.ListMutations(ctx, run.ID)
		if len(muts) != 3 {
			t.Fatalf("应有 3 条改动: %d", len(muts))
		}
		for _, m := range muts {
			if m.ID == undo.ID {
				if m.UndoneAt != nil {
					t.Errorf("撤销记录自己不应被标记")
				}
				continue
			}
			if m.UndoneAt == nil {
				t.Errorf("原改动 seq=%d 应被标记为已撤销", m.Seq)
			}
		}
	})
}

func TestAgentApprovals(t *testing.T) {
	ctx := context.Background()
	db := agentDB(t)
	r := NewAgentRepository(db)
	s := newSession(t, r, 1, 10)
	run := newRun(t, r, s, model.RunWaitingApproval)

	mk := func() *model.AgentApproval {
		a := &model.AgentApproval{RunID: run.ID, SessionID: s.ID, CanvasID: 10, UserID: 1, Kind: model.ApprovalGenerate, Status: model.ApprovalPending, QuoteCredits: 16}
		if err := r.CreateApproval(ctx, a); err != nil {
			t.Fatal(err)
		}
		return a
	}
	a := mk()
	if string(a.PayloadJSON) != "{}" {
		t.Errorf("空 JSON 应补成 {}: %s", a.PayloadJSON)
	}

	t.Run("决定是 CAS：只有待处理才能决定，重复点击返回冲突", func(t *testing.T) {
		got, err := r.UpdateApprovalIf(ctx, a.ID, []string{model.ApprovalPending}, map[string]any{"status": model.ApprovalApproved})
		if err != nil || got.Status != model.ApprovalApproved {
			t.Fatalf("got=%+v err=%v", got, err)
		}
		_, err = r.UpdateApprovalIf(ctx, a.ID, []string{model.ApprovalPending}, map[string]any{"status": model.ApprovalRejected})
		if !errors.Is(err, ErrAgentStateConflict) {
			t.Errorf("第二次应冲突: %v", err)
		}
	})

	t.Run("别人的审批查不到", func(t *testing.T) {
		if _, err := r.GetApproval(ctx, 2, a.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("应 ErrNotFound: %v", err)
		}
	})

	t.Run("停止运行时只让待处理的失效", func(t *testing.T) {
		pending := mk()
		n, err := r.ExpirePendingApprovals(ctx, run.ID)
		if err != nil || n != 1 {
			t.Fatalf("n=%d err=%v", n, err)
		}
		got, _ := r.GetApproval(ctx, 1, pending.ID)
		old, _ := r.GetApproval(ctx, 1, a.ID)
		if got.Status != model.ApprovalExpired || old.Status != model.ApprovalApproved {
			t.Errorf("pending=%s old=%s", got.Status, old.Status)
		}
		list, _ := r.ListApprovals(ctx, run.ID)
		if len(list) != 2 {
			t.Errorf("ListApprovals=%d", len(list))
		}
	})
}

// 服务重启时，运行进程随旧服务一起没了：还停在 queued / running 的运行要标为中断，
// 否则那个画布会被一个永远不会结束的运行占住；等审批、等回答的运行状态在库里，不受影响。
func TestInterruptActiveRuns(t *testing.T) {
	ctx := context.Background()
	db := agentDB(t)
	r := NewAgentRepository(db)
	mk := func(canvas uint64, status string) *model.AgentRun {
		s := newSession(t, r, 1, canvas)
		return newRun(t, r, s, status)
	}
	queued, running := mk(1, model.RunQueued), mk(2, model.RunRunning)
	waitingA, waitingI := mk(3, model.RunWaitingApproval), mk(4, model.RunWaitingInput)
	done := mk(5, model.RunSucceeded)

	n, err := r.InterruptActiveRuns(ctx)
	if err != nil || n != 2 {
		t.Fatalf("应只处理 queued 和 running 两个: n=%d err=%v", n, err)
	}
	status := func(id uint64) string {
		var run model.AgentRun
		db.First(&run, id)
		return run.Status
	}
	if status(queued.ID) != model.RunInterrupted || status(running.ID) != model.RunInterrupted {
		t.Error("queued 和 running 应变为 interrupted")
	}
	if status(waitingA.ID) != model.RunWaitingApproval || status(waitingI.ID) != model.RunWaitingInput || status(done.ID) != model.RunSucceeded {
		t.Error("等审批、等回答和已结束的运行不能动")
	}
	if n, _ := r.InterruptActiveRuns(ctx); n != 0 {
		t.Errorf("重复执行是幂等的: %d", n)
	}
	// 中断之后画布空出来了，可以开新的运行
	s := newSession(t, r, 1, 1)
	newRun(t, r, s, model.RunQueued)
}
