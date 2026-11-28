package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
)

// CanvasHistoryView 展示一个历史版本的作者、时间和变更对象。
type CanvasHistoryView struct {
	Revision       uint64         `json:"revision"`
	AuthorID       string         `json:"authorId,omitempty"`
	AuthorUsername string         `json:"authorUsername,omitempty"`
	Source         string         `json:"source,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
	Targets        datatypes.JSON `json:"targets"`
}

// HistoricalSnapshot 让当前所有者检查某一已确认版本的图谱。
func (s *CanvasCollaborationService) HistoricalSnapshot(ctx context.Context, user, canvas, epoch, revision uint64) (*model.CanvasProjectView, error) {
	// 1. 仅用检查点和已确认日志组装选中的版本。
	project, err := s.repo.HistoricalSnapshot(ctx, user, canvas, epoch, revision)
	if errors.Is(err, repository.ErrCanvasVersionUnavailable) {
		return nil, errcode.ErrCanvasConflict.WithMsg(err.Error())
	}
	if err != nil {
		return nil, collaborationError(err)
	}
	// 2. 返回只用于预览的快照。
	return project.View(), nil
}

// CopyHistoricalVersion 恢复一份个人画布，并保留历史版本中的同账号素材引用。
func (s *CanvasCollaborationService) CopyHistoricalVersion(ctx context.Context, user, canvas, epoch, revision uint64, opID string) (*model.CanvasProjectView, error) {
	// 1. 在授权仍有效的同一事务内重放指定版本、清理任务绑定并保存副本。
	project, err := s.repo.CopyHistoricalVersion(ctx, user, canvas, epoch, revision, opID, prepareHistoricalCanvas)
	if errors.Is(err, repository.ErrCanvasVersionUnavailable) {
		return nil, errcode.ErrCanvasConflict.WithMsg(err.Error())
	}
	if err != nil {
		return nil, collaborationError(err)
	}
	// 2. 返回个人画布视图，调用方可直接打开副本。
	return project.View(), nil
}

// prepareHistoricalCanvas 保留内容和同账号素材，去除旧任务对副本的回填绑定。
func prepareHistoricalCanvas(source *model.CanvasProject) (*model.CanvasProject, error) {
	var graph map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(source.PayloadJSON)))
	decoder.UseNumber()
	if err := decoder.Decode(&graph); err != nil {
		return nil, err
	}
	delete(graph, "coverAssetId")
	nodes, _ := graph["nodes"].([]any)
	for _, item := range nodes {
		if node, ok := item.(map[string]any); ok {
			if data, ok := node["data"].(map[string]any); ok {
				stripHistoryTaskLinks(data)
			}
		}
	}
	payload, err := json.Marshal(graph)
	if err != nil {
		return nil, err
	}
	title := []rune(fmt.Sprintf("版本 %d · %s", source.Revision, source.Title))
	return &model.CanvasProject{Title: string(title[:min(len(title), 255)]), UserID: source.UserID, Revision: 1, PayloadJSON: payload}, nil
}

// stripHistoryTaskLinks 移除嵌套任务标识，避免复制进行中的任务。
func stripHistoryTaskLinks(value any) {
	switch item := value.(type) {
	case map[string]any:
		for _, key := range []string{"taskId", "pendingTaskIds", "voiceTaskId", "errorTaskRef", "generationInput"} {
			delete(item, key)
		}
		if item["status"] == "running" {
			item["status"] = "idle"
		}
		for _, child := range item {
			stripHistoryTaskLinks(child)
		}
	case []any:
		for _, child := range item {
			stripHistoryTaskLinks(child)
		}
	}
}

// CanvasHistoryPage 包含一页版本及下一页的游标。
type CanvasHistoryPage struct {
	Entries []CanvasHistoryView `json:"entries"`
	Before  uint64              `json:"before,omitempty"`
}

// History 返回有编辑权限的成员可见的版本活动。
func (s *CanvasCollaborationService) History(ctx context.Context, user, canvas, epoch, before uint64) (*CanvasHistoryPage, error) {
	// 1. 仅读取当前授权代数的活动，不公开历史正文和私有任务数据。
	rows, err := s.repo.History(ctx, user, canvas, epoch, before)
	if err != nil {
		return nil, collaborationError(err)
	}
	// 2. 对账号标识进行编码并固定每页数量，方便长会话增量浏览。
	result := CanvasHistoryPage{Entries: make([]CanvasHistoryView, 0, min(len(rows), 30))}
	if len(rows) > 30 {
		rows = rows[:30]
		result.Before = rows[29].Revision
	}
	actors := make([]uint64, 0, len(rows))
	for _, row := range rows {
		if row.ActorID != 0 {
			actors = append(actors, row.ActorID)
		}
	}
	names, err := s.usernames(ctx, actors)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		view := CanvasHistoryView{Revision: row.Revision, CreatedAt: row.CreatedAt, Targets: row.Targets, Source: row.Source}
		if row.ActorID != 0 {
			view.AuthorID = idcodec.Encode(row.ActorID)
			view.AuthorUsername = names[row.ActorID]
		}
		result.Entries = append(result.Entries, view)
	}
	return &result, nil
}
