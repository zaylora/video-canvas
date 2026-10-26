package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

func agentDB(t *testing.T) *gorm.DB {
	return isolatedDB(t, &model.CanvasProject{}, &model.AgentSession{}, &model.AgentRun{}, &model.AgentEvent{}, &model.AgentMutation{}, &model.AgentApproval{}, &model.AgentCanvasRead{})
}

func newCanvas(t *testing.T, db *gorm.DB, userID uint64) *model.CanvasProject {
	t.Helper()
	c := &model.CanvasProject{UserID: userID, Title: "画布", PayloadJSON: []byte(`{"nodes":[]}`), Revision: 1}
	if err := db.Create(c).Error; err != nil {
		t.Fatal(err)
	}
	return c
}

func newSession(t *testing.T, db *gorm.DB, userID, canvasID uint64) *model.AgentSession {
	t.Helper()
	s := &model.AgentSession{UserID: userID, CanvasID: canvasID, Title: "新对话", Mode: model.AgentModeAll}
	if err := db.Create(s).Error; err != nil {
		t.Fatal(err)
	}
	return s
}

func newRun(t *testing.T, db *gorm.DB, s *model.AgentSession, status string) *model.AgentRun {
	t.Helper()
	run := &model.AgentRun{SessionID: s.ID, CanvasID: s.CanvasID, UserID: s.UserID, Status: status, Mode: "all", MaxSteps: 40, BudgetCredits: 50}
	if err := db.Create(run).Error; err != nil {
		t.Fatal(err)
	}
	return run
}

func TestCommitCanvasMutation(t *testing.T) {
	ctx := context.Background()
	db := agentDB(t)
	r := NewAgentRepository(db)
	canvas := newCanvas(t, db, 1)
	s := newSession(t, db, 1, canvas.ID)
	run := newRun(t, db, s, model.RunRunning)

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

// 服务重启时，运行进程随旧服务一起没了：还停在 queued / running 的运行要标为中断，
// 否则那个画布会被一个永远不会结束的运行占住；等审批、等回答的运行状态在库里，不受影响。
func TestInterruptActiveRuns(t *testing.T) {
	ctx := context.Background()
	db := agentDB(t)
	r := NewAgentRepository(db)
	mk := func(canvas uint64, status string) *model.AgentRun {
		s := newSession(t, db, 1, canvas)
		return newRun(t, db, s, status)
	}
	queued, running := mk(1, model.RunQueued), mk(2, model.RunRunning)
	waitingA, waitingI := mk(3, model.RunWaitingApproval), mk(4, model.RunWaitingInput)
	done := mk(5, model.RunSucceeded)

	n, err := r.InterruptActiveRuns(ctx)
	if err != nil || n != 4 {
		t.Fatalf("应处理排队、运行中、等审批、等回答四个（切换到独立 Agent 服务之前留下的旧运行，没有人会再推进）: n=%d err=%v", n, err)
	}
	status := func(id uint64) string {
		var run model.AgentRun
		db.First(&run, id)
		return run.Status
	}
	for _, id := range []uint64{queued.ID, running.ID, waitingA.ID, waitingI.ID} {
		if status(id) != model.RunInterrupted {
			t.Errorf("活跃的旧运行应变为 interrupted: %d 是 %s", id, status(id))
		}
	}
	if status(done.ID) != model.RunSucceeded {
		t.Error("已结束的运行不能动")
	}
	if n, _ := r.InterruptActiveRuns(ctx); n != 0 {
		t.Errorf("重复执行是幂等的: %d", n)
	}
}

// errTraceLogger 记录 GORM 当作错误打出来的语句，用来确认预期内的冲突没有被记成 ERROR。
type errTraceLogger struct {
	level gormlogger.LogLevel
	mu    *sync.Mutex
	errs  *[]error
}

func (l errTraceLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	l.level = level
	return l
}
func (l errTraceLogger) Info(context.Context, string, ...any)  {}
func (l errTraceLogger) Warn(context.Context, string, ...any)  {}
func (l errTraceLogger) Error(context.Context, string, ...any) {}
func (l errTraceLogger) Trace(_ context.Context, _ time.Time, _ func() (string, int64), err error) {
	if err != nil && l.level >= gormlogger.Error {
		l.mu.Lock()
		*l.errs = append(*l.errs, err)
		l.mu.Unlock()
	}
}
