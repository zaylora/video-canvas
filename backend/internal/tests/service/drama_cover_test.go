package service_test

import (
	"context"
	"testing"

	"video-canvas/internal/service"
)

// dramaCovers 按画布 ID 给出封面素材 key，模拟资产画布里第一张生成成功的图。
type dramaCovers map[uint64]string

func (c dramaCovers) CoverKeys(_ context.Context, _ uint64, canvasIDs []uint64) (map[uint64]string, error) {
	out := map[uint64]string{}
	for _, id := range canvasIDs {
		if key, ok := c[id]; ok {
			out[id] = key
		}
	}
	return out, nil
}

func thumbURL(key string) string { return "/files/" + key + "?v=thumb" }

func TestDramaListShowsCanvasCover(t *testing.T) {
	mem := newDramaMemory()
	s := service.NewDramaService(service.DramaDeps{Repo: mem, Covers: dramaCovers{mem.project.CanvasID: "u7/first.png"}, CoverURL: thumbURL})
	items, _, err := s.List(context.Background(), mem.project.UserID, "", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].CoverURL != "/files/u7/first.png?v=thumb" {
		t.Fatalf("封面应取资产画布的第一张图：%+v", items)
	}
}

func TestDramaListWithoutCoverStaysEmpty(t *testing.T) {
	mem := newDramaMemory()
	s := service.NewDramaService(service.DramaDeps{Repo: mem, Covers: dramaCovers{}, CoverURL: thumbURL})
	items, _, err := s.List(context.Background(), mem.project.UserID, "", 1, 20)
	if err != nil || len(items) != 1 || items[0].CoverURL != "" {
		t.Fatalf("画布还没有图时封面为空：%+v %v", items, err)
	}
}

func TestDramaListWithoutCoverDepsStaysEmpty(t *testing.T) {
	mem := newDramaMemory()
	s := service.NewDramaService(service.DramaDeps{Repo: mem})
	items, _, err := s.List(context.Background(), mem.project.UserID, "", 1, 20)
	if err != nil || len(items) != 1 || items[0].CoverURL != "" {
		t.Fatalf("没配封面依赖时不报错、封面为空：%+v %v", items, err)
	}
}
