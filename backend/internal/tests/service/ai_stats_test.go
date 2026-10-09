package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	. "video-canvas/internal/service"
	"video-canvas/internal/service/aiconfigfake"
)

// statsModels 是 AIStatsModels 的替身：返回固定的模型清单。
type statsModels struct {
	items []ConfigListItem
	err   error
}

func (m statsModels) ListConfigs(context.Context) ([]ConfigListItem, error) { return m.items, m.err }

// newStatsSvc 构造统计服务：时钟固定在上海时区的 2026-10-10 15:00，所以“今天”是 10 月 10 日。
func newStatsSvc(repo *aiconfigfake.StatsRepo, models statsModels) *AIStatsService {
	svc := NewAIStatsService(repo, models)
	loc, _ := time.LoadLocation("Asia/Shanghai")
	svc.Now = func() time.Time { return time.Date(2026, 10, 10, 15, 0, 0, 0, loc) }
	return svc
}

func TestAIStatsService_Stats_DaysMustBe7Or30(t *testing.T) {
	svc := newStatsSvc(&aiconfigfake.StatsRepo{}, statsModels{})
	for _, days := range []int{0, 1, 14, 31, -7} {
		_, err := svc.Stats(context.Background(), days)
		var e *errcode.Error
		if !errors.As(err, &e) || e.Code != errcode.ErrInvalidParams.Code {
			t.Fatalf("days=%d 应返回 10001，实际 %v", days, err)
		}
	}
}

func TestAIStatsService_Stats_DailyFilledAndOrdered(t *testing.T) {
	repo := &aiconfigfake.StatsRepo{Daily: []model.AIStatsDay{
		{Date: "2026-10-10", Succeeded: 5, Failed: 1, Other: 2},
		{Date: "2026-10-05", Succeeded: 3},
	}}
	got, err := newStatsSvc(repo, statsModels{}).Stats(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if got.Days != 7 || len(got.Daily) != 7 {
		t.Fatalf("应给出 7 天，实际 days=%d len=%d", got.Days, len(got.Daily))
	}
	if got.Daily[0].Date != "2026-10-04" || got.Daily[6].Date != "2026-10-10" {
		t.Fatalf("应从 10-04 到 10-10 升序，实际 %s … %s", got.Daily[0].Date, got.Daily[6].Date)
	}
	if got.Daily[1] != (model.AIStatsDay{Date: "2026-10-05", Succeeded: 3}) {
		t.Fatalf("10-05 应取到库里的数据：%+v", got.Daily[1])
	}
	if got.Daily[0] != (model.AIStatsDay{Date: "2026-10-04"}) {
		t.Fatalf("没有任务的日子应补 0：%+v", got.Daily[0])
	}
	if got.Daily[6] != (model.AIStatsDay{Date: "2026-10-10", Succeeded: 5, Failed: 1, Other: 2}) {
		t.Fatalf("今天应是最后一项：%+v", got.Daily[6])
	}
}

func TestAIStatsService_Stats_QueryRangeIsLocalDays(t *testing.T) {
	repo := &aiconfigfake.StatsRepo{}
	if _, err := newStatsSvc(repo, statsModels{}).Stats(context.Background(), 30); err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("Asia/Shanghai")
	wantFrom := time.Date(2026, 9, 11, 0, 0, 0, 0, loc) // 含今天共 30 天
	wantTo := time.Date(2026, 10, 11, 0, 0, 0, 0, loc)  // 不含：明天 0 点
	if !repo.From.Equal(wantFrom) || !repo.To.Equal(wantTo) {
		t.Fatalf("区间应是 [%s, %s)，实际 [%s, %s)", wantFrom, wantTo, repo.From, repo.To)
	}
	if repo.TZ != "Asia/Shanghai" {
		t.Fatalf("应按上海时区分天，实际 %q", repo.TZ)
	}
}

func TestAIStatsService_Stats_ByModelAndKind(t *testing.T) {
	repo := &aiconfigfake.StatsRepo{ModelKinds: []model.AIStatsModelKind{
		{Model: "kling-v2", Kind: "video", Count: 10},
		{Model: "img-a", Kind: "image", Count: 4},
		{Model: "img-b", Kind: "image", Count: 4},
		{Model: "gone", Kind: "video", Count: 1}, // 模型已被删除，清单里找不到
		{Model: "kling-v2", Kind: "image", Count: 2},
	}}
	models := statsModels{items: []ConfigListItem{
		{Key: "kling-v2", Label: "可灵 v2"},
		{Key: "img-a", Label: "图片 A"},
		{Key: "img-b", Label: ""}, // 没有展示名
	}}
	got, err := newStatsSvc(repo, models).Stats(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	wantModels := []model.AIStatsModelCount{
		{Model: "kling-v2", Label: "可灵 v2", Count: 12},
		{Model: "img-a", Label: "图片 A", Count: 4},
		{Model: "img-b", Label: "img-b", Count: 4}, // 没有展示名回退 key；同数量按 key 升序
		{Model: "gone", Label: "gone", Count: 1},   // 已删除回退 key
	}
	if len(got.ByModel) != len(wantModels) {
		t.Fatalf("by_model 数量不对：%+v", got.ByModel)
	}
	for i, w := range wantModels {
		if got.ByModel[i] != w {
			t.Fatalf("by_model[%d] = %+v，期望 %+v", i, got.ByModel[i], w)
		}
	}
	wantKinds := []model.AIStatsKindCount{{Kind: "video", Count: 11}, {Kind: "image", Count: 10}}
	if len(got.ByKind) != 2 || got.ByKind[0] != wantKinds[0] || got.ByKind[1] != wantKinds[1] {
		t.Fatalf("by_kind 不对：%+v", got.ByKind)
	}
}

func TestAIStatsService_Stats_EmptyIsNotNull(t *testing.T) {
	got, err := newStatsSvc(&aiconfigfake.StatsRepo{}, statsModels{}).Stats(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if got.ByModel == nil || got.ByKind == nil || len(got.ByModel) != 0 || len(got.ByKind) != 0 {
		t.Fatalf("没有任务时 by_model / by_kind 应是空数组而不是 null：%+v %+v", got.ByModel, got.ByKind)
	}
}

func TestAIStatsService_Stats_DependencyErrorsPropagate(t *testing.T) {
	boom := errors.New("boom")
	if _, err := newStatsSvc(&aiconfigfake.StatsRepo{Err: boom}, statsModels{}).Stats(context.Background(), 7); !errors.Is(err, boom) {
		t.Fatalf("仓储错误应原样向上传：%v", err)
	}
	if _, err := newStatsSvc(&aiconfigfake.StatsRepo{}, statsModels{err: boom}).Stats(context.Background(), 7); !errors.Is(err, boom) {
		t.Fatalf("模型清单错误应原样向上传：%v", err)
	}
}
