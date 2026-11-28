package repository_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/repository"
)

func TestCanvasCollaborationReadAccessUsesOneGrantQuery(t *testing.T) {
	db, repo, owner, editor, _, project := newCollaborationFixture(t)
	ctx := t.Context()
	shares := repository.NewCanvasShareRepository(db)
	if err := shares.GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	if err := shares.SetGenerationPermission(ctx, owner, project.ID, editor, true); err != nil {
		t.Fatal(err)
	}
	if err := shares.SetLibraryPermission(ctx, owner, project.ID, editor, true); err != nil {
		t.Fatal(err)
	}
	queries := 0
	if err := db.Callback().Query().Before("gorm:query").Register("test:count_canvas_grant_reads", func(tx *gorm.DB) {
		if tx.Statement.Table == "canvas_viewers" {
			queries++
		}
	}); err != nil {
		t.Fatal(err)
	}
	page, err := repo.Replay(ctx, editor, project.ID, project.CollaborationEpoch, project.Revision, 1)
	if err != nil || page.Role != model.CanvasRoleEditor || !page.CanGenerate || !page.CanCopyToLibrary {
		t.Fatalf("当前编辑授权应完整返回: %+v, %v", page, err)
	}
	if queries != 1 {
		t.Errorf("回放成员授权应只查一次，实际 %d 次", queries)
	}
	queries = 0
	snapshot, role, err := repo.Snapshot(ctx, editor, project.ID)
	if err != nil || role != model.CanvasRoleEditor || !snapshot.CollaborationCanGenerate || !snapshot.CollaborationCanCopyToLibrary {
		t.Fatalf("快照应返回相同授权: %+v, %s, %v", snapshot, role, err)
	}
	if queries != 1 {
		t.Errorf("快照成员授权应只查一次，实际 %d 次", queries)
	}
}

func TestCanvasCollaborationReplay(t *testing.T) {
	db, writer, owner, editor, viewer, project := newCollaborationFixture(t)
	ctx := t.Context()
	share := repository.NewCanvasShareRepository(db)
	reader := repository.NewCanvasCollaborationRepository(db)
	if err := share.GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Replay(ctx, viewer, project.ID, project.CollaborationEpoch, project.Revision, 1); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("未授权用户不能读取增量: %v", err)
	}
	if _, err := writer.Commit(ctx, owner, project.ID, collaborationEdit(project, "first", "data.label", `"甲"`)); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Commit(ctx, editor, project.ID, collaborationEdit(project, "second", "position", `{"x":42,"y":9}`)); err != nil {
		t.Fatal(err)
	}
	page, err := reader.Replay(ctx, editor, project.ID, project.CollaborationEpoch, project.Revision, 1)
	if err != nil || page.Revision != 10 || page.Cursor != 9 || len(page.Events) != 1 || page.Events[0].OpID != "" || page.SnapshotRequired {
		t.Fatalf("增量首屏应推进游标且隐藏其他人的操作 ID: %+v, %v", page, err)
	}
	if len(page.Events[0].Changes) != 1 || page.Events[0].Changes[0].PreviousRevision != 0 {
		t.Fatalf("其他成员不应收到前序编辑的记录版本: %+v", page.Events[0])
	}
	next, err := reader.Replay(ctx, editor, project.ID, project.CollaborationEpoch, page.Cursor, 1)
	if err != nil || next.Cursor != 10 || len(next.Events) != 1 || next.Events[0].OpID != "second" {
		t.Fatalf("等待确认的提交应能通过操作 ID 对账: %+v, %v", next, err)
	}
	if len(next.Events[0].Changes) != 1 || next.Events[0].Changes[0].PreviousRevision != 9 {
		t.Fatalf("重放自己的提交时应携带服务端观察的前一版记录: %+v", next.Events[0])
	}
	if err := db.Where("canvas_id = ? AND revision = ?", project.ID, 9).Delete(&model.CanvasCollaborationEvent{}).Error; err != nil {
		t.Fatal(err)
	}
	missing, err := reader.Replay(ctx, editor, project.ID, project.CollaborationEpoch, project.Revision, 100)
	if err != nil || !missing.SnapshotRequired || len(missing.Events) > 0 {
		t.Fatalf("日志缺口应要求读取快照: %+v, %v", missing, err)
	}
	if err := share.Remove(ctx, owner, project.ID, editor); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Replay(ctx, editor, project.ID, project.CollaborationEpoch, 10, 1); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("撤权后即使没有新版本也应拒绝追赶: %v", err)
	}
}

