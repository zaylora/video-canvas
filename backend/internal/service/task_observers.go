package service

import (
	"context"
	"errors"

	"video-canvas/internal/model"
)

// TaskObservers 把多个关联业务（短剧片段、营销脚本……）串成一个生成任务观察者：
// 每个观察者自己按节点前缀认领任务，互不影响；某个观察者出错不挡住其他观察者，错误合并后一起返回。
type TaskObservers []GenerationTaskObserver

// OnTaskChanged 依次通知全部观察者。
func (o TaskObservers) OnTaskChanged(ctx context.Context, t *model.GenerationTask) error {
	var errs []error
	for _, ob := range o {
		if err := ob.OnTaskChanged(ctx, t); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

var _ GenerationTaskObserver = TaskObservers(nil)
