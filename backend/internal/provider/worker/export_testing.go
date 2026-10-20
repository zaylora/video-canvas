package worker

// 本文件只为 internal/tests 下的外部测试包暴露包内符号，业务代码不要引用。

const (
	FailTransfer  = failTransfer
	FailTimeout   = failTimeout
	MaxTraceSteps = maxTraceSteps
)

var (
	ExtOf           = extOf
	ExtOfMime       = extOfMime
	FileExt         = fileExt
	FailureFor      = failureFor
	FirstDelay      = firstDelay
	PollDelay       = pollDelay
	SubmitBackoff   = submitBackoff
	TransferBackoff = transferBackoff
	MergeTrace      = mergeTrace
)
