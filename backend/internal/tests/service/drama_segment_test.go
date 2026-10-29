package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/service"
	"video-canvas/internal/service/dramasegmentfake"
)

func segmentService(r *dramasegmentfake.Repo) *service.DramaSegmentService {
	return service.NewDramaSegmentService(service.DramaSegmentDeps{Repo: r, Tasks: r, Assets: r, Broadcaster: r})
}

func segmentCode(t *testing.T, err error, code int) {
	t.Helper()
	var business *errcode.Error
	if !errors.As(err, &business) || business.Code != code {
		t.Fatalf("期望错误码%d，实际%v", code, err)
	}
}

func TestDramaSegmentService_CRUDAndOrder(t *testing.T) {
	ctx := context.Background()
	r := dramasegmentfake.New()
	s := segmentService(r)
	first, err := s.Create(ctx, 1, 7, 1, &model.CreateDramaSegmentReq{})
	if err != nil || first.Seq != 0 || first.Revision != 1 {
		t.Fatalf("新增默认片段：%+v %v", first, err)
	}
	after := 0
	second, err := s.Create(ctx, 1, 7, 1, &model.CreateDramaSegmentReq{AfterSeq: &after})
	if err != nil || second.Seq != 1 {
		t.Fatalf("插入片段：%+v %v", second, err)
	}
	_, err = s.Order(ctx, 1, 7, 1, &model.OrderDramaSegmentsReq{IDs: []idcodec.ID{first.ID, first.ID}})
	segmentCode(t, err, 67012)
	views, err := s.Order(ctx, 1, 7, 1, &model.OrderDramaSegmentsReq{IDs: []idcodec.ID{second.ID, first.ID}})
	if err != nil || views[0].ID != second.ID || views[1].Seq != 1 {
		t.Fatalf("全量重排：%+v %v", views, err)
	}
	prompt := "修改"
	_, err = s.Update(ctx, 1, uint64(first.ID), &model.UpdateDramaSegmentReq{Revision: 1, Prompt: &prompt})
	segmentCode(t, err, errcode.ErrCanvasConflict.Code)
	view, err := s.Update(ctx, 1, uint64(first.ID), &model.UpdateDramaSegmentReq{Revision: views[1].Revision, Prompt: &prompt})
	if err != nil || view.Prompt != prompt {
		t.Fatalf("更新片段：%+v %v", view, err)
	}
	_, err = s.Update(ctx, 2, uint64(first.ID), &model.UpdateDramaSegmentReq{Revision: view.Revision})
	segmentCode(t, err, 67009)
	if err := s.Delete(ctx, 1, uint64(first.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Generate(ctx, 1, uint64(first.ID), "", &model.GenerateDramaSegmentReq{ModelID: "video", Input: map[string]any{}}); err == nil {
		t.Fatal("删除后不可生成")
	}
}

func TestDramaSegmentService_TakesReconcileAndStatus(t *testing.T) {
	ctx := context.Background()
	r := dramasegmentfake.New()
	s := segmentService(r)
	v, err := s.Create(ctx, 1, 7, 1, &model.CreateDramaSegmentReq{})
	if err != nil {
		t.Fatal(err)
	}
	node := "seg:" + idcodec.Encode(uint64(v.ID))
	canvas := uint64(9)
	output, _ := json.Marshal([]model.TaskOutput{{AssetID: 41, MediaType: "video"}})
	r.Tasks = []model.GenerationTask{
		{ID: 21, UserID: 1, CanvasProjectID: &canvas, NodeID: node, Kind: "video", Status: "succeeded", OutputJSON: output, CreatedAt: time.Unix(1, 0)},
		{ID: 22, UserID: 1, CanvasProjectID: &canvas, NodeID: node, Kind: "video", Status: "running", CreatedAt: time.Unix(2, 0)},
		{ID: 23, UserID: 2, CanvasProjectID: &canvas, NodeID: node, Kind: "video", Status: "succeeded"},
	}
	items, err := s.List(ctx, 1, 7, 1)
	if err != nil || len(items[0].Takes) != 2 || items[0].ChosenTaskID != idcodec.Encode(21) || items[0].Status != "running" || items[0].Takes[0].Asset.ID != 41 {
		t.Fatalf("从真实任务恢复采用并优先最新状态：%+v %v", items, err)
	}
	bad := idcodec.ID(22)
	_, err = s.Update(ctx, 1, uint64(v.ID), &model.UpdateDramaSegmentReq{Revision: items[0].Revision, ChosenTaskID: &bad})
	segmentCode(t, err, 67011)
	r.Tasks[1].Status = "failed"
	if err := s.OnTaskChanged(ctx, &r.Tasks[1]); err != nil {
		t.Fatal(err)
	}
	if r.Messages[len(r.Messages)-1].Type != "drama.segment" {
		t.Fatalf("未推送片段状态：%+v", r.Messages)
	}
	items, err = s.List(ctx, 1, 7, 1)
	if err != nil || items[0].Status != "error" {
		t.Fatalf("失败take优先：%+v %v", items, err)
	}
	// 历史遗留的首帧任务（segkf: 节点）不再触发片段更新，也不出现在片段视图里。
	kf, _ := json.Marshal([]model.TaskOutput{{AssetID: 52, MediaType: "image"}})
	r.Tasks = append(r.Tasks, model.GenerationTask{ID: 24, UserID: 1, CanvasProjectID: &canvas, NodeID: "segkf:" + idcodec.Encode(uint64(v.ID)), Kind: "image", Status: "succeeded", OutputJSON: kf, CreatedAt: time.Unix(3, 0)})
	messages := len(r.Messages)
	if err := s.OnTaskChanged(ctx, &r.Tasks[len(r.Tasks)-1]); err != nil {
		t.Fatal(err)
	}
	if len(r.Messages) != messages {
		t.Fatalf("首帧任务不该再推送片段事件：%+v", r.Messages[messages:])
	}
	if err := s.Delete(ctx, 1, uint64(v.ID)); err != nil || len(r.Tasks) != 4 {
		t.Fatalf("删除保留生成任务：%v %+v", err, r.Tasks)
	}
}

func TestDramaSegmentService_GenerationDelegatesAndErrors(t *testing.T) {
	ctx := context.Background()
	r := dramasegmentfake.New()
	s := segmentService(r)
	v, err := s.Create(ctx, 1, 7, 1, &model.CreateDramaSegmentReq{})
	if err != nil {
		t.Fatal(err)
	}
	req := &model.GenerateDramaSegmentReq{ModelID: "model", Input: map[string]any{"prompt": "内容"}}
	if _, err := s.Generate(ctx, 1, uint64(v.ID), "same-request", req); err != nil {
		t.Fatal(err)
	}
	if r.Created.Kind != "video" || r.Created.CanvasID != 9 || r.Created.NodeID != "seg:"+idcodec.Encode(uint64(v.ID)) || r.IdempotencyKey != "same-request" {
		t.Fatalf("生成参数错误：%+v key=%s", r.Created, r.IdempotencyKey)
	}
	r.CreateError = &model.TaskItemError{Status: 402, Code: 42001, Message: "积分不足"}
	_, err = s.Generate(ctx, 1, uint64(v.ID), "new-request", req)
	segmentCode(t, err, 42001)
	r.ProjectDeleted = true
	_, err = s.Generate(ctx, 1, uint64(v.ID), "gone", req)
	segmentCode(t, err, 67009)
}

func TestDramaSegmentService_OrderRejectsMissingAndForeignIDs(t *testing.T) {
	ctx := context.Background()
	for _, ids := range [][]idcodec.ID{{}, {999}, {1, 999}} {
		r := dramasegmentfake.New()
		s := segmentService(r)
		if _, err := s.Create(ctx, 1, 7, 1, &model.CreateDramaSegmentReq{}); err != nil {
			t.Fatal(err)
		}
		_, err := s.Order(ctx, 1, 7, 1, &model.OrderDramaSegmentsReq{IDs: ids})
		segmentCode(t, err, 67012)
		rows, err := s.List(ctx, 1, 7, 1)
		if err != nil || len(rows) != 1 || rows[0].Seq != 0 || rows[0].Revision != 1 {
			t.Fatalf("非法全量集合不可部分写入：%+v %v", rows, err)
		}
	}
}

func TestDramaSegmentService_ManualChoiceSurvivesLaterSuccess(t *testing.T) {
	ctx := context.Background()
	r := dramasegmentfake.New()
	s := segmentService(r)
	v, err := s.Create(ctx, 1, 7, 1, &model.CreateDramaSegmentReq{})
	if err != nil {
		t.Fatal(err)
	}
	canvas := uint64(9)
	node := "seg:" + idcodec.Encode(uint64(v.ID))
	r.Tasks = []model.GenerationTask{
		{ID: 21, UserID: 1, CanvasProjectID: &canvas, NodeID: node, Kind: "video", Status: "succeeded", CreatedAt: time.Unix(1, 0)},
		{ID: 22, UserID: 1, CanvasProjectID: &canvas, NodeID: node, Kind: "video", Status: "succeeded", CreatedAt: time.Unix(2, 0)},
	}
	views, err := s.List(ctx, 1, 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	chosen := idcodec.ID(22)
	selected, err := s.Update(ctx, 1, uint64(v.ID), &model.UpdateDramaSegmentReq{Revision: views[0].Revision, ChosenTaskID: &chosen})
	if err != nil || selected.ChosenTaskID != idcodec.Encode(22) {
		t.Fatalf("手动采用成功结果：%+v %v", selected, err)
	}
	r.Tasks = append(r.Tasks, model.GenerationTask{ID: 23, UserID: 1, CanvasProjectID: &canvas, NodeID: node, Kind: "video", Status: "succeeded", CreatedAt: time.Unix(3, 0)})
	if err := s.OnTaskChanged(ctx, &r.Tasks[2]); err != nil {
		t.Fatal(err)
	}
	// 之后又有新的成功结果（23），已经手选的 22 不被覆盖。
	views, err = s.List(ctx, 1, 7, 1)
	if err != nil || views[0].ChosenTaskID != idcodec.Encode(22) {
		t.Fatalf("手选保留：%+v %v", views, err)
	}
	count, seconds, done, err := s.Summary(ctx, 1, 10)
	if err != nil || count != 1 || seconds != 5 || done != 1 {
		t.Fatalf("分集摘要：%d/%d/%d %v", count, seconds, done, err)
	}
}

func TestDramaSegmentService_ObserverIsolationAndUnknownErrors(t *testing.T) {
	ctx := context.Background()
	r := dramasegmentfake.New()
	s := segmentService(r)
	v, err := s.Create(ctx, 1, 7, 1, &model.CreateDramaSegmentReq{})
	if err != nil {
		t.Fatal(err)
	}
	canvas, other := uint64(9), uint64(99)
	node := "seg:" + idcodec.Encode(uint64(v.ID))
	r.Tasks = []model.GenerationTask{
		{ID: 1, UserID: 1, CanvasProjectID: &canvas, NodeID: node, Kind: "video", Status: "succeeded", IsTest: true},
		{ID: 2, UserID: 1, CanvasProjectID: &other, NodeID: node, Kind: "video", Status: "succeeded"},
		{ID: 3, UserID: 2, CanvasProjectID: &canvas, NodeID: node, Kind: "video", Status: "succeeded"},
	}
	before := len(r.Messages)
	for i := range r.Tasks {
		if err := s.OnTaskChanged(ctx, &r.Tasks[i]); err != nil {
			t.Fatal(err)
		}
	}
	views, err := s.List(ctx, 1, 7, 1)
	if err != nil || len(views[0].Takes) != 0 || views[0].ChosenTaskID != "" || len(r.Messages) != before {
		t.Fatalf("试跑、他人、错误画布不得污染片段：%+v %v", views, err)
	}
	want := errors.New("存储不可用")
	r.Err = want
	if _, err := s.List(ctx, 1, 7, 1); !errors.Is(err, want) {
		t.Fatalf("未知错误必须保留：%v", err)
	}
}
