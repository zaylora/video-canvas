package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/service"
)

// deleteEpisodeFake 是三集、没有进行中任务、画布上有各种出现集数资产的短剧。
func deleteEpisodeFake() *dramaCanvasFake {
	f := newDramaCanvasFake()
	f.jobs = map[uint64]model.DramaJob{}
	f.episodes[3] = model.DramaEpisode{UserID: 1, DramaID: 3, No: 3, Title: "第三集", Revision: 1}
	f.canvas.PayloadJSON = datatypes.JSON(`{"nodes":[
		{"id":"a","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"image","drama":{"type":"scene","name":"街道","episodes":[1,3]}}},
		{"id":"b","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"character","drama":{"type":"character","name":"林薇","episodes":[2]}}}
	],"edges":[]}`)
	return f
}

// assetEpisodes 读画布里某个节点的 drama.episodes 原始 JSON。
func assetEpisodes(t *testing.T, f *dramaCanvasFake, id string) string {
	t.Helper()
	d, _ := dramaGraph(t, f).Node(id).Data()["drama"].(map[string]any)
	b, _ := json.Marshal(d["episodes"])
	return string(b)
}

func TestDramaCanvasDeleteEpisodeShiftsEpisodesAndAssets(t *testing.T) {
	ctx := context.Background()
	f := deleteEpisodeFake()
	bc := &dramaCanvasBroadcast{}
	svc := service.NewDramaCanvasService(f, bc)
	if err := svc.DeleteEpisode(ctx, 1, 3, 7, 2); err != nil {
		t.Fatal(err)
	}
	// 第 2 集删掉，第 3 集补到第 2 集
	if !reflect.DeepEqual(f.deletedEpisodes, []int{2}) || len(f.episodes) != 2 || f.episodes[2].Title != "第三集" {
		t.Fatalf("分集：deleted=%v episodes=%+v", f.deletedEpisodes, f.episodes)
	}
	// 资产的出现集数跟着改：3 → 2；只在第 2 集的资产变未分类
	if got := assetEpisodes(t, f, "a"); got != `[1,2]` {
		t.Fatalf("a=%s", got)
	}
	if got := assetEpisodes(t, f, "b"); got != `null` {
		t.Fatalf("b=%s", got)
	}
	if f.canvas.Revision != 2 {
		t.Fatalf("画布 revision=%d", f.canvas.Revision)
	}
	// 推画布改动，开着的资产画布能同步
	if len(bc.messages) != 1 || bc.messages[0].Type != "canvas.patch" {
		t.Fatalf("broadcast=%+v", bc.messages)
	}
}

func TestDramaServiceDeleteEpisodeUsesTheDramaCanvas(t *testing.T) {
	ctx := context.Background()
	r := newDramaMemory()
	canvas := &dramaPipelineCanvas{}
	svc, _ := dramaTestService(r, &dramaTextStreamer{}, canvas)
	if err := svc.DeleteEpisode(ctx, 7, 1, 2); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(canvas.deleted, [][3]uint64{{1, 2, 2}}) {
		t.Fatalf("应按项目的画布删集：%+v", canvas.deleted)
	}
	// 不是自己的短剧：67001，不动画布
	err := svc.DeleteEpisode(ctx, 8, 1, 2)
	var got *errcode.Error
	if !errors.As(err, &got) || got.Code != errcode.ErrDramaNotFound.Code || len(canvas.deleted) != 1 {
		t.Fatalf("别人的短剧：err=%v deleted=%+v", err, canvas.deleted)
	}
}

func TestDramaCanvasDeleteEpisodeRejectsBusyAndMissing(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		setup func(*dramaCanvasFake)
		no    int
		want  *errcode.Error
	}{
		"有任务在跑": {func(f *dramaCanvasFake) {
			f.jobs[9] = model.DramaJob{UserID: 1, DramaID: 3, Step: "breakdown", Status: "running"}
		}, 2, errcode.ErrDramaJobBusy},
		"集不存在": {func(*dramaCanvasFake) {}, 9, errcode.ErrDramaNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			f := deleteEpisodeFake()
			tc.setup(f)
			before := string(f.canvas.PayloadJSON)
			err := service.NewDramaCanvasService(f, nil).DeleteEpisode(ctx, 1, 3, 7, tc.no)
			var got *errcode.Error
			if !errors.As(err, &got) || got.Code != tc.want.Code {
				t.Fatalf("want %d, got %v", tc.want.Code, err)
			}
			if len(f.deletedEpisodes) != 0 || len(f.episodes) != 3 || string(f.canvas.PayloadJSON) != before {
				t.Fatal("失败时不该删集或改画布")
			}
		})
	}
}
