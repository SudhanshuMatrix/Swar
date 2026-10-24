package catalog

import (
	"context"
	"errors"
)

type priorityKey struct{}

// Background marks ctx as speculative work (prefetching the next song).
// Sources that run expensive work give it lower priority than requests a
// listener is waiting for.
func Background(ctx context.Context) context.Context {
	return context.WithValue(ctx, priorityKey{}, true)
}

// IsBackground reports whether ctx was marked with Background.
func IsBackground(ctx context.Context) bool {
	v, _ := ctx.Value(priorityKey{}).(bool)
	return v
}

// ErrBackgroundBusy is returned (wrapped in a KindRateLimited error) when
// background work was refused so that capacity stays free for listeners.
// It is never cached; a listener's own request retries instead.
var ErrBackgroundBusy = errors.New("background work refused: capacity reserved for foreground requests")

// IsBackgroundBusy reports whether err is a refusal of background work.
func IsBackgroundBusy(err error) bool { return errors.Is(err, ErrBackgroundBusy) }
