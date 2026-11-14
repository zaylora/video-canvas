package logger

import (
	"context"

	"go.uber.org/zap"
)

// Call 是业务事先声明的一次上游调用：用途和业务位置（如 call=episode=3、try=2）。
// 网关、插件宿主、统一出站客户端记录这次调用时取它，app.log 的调用行和 payload.log 用同一个 ID。
type Call struct {
	ID      string
	Purpose string      // 如 drama.scripts、mkt.reverse、agent.main
	Fields  []zap.Field // 业务位置等附加字段
}

type callKey struct{}

// WithCall 声明接下来的一次上游调用，返回带调用信息的 ctx 和它的 call_id；业务可以用这个 ID 在 run.step 里指向这次调用。
func WithCall(ctx context.Context, purpose string, fields ...zap.Field) (context.Context, string) {
	c := Call{ID: NewCallID(), Purpose: purpose, Fields: fields}
	return context.WithValue(ctx, callKey{}, c), c.ID
}

// WithPurpose 只声明接下来调用的用途，不固定 call_id：适合一次声明、内部可能调用多次的场景（如带重写的环节运行），
// 每次调用各自生成 call_id。
func WithPurpose(ctx context.Context, purpose string, fields ...zap.Field) context.Context {
	return context.WithValue(ctx, callKey{}, Call{Purpose: purpose, Fields: fields})
}

// CallOf 返回 ctx 上声明的调用；没有声明时 ID 为空，调用方自己生成。
func CallOf(ctx context.Context) Call {
	if ctx == nil {
		return Call{}
	}
	c, _ := ctx.Value(callKey{}).(Call)
	return c
}
