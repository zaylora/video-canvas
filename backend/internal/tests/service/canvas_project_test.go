package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service"
)

// fakeCanvasUpdateRepo 只实现更新路径，嵌入接口让意外的重查直接使测试失败。
type fakeCanvasUpdateRepo struct {
	CanvasProjectRepo
	update func(context.Context, uint64, uint64, uint64, map[string]any) (*model.CanvasProject, error)
}

func (r *fakeCanvasUpdateRepo) Update(ctx context.Context, userID, id, revision uint64, fields map[string]any) (*model.CanvasProject, error) {
	return r.update(ctx, userID, id, revision, fields)
}

func TestCanvasProjectService_Update(t *testing.T) {
	title := "新标题"
	storageErr := errors.New("storage unavailable")
	tests := []struct {
		name     string
		req      model.UpdateCanvasProjectReq
		repoErr  error
		wantCode int
		called   bool
	}{
		{name: "只改标题返回本次快照", req: model.UpdateCanvasProjectReq{Revision: 10, Title: &title}, called: true},
		{name: "只改图谱返回本次快照", req: model.UpdateCanvasProjectReq{Revision: 10, PayloadJSON: json.RawMessage(`{"nodes":[]}`)}, called: true},
		{name: "空更新", req: model.UpdateCanvasProjectReq{Revision: 10}, wantCode: errcode.ErrCanvasNoChange.Code},
		{name: "图谱不是对象", req: model.UpdateCanvasProjectReq{Revision: 10, PayloadJSON: json.RawMessage(`[]`)}, wantCode: errcode.ErrCanvasPayload.Code},
		{name: "图谱不是合法JSON", req: model.UpdateCanvasProjectReq{Revision: 10, PayloadJSON: json.RawMessage(`{`)}, wantCode: errcode.ErrCanvasPayload.Code},
		{name: "画布不存在", req: model.UpdateCanvasProjectReq{Revision: 10, Title: &title}, repoErr: repository.ErrNotFound, wantCode: errcode.ErrCanvasNotFound.Code, called: true},
		{name: "版本冲突", req: model.UpdateCanvasProjectReq{Revision: 10, Title: &title}, repoErr: repository.ErrRevisionConflict, wantCode: errcode.ErrCanvasConflict.Code, called: true},
		{name: "数据库错误原样返回", req: model.UpdateCanvasProjectReq{Revision: 10, Title: &title}, repoErr: storageErr, called: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			saved := &model.CanvasProject{BaseModel: model.BaseModel{ID: 7}, UserID: 3, Title: title, Revision: 11}
			called := false
			repo := &fakeCanvasUpdateRepo{update: func(gotCtx context.Context, userID, id, revision uint64, fields map[string]any) (*model.CanvasProject, error) {
				called = true
				if gotCtx != ctx || userID != 3 || id != 7 || revision != 10 {
					t.Fatalf("更新条件未透传：user=%d id=%d revision=%d", userID, id, revision)
				}
				if tt.req.Title != nil && fields["title"] != title {
					t.Fatalf("标题未透传：%v", fields)
				}
				if len(tt.req.PayloadJSON) > 0 && string(fields["payload_json"].(datatypes.JSON)) != string(tt.req.PayloadJSON) {
					t.Fatalf("图谱未透传：%v", fields)
				}
				if tt.repoErr != nil {
					return nil, tt.repoErr
				}
				return saved, nil
			}}
			got, err := NewCanvasProjectService(repo).Update(ctx, 3, 7, &tt.req)
			if called != tt.called {
				t.Fatalf("仓储调用=%v，期望=%v", called, tt.called)
			}
			switch {
			case tt.wantCode != 0:
				var biz *errcode.Error
				if !errors.As(err, &biz) || biz.Code != tt.wantCode {
					t.Fatalf("期望错误码 %d，实际：%v", tt.wantCode, err)
				}
			case tt.repoErr != nil:
				if !errors.Is(err, tt.repoErr) {
					t.Fatalf("应透传存储错误：%v", err)
				}
			default:
				if err != nil || got != saved {
					t.Fatalf("应直接返回本次快照，不能重新读取：got=%+v err=%v", got, err)
				}
			}
		})
	}
}
