package service_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// TestDramaMediaIntegration_ConcurrentSegments 验证片段任务插入和项目互斥在同一真实事务，两个并发请求只能接受一个。
func TestDramaMediaIntegration_ConcurrentSegments(t *testing.T) {
	e := newGTIEnv(t, defaultTaskLimits())
	if err := e.db.AutoMigrate(&model.DramaProject{}, &model.DramaEpisode{}, &model.DramaSegment{}, &model.DramaJob{}); err != nil {
		t.Fatal(err)
	}
	p := model.DramaProject{UserID: e.user, CanvasID: 7, Title: "并行测试", SourceText: "原文", StyleKey: "custom"}
	if err := e.db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	ep := model.DramaEpisode{UserID: e.user, DramaID: p.ID, No: 1, LockedAt: &now}
	if err := e.db.Create(&ep).Error; err != nil {
		t.Fatal(err)
	}
	segments := []model.DramaSegment{{UserID: e.user, EpisodeID: ep.ID, Seq: 0}, {UserID: e.user, EpisodeID: ep.ID, Seq: 1}}
	if err := e.db.Create(&segments).Error; err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i, row := range segments {
		wg.Add(1)
		go func(i int, row model.DramaSegment) {
			defer wg.Done()
			<-start
			req := validCreateReq()
			req.NodeID = "seg:" + idcodec.Encode(row.ID)
			req.CanvasID = 7
			_, err := createSingle(context.Background(), e.svc, e.user, "media-"+idcodec.Encode(uint64(i+1)), req)
			errs <- err
		}(i, row)
	}
	close(start)
	wg.Wait()
	close(errs)
	accepted, rejected := 0, 0
	for err := range errs {
		if err == nil {
			accepted++
		} else {
			assertTaskCode(t, err, errcode.ErrDramaJobBusy.Code)
			rejected++
		}
	}
	if accepted != 1 || rejected != 1 {
		t.Fatalf("接受 %d 拒绝 %d", accepted, rejected)
	}
	// 已排队的视频不应被自动规划阻塞，也不能被同集零费用合成绕过。
	dramaSvc := service.NewDramaService(service.DramaDeps{Repo: repository.NewDramaRepository(e.db)})
	_, err := dramaSvc.CreateJob(context.Background(), e.user, p.ID, &model.DramaJobReq{Step: "compose", Episodes: []int{1}})
	assertTaskCode(t, err, 67004)
	ep2 := model.DramaEpisode{UserID: e.user, DramaID: p.ID, No: 2, LockedAt: &now}
	if err := e.db.Create(&ep2).Error; err != nil {
		t.Fatal(err)
	}
	other := model.DramaSegment{UserID: e.user, EpisodeID: ep2.ID, Seq: 0}
	if err := e.db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	req := validCreateReq()
	req.NodeID = "seg:" + idcodec.Encode(other.ID)
	if _, err := createSingle(context.Background(), e.svc, e.user, "other-episode", req); err != nil {
		t.Fatalf("不同集应能并行: %v", err)
	}
}
