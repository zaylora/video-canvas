package queue_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

// stubMarketing 记录收到的轮次 id。
type stubMarketing struct {
	mu  sync.Mutex
	ids []uint64
}

func (s *stubMarketing) HandleMarketingRound(_ context.Context, id uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ids = append(s.ids, id)
	return nil
}

func (s *stubMarketing) seen() []uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]uint64(nil), s.ids...)
}

// 轮次行与 job 在同一事务提交后，worker 收到该轮次 id。
func TestMarketingRoundJobRunsAfterCommit(t *testing.T) {
	db := queueDB(t)
	if err := db.AutoMigrate(&model.MarketingProject{}, &model.MarketingRound{}); err != nil {
		t.Fatal(err)
	}
	stub := &stubMarketing{}
	r := newRig(t, db, rigOpts{marketing: stub})
	repo := repository.NewMarketingRepository(db)
	repo.SetEnqueuer(r.rt)
	var id uint64
	err := repo.WithTx(context.Background(), func(tx repository.MarketingTx) error {
		row := &model.MarketingRound{UserID: 1, ProjectID: 1, Status: model.MarketingRoundScripting, Settings: []byte("{}"), Warnings: []byte("[]")}
		if err := tx.InsertRound(context.Background(), row); err != nil {
			return err
		}
		id = row.ID
		return tx.EnqueueRound(context.Background(), row.ID)
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "写脚本 job 被执行", func() bool { return len(stub.seen()) == 1 })
	if stub.seen()[0] != id {
		t.Fatalf("worker 收到的轮次 id 不对：%d != %d", stub.seen()[0], id)
	}
}

// 事务回滚时轮次与 job 一起消失。
func TestMarketingRoundJobRollsBackWithTransaction(t *testing.T) {
	db := queueDB(t)
	if err := db.AutoMigrate(&model.MarketingProject{}, &model.MarketingRound{}); err != nil {
		t.Fatal(err)
	}
	stub := &stubMarketing{}
	r := newRig(t, db, rigOpts{marketing: stub})
	repo := repository.NewMarketingRepository(db)
	repo.SetEnqueuer(r.rt)
	_ = repo.WithTx(context.Background(), func(tx repository.MarketingTx) error {
		row := &model.MarketingRound{UserID: 1, ProjectID: 1, Settings: []byte("{}"), Warnings: []byte("[]")}
		_ = tx.InsertRound(context.Background(), row)
		_ = tx.EnqueueRound(context.Background(), row.ID)
		return context.Canceled
	})
	time.Sleep(1500 * time.Millisecond)
	if len(stub.seen()) != 0 {
		t.Fatalf("回滚后不应有 job：%v", stub.seen())
	}
}
