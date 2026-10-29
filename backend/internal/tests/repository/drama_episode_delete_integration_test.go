package repository_test

import (
	"context"
	"encoding/json"
	"testing"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// 真实数据库上删中间一集：集号往前补位不撞 (drama_id, no) 唯一索引，片段软删，画布出现集数一起改。
func TestDramaDeleteEpisodeRenumbersOnRealDB(t *testing.T) {
	db := isolatedDB(t, &model.CanvasProject{}, &model.DramaProject{}, &model.DramaEpisode{}, &model.DramaJob{}, &model.DramaSegment{})
	ctx := context.Background()
	canvas := model.CanvasProject{UserID: 11, Title: "短剧画布", Revision: 1, PayloadJSON: datatypes.JSON(`{"nodes":[
		{"id":"a","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"image","drama":{"type":"scene","name":"街道","episodes":[2,4]}}},
		{"id":"b","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"character","drama":{"type":"character","name":"林薇","episodes":[2]}}}
	],"edges":[]}`)}
	if err := db.Create(&canvas).Error; err != nil {
		t.Fatal(err)
	}
	project := model.DramaProject{UserID: 11, CanvasID: canvas.ID, Title: "测试剧", SourceText: "原文", StyleKey: "anime"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	ids := map[int]uint64{}
	for no := 1; no <= 4; no++ {
		ep := model.DramaEpisode{UserID: 11, DramaID: project.ID, No: no, Title: "原第" + string(rune('0'+no)) + "集", Revision: 1}
		if err := db.Create(&ep).Error; err != nil {
			t.Fatal(err)
		}
		ids[no] = ep.ID
	}
	seg := model.DramaSegment{UserID: 11, EpisodeID: ids[2], Seconds: 5, Revision: 1}
	keep := model.DramaSegment{UserID: 11, EpisodeID: ids[3], Seconds: 5, Revision: 1}
	if err := db.Create(&seg).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&keep).Error; err != nil {
		t.Fatal(err)
	}

	svc := service.NewDramaCanvasService(repository.NewDramaCanvasRepository(db), nil)
	if err := svc.DeleteEpisode(ctx, 11, project.ID, canvas.ID, 2); err != nil {
		t.Fatal(err)
	}

	var rows []model.DramaEpisode
	if err := db.Where("drama_id = ?", project.ID).Order("no").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	got := map[int]string{}
	for _, row := range rows {
		got[row.No] = row.Title
	}
	want := map[int]string{1: "原第1集", 2: "原第3集", 3: "原第4集"}
	if len(got) != 3 || got[1] != want[1] || got[2] != want[2] || got[3] != want[3] {
		t.Fatalf("补位后的分集：%+v", got)
	}
	// 被删集的片段软删；别的集的片段还在
	var alive []model.DramaSegment
	if err := db.Find(&alive).Error; err != nil {
		t.Fatal(err)
	}
	if len(alive) != 1 || alive[0].ID != keep.ID {
		t.Fatalf("片段：%+v", alive)
	}
	// 画布：[2,4] → [3]；只在第 2 集的 → null
	var stored model.CanvasProject
	if err := db.First(&stored, canvas.ID).Error; err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Nodes []struct {
			ID   string `json:"id"`
			Data struct {
				Drama struct {
					Episodes json.RawMessage `json:"episodes"`
				} `json:"drama"`
			} `json:"data"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(stored.PayloadJSON, &payload); err != nil {
		t.Fatal(err)
	}
	episodes := map[string]string{}
	for _, n := range payload.Nodes {
		episodes[n.ID] = string(n.Data.Drama.Episodes)
	}
	if episodes["a"] != "[3]" || episodes["b"] != "null" || stored.Revision != 2 {
		t.Fatalf("画布：%+v revision=%d", episodes, stored.Revision)
	}
	// 再新增一集，集号接在补位后的末尾（4），不会撞上已删的号
	var maxNo int
	if err := db.Model(&model.DramaEpisode{}).Where("drama_id = ?", project.ID).Select("MAX(no)").Scan(&maxNo).Error; err != nil || maxNo != 3 {
		t.Fatalf("max no=%d err=%v", maxNo, err)
	}
}
