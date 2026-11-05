package queue

import (
	"context"
	"time"

	"github.com/riverqueue/river"
)

// dramaDraftCleaner 由短剧服务提供过期草稿正文清理。
type dramaDraftCleaner interface{ CleanupDramaDrafts(context.Context) error }

type dramaCleanupArgs struct{}

// Kind 是周期回收任务的持久类型。
func (dramaCleanupArgs) Kind() string { return "drama_source_cleanup" }

// InsertOpts 保证多次启动不会堆积清理任务。
func (dramaCleanupArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "drama", MaxAttempts: jobMaxAttempts, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: liveStates}}
}

type dramaCleanupWorker struct {
	river.WorkerDefaults[dramaCleanupArgs]
	cleaner dramaDraftCleaner
}

// Work 只回收已经过期的草稿，不改变有效 token。
func (w *dramaCleanupWorker) Work(ctx context.Context, _ *river.Job[dramaCleanupArgs]) error {
	return w.cleaner.CleanupDramaDrafts(ctx)
}

// registerDramaCleanup 通过 River leader 每小时调度，启动时补一次，停机期间的过期正文也会回收。
func registerDramaCleanup(cfg *river.Config, workers *river.Workers, handler DramaHandler) {
	cleaner, ok := handler.(dramaDraftCleaner)
	if !ok {
		return
	}
	river.AddWorker(workers, &dramaCleanupWorker{cleaner: cleaner})
	cfg.PeriodicJobs = append(cfg.PeriodicJobs, river.NewPeriodicJob(river.PeriodicInterval(time.Hour), func() (river.JobArgs, *river.InsertOpts) { return dramaCleanupArgs{}, nil }, &river.PeriodicJobOpts{ID: "drama_source_cleanup", RunOnStart: true}))
}
