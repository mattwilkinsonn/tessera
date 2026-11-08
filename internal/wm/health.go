package wm

import (
	"context"
	"sync/atomic"
	"time"
)

const SpawnReadyUptime = 30 * time.Second

type SpawnHealth interface {
	ServiceUptime(context.Context) (time.Duration, error)
}

type queryFailureKey struct{}

func TrackQueryFailures(ctx context.Context) context.Context {
	if _, ok := ctx.Value(queryFailureKey{}).(*atomic.Bool); ok {
		return ctx
	}
	return context.WithValue(ctx, queryFailureKey{}, &atomic.Bool{})
}

func MarkQueryFailed(ctx context.Context) {
	if failed, ok := ctx.Value(queryFailureKey{}).(*atomic.Bool); ok {
		failed.Store(true)
	}
}

func QueryFailed(ctx context.Context) bool {
	failed, ok := ctx.Value(queryFailureKey{}).(*atomic.Bool)
	return ok && failed.Load()
}
