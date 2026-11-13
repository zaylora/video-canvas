package repository_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
	"video-canvas/internal/service/stepfake"
	"video-canvas/internal/stepkit"
)

// 本文件是环节协议层仓储的集成测试，连 TEST_DATABASE_DSN（video_canvas_test）里的独立 schema；未设置时跳过。

// stepTestDB 迁移环节相关的全部表。
func stepTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return isolatedDB(t, &model.User{}, &model.AgentSkill{}, &model.UserCredit{}, &model.CreditLedger{},
		&model.StepBinding{}, &model.StepBindingHistory{}, &model.StepTrial{})
}

// noopEnqueuer 是不接队列的入队器，记录入队次数。
type noopEnqueuer struct{ n atomic.Int32 }

func (e *noopEnqueuer) EnqueueStepTrialTx(_ context.Context, tx *sql.Tx, _ uint64) error {
	if tx == nil {
		return errors.New("入队必须带事务")
	}
	e.n.Add(1)
	return nil
}

func newTrial(key string, status string, created time.Time) *model.StepTrial {
	return &model.StepTrial{StepKey: key, Source: "builtin", SampleKey: "s1", TextModelID: "m", Status: status, CreatedBy: 1,
		Errors: []byte("[]"), Flags: []byte("[]"), CreatedAt: created}
}

