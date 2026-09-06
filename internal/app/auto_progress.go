package app

import (
	"context"

	"github.com/design-maestro/fastlane/internal/domain"
)

type autoHealthProgressKey struct{}

// WithAutoHealthProgress attaches a synchronous observer to a health pass.
// The callback runs after each node result is normalized, allowing the daemon
// to publish progress without coupling the probe engine to files or LuCI.
func WithAutoHealthProgress(ctx context.Context, callback func(domain.NodeHealth)) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if callback == nil {
		return ctx
	}
	return context.WithValue(ctx, autoHealthProgressKey{}, callback)
}

func reportAutoHealthProgress(ctx context.Context, health domain.NodeHealth) {
	if ctx == nil {
		return
	}
	callback, _ := ctx.Value(autoHealthProgressKey{}).(func(domain.NodeHealth))
	if callback != nil {
		callback(health)
	}
}
