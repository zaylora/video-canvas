package repository_test

import (
	"context"
	"testing"
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

func TestAdminRecentRequests_UserChannelModelTimeAndLimit(t *testing.T) {
	db := isolatedDB(t, &model.GenerationTask{})
	now := time.Now().UTC()
	for _, tc := range []struct {
		id, user               uint64
		channel, model, status string
		age                    time.Duration
	}{
		{1, 7, "main", "m", model.TaskFailed, time.Minute},
		{2, 8, "main", "m", model.TaskFailed, time.Minute},
		{3, 7, "other", "m", model.TaskFailed, time.Minute},
		{4, 7, "main", "m", model.TaskFailed, 48 * time.Hour},
		{5, 7, "main", "m", model.TaskSucceeded, time.Minute},
		{6, 7, "main", "other", model.TaskFailed, time.Minute},
	} {
		row := model.GenerationTask{ID: tc.id, UserID: tc.user, ModelKey: tc.model, Provider: "test", Status: tc.status, InputJSON: datatypes.JSON(`{}`), ConfigSnapshot: datatypes.JSON(`{"channel":{"key":"` + tc.channel + `"}}`), CreatedAt: now.Add(-tc.age)}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	r := repository.NewAdminAssistantRepository(db)
	filter := repository.AdminRequestFilter{UserID: 7, ModelKey: "m", ChannelKey: "main", Since: now.Add(-24 * time.Hour), Limit: 3}
	rows, err := r.RecentRequests(context.Background(), filter)
	if err != nil || len(rows) != 1 || rows[0].ID != 1 || rows[0].ChannelKey != "main" {
		t.Fatalf("跨用户/时间/状态过滤错误：%+v %v", rows, err)
	}
	filter.TaskID = 2
	rows, err = r.RecentRequests(context.Background(), filter)
	if err != nil || len(rows) != 0 {
		t.Fatalf("指定别人的任务也不能读取：%+v %v", rows, err)
	}
	filter.TaskID = 0
	filter.ChannelKey = ""
	filter.ModelKey = ""
	filter.Limit = 1
	rows, err = r.RecentRequests(context.Background(), filter)
	if err != nil || len(rows) != 1 {
		t.Fatalf("数量限制失效：%+v %v", rows, err)
	}
}
