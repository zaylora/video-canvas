package plugin

import (
	"context"
	"errors"
	"fmt"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/netguard"
)

// 宿主自己产生、provider 包之外的错误码（provider.Error.Code）。
const (
	codeSecretUnavailable = "secret_unavailable" // 渠道 Key 未设置或无法解密
	codeInvalidInput      = "invalid_input"      // 任务输入不符合 input_schema（防御性再校验）
	codeAssetUnavailable  = "asset_unavailable"  // 文件引用指向的素材不存在、不属于任务用户或没有可访问地址
	codeMediaTooLarge     = "media_too_large"    // base64 / dataUrl 内联的素材超过上限
	codeResponseTooLarge  = "response_too_large" // 上游响应体超过读取上限
	codeUnsupportedKind   = "unsupported_kind"   // 渠道固定的插件版本不支持模型的 kind
	codeInvalidSnapshot   = "invalid_snapshot"   // 快照缺失或渠道 base_url 不合法
	codeHostMisconfigured = "host_misconfigured" // 宿主缺少必需的依赖（素材存储、凭证解析器、代码仓库）
	codePreparePersist    = "prepare_persist"    // 准备阶段结果落库失败
	codeDownloadFailed    = "download_failed"    // 结果下载的非 2xx
	codeHTTPPrefix        = "http_"              // 上游非 2xx 的默认错误码前缀，后接状态码
)

// pluginFault 构造插件级失败：terminal + CodePluginError + PluginFault。插件抛异常、超时、返回值或请求描述不合规都走这里。
func pluginFault(msg string) *provider.Error {
	return &provider.Error{Class: provider.ClassTerminal, Code: provider.CodePluginError, Message: msg, PluginFault: true}
}

// pluginFaultf 是带格式化参数的 pluginFault。
func pluginFaultf(format string, args ...any) *provider.Error {
	return pluginFault(fmt.Sprintf(format, args...))
}

// terminalErr 构造不可重试、不算插件级失败的错误（配置问题、素材问题、上游拒绝）。
func terminalErr(code, msg string, cause error) *provider.Error {
	return &provider.Error{Class: provider.ClassTerminal, Code: code, Message: msg, Cause: cause}
}

// retryableErr 构造可重试的错误（网络抖动、存储暂时不可用）。
func retryableErr(code, msg string, cause error) *provider.Error {
	return &provider.Error{Class: provider.ClassRetryable, Code: code, Message: msg, Cause: cause}
}

// safeSentinels 是 redactedError 保留下来、供上层 errors.Is 判断的哨兵。
var safeSentinels = []error{
	context.Canceled, context.DeadlineExceeded,
	netguard.ErrBlockedAddress, netguard.ErrHostNotAllowed, netguard.ErrTooManyRedirects, netguard.ErrResponseTooLarge,
	ErrRunnerUnavailable, ErrRunnerCrashed, provider.ErrAssetNotFound,
}

// redactedError 是脱敏后的错误。标准库的 *url.Error 会把完整 URL（query 鉴权时含 Key）带进错误文本，
// 所以宿主从不把原始传输错误放进 provider.Error.Cause：Error() 只给脱敏后的文本，Unwrap 只暴露已知哨兵，
// 这样沿错误链展开也拿不到明文，errors.Is 仍然有效。
type redactedError struct {
	msg       string
	sentinels []error
}

func (e *redactedError) Error() string { return e.msg }

func (e *redactedError) Unwrap() []error { return e.sentinels }

// redactError 把 err 转成脱敏错误；err 为 nil 时返回 nil。
func redactError(err error, redact func(string) string) error {
	if err == nil {
		return nil
	}
	out := &redactedError{msg: redact(err.Error())}
	for _, s := range safeSentinels {
		if errors.Is(err, s) {
			out.sentinels = append(out.sentinels, s)
		}
	}
	return out
}
