package managementhttp

import (
	"context"
	"errors"
	"sync"
	"time"
)

type jobSnapshot struct {
	Sequence    uint64 `json:"sequence"`
	Kind        string `json:"kind,omitempty"`
	Running     bool   `json:"running"`
	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	Succeeded   bool   `json:"succeeded"`
	Cancelled   bool   `json:"cancelled,omitempty"`
	Error       string `json:"error,omitempty"`
}

type jobTracker struct {
	mu       sync.Mutex
	sequence uint64
	current  jobSnapshot
	cancel   context.CancelFunc
}

func (j *jobTracker) start(ctx context.Context, kind string, run func(context.Context) error) (jobSnapshot, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.current.Running {
		return j.current, false
	}

	j.sequence++
	now := time.Now().UTC()
	j.current = jobSnapshot{
		Sequence:  j.sequence,
		Kind:      kind,
		Running:   true,
		StartedAt: now.Format(time.RFC3339),
	}
	started := j.current
	ctx, cancel := context.WithCancel(ctx)
	j.cancel = cancel

	go func(sequence uint64) {
		defer cancel()
		err := run(ctx)
		completedAt := time.Now().UTC().Format(time.RFC3339)

		j.mu.Lock()
		defer j.mu.Unlock()
		if j.current.Sequence != sequence {
			return
		}
		j.current.Running = false
		j.current.CompletedAt = completedAt
		j.current.Succeeded = err == nil
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			j.current.Succeeded = false
			j.current.Cancelled = true
			j.current.Error = "operation_cancelled"
			return
		}
		if err != nil {
			j.current.Error = "operation_failed"
			var public panelJobError
			if errors.As(err, &public) {
				j.current.Error = string(public)
			}
		}
	}(started.Sequence)

	return started, true
}

// Cancellation applies only to a currently running check, never to settings,
// subscription writes, or a different operation that replaced an old check.
func (j *jobTracker) cancelCheck(sequence uint64) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.current.Running || j.current.Sequence != sequence || (j.current.Kind != "health-check" && j.current.Kind != "connect-auto") || j.cancel == nil {
		return false
	}
	j.cancel()
	return true
}

func (j *jobTracker) snapshot() jobSnapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.current
}
