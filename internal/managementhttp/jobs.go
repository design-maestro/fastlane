package managementhttp

import (
	"context"
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
	Error       string `json:"error,omitempty"`
}

type jobTracker struct {
	mu       sync.Mutex
	sequence uint64
	current  jobSnapshot
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

	go func(sequence uint64) {
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
		if err != nil {
			j.current.Error = "operation_failed"
		}
	}(started.Sequence)

	return started, true
}

func (j *jobTracker) snapshot() jobSnapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.current
}
