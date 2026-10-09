package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

func convDB(t *testing.T) *gorm.DB {
	return isolatedDB(t, &model.Conversation{}, &model.ConversationRecord{})
}

func newConv(t *testing.T, r *ConversationRepository, userID uint64, title string) *model.Conversation {
	t.Helper()
	c := &model.Conversation{UserID: userID, Title: title}
	if err := r.Create(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	return c
}

func newRec(t *testing.T, r *ConversationRepository, c *model.Conversation, key string) *model.ConversationRecord {
	t.Helper()
	rec := &model.ConversationRecord{ConversationID: c.ID, UserID: c.UserID, Kind: "image", ModelKey: "m1", Prompt: "p", Count: 1, IdempotencyKey: key}
	if err := r.CreateRecord(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	return rec
}

// 列表：按最近记录时间倒序，没有记录的排在后面（时间相同按 id 倒序）；只含自己的、未删除的。
func TestConversationRepo_ListOrderAndIsolation(t *testing.T) {
	ctx := context.Background()
	r := NewConversationRepository(convDB(t))
	a := newConv(t, r, 1, "a")
	b := newConv(t, r, 1, "b")
	empty := newConv(t, r, 1, "empty")
	newConv(t, r, 2, "别人的")
	gone := newConv(t, r, 1, "gone")
	_ = r.Delete(ctx, 1, gone.ID)

	now := time.Now()
	_ = r.AddRecordCount(ctx, a.ID, 1, ptr(now.Add(-time.Hour)))
	_ = r.AddRecordCount(ctx, b.ID, 1, ptr(now))

	got, err := r.List(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, c := range got {
		titles = append(titles, c.Title)
	}
	want := []string{b.Title, a.Title, empty.Title}
	if len(titles) != len(want) {
		t.Fatalf("应只返回自己未删除的 3 段：%v", titles)
	}
	for i := range want {
		if titles[i] != want[i] {
			t.Fatalf("排序不对：%v 期望 %v", titles, want)
		}
	}
	if n, _ := r.Count(ctx, 1); n != 3 {
		t.Errorf("Count 应与列表一致：%d", n)
	}
}

func TestConversationRepo_RenameDeleteScopedToUser(t *testing.T) {
	ctx := context.Background()
	r := NewConversationRepository(convDB(t))
	c := newConv(t, r, 1, "a")

	if err := r.Rename(ctx, 2, c.ID, "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("别人改名应是 ErrNotFound：%v", err)
	}
	if err := r.Rename(ctx, 1, c.ID, "新名字"); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.GetByID(ctx, 1, c.ID); got.Title != "新名字" {
		t.Errorf("标题未更新：%+v", got)
	}
	if _, err := r.GetByID(ctx, 2, c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("别人读取应是 ErrNotFound：%v", err)
	}
	if err := r.Delete(ctx, 2, c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("别人删除应是 ErrNotFound：%v", err)
	}
	if err := r.Delete(ctx, 1, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetByID(ctx, 1, c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("软删除后读不到：%v", err)
	}
}

// 记录数：加减都生效，不会减成负数；传了时间才更新最近记录时间。
func TestConversationRepo_AddRecordCount(t *testing.T) {
	ctx := context.Background()
	r := NewConversationRepository(convDB(t))
	c := newConv(t, r, 1, "a")
	at := time.Now().Truncate(time.Second)

	_ = r.AddRecordCount(ctx, c.ID, 1, &at)
	_ = r.AddRecordCount(ctx, c.ID, 1, nil)
	got, _ := r.GetByID(ctx, 1, c.ID)
	if got.RecordCount != 2 || got.LastRecordAt == nil || !got.LastRecordAt.Equal(at) {
		t.Fatalf("加 2 且时间只被第一次更新：%+v", got)
	}
	_ = r.AddRecordCount(ctx, c.ID, -5, nil)
	if got, _ = r.GetByID(ctx, 1, c.ID); got.RecordCount != 0 {
		t.Errorf("不应减成负数：%d", got.RecordCount)
	}
	if err := r.AddRecordCount(ctx, 9999, 1, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("对话不存在应是 ErrNotFound：%v", err)
	}
}

// 幂等键：同一用户下唯一，不同用户可以相同，空键不参与唯一约束。
func TestConversationRepo_RecordIdempotency(t *testing.T) {
	ctx := context.Background()
	r := NewConversationRepository(convDB(t))
	c1 := newConv(t, r, 1, "a")
	c2 := newConv(t, r, 2, "b")
	first := newRec(t, r, c1, "k")

	dup := &model.ConversationRecord{ConversationID: c1.ID, UserID: 1, Kind: "image", ModelKey: "m", Count: 1, IdempotencyKey: "k"}
	if err := r.CreateRecord(ctx, dup); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("同用户同 key 应是 ErrDuplicate：%v", err)
	}
	newRec(t, r, c2, "k") // 别的用户可以用同一个 key
	newRec(t, r, c1, "")
	newRec(t, r, c1, "") // 空 key 可以重复

	got, err := r.FindRecordByIdem(ctx, 1, "k")
	if err != nil || got.ID != first.ID {
		t.Fatalf("按 key 应找到第一条：%+v %v", got, err)
	}
	if _, err := r.FindRecordByIdem(ctx, 2, "none"); !errors.Is(err, ErrNotFound) {
		t.Errorf("没有时应是 ErrNotFound：%v", err)
	}
}

func TestConversationRepo_ListRecordsPaging(t *testing.T) {
	ctx := context.Background()
	r := NewConversationRepository(convDB(t))
	c := newConv(t, r, 1, "a")
	other := newConv(t, r, 1, "b")
	var ids []uint64
	for i := 0; i < 5; i++ {
		ids = append(ids, newRec(t, r, c, "").ID)
	}
	newRec(t, r, other, "")
	deleted := newRec(t, r, c, "")
	_ = r.DeleteRecord(ctx, 1, deleted.ID)

	page, err := r.ListRecords(ctx, 1, c.ID, 0, 3)
	if err != nil || len(page) != 3 || page[0].ID != ids[4] || page[2].ID != ids[2] {
		t.Fatalf("第一页应是最新 3 条（id 倒序，不含别的对话和已删除的）：%+v %v", page, err)
	}
	page, _ = r.ListRecords(ctx, 1, c.ID, ids[2], 3)
	if len(page) != 2 || page[0].ID != ids[1] {
		t.Fatalf("before 之后还剩 2 条：%+v", page)
	}
	if page, _ = r.ListRecords(ctx, 2, c.ID, 0, 3); len(page) != 0 {
		t.Errorf("别的用户看不到：%+v", page)
	}
}

func TestConversationRepo_SaveRecordTasksAndDelete(t *testing.T) {
	ctx := context.Background()
	r := NewConversationRepository(convDB(t))
	c := newConv(t, r, 1, "a")
	rec := newRec(t, r, c, "")

	if err := r.SaveRecordTasks(ctx, rec.ID, []byte(`[11,null]`), []byte(`[{"index":1,"status":402,"code":40001,"message":"积分不足"}]`), 6); err != nil {
		t.Fatal(err)
	}
	got, err := r.GetRecord(ctx, 1, rec.ID)
	if err != nil || got.QuoteCredits != 6 || !jsonEqual(got.TaskIDsJSON, `[11,null]`) || !jsonEqual(got.SubmitErrorsJSON, `[{"index":1,"status":402,"code":40001,"message":"积分不足"}]`) {
		t.Fatalf("任务 id 和失败原因应原样保存：%+v %v", got, err)
	}
	if err := r.SaveRecordTasks(ctx, 9999, []byte(`[]`), []byte(`[]`), 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("记录不存在应是 ErrNotFound：%v", err)
	}
	if _, err := r.GetRecord(ctx, 2, rec.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("别人读取应是 ErrNotFound：%v", err)
	}
	if err := r.DeleteRecord(ctx, 2, rec.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("别人删除应是 ErrNotFound：%v", err)
	}
	if err := r.DeleteRecord(ctx, 1, rec.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetRecord(ctx, 1, rec.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("删除后读不到：%v", err)
	}
	// 删除后同一个幂等键不再被占用
	k1 := newRec(t, r, c, "k")
	_ = r.DeleteRecord(ctx, 1, k1.ID)
	newRec(t, r, c, "k")
}

func TestConversationRepo_ConversationIDsByRecordIDs(t *testing.T) {
	ctx := context.Background()
	r := NewConversationRepository(convDB(t))
	a, b, gone := newConv(t, r, 1, "a"), newConv(t, r, 1, "b"), newConv(t, r, 1, "gone")
	other := newConv(t, r, 2, "o")
	ra1, ra2, rb := newRec(t, r, a, ""), newRec(t, r, a, ""), newRec(t, r, b, "")
	rg, ro := newRec(t, r, gone, ""), newRec(t, r, other, "")
	_ = r.Delete(ctx, 1, gone.ID)

	got, err := r.ConversationIDsByRecordIDs(ctx, 1, []uint64{ra1.ID, ra2.ID, rb.ID, rg.ID, ro.ID})
	if err != nil {
		t.Fatal(err)
	}
	set := map[uint64]bool{}
	for _, id := range got {
		set[id] = true
	}
	if len(got) != 2 || !set[a.ID] || !set[b.ID] {
		t.Fatalf("应去重，且排除已删除的对话和别人的记录：%v", got)
	}
}

func ptr[T any](v T) *T { return &v }
