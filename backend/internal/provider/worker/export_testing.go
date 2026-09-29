package worker

import (
	"context"
	"sync"
)

// 本文件只为 internal/tests 下的外部测试包暴露包内符号，业务代码不要引用。

const (
	FailTransfer = failTransfer
	FailTimeout  = failTimeout
)

var (
	ExtOf           = extOf
	FailureFor      = failureFor
	FirstDelay      = firstDelay
	PollDelay       = pollDelay
	SubmitBackoff   = submitBackoff
	TransferBackoff = transferBackoff
)

func (w *Worker) ClaimAndRun(ctx context.Context) (int, error) { return w.claimAndRun(ctx) }

func (w *Worker) Dispatch(ctx context.Context) { w.dispatch(ctx) }

func (w *Worker) WaitGroup() *sync.WaitGroup { return &w.wg }
