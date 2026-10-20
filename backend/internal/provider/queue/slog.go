package queue

import (
	"context"
	"fmt"
	"log/slog"

	"go.uber.org/zap"
)

// zapSlogHandler 把 River（使用 slog）的日志转进项目的 zap：Warn 及以上按原级别输出，
// Info 和 Debug 降为 Debug——River 启停、选主、维护任务的 Info 很频繁，放进主日志只会淹没业务日志。
type zapSlogHandler struct {
	log   *zap.Logger
	attrs []slog.Attr
	group string
}

func newSlog(log *zap.Logger) *slog.Logger {
	return slog.New(&zapSlogHandler{log: log.Named("river")})
}

// Enabled 实现 slog.Handler：交给 zap 自己按级别过滤。
func (h *zapSlogHandler) Enabled(context.Context, slog.Level) bool { return true }

// Handle 实现 slog.Handler。
func (h *zapSlogHandler) Handle(_ context.Context, r slog.Record) error {
	fields := make([]zap.Field, 0, len(h.attrs)+r.NumAttrs())
	for _, a := range h.attrs {
		fields = append(fields, h.field(a))
	}
	r.Attrs(func(a slog.Attr) bool {
		fields = append(fields, h.field(a))
		return true
	})
	switch {
	case r.Level >= slog.LevelError:
		h.log.Error(r.Message, fields...)
	case r.Level >= slog.LevelWarn:
		h.log.Warn(r.Message, fields...)
	default:
		h.log.Debug(r.Message, fields...)
	}
	return nil
}

func (h *zapSlogHandler) field(a slog.Attr) zap.Field {
	key := a.Key
	if h.group != "" {
		key = h.group + "." + key
	}
	return zap.String(key, fmt.Sprint(a.Value.Resolve().Any()))
}

// WithAttrs 实现 slog.Handler。
func (h *zapSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	n := *h
	n.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &n
}

// WithGroup 实现 slog.Handler。
func (h *zapSlogHandler) WithGroup(name string) slog.Handler {
	n := *h
	if n.group != "" {
		name = n.group + "." + name
	}
	n.group = name
	return &n
}
