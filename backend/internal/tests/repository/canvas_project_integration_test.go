package repository_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"gorm.io/gorm"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// 在本次 UPDATE 提交后立即插入另一笔写入，确定性地覆盖保存与响应之间的并发窗口。
func TestCanvasProjectRepository_UpdateSnapshot(t *testing.T) {
	db := isolatedDB(t, &model.CanvasProject{})
	p := &model.CanvasProject{UserID: 1, Title: "画布", Revision: 10, PayloadJSON: []byte(`{"writer":"initial"}`)}
	if err := db.Create(p).Error; err != nil {
		t.Fatal(err)
	}
	injected := false
	err := db.Callback().Update().After("gorm:commit_or_rollback_transaction").Register("test:next_canvas_write", func(tx *gorm.DB) {
		if injected || tx.Error != nil {
			return
		}
		injected = true
		// 不靠 sleep 或调度运气：模拟 Agent 在浏览器保存提交后、服务返回前写入版本 12。
		if err := db.Model(&model.CanvasProject{}).Where("id = ? AND user_id = ?", p.ID, 1).
			UpdateColumns(map[string]any{"revision": 12, "payload_json": `{"writer":"agent"}`}).Error; err != nil {
			_ = tx.AddError(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	repo := NewCanvasProjectRepository(db)
	saved, err := service.NewCanvasProjectService(repo).Update(context.Background(), 1, p.ID,
		&model.UpdateCanvasProjectReq{Revision: 10, PayloadJSON: json.RawMessage(`{"writer":"browser"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !injected || saved.Revision != 11 || !jsonEqual(saved.PayloadJSON, `{"writer":"browser"}`) {
		t.Fatalf("保存响应必须是本次写入的快照，不能返回随后 Agent 的版本：injected=%v saved=%+v", injected, saved)
	}
	latest, err := repo.GetByID(context.Background(), 1, p.ID)
	if err != nil || latest.Revision != 12 || !jsonEqual(latest.PayloadJSON, `{"writer":"agent"}`) {
		t.Fatalf("随后 Agent 的写入应保留在数据库：latest=%+v err=%v", latest, err)
	}
}

func TestCanvasProjectRepository_Update(t *testing.T) {
	ctx := context.Background()
	db := isolatedDB(t, &model.CanvasProject{})
	repo := NewCanvasProjectRepository(db)
	for _, tt := range []struct {
		name     string
		userID   uint64
		revision uint64
		deleted  bool
		wantErr  error
	}{
		{"更新返回完整快照", 1, 10, false, nil},
		{"版本过期", 1, 9, false, ErrRevisionConflict},
		{"不属于当前用户", 2, 10, false, ErrNotFound},
		{"软删除画布", 1, 10, true, ErrNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := &model.CanvasProject{UserID: 1, Title: "原标题", Revision: 10, PayloadJSON: []byte(`{"nodes":[]}`)}
			if err := db.Create(p).Error; err != nil {
				t.Fatal(err)
			}
			before, err := repo.GetByID(ctx, 1, p.ID)
			if err != nil {
				t.Fatal(err)
			}
			if tt.deleted {
				if err := db.Delete(p).Error; err != nil {
					t.Fatal(err)
				}
			}
			saved, err := repo.Update(ctx, tt.userID, p.ID, tt.revision, map[string]any{"title": "新标题"})
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) || saved != nil {
					t.Fatalf("期望 %v 和空快照，实际 saved=%+v err=%v", tt.wantErr, saved, err)
				}
				return
			}
			if err != nil || saved.ID != p.ID || saved.UserID != 1 || saved.Title != "新标题" || saved.Revision != 11 ||
				!jsonEqual(saved.PayloadJSON, `{"nodes":[]}`) || !saved.CreatedAt.Equal(before.CreatedAt) || saved.UpdatedAt.IsZero() {
				t.Fatalf("应返回完整的本次写入快照：saved=%+v err=%v", saved, err)
			}
		})
	}
	if saved, err := repo.Update(ctx, 1, 999999, 10, map[string]any{"title": "x"}); !errors.Is(err, ErrNotFound) || saved != nil {
		t.Fatalf("不存在的画布应返回 ErrNotFound：saved=%+v err=%v", saved, err)
	}
}
