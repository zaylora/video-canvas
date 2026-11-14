package logger

import (
	"context"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// 关联字段：挂在 context 上，Ctx(ctx) 打出的每一行自动带上，业务代码不用手写编号（设计见 docs/design/统一服务器日志）。

// ctxKey 是 context 里存放 state 的键。
type ctxKey struct{}

// state 是一份不可变的关联字段快照：每次 With* 复制一份再改，不影响父 context。
type state struct {
	requestID  string
	userID     uint64
	attempt    int
	parentKind string
	parentID   string
	extra      []zap.Field   // 其他固定字段，如队列任务的 job_kind
	run        *Run          // 当前运行；可能被多个协程共享，内部字段并发安全
	request    *requestScope // 同一个 HTTP 请求共享，访问日志从这里读请求里创建的运行
}

// requestScope 记下一个 HTTP 请求里创建或开始的运行，访问日志汇总行据此带上 run_kind / run_id。
type requestScope struct {
	mu   sync.Mutex
	kind string
	id   string
}

// from 取 ctx 上的 state。handler 有时直接把 *gin.Context 当 ctx 传下去，而 gin 默认不回落到请求的 context，
// 所以拿不到时再从 gin 挂着的 *http.Request 上取一次。
func from(ctx context.Context) state {
	if ctx == nil {
		return state{}
	}
	if s, ok := ctx.Value(ctxKey{}).(state); ok {
		return s
	}
	if req, ok := ctx.Value(gin.ContextRequestKey).(*http.Request); ok && req != nil {
		if s, ok := req.Context().Value(ctxKey{}).(state); ok {
			return s
		}
	}
	return state{}
}

// with 复制当前 state，改完挂回 ctx。
func with(ctx context.Context, edit func(*state)) context.Context {
	s := from(ctx)
	edit(&s)
	return context.WithValue(ctx, ctxKey{}, s)
}

// WithRequest 挂上 HTTP 请求编号，并开一个请求范围（访问日志用它记下请求里创建的运行）。由 RequestID 中间件调用。
func WithRequest(ctx context.Context, requestID string) context.Context {
	return with(ctx, func(s *state) {
		s.requestID = requestID
		s.request = &requestScope{}
	})
}

// WithUser 挂上当前用户。由鉴权中间件调用。
func WithUser(ctx context.Context, userID uint64) context.Context {
	return with(ctx, func(s *state) { s.userID = userID })
}

// WithAttempt 挂上后台任务的第几次执行（从 1 开始）。由队列中间件调用。
func WithAttempt(ctx context.Context, attempt int) context.Context {
	return with(ctx, func(s *state) { s.attempt = attempt })
}

// WithParent 标记本次运行由哪个运行派生（如 Agent 生成的任务指向那次 Agent 运行）。
func WithParent(ctx context.Context, kind, id string) context.Context {
	return with(ctx, func(s *state) { s.parentKind, s.parentID = kind, id })
}

// WithFields 挂上额外的固定字段，之后 Ctx(ctx) 的每一行都带上（如队列任务的 job_kind）。
func WithFields(ctx context.Context, fields ...zap.Field) context.Context {
	return with(ctx, func(s *state) { s.extra = append(append([]zap.Field(nil), s.extra...), fields...) })
}

// NoteRun 把请求里创建的运行记到请求范围上（如入队了一个任务），访问日志汇总行会带上它。不在 HTTP 请求里时是空操作。
func NoteRun(ctx context.Context, kind, id string) {
	if rs := from(ctx).request; rs != nil {
		rs.mu.Lock()
		rs.kind, rs.id = kind, id
		rs.mu.Unlock()
	}
}

// RequestRun 返回请求里最后记下的运行；没有时返回空串。
func RequestRun(ctx context.Context) (kind, id string) {
	rs := from(ctx).request
	if rs == nil {
		return "", ""
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.kind, rs.id
}

// RequestIDOf 返回 ctx 上的请求编号；运行自带的优先。没有时返回空串。
func RequestIDOf(ctx context.Context) string {
	s := from(ctx)
	if s.run != nil && s.run.ref.RequestID != "" {
		return s.run.ref.RequestID
	}
	return s.requestID
}

// Fields 返回 ctx 上的关联字段；没有的字段不输出。运行自带的用户和请求编号优先于 ctx 上的（任务执行时没有 HTTP 请求）。
func Fields(ctx context.Context) []zap.Field {
	s := from(ctx)
	requestID, userID := s.requestID, s.userID
	parentKind, parentID := s.parentKind, s.parentID
	var fields []zap.Field
	if r := s.run; r != nil {
		if r.ref.RequestID != "" {
			requestID = r.ref.RequestID
		}
		if uid := r.user(); uid != 0 {
			userID = uid
		}
		if r.ref.ParentKind != "" {
			parentKind, parentID = r.ref.ParentKind, r.ref.ParentID
		}
	}
	if requestID != "" {
		fields = append(fields, zap.String("request_id", requestID))
	}
	if userID != 0 {
		fields = append(fields, zap.Uint64("user_id", userID))
	}
	if r := s.run; r != nil {
		fields = append(fields, zap.String("run_kind", r.ref.Kind), zap.String("run_id", r.ref.ID))
	}
	if parentKind != "" {
		fields = append(fields, zap.String("parent_kind", parentKind), zap.String("parent_id", parentID))
	}
	if s.attempt > 0 {
		fields = append(fields, zap.Int("attempt", s.attempt))
	}
	return append(fields, s.extra...)
}

// Ctx 返回带关联字段的 logger。运行相关的日志都用它打，不要手写 task_id / run_id 等编号字段。
func Ctx(ctx context.Context) *zap.Logger {
	fields := Fields(ctx)
	if len(fields) == 0 {
		return l
	}
	return l.With(fields...)
}
