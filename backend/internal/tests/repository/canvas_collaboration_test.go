package repository_test

import (
	"errors"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

const collaborationFixture = `{"nodes":[{"id":"group","type":"group"},{"id":"node","type":"canvas","parentId":"group"}],"edges":[],"appearance":{"background":"grid"},"viewport":{"x":5,"y":8,"zoom":0.6}}`

func TestCanvasCollaborationRetirement(t *testing.T) {
	db := isolatedDB(t, &model.CanvasProject{}, &model.CanvasViewer{}, &model.CanvasCollaborationRecord{}, &model.CanvasCollaborationCheckpoint{}, &model.GenerationTask{}, &model.StoryboardSplitJob{}, &model.CanvasComposeJob{})
	ctx := t.Context()
	project := &model.CanvasProject{UserID: 10, Revision: 7, Title: "协作画布", PayloadJSON: []byte(collaborationFixture)}
	if err := db.Create(project).Error; err != nil {
		t.Fatal(err)
	}
	repo := repository.NewCanvasCollaborationRepository(db)
	if _, err := repo.SetActive(ctx, 20, project.ID, 7, true); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("非所有者切换协作模式: %v", err)
	}
	if _, err := repo.SetActive(ctx, 10, project.ID, 6, true); !errors.Is(err, repository.ErrRevisionConflict) {
		t.Fatalf("旧版本切换协作模式: %v", err)
	}
	active, err := repo.SetActive(ctx, 10, project.ID, 7, true)
	if err != nil || !active.CollaborationActive || active.CollaborationEpoch != 1 || active.Revision != 8 || !jsonEqual(active.PayloadJSON, collaborationFixture) {
		t.Fatalf("开启协作应保留内容并推进版本: %+v, %v", active, err)
	}
	canvasRepo := repository.NewCanvasProjectRepository(db)
	if _, err := canvasRepo.Update(ctx, 10, project.ID, 8, map[string]any{"payload_json": `{"nodes":[]}`}); !errors.Is(err, repository.ErrCanvasCollaborationActive) {
		t.Fatalf("旧整图入口写入协作画布: %v", err)
	}
	if err := canvasRepo.Delete(ctx, 10, project.ID); !errors.Is(err, repository.ErrCanvasCollaborationActive) {
		t.Fatalf("旧删除入口退役协作画布: %v", err)
	}
	if result, err := canvasRepo.GetByID(ctx, 10, project.ID); err != nil || !jsonEqual(result.PayloadJSON, collaborationFixture) {
		t.Fatalf("普通详情应读取已确认图: %+v, %v", result, err)
	}
	var records []model.CanvasCollaborationRecord
	if err := db.Order("ordinal").Find(&records).Error; err != nil || len(records) != 2 || records[0].Kind != "group" || records[1].Kind != "node" {
		t.Fatalf("应独立保存组和节点: %+v, %v", records, err)
	}
	if err := db.Model(&records[1]).Update("body", `{"id":"node","type":"canvas","parentId":"group","data":{"label":"已确认","voiceTaskId":"7","characterImageTaskId":"8","storyboardSplitRunning":true}}`).Error; err != nil {
		t.Fatal(err)
	}
	restored, err := repo.SetActive(ctx, 10, project.ID, 8, false)
	want := `{"nodes":[{"id":"group","type":"group"},{"id":"node","type":"canvas","parentId":"group","data":{"label":"已确认"}}],"edges":[],"appearance":{"background":"grid"},"viewport":{"x":5,"y":8,"zoom":0.6}}`
	if err != nil || restored.CollaborationActive || restored.CollaborationEpoch != 2 || restored.Revision != 9 || !jsonEqual(restored.PayloadJSON, want) {
		t.Fatalf("关闭应先物化已确认记录: %+v, %v", restored, err)
	}
}

func TestCanvasCollaborationActivationValidatesStructure(t *testing.T) {
	const vertices = `[{"id":"first","type":"canvas"},{"id":"second","type":"canvas"}]`
	cases := map[string]string{
		"悬空连线": `{"nodes":` + vertices + `,"edges":[{"id":"edge","source":"first","target":"missing"}]}`,
		"重复连线": `{"nodes":` + vertices + `,"edges":[{"id":"edge-1","source":"first","target":"second"},{"id":"edge-2","source":"first","target":"second"}]}`,
		"无效分组": `{"nodes":[{"id":"first","type":"canvas","parentId":"second"},{"id":"second","type":"canvas"}],"edges":[]}`,
	}
	db := isolatedDB(t, &model.CanvasProject{}, &model.CanvasViewer{}, &model.CanvasCollaborationRecord{}, &model.CanvasCollaborationCheckpoint{}, &model.GenerationTask{}, &model.StoryboardSplitJob{}, &model.CanvasComposeJob{})
	repo := repository.NewCanvasCollaborationRepository(db)
	projects := repository.NewCanvasProjectRepository(db)
	for label, payload := range cases {
		t.Run(label, func(t *testing.T) {
			ctx := t.Context()
			project := &model.CanvasProject{UserID: 10, Revision: 7, Title: "个人画布", PayloadJSON: []byte(payload)}
			if err := db.Create(project).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := repo.SetActive(ctx, project.UserID, project.ID, project.Revision, true); !errors.Is(err, repository.ErrCanvasCollaborationInvalidDocument) {
				t.Fatalf("应拒绝无法正常协作的图结构: %v", err)
			}
			retained, err := projects.GetByID(ctx, project.UserID, project.ID)
			if err != nil || retained.CollaborationActive || retained.Revision != project.Revision || retained.CollaborationEpoch != 0 || !jsonEqual(retained.PayloadJSON, payload) {
				t.Fatalf("应保留原个人画布: %+v, %v", retained, err)
			}
			const repaired = `{"nodes":` + vertices + `,"edges":[{"id":"edge","source":"first","target":"second"}]}`
			saved, err := projects.Update(ctx, project.UserID, project.ID, project.Revision, map[string]any{"payload_json": repaired})
			if err != nil {
				t.Fatalf("个人画布应可保存修正: %v", err)
			}
			opened, err := repo.SetActive(ctx, saved.UserID, saved.ID, saved.Revision, true)
			if err != nil || !opened.CollaborationActive || opened.CollaborationEpoch != 1 || !jsonEqual(opened.PayloadJSON, repaired) {
				t.Fatalf("修正后应可完整迁移: %+v, %v", opened, err)
			}
		})
	}
}
