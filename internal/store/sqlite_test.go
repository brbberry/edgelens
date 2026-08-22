package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/brbberry/edgelens/internal/wire"
)

func TestWriteMeasurement(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "measurements.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	measurement := wire.Measurement{
		Version:      wire.Version,
		Host:         "edge-01",
		Timestamp:    123,
		CPUUsagePct:  42.5,
		MemUsedBytes: 1024,
		TempZone:     "CPU",
		TempType:     "temperature",
		TempCelsius:  61.25,
	}

	if err := database.WriteMeasurement(context.Background(), measurement); err != nil {
		t.Fatal(err)
	}
	if err := database.WriteMeasurement(context.Background(), measurement); err != nil {
		t.Fatal(err)
	}

	var host string
	var cpuUsage float64
	var usedBytes uint64
	var temperature float64
	if err := database.db.QueryRowContext(context.Background(), `
SELECT host, cpu_usage_pct, mem_used_bytes, temp_celsius
FROM measurements
`).Scan(&host, &cpuUsage, &usedBytes, &temperature); err != nil {
		t.Fatal(err)
	}

	if host != measurement.Host || cpuUsage != measurement.CPUUsagePct || usedBytes != measurement.MemUsedBytes || temperature != measurement.TempCelsius {
		t.Fatalf("stored measurement = %q, %v, %d, %v; want %q, %v, %d, %v", host, cpuUsage, usedBytes, temperature, measurement.Host, measurement.CPUUsagePct, measurement.MemUsedBytes, measurement.TempCelsius)
	}

	var count int
	if err := database.db.QueryRow("SELECT COUNT(*) FROM measurements").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("measurement row count = %d, want 1", count)
	}
}

