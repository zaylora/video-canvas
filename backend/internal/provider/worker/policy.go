package worker

import (
	"path"
	"strings"
	"time"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/pluginmeta"
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
// 拿到了上游的错误原文（upstream 非空）时文案就是原文（去掉上游的请求编号），让用户看到上游真实的失败原因；
// 宿主自己判定的失败（超时、转存失败、提交结果未知）没有上游原文，用固定文案。
// 插件级失败（异常、超时、结构不合规、runner 崩溃）由调用方传空 upstream，落到“平台繁忙”，用户看不到插件的存在。
func failureFor(class provider.ErrorClass, platformCode, upstream string) (code, message string) {
	switch class {
	case provider.ClassSubmitUnknown:
		return codeSubmitUnknown, "提交结果未知，积分已退回"
	case failTimeout:
		return codeTimeout, "生成超时，积分已退回"
	case failTransfer:
		return codeTransferFailed, "生成结果保存失败，积分已退回"
	}
	code, message = codeProviderError, "平台繁忙，请稍后重试"
	switch {
	case class == provider.ClassModeration:
		code, message = codeModeration, "内容未通过审核"
	case class == provider.ClassProviderBalance:
		code, message = codeProviderBalance, "服务繁忙，请稍后再试"
	case platformCode == codeInvalidParam:
		code, message = codeInvalidParam, "参数不合法，请调整后重试"
	}
	// 上游自己的请求编号对用户没用，去掉；排查用平台的任务编号（界面上和失败原因一起展示）
	if upstream = clipMessage(provider.StripTraceIDs(upstream)); upstream != "" {
		message = upstream
	}
	return code, message
}

// maxMessageRunes 是给用户看的失败文案的字数上限：任务表 error_message 是 varchar(512)。
const maxMessageRunes = 500

// clipMessage 按字数截断失败文案，超出加省略号。
func clipMessage(s string) string {
	if r := []rune(s); len(r) > maxMessageRunes {
		return string(r[:maxMessageRunes]) + "…"
	}
	return s
}

// 轮询与退避的默认值（插件 meta.poll 没写时使用，见 backend/docs/plugin-contract.md §2）。
const (
	defaultFirstDelay  = 10 * time.Second
	defaultInterval    = 5 * time.Second
	defaultMaxInterval = 15 * time.Second
	defaultJitter      = 0.2
)

// seconds 把 meta.poll 里以秒为单位的数值换成 time.Duration；非正数返回 0，由调用方回落到默认值。
func seconds(v float64) time.Duration {
	if v <= 0 {
		return 0
	}
	return time.Duration(v * float64(time.Second))
}

// firstDelay 是提交成功后第一次查询的延迟，插件没声明时缺省 10 秒。
func firstDelay(p *pluginmeta.PollMeta) time.Duration {
	if p != nil {
		if d := seconds(p.FirstDelay); d > 0 {
			return d
		}
	}
	return defaultFirstDelay
}

// pollDelay 是第 attempts 次查询之后到下一次查询的间隔：从 interval 起每次翻倍，封顶 maxInterval，
// 再加 ±jitter 抖动（避免大量任务同时查询）。缺省 5s → 15s、20%。attempts 从 1 开始；p 为 nil 全部用默认值。
func pollDelay(p *pluginmeta.PollMeta, attempts int, rnd func() float64) time.Duration {
	var cfg pluginmeta.PollMeta
	if p != nil {
		cfg = *p
	}
	interval := seconds(cfg.Interval)
	if interval <= 0 {
		interval = defaultInterval
	}
	maxInterval := seconds(cfg.MaxInterval)
	if maxInterval <= 0 {
		maxInterval = defaultMaxInterval
	}
	// 插件把封顶值写得比起始间隔还小时，以起始间隔为准，避免间隔越退越短
	if maxInterval < interval {
		maxInterval = interval
	}
	jitter := cfg.Jitter
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

// extOf 根据产物类型（如 mp4）生成文件扩展名；只保留字母数字，避免奇怪的文件名。
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

// mimeSubtypeExt 是子类型名与常用扩展名不一致的 MIME。不用标准库 mime.ExtensionsByType：
// 它在 Windows 上读注册表，结果随机器而变。
var mimeSubtypeExt = map[string]string{
	"mpeg":      "mp3",
	"quicktime": "mov",
	"jpeg":      "jpg",
	"x-wav":     "wav",
	"x-m4a":     "m4a",
	"svg+xml":   "svg",
}

// extOfMime 由 MIME（如 video/mp4、audio/mpeg; charset=…）推断扩展名，推断不出返回空串。
func extOfMime(mime string) string {
	mime = strings.ToLower(strings.TrimSpace(mime))
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	_, sub, ok := strings.Cut(mime, "/")
	if !ok {
		return ""
	}
	if e, found := mimeSubtypeExt[sub]; found {
		sub = e
	}
	return extOf(sub)
}

// fileExt 给转存的产物挑一个扩展名：插件声明的 MIME 优先，其次下载响应的 Content-Type，最后看 URL 路径的后缀。
func fileExt(declaredMime, contentType, rawURL string) string {
	if e := extOfMime(declaredMime); e != "" {
		return e
	}
	if e := extOfMime(contentType); e != "" {
		return e
	}
	p := rawURL
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	return extOf(path.Ext(p))
}
