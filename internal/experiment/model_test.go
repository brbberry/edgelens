package experiment

import (
	"slices"
	"testing"
	"time"
)

func TestNewRunningRun(t *testing.T) {
	startedAt := time.Date(
		2026, time.August, 22, 12, 30, 0, 0,
		time.FixedZone("EDT", -4*60*60),
	)
	args := []string{"--label", "", "--size", "4096"}
	capture := CaptureSpec{
		PerfEvents: []string{"cycles", "instructions"},
		ByteLimit:  32 * 1024,
	}

	run, err := NewRunningRun(
		"run-123", "compute-01", "./benchmark",
		args, startedAt, 4321, capture,
	)
	if err != nil {
		t.Fatalf("NewRunningRun() unexpected error: %v", err)
	}

	if run.ID != "run-123" || run.Host != "compute-01" {
		t.Fatalf("unexpected identity: ID=%q Host=%q", run.ID, run.Host)
	}
	if !run.StartTime.Equal(startedAt) {
		t.Fatalf("StartTime = %v, want instant %v", run.StartTime, startedAt)
	}
	if run.StartTime.Location() != time.UTC {
		t.Fatalf("StartTime location = %v, want UTC", run.StartTime.Location())
	}
	if run.ChildPID != 4321 {
		t.Fatalf("ChildPID = %v, want 4321", run.ChildPID)
	}
	if !slices.Equal(run.Args, args) {
		t.Fatalf("Args = %q, want %q", run.Args, args)
	}
}

func TestNewRunningRunRejectsInvalidInput(t *testing.T) {
	startedAt := time.Date(2026, time.August, 22, 12, 0, 0, 0, time.UTC)
	validCapture := CaptureSpec{
		PerfEvents: []string{"cycles", "instructions"},
		ByteLimit:  1024,
	}

	tests := []struct {
		name    string
		id      string
		host    string
		command string
		start   time.Time
		pid     int
		capture CaptureSpec
	}{
		{"blank ID", " ", "host", "true", startedAt, 42, validCapture},
		{"blank host", "id", " ", "true", startedAt, 42, validCapture},
		{"blank command", "id", "host", " ", startedAt, 42, validCapture},
		{"zero start time", "id", "host", "true", time.Time{}, 42, validCapture},
		{"zero PID", "id", "host", "true", startedAt, 0, validCapture},
		{"negative PID", "id", "host", "true", startedAt, -1, validCapture},
		{"zero byte limit", "id", "host", "true", startedAt, 42,
			CaptureSpec{PerfEvents: []string{"cycles"}}},
		{"no events", "id", "host", "true", startedAt, 42,
			CaptureSpec{ByteLimit: 1024}},
		{"blank event", "id", "host", "true", startedAt, 42,
			CaptureSpec{PerfEvents: []string{"cycles", " "}, ByteLimit: 1024}},
		{"duplicate event", "id", "host", "true", startedAt, 42,
			CaptureSpec{PerfEvents: []string{"cycles", "cycles"}, ByteLimit: 1024}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewRunningRun(
				test.id, test.host, test.command, nil,
				test.start, test.pid, test.capture,
			)
			if err == nil {
				t.Fatal("NewRunningRun() returned nil error")
			}
		})
	}
}

func TestNewRunningRunCopiesInputSlices(t *testing.T) {
	args := []string{"--size", "4096"}
	events := []string{"cycles", "instructions"}

	run, err := NewRunningRun(
		"run-123", "compute-01", "./benchmark", args,
		time.Now(), 4321,
		CaptureSpec{PerfEvents: events, ByteLimit: 1024},
	)
	if err != nil {
		t.Fatalf("NewRunningRun() unexpected error: %v", err)
	}

	args[1] = "8192"
	events[0] = "cache-misses"

	if run.Args[1] != "4096" {
		t.Fatalf("run arguments changed through caller slice: %q", run.Args)
	}
	if run.CaptureSpec.PerfEvents[0] != "cycles" {
		t.Fatalf("run events changed through caller slice: %q",
			run.CaptureSpec.PerfEvents)
	}
}
