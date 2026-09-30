package initialize

import (
	"context"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
	"video-canvas/internal/service"
	"video-canvas/plugins"
)

// 登记内置插件时等 runner 就绪的重试参数：runner 是另一个进程 / 容器，可能比主服务晚几秒起来。
const (
	builtinRegisterAttempts = 5
	builtinRegisterWait     = 2 * time.Second
)

// registerBuiltinPlugins 把随二进制发布的内置插件登记为 builtin 来源（按 key + version，幂等）。
// 登记要先过 runner 的预检，所以 runner 暂时连不上时按固定间隔重试几次；其余错误（预检不通过、
// 版本内容变了没升版本号、key 被占用）重试没有意义，直接记 Error 日志。
// 登记失败不阻止服务启动：主服务的其他功能不依赖它，修好后重启即可补登记。
func registerBuiltinPlugins(ctx context.Context, svc *service.AIPluginService) {
	sources, err := plugins.Builtin()
	if err != nil {
		logger.Error("读取内置插件失败", zap.Error(err))
		return
	}
	for attempt := 1; ; attempt++ {
		regs, err := svc.RegisterBuiltin(ctx, sources)
		for _, r := range regs {
			if r.Registered {
				logger.Info("登记内置插件", zap.String("key", r.Key), zap.String("version", r.Version))
			}
		}
		if err == nil {
			return
		}
		if provider.CodeOf(err) != provider.CodeRunnerUnavailable || attempt >= builtinRegisterAttempts {
			logger.Error("登记内置插件失败", zap.Error(err), zap.Int("attempt", attempt))
			return
		}
		logger.Warn("plugin-runner 暂不可用，稍后重试登记内置插件", zap.Int("attempt", attempt), zap.Error(err))
		select {
		case <-ctx.Done():
			logger.Error("登记内置插件被中断", zap.Error(ctx.Err()))
			return
		case <-time.After(builtinRegisterWait):
		}
	}
}
