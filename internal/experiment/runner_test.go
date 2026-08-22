package experiment

import (
	"context"
	"errors"
	"testing"
	"time"

	perfadapter "github.com/brbberry/edgelens/internal/perf"
)

type fakeCaptureSession struct {
	pid     int
	release <-chan struct{}
	result  perfadapter.Result
}

func (session *fakeCaptureSession) PID() int { return session.pid }
func (session *fakeCaptureSession) Wait() perfadapter.Result {
	<-session.release
	return session.result
}

type fakeCaptureBackend struct {
	session    CaptureSession
	prepareErr error
	startErr   error
	heapText   string
}

func (backend *fakeCaptureBackend) Preflight(context.Context, []string, bool) (perfadapter.PreflightResult, error) {
	return perfadapter.PreflightResult{Executable: "perf", Version: "perf test"}, backend.prepareErr
}
func (backend *fakeCaptureBackend) Start(context.Context, perfadapter.PreflightResult, string, []string, []string, bool, int) (CaptureSession, error) {
	return backend.session, backend.startErr
}
func (backend *fakeCaptureBackend) AnalyzeGoHeap(context.Context, string, int) (string, error) {
	return backend.heapText, nil
}

func TestStartRunReturnsBeforeProcessCompletion(t *testing.T) {
	release := make(chan struct{})
	exitCode := 0
	finishedAt := time.Now().UTC()
	backend := &fakeCaptureBackend{session: &fakeCaptureSession{
		pid: 42, release: release,
		result: perfadapter.Result{FinishedAt: finishedAt, Elapsed: time.Second, ExitCode: &exitCode, StatText: "cycles"},
	}, heapText: "heap top"}
	capture := CaptureSpec{PerfEvents: []string{"cycles"}, ByteLimit: 1024}

	run, outcomes, err := StartRun(context.Background(), backend, "run-1", "node-1", "work", nil, capture, true, "heap.pb.gz")
	if err != nil {
		t.Fatal(err)
	}
	if run.ChildPID != 42 {
		t.Fatalf("running record = %+v", run)
	}
	select {
	case <-outcomes:
		t.Fatal("outcome arrived before fake process completed")
	default:
	}

	close(release)
	outcome, ok := <-outcomes
	if !ok || outcome.PerfStat != "cycles" || outcome.HeapSummary != "heap top" || outcome.PerfVersion != "perf test" {
		t.Fatalf("outcome = %+v, open = %t", outcome, ok)
	}
	if _, ok := <-outcomes; ok {
		t.Fatal("outcome channel produced more than one result")
	}
}

func TestStartRunFailsBeforeLaunchWhenPreflightFails(t *testing.T) {
	backend := &fakeCaptureBackend{prepareErr: errors.New("permission denied")}
	_, outcomes, err := StartRun(context.Background(), backend, "run", "host", "work", nil,
		CaptureSpec{PerfEvents: []string{"cycles"}, ByteLimit: 1024}, false, "")
	if err == nil || outcomes != nil {
		t.Fatalf("StartRun() outcomes = %v, error = %v", outcomes, err)
	}
}
