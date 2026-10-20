package repository_test

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

func traceDB(t *testing.T) *gorm.DB {
	return isolatedDB(t, &model.User{}, &model.CanvasProject{}, &model.AgentSession{}, &model.AgentRun{}, &model.AgentEvent{},
		&model.AgentModelCall{}, &model.AgentTraceSpan{})
}

func newUser(t *testing.T, db *gorm.DB, username, email string) *model.User {
	t.Helper()
	u := &model.User{Username: username, Email: email, Password: "x", Role: "user", Status: "active"}
	if err := db.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	return u
}

// 运行列表：用户邮箱包含匹配、状态、来源、时间范围，并带出画布标题和工具报错数。
func TestListAdminRuns(t *testing.T) {
	ctx := context.Background()
	db := traceDB(t)
	r := NewAgentRepository(db)
	alice := newUser(t, db, "alice", "alice@studio.cn")
	bob := newUser(t, db, "bob", "bob@other.com")
	ca := newCanvas(t, db, alice.ID)
	cb := newCanvas(t, db, bob.ID)
	sa := newSession(t, r, alice.ID, ca.ID)
	sb := newSession(t, r, bob.ID, cb.ID)
	run1 := newRun(t, r, sa, model.RunSucceeded)
	run2 := newRun(t, r, sb, model.RunFailed)
	if err := db.Model(&model.AgentRun{}).Where("id = ?", run2.ID).Update("harness_case", "storyboard-9x16").Error; err != nil {
		t.Fatal(err)
	}
	// run1 在十天前创建
	old := time.Now().Add(-10 * 24 * time.Hour)
	if err := db.Model(&model.AgentRun{}).Where("id = ?", run1.ID).Update("created_at", old).Error; err != nil {
		t.Fatal(err)
	}
	// run2 有两个工具报错和一个成功
	for i, st := range []string{"error", "error", "ok"} {
		if err := r.CreateTraceSpan(ctx, &model.AgentTraceSpan{RunID: run2.ID, Seq: i + 1, SpanID: "t" + string(rune('a'+i)), Kind: model.TraceSpanExecuteTool, Status: st, StartedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	// 一个 segment 行的 error 不算工具报错
	if err := r.CreateTraceSpan(ctx, &model.AgentTraceSpan{RunID: run2.ID, Seq: 9, SpanID: "s1", Kind: model.TraceSpanSegment, Status: "error", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	from := time.Now().Add(-30 * 24 * time.Hour)
	yes, no := true, false

	rows, total, err := r.ListAdminRuns(ctx, AdminRunFilter{From: from, Limit: 20})
	if err != nil || total != 2 || len(rows) != 2 {
		t.Fatalf("全量: rows=%d total=%d err=%v", len(rows), total, err)
	}
	if rows[0].ID != run2.ID {
		t.Errorf("创建时间倒序，最新的在前: %d", rows[0].ID)
	}
	if rows[0].UserEmail != "bob@other.com" || rows[0].CanvasTitle != "画布" || rows[0].ToolErrors != 2 {
		t.Errorf("联查字段不对: %+v", rows[0])
	}

	cases := []struct {
		name string
		f    AdminRunFilter
		want []uint64
	}{
		{"最近 7 天", AdminRunFilter{From: time.Now().Add(-7 * 24 * time.Hour)}, []uint64{run2.ID}},
		{"邮箱包含匹配（大小写不敏感）", AdminRunFilter{UserLike: "ALICE@"}, []uint64{run1.ID}},
		{"通配符按字面匹配", AdminRunFilter{UserLike: "%"}, nil},
		{"用户 id", AdminRunFilter{UserID: bob.ID}, []uint64{run2.ID}},
		{"用户 id 或邮箱满足任一", AdminRunFilter{UserID: bob.ID, UserLike: "alice"}, []uint64{run2.ID, run1.ID}},
		{"画布", AdminRunFilter{CanvasID: ca.ID}, []uint64{run1.ID}},
		{"运行 id", AdminRunFilter{RunID: run2.ID}, []uint64{run2.ID}},
		{"状态", AdminRunFilter{Status: model.RunFailed}, []uint64{run2.ID}},
		{"只看 harness", AdminRunFilter{Harness: &yes}, []uint64{run2.ID}},
		{"只看真实用户", AdminRunFilter{Harness: &no}, []uint64{run1.ID}},
		{"结束时间", AdminRunFilter{To: &old}, nil},
	}
	for _, tc := range cases {
		f := tc.f
		if f.From.IsZero() {
			f.From = from
		}
		f.Limit = 20
		rows, total, err := r.ListAdminRuns(ctx, f)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if int(total) != len(tc.want) || len(rows) != len(tc.want) {
			t.Errorf("%s: rows=%d total=%d, want %v", tc.name, len(rows), total, tc.want)
			continue
		}
		for i, id := range tc.want {
			if rows[i].ID != id {
				t.Errorf("%s: 第 %d 行是 %d，期望 %d", tc.name, i, rows[i].ID, id)
			}
		}
	}

	t.Run("分页", func(t *testing.T) {
		rows, total, err := r.ListAdminRuns(ctx, AdminRunFilter{From: from, Offset: 1, Limit: 1})
		if err != nil || total != 2 || len(rows) != 1 || rows[0].ID != run1.ID {
			t.Fatalf("第二页: rows=%v total=%d err=%v", rows, total, err)
		}
	})
}

// 序号：新片段接着已有的数，两张表（工具 / 片段、模型调用）一起算。
func TestTraceCounters(t *testing.T) {
	ctx := context.Background()
	db := traceDB(t)
	r := NewAgentRepository(db)
	if seq, seg, err := r.TraceCounters(ctx, 1); err != nil || seq != 0 || seg != 0 {
		t.Fatalf("空运行: seq=%d seg=%d err=%v", seq, seg, err)
	}
	if err := r.CreateTraceSpan(ctx, &model.AgentTraceSpan{RunID: 1, Seq: 3, SpanID: "t1", Kind: model.TraceSpanExecuteTool, Status: "ok", SegmentSeq: 1, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AgentModelCall{RunID: 1, UserID: 1, ModelKey: "m", Status: "settled", Seq: 7, SegmentSeq: 2, SpanID: "c1"}).Error; err != nil {
		t.Fatal(err)
	}
	if seq, seg, err := r.TraceCounters(ctx, 1); err != nil || seq != 7 || seg != 2 {
		t.Fatalf("两张表取最大: seq=%d seg=%d err=%v", seq, seg, err)
	}
	if seq, _, _ := r.TraceCounters(ctx, 2); seq != 0 {
		t.Errorf("别的运行不受影响: %d", seq)
	}
}

// 运行行的轨迹字段：字节数累加、完整性只会变 false、技能版本合并而不是覆盖。
func TestUpdateRunTrace(t *testing.T) {
	ctx := context.Background()
	db := traceDB(t)
	r := NewAgentRepository(db)
	s := newSession(t, r, 1, 10)
	run := newRun(t, r, s, model.RunRunning)

	if err := r.UpdateRunTrace(ctx, run.ID, RunTraceUpdate{AddBytes: 100, ToolsHash: "abcd1234abcd1234", SkillPins: map[string]uint64{"a": 1}}); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateRunTrace(ctx, run.ID, RunTraceUpdate{AddBytes: 50, Incomplete: true, SkillPins: map[string]uint64{"b": 2}}); err != nil {
		t.Fatal(err)
	}
	got, err := r.GetRunAny(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.TraceBytes != 150 || got.TraceComplete || got.ToolsHash != "abcd1234abcd1234" {
		t.Errorf("字段不对: %+v", got)
	}
	if !jsonEqual(got.SkillPins, `{"a":1,"b":2}`) {
		t.Errorf("技能版本应合并: %s", got.SkillPins)
	}
	if err := r.UpdateRunTrace(ctx, run.ID, RunTraceUpdate{}); err != nil {
		t.Errorf("没有任何要更新的字段不是错误: %v", err)
	}
	if err := r.UpdateRunTrace(ctx, 999, RunTraceUpdate{AddBytes: 1}); err != ErrNotFound {
		t.Errorf("运行不存在应返回 ErrNotFound: %v", err)
	}
	if _, err := r.GetRunAny(ctx, 999); err != ErrNotFound {
		t.Errorf("GetRunAny 不存在应返回 ErrNotFound: %v", err)
	}
}

// 新建运行的默认值：轨迹完整、没开轨迹、快照字段是空值而不是 NULL。
func TestAgentRunTraceDefaults(t *testing.T) {
	ctx := context.Background()
	db := traceDB(t)
	r := NewAgentRepository(db)
	s := newSession(t, r, 1, 10)
	run := newRun(t, r, s, model.RunRunning)
	got, err := r.GetRunAny(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.TraceComplete || got.TraceEnabled || !jsonEqual(got.SkillPins, `{}`) || !jsonEqual(got.InputJSON, `{}`) {
		t.Errorf("默认值不对: %+v", got)
	}
}

// 清理：带对象的片段行要先列出（调用方删对象），其余过期行和事件分批删。
func TestTraceCleanupQueries(t *testing.T) {
	ctx := context.Background()
	db := traceDB(t)
	r := NewAgentRepository(db)
	old := time.Now().Add(-20 * 24 * time.Hour)
	fresh := time.Now()
	mk := func(spanID, kind, key string, at time.Time) {
		t.Helper()
		if err := r.CreateTraceSpan(ctx, &model.AgentTraceSpan{RunID: 1, Seq: 1, SpanID: spanID, Kind: kind, Status: "ok", ObjectKey: key, StorageID: 5, StartedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	mk("s-old", model.TraceSpanSegment, "agent-traces/a.jsonl.gz", old)
	mk("s-old-nokey", model.TraceSpanSegment, "", old)
	mk("t-old", model.TraceSpanExecuteTool, "", old)
	mk("s-new", model.TraceSpanSegment, "agent-traces/b.jsonl.gz", fresh)

	cutoff := time.Now().Add(-14 * 24 * time.Hour)
	segs, err := r.ListExpiredTraceSegments(ctx, cutoff, 10)
	if err != nil || len(segs) != 1 || segs[0].SpanID != "s-old" || segs[0].StorageID != 5 {
		t.Fatalf("只应列出过期且带对象的片段: %+v err=%v", segs, err)
	}
	n, err := r.DeleteExpiredTraceSpans(ctx, cutoff, 10)
	if err != nil || n != 2 {
		t.Fatalf("过期的无对象行应被删（工具行和无 key 的片段行）: n=%d err=%v", n, err)
	}
	var left int64
	db.Model(&model.AgentTraceSpan{}).Where("span_id = ?", "s-old").Count(&left)
	if left != 1 {
		t.Error("带对象的过期片段行必须保留到对象删掉以后")
	}
	if n, err := r.DeleteTraceSpansByID(ctx, []uint64{segs[0].ID}); err != nil || n != 1 {
		t.Fatalf("按 id 删除: n=%d err=%v", n, err)
	}
	if n, err := r.DeleteTraceSpansByID(ctx, nil); err != nil || n != 0 {
		t.Errorf("空列表不是错误: n=%d err=%v", n, err)
	}

	// 事件：只删过期的，一次最多 limit 条
	for i := 0; i < 5; i++ {
		at := old
		if i == 4 {
			at = fresh
		}
		if err := db.Create(&model.AgentEvent{SessionID: 1, RunID: 1, Seq: int64(i + 1), Type: "x", CreatedAt: at}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if n, err := r.DeleteEventsBefore(ctx, cutoff, 3); err != nil || n != 3 {
		t.Fatalf("limit 生效: n=%d err=%v", n, err)
	}
	if n, err := r.DeleteEventsBefore(ctx, cutoff, 3); err != nil || n != 1 {
		t.Fatalf("剩下的过期事件: n=%d err=%v", n, err)
	}
	var events int64
	db.Model(&model.AgentEvent{}).Count(&events)
	if events != 1 {
		t.Errorf("没过期的事件要保留: %d", events)
	}
}

// 结算时把 chat span 的索引字段一并写回模型调用记录。
func TestSettleModelCall_WritesTraceFields(t *testing.T) {
	ctx := context.Background()
	db := isolatedDB(t, &model.UserCredit{}, &model.CreditLedger{}, &model.GenerationTask{}, &model.AgentModelCall{})
	r := NewAgentRepository(db)
	openAccount(t, db, 1, 100, 0)
	c := &model.AgentModelCall{RunID: 1, UserID: 1, ModelKey: "m", Status: model.ModelCallPending, SpanID: "c1", Seq: 3, Purpose: "chat", SegmentSeq: 1}
	if err := r.CreateModelCall(ctx, c); err != nil {
		t.Fatal(err)
	}
	out, err := r.SettleModelCall(ctx, SettleCallInput{CallID: c.ID, UserID: 1, InputTokens: 10, OutputTokens: 5, Credits: 1,
		FirstTokenMS: 120, DurationMS: 900, FinishReason: "tool_calls", UpstreamModel: "claude-up", ChannelID: "ch1"})
	if err != nil {
		t.Fatal(err)
	}
	if out.SpanID != "c1" || out.Seq != 3 || out.Purpose != "chat" || out.SegmentSeq != 1 {
		t.Errorf("开始时写的字段应保留: %+v", out)
	}
	if out.FirstTokenMS != 120 || out.DurationMS != 900 || out.FinishReason != "tool_calls" || out.UpstreamModel != "claude-up" || out.ChannelID != "ch1" {
		t.Errorf("结算时应写回轨迹字段: %+v", out)
	}
}