func TestExperimentLifecycleIsIdempotent(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "measurements.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	startedAt := time.Now().UnixMilli()
	start := wire.ExperimentEvent{
		SchemaVersion: wire.ExperimentVersion, MessageID: "start-1", Kind: wire.PacketRunStarted,
		RunID: "run-1", Host: "node-1", EventAtMS: startedAt,
		Started: &wire.RunStartedPayload{Command: "true", StartedAtMS: startedAt, ChildPID: 42,
			Capture: wire.CaptureSpec{PerfEvents: []string{"cycles"}, ArtifactMaxBytes: 1024}},
	}
	if err := database.CreateRun(ctx, start); err != nil {
		t.Fatal(err)
	}
	if err := database.CreateRun(ctx, start); err != nil {
		t.Fatal(err)
	}

	sample := wire.ExperimentEvent{
		SchemaVersion: wire.ExperimentVersion, MessageID: "sample-1", Kind: wire.PacketProcessSample,
		RunID: "run-1", Host: "node-1", EventAtMS: startedAt + 1,
		Sample: &wire.ProcessSamplePayload{SampledAtMS: startedAt + 1, RSSBytes: 4096, HeapDataBytes: 2048,
			ThreadCount: 2, ProcessState: "R"},
	}
	if err := database.WriteProcessSample(ctx, sample); err != nil {
		t.Fatal(err)
	}
	if err := database.WriteProcessSample(ctx, sample); err != nil {
		t.Fatal(err)
	}

	exitCode := 0
	artifact := wire.NewTextArtifact("perf-stat", "1,cycles\n")
	artifactEvent := wire.ExperimentEvent{
		SchemaVersion: wire.ExperimentVersion, MessageID: "artifact-1", Kind: wire.PacketArtifact,
		RunID: "run-1", Host: "node-1", EventAtMS: startedAt + 2, Artifact: &artifact,
	}
	if err := database.WriteArtifact(ctx, artifactEvent); err != nil {
		t.Fatal(err)
	}
	if err := database.WriteArtifact(ctx, artifactEvent); err != nil {
		t.Fatal(err)
	}
	finish := wire.ExperimentEvent{
		SchemaVersion: wire.ExperimentVersion, MessageID: "finish-1", Kind: wire.PacketRunFinished,
		RunID: "run-1", Host: "node-1", EventAtMS: startedAt + 2,
		Finished: &wire.RunFinishedPayload{Status: "completed", FinishedAtMS: startedAt + 2,
			ElapsedNS: 1000, ExitCode: &exitCode, PerfVersion: "perf version test"},
	}
	if err := database.FinalizeRun(ctx, finish); err != nil {
		t.Fatal(err)
	}
	if err := database.FinalizeRun(ctx, finish); err != nil {
		t.Fatal(err)
	}

	run, err := database.ReadRun(ctx, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "completed" || run.ExitCode == nil || *run.ExitCode != 0 {
		t.Fatalf("stored run = %+v", run)
	}
	samples, err := database.ReadProcessSamples(ctx, "run-1", 10)
	if err != nil || len(samples) != 1 || samples[0].HeapDataBytes != 2048 {
		t.Fatalf("samples = %+v, error = %v", samples, err)
	}
	storedArtifact, err := database.ReadArtifact(ctx, "run-1", "perf-stat")
	if err != nil || storedArtifact.SHA256 != artifact.SHA256 {
		t.Fatalf("artifact = %+v, error = %v", storedArtifact, err)
	}
}

func TestExperimentRejectsUnknownRunAndTerminalOverwrite(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "measurements.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	now := time.Now().UnixMilli()
	sample := wire.ExperimentEvent{
		SchemaVersion: wire.ExperimentVersion, MessageID: "sample", Kind: wire.PacketProcessSample,
		RunID: "missing", Host: "node", EventAtMS: now,
		Sample: &wire.ProcessSamplePayload{SampledAtMS: now, ThreadCount: 1, ProcessState: "R"},
	}
	if err := database.WriteProcessSample(ctx, sample); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown-run error = %v, want ErrNotFound", err)
	}

	start := wire.ExperimentEvent{
		SchemaVersion: wire.ExperimentVersion, MessageID: "start", Kind: wire.PacketRunStarted,
		RunID: "run", Host: "node", EventAtMS: now,
		Started: &wire.RunStartedPayload{Command: "true", StartedAtMS: now, ChildPID: 1,
			Capture: wire.CaptureSpec{PerfEvents: []string{"cycles"}, ArtifactMaxBytes: 1024}},
	}
	if err := database.CreateRun(ctx, start); err != nil {
		t.Fatal(err)
	}
	exitCode := 0
	completed := wire.ExperimentEvent{
		SchemaVersion: wire.ExperimentVersion, MessageID: "done", Kind: wire.PacketRunFinished,
		RunID: "run", Host: "node", EventAtMS: now + 1,
		Finished: &wire.RunFinishedPayload{Status: "completed", FinishedAtMS: now + 1, ExitCode: &exitCode},
	}
	if err := database.FinalizeRun(ctx, completed); err != nil {
		t.Fatal(err)
	}
	failed := completed
	failed.MessageID = "failed"
	failed.Finished = &wire.RunFinishedPayload{Status: "failed", FinishedAtMS: now + 2, FailureReason: "late packet"}
	if err := database.FinalizeRun(ctx, failed); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("terminal overwrite error = %v, want ErrInvalidTransition", err)
	}
}

func TestExperimentIndexesExist(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "measurements.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, name := range []string{"idx_experiment_runs_host_started", "idx_process_samples_run_time", "idx_artifacts_run"} {
		var count int
		if err := database.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&count); err != nil || count != 1 {
			t.Fatalf("index %q count = %d, error = %v", name, count, err)
		}
	}
}

func TestReadMeasurements(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "measurements.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	for _, measurement := range []wire.Measurement{
		{Version: wire.Version, Host: "edge-01", Timestamp: 300},
		{Version: wire.Version, Host: "edge-01", Timestamp: 100},
		{Version: wire.Version, Host: "edge-01", Timestamp: 200},
	} {
		if err := database.WriteMeasurement(context.Background(), measurement); err != nil {
			t.Fatal(err)
		}
	}

	measurements, err := database.ReadMeasurements(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}

	if len(measurements) != 2 {
		t.Fatalf("measurement count = %d, want 2", len(measurements))
	}
	if measurements[0].Timestamp != 200 || measurements[1].Timestamp != 300 {
		t.Fatalf("timestamps = %d, %d; want 200, 300", measurements[0].Timestamp, measurements[1].Timestamp)
	}
}
