package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/brbberry/edgelens/internal/store"
	"github.com/brbberry/edgelens/internal/wire"
)

func TestMeasurementsAPI(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "measurements.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	for _, timestamp := range []int64{100, 200} {
		if err := database.WriteMeasurement(context.Background(), wire.Measurement{
			Version: wire.Version, Host: "edge-01", Timestamp: timestamp,
		}); err != nil {
			t.Fatal(err)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/api/measurements?limit=1", nil)
	response := httptest.NewRecorder()
	NewHandler(database).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got, want := response.Header().Get("Content-Type"), "application/json; charset=utf-8"; got != want {
		t.Fatalf("Content-Type = %q, want %q", got, want)
	}
	if got, want := response.Body.String(), "[{\"v\":1,\"host\":\"edge-01\",\"ts\":200"; len(got) < len(want) || got[:len(want)] != want {
		t.Fatalf("response body = %q, want it to start with %q", got, want)
	}
}

func TestExperimentAPIs(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "measurements.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()
	now := time.Now().UnixMilli()
	start := wire.ExperimentEvent{
		SchemaVersion: wire.ExperimentVersion, MessageID: "start", Kind: wire.PacketRunStarted,
		RunID: "run-1", Host: "node-1", EventAtMS: now,
		Started: &wire.RunStartedPayload{Command: "work", Args: []string{"--size", "4"}, StartedAtMS: now, ChildPID: 42,
			Capture: wire.CaptureSpec{PerfEvents: []string{"cycles"}, ArtifactMaxBytes: 1024}},
	}
	if err := database.CreateRun(ctx, start); err != nil {
		t.Fatal(err)
	}
	sample := wire.ExperimentEvent{
		SchemaVersion: wire.ExperimentVersion, MessageID: "sample", Kind: wire.PacketProcessSample,
		RunID: "run-1", Host: "node-1", EventAtMS: now + 1,
		Sample: &wire.ProcessSamplePayload{SampledAtMS: now + 1, RSSBytes: 2048, ThreadCount: 1, ProcessState: "R"},
	}
	if err := database.WriteProcessSample(ctx, sample); err != nil {
		t.Fatal(err)
	}
	exitCode := 0
	artifact := wire.NewTextArtifact("perf-stat", "cycles")
	if err := database.WriteArtifact(ctx, wire.ExperimentEvent{
		SchemaVersion: wire.ExperimentVersion, MessageID: "artifact", Kind: wire.PacketArtifact,
		RunID: "run-1", Host: "node-1", EventAtMS: now + 2, Artifact: &artifact,
	}); err != nil {
		t.Fatal(err)
	}
	finish := wire.ExperimentEvent{
		SchemaVersion: wire.ExperimentVersion, MessageID: "finish", Kind: wire.PacketRunFinished,
		RunID: "run-1", Host: "node-1", EventAtMS: now + 2,
		Finished: &wire.RunFinishedPayload{Status: "completed", FinishedAtMS: now + 2, ExitCode: &exitCode},
	}
	if err := database.FinalizeRun(ctx, finish); err != nil {
		t.Fatal(err)
	}

	handler := NewHandler(database)
	for _, test := range []struct {
		path string
		want any
	}{
		{"/api/runs", &[]store.RunRecord{}},
		{"/api/runs/run-1", &store.RunRecord{}},
		{"/api/runs/run-1/process-samples", &[]store.ProcessSampleRecord{}},
		{"/api/runs/run-1/artifacts/perf-stat", &store.ArtifactRecord{}},
	} {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			if err := json.Unmarshal(response.Body.Bytes(), test.want); err != nil {
				t.Fatalf("decode response: %v", err)
			}
		})
	}
}

func TestExperimentAPINotFoundAndLimits(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "measurements.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	handler := NewHandler(database)
	for _, test := range []struct {
		path string
		code int
	}{
		{"/api/runs/missing", http.StatusNotFound},
		{"/api/runs?limit=0", http.StatusBadRequest},
		{"/api/runs/missing/process-samples", http.StatusNotFound},
		{"/api/runs/missing/artifacts/unknown", http.StatusBadRequest},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.code {
			t.Fatalf("%s status = %d, want %d", test.path, response.Code, test.code)
		}
	}
}
