package experiment

import (
	"context"
	"errors"
	"fmt"
	"time"

	perfadapter "github.com/brbberry/edgelens/internal/perf"
)

type ProcessOutcome struct {
	FinishedAt   time.Time
	Elapsed      time.Duration
	ExitCode     *int
	Signal       string
	PerfVersion  string
	PerfStat     string
	FoldedStacks string
	HeapSummary  string
	Err          error
}

type CaptureSession interface {
	PID() int
	Wait() perfadapter.Result
}

type CaptureBackend interface {
	Preflight(context.Context, []string, bool) (perfadapter.PreflightResult, error)
	Start(context.Context, perfadapter.PreflightResult, string, []string, []string, bool, int) (CaptureSession, error)
	AnalyzeGoHeap(context.Context, string, int) (string, error)
}

type OSCaptureBackend struct{}

func (OSCaptureBackend) Preflight(ctx context.Context, events []string, collectFlame bool) (perfadapter.PreflightResult, error) {
	return perfadapter.Preflight(ctx, perfadapter.OSCommandRunner{}, events, collectFlame)
}

func (OSCaptureBackend) Start(
	ctx context.Context,
	preflight perfadapter.PreflightResult,
	command string,
	args, events []string,
	collectFlame bool,
	maxBytes int,
) (CaptureSession, error) {
	return perfadapter.Start(ctx, preflight, command, args, events, collectFlame, maxBytes)
}

func (OSCaptureBackend) AnalyzeGoHeap(ctx context.Context, path string, maxBytes int) (string, error) {
	return perfadapter.AnalyzeGoHeap(ctx, path, maxBytes)
}

func StartRun(
	ctx context.Context,
	backend CaptureBackend,
	id, host, command string,
	args []string,
	captureSpec CaptureSpec,
	collectFlame bool,
	heapProfilePath string,
) (Run, <-chan ProcessOutcome, error) {
	if backend == nil {
		return Run{}, nil, fmt.Errorf("capture backend must not be nil")
	}
	// Validate all request-controlled fields before launching a workload. PID 1
	// is a temporary valid placeholder; the returned record is discarded.
	startedAt := time.Now()
	if _, err := NewRunningRun(id, host, command, args, startedAt, 1, captureSpec); err != nil {
		return Run{}, nil, err
	}

	preflight, err := backend.Preflight(ctx, captureSpec.PerfEvents, collectFlame)
	if err != nil {
		return Run{}, nil, err
	}
	session, err := backend.Start(ctx, preflight, command, args, captureSpec.PerfEvents, collectFlame, int(captureSpec.ByteLimit))
	if err != nil {
		return Run{}, nil, err
	}
	run, err := NewRunningRun(id, host, command, args, startedAt, session.PID(), captureSpec)
	if err != nil {
		// Start succeeded, so preserve the required Start/Wait pairing even for
		// a backend that returned an invalid PID.
		go session.Wait()
		return Run{}, nil, fmt.Errorf("construct running experiment: %w", err)
	}

	outcomes := make(chan ProcessOutcome, 1)
	go func() {
		defer close(outcomes)
		result := session.Wait()
		outcome := ProcessOutcome{
			FinishedAt: result.FinishedAt, Elapsed: result.Elapsed,
			ExitCode: result.ExitCode, Signal: result.Signal,
			PerfVersion: preflight.Version, PerfStat: result.StatText,
			FoldedStacks: result.FoldedStacks, Err: result.Err,
		}
		if heapProfilePath != "" {
			heapContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			outcome.HeapSummary, err = backend.AnalyzeGoHeap(heapContext, heapProfilePath, int(captureSpec.ByteLimit))
			cancel()
			if err != nil {
				outcome.Err = errors.Join(outcome.Err, fmt.Errorf("heap analysis: %w", err))
			}
		}
		outcomes <- outcome
	}()
	return run, outcomes, nil
}
