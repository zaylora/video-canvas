package provider

import "context"

type canvasAccessKey struct{}

type canvasAccess struct{ canvas, epoch uint64 }

// WithCanvasAccess 限定当前模型调用可引用的协作会话。
func WithCanvasAccess(ctx context.Context, canvas, epoch uint64) context.Context {
	if canvas == 0 || epoch == 0 {
		return ctx
	}
	return context.WithValue(ctx, canvasAccessKey{}, canvasAccess{canvas, epoch})
}

// CanvasAccess 返回素材查找需要复核的协作画布和会话。
func CanvasAccess(ctx context.Context) (canvas uint64, epoch uint64) {
	access, _ := ctx.Value(canvasAccessKey{}).(canvasAccess)
	return access.canvas, access.epoch
}
