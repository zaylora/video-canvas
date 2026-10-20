package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/storage"
)

// 保留期清理：每天删除超过保留天数的轨迹索引行和原文对象，并补上 agent_events 一直缺的保留期清理（同样的天数）。

// TraceCleanupBatch 是每一批最多处理的行数：批量太大会长时间占用事务与锁。
const TraceCleanupBatch = 500

// TraceCleanupRepo 是清理需要的数据访问，由 *repository.AgentRepository 实现。
type TraceCleanupRepo interface {
	// ListExpiredTraceSegments 列出开始时间早于 before、带原文对象的片段行。
	ListExpiredTraceSegments(ctx context.Context, before time.Time, limit int) ([]model.AgentTraceSpan, error)
	// DeleteTraceSpansByID 按主键删除索引行。
	DeleteTraceSpansByID(ctx context.Context, ids []uint64) (int64, error)
	// DeleteExpiredTraceSpans 删除开始时间早于 before 的其余索引行（不含带对象的片段行）。
	DeleteExpiredTraceSpans(ctx context.Context, before time.Time, limit int) (int64, error)
	// DeleteEventsBefore 删除创建时间早于 before 的 Agent 事件。
	DeleteEventsBefore(ctx context.Context, before time.Time, limit int) (int64, error)
}

// TraceCleanupResult 是一轮清理删掉的数量。
type TraceCleanupResult struct {
	Objects int64 // 删掉的原文对象数
	Spans   int64 // 删掉的索引行数（含片段行）
	Events  int64 // 删掉的 agent_events 行数
}

// TraceCleaner 清理过期的轨迹和事件。
type TraceCleaner struct {
	repo          TraceCleanupRepo
	stores        TraceStores
	retentionDays int
	batch         int
	now           func() time.Time
}

// NewTraceCleaner 创建清理器；retentionDays<=0 取 14。now 为 nil 取系统时间。
func NewTraceCleaner(repo TraceCleanupRepo, stores TraceStores, retentionDays int, now func() time.Time) *TraceCleaner {
	if retentionDays <= 0 {
		retentionDays = 14
	}
	if now == nil {
		now = time.Now
	}
	return &TraceCleaner{repo: repo, stores: stores, retentionDays: retentionDays, batch: TraceCleanupBatch, now: now}
}

// Cleanup 删除超过保留期的数据，返回删掉的数量。分三步，每步分批，每批前先看 ctx，退出信号来了就停：
//  1. 带原文对象的片段行：先删对象再删行。对象删失败就留着行，下一轮重试——先删行会让对象失去索引、成为永远清不掉的孤儿；
//  2. 其余过期的索引行（工具、没有对象的片段）；
//  3. 过期的 agent_events。
//
// 某一步失败立即返回已删数量与错误（下一轮定时任务会继续）。多实例同时跑也安全：删除都是幂等的，互相只会让对方少删几行。
func (c *TraceCleaner) Cleanup(ctx context.Context) (TraceCleanupResult, error) {
	var res TraceCleanupResult
	cutoff := c.now().AddDate(0, 0, -c.retentionDays)

	// 1. 原文对象和它们的片段行
	for {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		segs, err := c.repo.ListExpiredTraceSegments(ctx, cutoff, c.batch)
		if err != nil {
			return res, fmt.Errorf("列出过期的轨迹片段失败：%w", err)
		}
		if len(segs) == 0 {
			break
		}
		var done []uint64
		var firstErr error
		for _, sg := range segs {
			if err := ctx.Err(); err != nil {
				return res, err
			}
			if err := c.deleteObject(ctx, sg); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			res.Objects++
			done = append(done, sg.ID)
		}
		n, err := c.repo.DeleteTraceSpansByID(ctx, done)
		res.Spans += n
		if err != nil {
			return res, fmt.Errorf("删除过期的轨迹片段行失败：%w", err)
		}
		if len(done) == 0 {
			// 一批里一个都没删成（存储不可用）：再列还是这一批，别空转，留到下一轮
			return res, fmt.Errorf("删除轨迹原文对象失败：%w", firstErr)
		}
		if len(segs) < c.batch && firstErr == nil {
			break
		}
	}

	// 2. 其余过期的索引行
	for {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		n, err := c.repo.DeleteExpiredTraceSpans(ctx, cutoff, c.batch)
		res.Spans += n
		if err != nil {
			return res, fmt.Errorf("删除过期的轨迹索引失败：%w", err)
		}
		if n < int64(c.batch) {
			break
		}
	}

	// 3. 过期的事件
	for {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		n, err := c.repo.DeleteEventsBefore(ctx, cutoff, c.batch)
		res.Events += n
		if err != nil {
			return res, fmt.Errorf("删除过期的 Agent 事件失败：%w", err)
		}
		if n < int64(c.batch) {
			break
		}
	}
	return res, nil
}

// deleteObject 删除片段的原文对象。存储配置已被删掉时对象无从删起，也算完成（否则这一行会永远卡住清理）；
// 对象本来就不存在不算错误（Storage.Delete 约定）。
func (c *TraceCleaner) deleteObject(ctx context.Context, sg model.AgentTraceSpan) error {
	h, err := c.stores.Get(ctx, sg.StorageID)
	if errors.Is(err, storage.ErrStorageNotFound) {
		logger.Warn("轨迹：原文所在的存储配置已不存在，只清索引", zap.Uint64("storage_id", sg.StorageID), zap.String("key", sg.ObjectKey))
		return nil
	}
	if err != nil {
		return fmt.Errorf("取存储 %d 失败：%w", sg.StorageID, err)
	}
	if err := h.Storage.Delete(ctx, sg.ObjectKey); err != nil {
		return fmt.Errorf("删除对象 %s 失败：%w", sg.ObjectKey, err)
	}
	return nil
}
