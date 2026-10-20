package queue

import "sync/atomic"

// gate 是本实例上的可调并发闸门。River 队列的 MaxWorkers 启动后改不了，所以让它等于配置文件里的硬上限，
// 实际并发由 gate 控制：后台修改并发只是改 limit，立即生效；调低不会中断正在执行的任务，只影响新的获取。
type gate struct {
	limit    atomic.Int32 // 当前生效的并发上限
	inflight atomic.Int32 // 正在执行的数量
	cap      int32        // 硬上限（配置文件）
}

func newGate(limit, hardCap int) *gate {
	g := &gate{cap: int32(hardCap)}
	g.setLimit(limit)
	return g
}

// setLimit 设置并发上限，裁剪到 [1, 硬上限]，返回实际生效的值。
func (g *gate) setLimit(n int) int {
	n = max(1, min(n, int(g.cap)))
	g.limit.Store(int32(n))
	return n
}

// tryAcquire 尝试占一个执行名额，满了返回 false（调用方稍后再试）。
func (g *gate) tryAcquire() bool {
	for {
		cur := g.inflight.Load()
		if cur >= g.limit.Load() {
			return false
		}
		if g.inflight.CompareAndSwap(cur, cur+1) {
			return true
		}
	}
}

// release 归还一个执行名额。
func (g *gate) release() { g.inflight.Add(-1) }
