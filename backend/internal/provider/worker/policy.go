package worker

import (
	"strings"
	"time"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/dsl"
)

// 仅 worker 内部使用的伪分类，用来复用 failureFor 的统一映射。
const (
	failTransfer provider.ErrorClass = "transfer_failed" // 转存失败
	failTimeout  provider.ErrorClass = "timeout"         // 超时
)

// 统一错误码（generation_tasks.error_code）。
const (
	codeModeration      = "moderation"
	codeProviderBalance = "provider_balance"
	codeSubmitUnknown   = "submit_unknown"
	codeProviderError   = "provider_error"
	codeTimeout         = "timeout"
	codeTransferFailed  = "transfer_failed"
	codeInvalidParam    = "invalid_param"
)

// failureFor 把错误分类映射成（统一错误码，给用户看的文案）。
// 文案是固定的，不带任何平台原始信息；原始信息只进日志。
func failureFor(class provider.ErrorClass, platformCode string) (code, message string) {
	switch class {
	case provider.ClassModeration:
		return codeModeration, "内容未通过审核"
	case provider.ClassProviderBalance:
		return codeProviderBalance, "服务繁忙，请稍后再试"
	case provider.ClassSubmitUnknown:
		return codeSubmitUnknown, "提交结果未知，积分已退回"
	case failTimeout:
		return codeTimeout, "生成超时，积分已退回"
	case failTransfer:
		return codeTransferFailed, "生成结果保存失败，积分已退回"
	}
	if platformCode == codeInvalidParam {
		return codeInvalidParam, "参数不合法，请调整后重试"
	}
	return codeProviderError, "平台繁忙，请稍后重试"
}

// 轮询与退避的默认值（平台配置没写时使用）。
const (
	defaultFirstDelay  = 10 * time.Second
	defaultInterval    = 5 * time.Second
	defaultMaxInterval = 15 * time.Second
	defaultJitter      = 0.2
)

// firstDelay 是提交成功后第一次查询的延迟，缺省 10 秒。
func firstDelay(p dsl.PollConfig) time.Duration {
	if d := p.FirstDelay.D(); d > 0 {
		return d
	}
	return defaultFirstDelay
}

// pollDelay 是第 attempts 次查询之后到下一次查询的间隔：从 interval 起每次翻倍，封顶 max_interval，
// 再加 ±jitter 抖动（避免大量任务同时查询）。缺省 5s → 15s、20%。attempts 从 1 开始。
func pollDelay(p dsl.PollConfig, attempts int, rnd func() float64) time.Duration {
	interval := p.Interval.D()
	if interval <= 0 {
		interval = defaultInterval
	}
	maxInterval := p.MaxInterval.D()
	if maxInterval <= 0 {
		maxInterval = defaultMaxInterval
	}
	if maxInterval < interval {
		maxInterval = interval
	}
	jitter := p.Jitter
	if jitter <= 0 {
		jitter = defaultJitter
	}
	if jitter > 1 {
		jitter = 1
	}
	return applyJitter(expBackoff(interval, maxInterval, attempts), jitter, rnd)
}

// submitBackoff 提交重试的退避：2s、4s、8s …… 封顶 60s。
func submitBackoff(attempt int) time.Duration {
	return expBackoff(2*time.Second, 60*time.Second, attempt)
}

// transferBackoff 转存重试的退避：10s、20s、40s …… 封顶 10 分钟。
func transferBackoff(attempt int) time.Duration {
	return expBackoff(10*time.Second, 10*time.Minute, attempt)
}

// expBackoff 返回 base * 2^(attempt-1)，不超过 limit。attempt < 1 按 1 处理。
func expBackoff(base, limit time.Duration, attempt int) time.Duration {
	d := base
	for i := 1; i < attempt && d < limit; i++ {
		d *= 2
	}
	if d > limit {
		d = limit
	}
	return d
}

// applyJitter 给 d 加上 ±ratio 的随机抖动，rnd 返回 [0,1)。
func applyJitter(d time.Duration, ratio float64, rnd func() float64) time.Duration {
	if ratio <= 0 {
		return d
	}
	factor := 1 + ratio*(2*rnd()-1)
	return time.Duration(float64(d) * factor)
}

// extOf 根据平台给的产物类型（如 mp4）生成文件扩展名；只保留字母数字，避免奇怪的文件名。
func extOf(typ string) string {
	typ = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(typ)), ".")
	if typ == "" || len(typ) > 8 {
		return ""
	}
	for _, r := range typ {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return ""
		}
	}
	return "." + typ
}
