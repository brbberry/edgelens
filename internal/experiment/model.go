package experiment

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

type CaptureSpec struct {
	PerfEvents []string
	ByteLimit  int64
}

// Run records the durable facts associated with one experiment.
type Run struct {
	ID          string
	Host        string
	Command     string
	Args        []string
	StartTime   time.Time
	ChildPID    int
	CaptureSpec CaptureSpec
}

// NewRunningRun constructs the durable record for a child that has already
// started successfully. Process launch and supervision belong to the runner.
func NewRunningRun(
	id string,
	host string,
	command string,
	args []string,
	startedAt time.Time,
	childPID int,
	captureSpec CaptureSpec,
) (Run, error) {
	if strings.TrimSpace(id) == "" {
		return Run{}, fmt.Errorf("run ID must not be blank")
	}
	if strings.TrimSpace(host) == "" {
		return Run{}, fmt.Errorf("run host must not be blank")
	}
	if strings.TrimSpace(command) == "" {
		return Run{}, fmt.Errorf("run command must not be blank")
	}
	if startedAt.IsZero() {
		return Run{}, fmt.Errorf("run start time must be set")
	}
	if childPID <= 0 {
		return Run{}, fmt.Errorf("run child PID must be positive")
	}
	if captureSpec.ByteLimit <= 0 {
		return Run{}, fmt.Errorf("capture byte limit must be positive")
	}
	if len(captureSpec.PerfEvents) == 0 {
		return Run{}, fmt.Errorf("capture must request at least one perf event")
	}

	seenEvents := make(map[string]struct{}, len(captureSpec.PerfEvents))
	for _, event := range captureSpec.PerfEvents {
		if strings.TrimSpace(event) == "" {
			return Run{}, fmt.Errorf("capture perf event must not be blank")
		}
		if _, exists := seenEvents[event]; exists {
			return Run{}, fmt.Errorf("capture perf event %q is duplicated", event)
		}
		seenEvents[event] = struct{}{}
	}

	return Run{
		ID:        id,
		Host:      host,
		Command:   command,
		Args:      slices.Clone(args),
		StartTime: startedAt.UTC(),
		ChildPID:  childPID,
		CaptureSpec: CaptureSpec{
			PerfEvents: slices.Clone(captureSpec.PerfEvents),
			ByteLimit:  captureSpec.ByteLimit,
		},
	}, nil
}
