package queue_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

// stubTrials 是环节试跑执行器的桩：记录收到的试跑 id；fail 非空时返回它，模拟基础设施错误。
type stubTrials struct {
	mu   sync.Mutex
	ids  []uint64
	fail error
}

func (s *stubTrials) HandleStepTrial(_ context.Context, id uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	s.ids = append(s.ids, id)
	return nil
}

func (s *stubTrials) seen() []uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]uint64(nil), s.ids...)
}

// newStepRepo 建一个接上队列运行时的环节仓储（和 initialize 里的装配一致）。
func newStepRepo(t *testing.T, r *rig) *repository.StepRepository {
	t.Helper()
	repo := repository.NewStepRepository(r.db)
	repo.SetEnqueuer(r.rt)
	return repo
}

func insertTrialAndEnqueue(ctx context.Context, tx repository.StepTx, key string) (uint64, error) {
	tr := &model.StepTrial{StepKey: key, Source: "builtin", SampleKey: "s", TextModelID: "m", Status: model.StepTrialQueued, CreatedBy: 1,
		Errors: []byte("[]"), Flags: []byte("[]"), CreatedAt: time.Now()}
	if err := tx.InsertTrial(ctx, tr); err != nil {
		return 0, err
	}
	return tr.ID, tx.EnqueueTrial(ctx, tr.ID)
}

// 试跑行和 job 在同一个事务里提交：提交后 worker 收到该试跑 id。
func TestStepTrialJobRunsAfterCommit(t *testing.T) {
	db := queueDB(t)
	stub := &stubTrials{}
	r := newRig(t, db, rigOpts{stepTrial: stub})
	repo := newStepRepo(t, r)
	var id uint64
	err := repo.WithTx(context.Background(), func(tx repository.StepTx) error {
		var err error
		id, err = insertTrialAndEnqueue(context.Background(), tx, "test.step")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "试跑 job 被执行", func() bool { return len(stub.seen()) == 1 })
	if got := stub.seen()[0]; got != id {
		t.Fatalf("worker 收到的试跑 id 不对：%d != %d", got, id)
	}
}

// 事务回滚时试跑行和 job 一起消失，worker 不会收到任何东西。
func TestStepTrialJobRollsBackWithTransaction(t *testing.T) {
	db := queueDB(t)
	stub := &stubTrials{}
	r := newRig(t, db, rigOpts{stepTrial: stub})
	repo := newStepRepo(t, r)
	boom := errors.New("回滚")
	err := repo.WithTx(context.Background(), func(tx repository.StepTx) error {
		if _, err := insertTrialAndEnqueue(context.Background(), tx, "test.step"); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("%v", err)
	}
	time.Sleep(1500 * time.Millisecond) // River 拉取间隔 200ms：给它足够时间证明「没有 job」
	if n := len(stub.seen()); n != 0 {
		t.Fatalf("回滚后不应有 job：%d", n)
	}
	var cnt int64
	db.Model(&model.StepTrial{}).Count(&cnt)
	if cnt != 0 {
		t.Fatalf("回滚后不应有试跑行：%d", cnt)
	}
}

// 没有注册试跑执行器时入队报错，而不是悄悄丢任务。
func TestStepTrialEnqueueWithoutHandlerFails(t *testing.T) {
	db := queueDB(t)
	r := newRig(t, db, rigOpts{noStart: true})
	repo := newStepRepo(t, r)
	err := repo.WithTx(context.Background(), func(tx repository.StepTx) error {
		_, err := insertTrialAndEnqueue(context.Background(), tx, "test.step")
		return err
	})
	if err == nil {
		t.Fatal("没有执行器时入队应失败")
	}
}