func TestCanvasCollaborationNoticeOutbox(t *testing.T) {
	_, writer, owner, _, _, project := newCollaborationFixture(t)
	ctx := t.Context()
	result, err := writer.Commit(ctx, owner, project.ID, collaborationEdit(project, "dispatch", "data.label", `"待派发"`))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		pending, err := writer.PendingNotices(ctx, 10)
		if err != nil || len(pending) != 1 || pending[0].Revision != result.Revision {
			t.Fatalf("无确认的通知应能重读: %+v %v", pending, err)
		}
	}
	if err := writer.AcknowledgeNotice(ctx, project.ID, result.Epoch, result.Revision); err != nil {
		t.Fatal(err)
	}
	if pending, err := writer.PendingNotices(ctx, 10); err != nil || len(pending) != 0 {
		t.Fatalf("已确认派发应退出重试队列: %+v %v", pending, err)
	}
	if page, err := writer.Replay(ctx, owner, project.ID, result.Epoch, project.Revision, 10); err != nil || len(page.Events) != 1 {
		t.Fatalf("通知确认不能删除回放记录: %+v %v", page, err)
	}
}

func TestCanvasCollaborationRedisRecovery(t *testing.T) {
	address := os.Getenv("TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("未设置 TEST_REDIS_ADDR")
	}
	_, repo, owner, _, _, project := newCollaborationFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var redisOnline atomic.Bool
	writer := redis.NewClient(&redis.Options{Addr: address, Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
		if !redisOnline.Load() {
			return nil, net.ErrClosed
		}
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}})
	defer func() { _ = writer.Close() }()
	reader := redis.NewClient(&redis.Options{Addr: address})
	defer func() { _ = reader.Close() }()
	remote := reader.Subscribe(ctx, "video-canvas:collaboration:notice")
	defer func() { _ = remote.Close() }()
	if _, err := remote.Receive(ctx); err != nil {
		t.Fatal(err)
	}
	hub := ws.NewHub()
	defer hub.Close()
	bus := ws.NewCanvasCollaborationBus(hub, writer).WithNoticeAcknowledgement(func(ctx context.Context, notice ws.CanvasCollaborationNotice) {
		if err := repo.AcknowledgeNotice(ctx, project.ID, notice.Epoch, notice.Revision); err != nil {
			t.Error(err)
		}
	})
	result, err := repo.Commit(ctx, owner, project.ID, collaborationEdit(project, "redis-offline", "data.label", `"离线期间已确认"`))
	if err != nil {
		t.Fatal(err)
	}
	notice := ws.CanvasCollaborationNotice{CanvasID: idcodec.Encode(project.ID), Epoch: result.Epoch, Revision: result.Revision}
	if bus.Notice(ctx, notice) {
		t.Fatal("Redis 中断时不应确认跨实例通知已派发")
	}
	if pending, err := repo.PendingNotices(ctx, 10); err != nil || len(pending) != 1 || pending[0].Revision != notice.Revision {
		t.Fatalf("中断时确认版本应保留在发件箱: %+v, %v", pending, err)
	}
	redisOnline.Store(true)
	if !bus.Notice(ctx, notice) {
		t.Fatal("恢复连接后应能重试已提交的通知")
	}
	assertCanvasRecoveredNotice(ctx, t, remote, notice)
	if pending, err := repo.PendingNotices(ctx, 10); err != nil || len(pending) != 0 {
		t.Fatalf("已派发通知仍然出现于发件箱: %+v, %v", pending, err)
	}
	if page, err := repo.Replay(ctx, owner, project.ID, result.Epoch, project.Revision, 1); err != nil || len(page.Events) != 1 || page.Cursor != notice.Revision {
		t.Fatalf("恢复后客户端仍可追赶完整版本: %+v, %v", page, err)
	}
}

func assertCanvasRecoveredNotice(ctx context.Context, t *testing.T, remote *redis.PubSub, want ws.CanvasCollaborationNotice) {
	t.Helper()
	for {
		message, err := remote.ReceiveMessage(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var got ws.CanvasCollaborationNotice
		if err := json.Unmarshal([]byte(message.Payload), &got); err != nil {
			t.Fatal(err)
		}
		if got.CanvasID != want.CanvasID {
			continue
		}
		if got.EmittedAt <= 0 {
			t.Fatalf("恢复通知缺少派发时间: %+v", got)
		}
		got.EmittedAt = 0
		if got != want {
			t.Fatalf("远端收到的版本通知与确认记录不符: %+v", got)
		}
		return
	}
}
