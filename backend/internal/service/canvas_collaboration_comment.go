package service

import (
	"context"
	"encoding/json"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/ws"
)

// CanvasCommentView 返回节点讨论使用的评论和画布内账号标识。
type CanvasCommentView struct {
	ID             string    `json:"id"`
	AuthorID       string    `json:"authorId"`
	AuthorUsername string    `json:"authorUsername,omitempty"`
	Body           string    `json:"body"`
	Mentions       []string  `json:"mentions,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
}

// CanvasMentionView contains only the identifiers needed to open an authorized discussion.
type CanvasMentionView struct {
	ID     string `json:"id"`
	NodeID string `json:"nodeId"`
}

// Mentions 返回当前编辑成员待查看的节点提及。
func (s *CanvasCollaborationService) Mentions(ctx context.Context, user, canvas, epoch, after uint64) ([]CanvasMentionView, error) {
	rows, err := s.repo.Mentions(ctx, user, canvas, epoch, after)
	if err != nil {
		return nil, collaborationError(err)
	}
	items := make([]CanvasMentionView, 0, len(rows))
	for _, row := range rows {
		items = append(items, CanvasMentionView{ID: idcodec.Encode(row.ID), NodeID: row.NodeID})
	}
	return items, nil
}

// CanvasCommentPage 包含一页评论、翻页游标及可提及成员。
type CanvasCommentPage struct {
	Comments  []CanvasCommentView `json:"comments"`
	Members   []string            `json:"members"`
	Usernames map[string]string   `json:"usernames,omitempty"`
	Before    string              `json:"before,omitempty"`
}

// CanvasCommentEventView 包含评论及其所属节点。
type CanvasCommentEventView struct {
	CanvasCommentView
	NodeID string `json:"nodeId"`
}

// CanvasCommentReplayView 包含一页可见评论和下一次请求使用的游标。
type CanvasCommentReplayView struct {
	Cursor string                   `json:"cursor"`
	Items  []CanvasCommentEventView `json:"items"`
	More   bool                     `json:"more"`
}

// ReplayComments 返回当前协作代数中有权阅读的评论及独立游标。
func (s *CanvasCollaborationService) ReplayComments(ctx context.Context, user, canvas, epoch uint64, after *uint64) (*CanvasCommentReplayView, error) {
	const limit = 50
	page, err := s.repo.ReplayComments(ctx, user, canvas, epoch, after, limit+1)
	if err != nil {
		return nil, collaborationError(err)
	}
	result := CanvasCommentReplayView{Items: make([]CanvasCommentEventView, 0, min(len(page.Events), limit)), More: len(page.Events) > limit}
	actors := make([]uint64, 0, len(result.Items))
	for _, row := range page.Events[:min(len(page.Events), limit)] {
		page.Cursor = row.ID
		if !row.Visible {
			continue
		}
		view, _, err := canvasCommentView(row.CanvasCollaborationComment)
		if err != nil {
			return nil, err
		}
		result.Items = append(result.Items, CanvasCommentEventView{CanvasCommentView: view, NodeID: row.NodeID})
		actors = append(actors, row.ActorID)
	}
	names, err := s.usernames(ctx, actors)
	if err != nil {
		return nil, err
	}
	for index := range result.Items {
		result.Items[index].AuthorUsername = names[actors[index]]
	}
	result.Cursor = "0"
	if page.Cursor != 0 {
		result.Cursor = idcodec.Encode(page.Cursor)
	}
	return &result, nil
}

// Comments 读取指定节点的评论，不返回成员的邮箱等私有资料。
func (s *CanvasCollaborationService) Comments(ctx context.Context, user, canvas, epoch uint64, node string, before uint64) (*CanvasCommentPage, error) {
	// 1. 在同一数据库视图中校验当前协作者权限、节点和代数。
	thread, err := s.repo.CommentThread(ctx, user, canvas, epoch, node, before)
	if err != nil {
		return nil, collaborationError(err)
	}
	// 2. 用不透明账号标识投影评论，返回固定 50 条的翻页窗口。
	result := CanvasCommentPage{Comments: make([]CanvasCommentView, 0, min(len(thread.Rows), 50)), Members: make([]string, 0, len(thread.Members))}
	if len(thread.Rows) > 50 {
		thread.Rows = thread.Rows[:50]
		result.Before = idcodec.Encode(thread.Rows[49].ID)
	}
	actors := make([]uint64, 0, len(thread.Members)+len(thread.Rows))
	for _, row := range thread.Rows {
		view, mentioned, err := canvasCommentView(row)
		if err != nil {
			return nil, err
		}
		result.Comments = append(result.Comments, view)
		actors = append(actors, row.ActorID)
		actors = append(actors, mentioned...)
	}
	for _, member := range thread.Members {
		result.Members = append(result.Members, idcodec.Encode(member))
	}
	names, err := s.usernames(ctx, append(actors, thread.Members...))
	if err != nil {
		return nil, err
	}
	result.Usernames = make(map[string]string, len(names))
	for actor, username := range names {
		result.Usernames[idcodec.Encode(actor)] = username
	}
	return &result, nil
}

// AddComment 依照当前成员权限和幂等键添加节点评论。
func (s *CanvasCollaborationService) AddComment(ctx context.Context, user, canvas uint64, request model.CanvasCommentRequest) (CanvasCommentView, error) {
	// 1. 在画布提交边界检查被评论节点和被提及账号，阻止过期权限写入。
	row, err := s.repo.AddComment(ctx, user, canvas, request)
	if err != nil {
		return CanvasCommentView{}, collaborationError(err)
	}
	if s.bus != nil {
		s.bus.Notice(ctx, ws.CanvasCollaborationNotice{CanvasID: idcodec.Encode(canvas), CommentChanged: true})
	}
	// 2. 复用评论列表的对外投影，便于原请求重试后对账。
	view, _, err := canvasCommentView(row)
	if err != nil {
		return view, err
	}
	names, _ := s.usernames(ctx, []uint64{row.ActorID})
	view.AuthorUsername = names[row.ActorID]
	return view, nil
}

// canvasCommentView 把内部主键编码为画布评论对外标识。
func canvasCommentView(row model.CanvasCollaborationComment) (CanvasCommentView, []uint64, error) {
	view := CanvasCommentView{ID: idcodec.Encode(row.ID), AuthorID: idcodec.Encode(row.ActorID), Body: row.Body, CreatedAt: row.CreatedAt}
	var ids []uint64
	if err := json.Unmarshal(row.Mentions, &ids); err != nil {
		return CanvasCommentView{}, nil, err
	}
	for _, id := range ids {
		view.Mentions = append(view.Mentions, idcodec.Encode(id))
	}
	return view, ids, nil
}