func TestStepBindingJoinsSkillName(t *testing.T) {
	db := stepTestDB(t)
	r := repository.NewStepRepository(db)
	ctx := context.Background()
	if _, err := r.GetBinding(ctx, "test.step"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("没有记录应返回 ErrNotFound：%v", err)
	}
	skill := model.AgentSkill{Name: "tvc", Title: "TVC"}
	if err := db.Create(&skill).Error; err != nil {
		t.Fatal(err)
	}
	version, trial := 2, uint64(5)
	err := r.WithTx(ctx, func(tx repository.StepTx) error {
		return tx.SaveBinding(ctx, &model.StepBinding{StepKey: "test.step", Source: "skill", SkillID: &skill.ID, SkillVersion: &version, TrialID: &trial, UpdatedBy: 3})
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := r.GetBinding(ctx, "test.step")
	if err != nil || row.SkillName != "tvc" || *row.SkillVersion != 2 || *row.TrialID != 5 || row.UpdatedBy != 3 {
		t.Fatalf("应带技能名和钉死的版本：%+v %v", row, err)
	}
	// 再次保存是覆盖（upsert）：改回内置，技能列清空
	err = r.WithTx(ctx, func(tx repository.StepTx) error {
		return tx.SaveBinding(ctx, &model.StepBinding{StepKey: "test.step", Source: "builtin", UpdatedBy: 4})
	})
	if err != nil {
		t.Fatal(err)
	}
	row, _ = r.GetBinding(ctx, "test.step")
	if row.Source != "builtin" || row.SkillID != nil || row.SkillVersion != nil || row.SkillName != "" || row.UpdatedBy != 4 {
		t.Fatalf("覆盖后应是内置：%+v", row)
	}
	all, err := r.ListBindings(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("同一环节最多一行绑定：%v %v", all, err)
	}
	// 技能被整个删除：绑定留着，技能名为空（运行时据此退回内置）
	err = r.WithTx(ctx, func(tx repository.StepTx) error {
		return tx.SaveBinding(ctx, &model.StepBinding{StepKey: "test.step", Source: "skill", SkillID: &skill.ID, SkillVersion: &version})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&skill).Error; err != nil {
		t.Fatal(err)
	}
	row, err = r.GetBinding(ctx, "test.step")
	if err != nil || row.Source != "skill" || row.SkillName != "" {
		t.Fatalf("技能删除后绑定仍在，技能名为空：%+v %v", row, err)
	}
}

func TestStepHistoryOrderAndActor(t *testing.T) {
	db := stepTestDB(t)
	r := repository.NewStepRepository(db)
	ctx := context.Background()
	u := model.User{Username: "operator", Password: "x"}
	if err := db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	for i, to := range []string{`{"source":"skill"}`, `{"source":"builtin"}`} {
		h := &model.StepBindingHistory{StepKey: "test.step", FromBinding: []byte(`{"source":"builtin"}`), ToBinding: []byte(to), ActorID: u.ID, CreatedAt: time.Now().Add(time.Duration(i) * time.Second)}
		if err := r.WithTx(ctx, func(tx repository.StepTx) error { return tx.InsertHistory(ctx, h) }); err != nil {
			t.Fatal(err)
		}
	}
	other := &model.StepBindingHistory{StepKey: "other.step", FromBinding: []byte(`{}`), ToBinding: []byte(`{}`), ActorID: 999999}
	if err := r.WithTx(ctx, func(tx repository.StepTx) error { return tx.InsertHistory(ctx, other) }); err != nil {
		t.Fatal(err)
	}
	rows, err := r.History(ctx, "test.step", 10)
	if err != nil || len(rows) != 2 {
		t.Fatalf("只返回本环节的历史：%v %v", rows, err)
	}
	if rows[0].ActorName != "operator" || !jsonEqual(rows[0].ToBinding, `{"source":"builtin"}`) {
		t.Fatalf("新的在前，带操作人用户名：%+v", rows[0])
	}
	rows, _ = r.History(ctx, "other.step", 10)
	if len(rows) != 1 || rows[0].ActorName != "" {
		t.Fatalf("操作人不存在时用户名为空：%+v", rows)
	}
	if limited, _ := r.History(ctx, "test.step", 1); len(limited) != 1 {
		t.Fatal("limit 应生效")
	}
}

func TestStepStartTrialStateMachine(t *testing.T) {
	db := stepTestDB(t)
	r := repository.NewStepRepository(db)
	ctx := context.Background()
	tr := newTrial("test.step", model.StepTrialQueued, time.Now())
	if err := db.Create(tr).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 { // queued → running；running 允许再次进入（job 被救回重试）
		got, err := r.StartTrial(ctx, tr.ID)
		if err != nil || got.Status != model.StepTrialRunning {
			t.Fatalf("%+v %v", got, err)
		}
	}
	db.Model(&model.StepTrial{}).Where("id = ?", tr.ID).Update("status", model.StepTrialSucceeded)
	if _, err := r.StartTrial(ctx, tr.ID); !errors.Is(err, repository.ErrStateConflict) {
		t.Fatalf("已结束的试跑不能再开始：%v", err)
	}
	if _, err := r.StartTrial(ctx, 987654); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("不存在：%v", err)
	}
}

func TestStepLastTrialsPicksLatestFinishedPerStep(t *testing.T) {
	db := stepTestDB(t)
	r := repository.NewStepRepository(db)
	now := time.Now()
	rows := []*model.StepTrial{
		newTrial("a.step", model.StepTrialSucceeded, now.Add(-3*time.Hour)),
		newTrial("a.step", model.StepTrialFailed, now.Add(-2*time.Hour)),
		newTrial("a.step", model.StepTrialRunning, now.Add(-time.Hour)), // 进行中的不算
		newTrial("b.step", model.StepTrialSucceeded, now.Add(-time.Hour)),
	}
	for _, tr := range rows {
		if err := db.Create(tr).Error; err != nil {
			t.Fatal(err)
		}
	}
	got, err := r.LastTrials(context.Background())
	if err != nil || len(got) != 2 {
		t.Fatalf("%v %v", got, err)
	}
	if got["a.step"].ID != rows[1].ID || got["b.step"].ID != rows[3].ID {
		t.Fatalf("应取每个环节最近一次已结束的试跑：%+v", got)
	}
}

// 流水以 (step_trial_id, type) 唯一：同一次试跑的冻结 / 结算 / 退款各最多一条，重复写返回 inserted=false。
func TestStepLedgerIdempotentPerTrial(t *testing.T) {
	db := stepTestDB(t)
	r := repository.NewStepRepository(db)
	ctx := context.Background()
	id := uint64(11)
	insert := func(typ string, amount int) bool {
		var ok bool
		err := r.WithTx(ctx, func(tx repository.StepTx) error {
			var e error
			ok, e = tx.InsertLedger(ctx, &model.CreditLedger{UserID: 1, StepTrialID: &id, Type: typ, Amount: amount})
			return e
		})
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !insert(model.LedgerFreeze, 4) || insert(model.LedgerFreeze, 4) {
		t.Fatal("冻结流水应只写入一次")
	}
	if !insert(model.LedgerSettle, 2) || !insert(model.LedgerRefund, 2) || insert(model.LedgerSettle, 2) {
		t.Fatal("结算和退款各一次")
	}
	var n int64
	db.Model(&model.CreditLedger{}).Where("step_trial_id = ?", id).Count(&n)
	if n != 3 {
		t.Fatalf("应有 3 条流水：%d", n)
	}
}

// 环节锁串行化「检查进行中 + 写入」：并发发起时只有一个事务能写入，其余看到进行中。
func TestStepLockSerializesActiveTrialCheck(t *testing.T) {
	db := stepTestDB(t)
	r := repository.NewStepRepository(db)
	ctx := context.Background()
	var inserted, blocked atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := r.WithTx(ctx, func(tx repository.StepTx) error {
				if err := tx.LockStep(ctx, "test.step"); err != nil {
					return err
				}
				n, err := tx.ActiveTrials(ctx, "test.step", time.Now().Add(-time.Hour))
				if err != nil {
					return err
				}
				if n > 0 {
					blocked.Add(1)
					return nil
				}
				time.Sleep(50 * time.Millisecond) // 放大检查与写入之间的窗口
				if err := tx.InsertTrial(ctx, newTrial("test.step", model.StepTrialQueued, time.Now())); err != nil {
					return err
				}
				inserted.Add(1)
				return nil
			})
			if err != nil {
				t.Errorf("事务失败：%v", err)
			}
		}()
	}
	wg.Wait()
	if inserted.Load() != 1 || blocked.Load() != 7 {
		t.Fatalf("应只有 1 个写入、7 个被挡住：inserted=%d blocked=%d", inserted.Load(), blocked.Load())
	}
}

// 用真实仓储跑服务：并发发起试跑时只有一个成功，其余返回 68017；成功的那个冻结了积分并入队。
func TestStepServiceConcurrentTrialsRejectedWith68017(t *testing.T) {
	db := stepTestDB(t)
	repo := repository.NewStepRepository(db)
	enq := &noopEnqueuer{}
	repo.SetEnqueuer(enq)
	reg := stepkit.NewRegistry()
	reg.MustRegister(integrationStep())
	if err := db.Create(&model.UserCredit{UserID: 1, Balance: 100}).Error; err != nil {
		t.Fatal(err)
	}
	svc := service.NewStepAdminService(service.StepAdminDeps{
		Registry: reg, Repo: repo, Skills: stepfake.NewLibrary(), Models: stepfake.NewModels(), Credits: stepfake.Policy{Initial: 50},
		Caller: &stepfake.Caller{},
	})
	req := &model.CreateStepTrialReq{Candidate: model.StepCandidateReq{Source: "builtin"}, SampleKey: "s1", TextModel: "text-a"}
	var ok, busy atomic.Int32
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.CreateTrials(context.Background(), 1, "test.step", req)
			var e *errcode.Error
			switch {
			case err == nil:
				ok.Add(1)
			case errors.As(err, &e) && e.Code == 68017:
				busy.Add(1)
			default:
				t.Errorf("意外错误：%v", err)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 || busy.Load() != 5 {
		t.Fatalf("应 1 成功 5 个 68017：ok=%d busy=%d", ok.Load(), busy.Load())
	}
	var c model.UserCredit
	db.First(&c, 1)
	if c.Frozen != 4 || enq.n.Load() != 1 {
		t.Fatalf("只应冻结并入队一次：frozen=%d enqueued=%d", c.Frozen, enq.n.Load())
	}
	var ledger int64
	db.Model(&model.CreditLedger{}).Where("step_trial_id IS NOT NULL AND type = ?", model.LedgerFreeze).Count(&ledger)
	if ledger != 1 {
		t.Fatalf("冻结流水应关联试跑且只有一条：%d", ledger)
	}
}

// integrationStep 是集成测试用的环节：永远通过校验，带样例 s1。
func integrationStep() stepkit.Step {
	return stepkit.Step{
		Key: "test.step", Title: "测试", Studio: "test", Duty: "写。", Media: stepkit.MediaNone, Builtin: "内置",
		Schema:   []byte(`{"type":"object"}`),
		Samples:  []stepkit.Sample{{Key: "s1", Name: "样例", Build: func(context.Context) (stepkit.Input, error) { return stepkit.Input{}, nil }}},
		Validate: func(json.RawMessage, stepkit.Input) (stepkit.Flags, error) { return nil, nil },
	}
}
