package engine

import (
	"context"
	"time"
)

// 本文件只为 internal/tests 下的外部测试包暴露包内符号，业务代码不要引用。

const TraceBodyLimit = traceBodyLimit

var (
	CheckURLAllowed    = checkURLAllowed
	NewTokenBucket     = newTokenBucket
	NewProviderLimiter = newProviderLimiter
	NewLimiterSet      = newLimiterSet
)

func (b *tokenBucket) Take(now time.Time) time.Duration { return b.take(now) }

func (b *tokenBucket) Wait(ctx context.Context) error { return b.wait(ctx) }

func (l *providerLimiter) Acquire(ctx context.Context) (release func(), err error) {
	return l.acquire(ctx)
}

// InFlight 返回当前占用的并发名额。
func (l *providerLimiter) InFlight() int { return len(l.sem) }

func (s *limiterSet) Get(key string, rps float64, maxConc int) *providerLimiter {
	return s.get(key, rps, maxConc)
}
