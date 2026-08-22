package wire

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	ExperimentVersion        = 1
	MaxArtifactBytes         = 48 * 1024
	MaxExperimentPacketBytes = 60 * 1024
)

type PacketKind string

const (
	PacketMeasurement   PacketKind = "measurement"
	PacketRunStarted    PacketKind = "run_started"
	PacketProcessSample PacketKind = "process_sample"
	PacketRunFinished   PacketKind = "run_finished"
	PacketArtifact      PacketKind = "artifact"
)

type Packet struct {
	Kind        PacketKind       `json:"kind"`
	Measurement *Measurement     `json:"measurement,omitempty"`
	Experiment  *ExperimentEvent `json:"experiment,omitempty"`
}

type ExperimentEvent struct {
	SchemaVersion int                   `json:"schema_version"`
	MessageID     string                `json:"message_id"`
	Kind          PacketKind            `json:"kind"`
	RunID         string                `json:"run_id"`
	Host          string                `json:"host"`
	EventAtMS     int64                 `json:"event_at_ms"`
	Started       *RunStartedPayload    `json:"started,omitempty"`
	Sample        *ProcessSamplePayload `json:"sample,omitempty"`
	Finished      *RunFinishedPayload   `json:"finished,omitempty"`
	Artifact      *ArtifactPayload      `json:"artifact,omitempty"`
}

type CaptureSpec struct {
	PerfEvents       []string `json:"perf_events"`
	ArtifactMaxBytes int64    `json:"artifact_max_bytes"`
	CollectFlame     bool     `json:"collect_flame"`
	HeapProfilePath  string   `json:"heap_profile_path,omitempty"`
}

type RunStartedPayload struct {
	Command     string      `json:"command"`
	Args        []string    `json:"args"`
	StartedAtMS int64       `json:"started_at_ms"`
	ChildPID    int         `json:"child_pid"`
	Capture     CaptureSpec `json:"capture"`
}

type ProcessSamplePayload struct {
	SampledAtMS   int64   `json:"sampled_at_ms"`
	CPUTicks      uint64  `json:"cpu_ticks"`
	CPUPercent    float64 `json:"cpu_percent"`
	RSSBytes      uint64  `json:"rss_bytes"`
	VirtualBytes  uint64  `json:"virtual_bytes"`
	HeapDataBytes uint64  `json:"heap_data_bytes"`
	ReadBytes     uint64  `json:"read_bytes"`
	WriteBytes    uint64  `json:"write_bytes"`
	ReadBPS       float64 `json:"read_bps"`
	WriteBPS      float64 `json:"write_bps"`
	ThreadCount   int     `json:"thread_count"`
	MinorFaults   uint64  `json:"minor_faults"`
	MajorFaults   uint64  `json:"major_faults"`
	ProcessState  string  `json:"process_state"`
}

type RunFinishedPayload struct {
	Status        string `json:"status"`
	FinishedAtMS  int64  `json:"finished_at_ms"`
	ElapsedNS     int64  `json:"elapsed_ns"`
	ExitCode      *int   `json:"exit_code,omitempty"`
	Signal        string `json:"signal,omitempty"`
	FailureReason string `json:"failure_reason,omitempty"`
	PerfVersion   string `json:"perf_version,omitempty"`
}

type ArtifactPayload struct {
	Kind          string `json:"kind"`
	Text          string `json:"text"`
	SHA256        string `json:"sha256"`
	OriginalBytes int64  `json:"original_bytes"`
}

func NewTextArtifact(kind, text string) ArtifactPayload {
	sum := sha256.Sum256([]byte(text))
	return ArtifactPayload{
		Kind:          kind,
		Text:          text,
		SHA256:        hex.EncodeToString(sum[:]),
		OriginalBytes: int64(len(text)),
	}
}

func (event ExperimentEvent) Validate() error {
	if event.SchemaVersion != ExperimentVersion {
		return fmt.Errorf("unsupported experiment schema version %d", event.SchemaVersion)
	}
	if strings.TrimSpace(event.MessageID) == "" {
		return fmt.Errorf("experiment message ID must not be blank")
	}
	if strings.TrimSpace(event.RunID) == "" {
		return fmt.Errorf("experiment run ID must not be blank")
	}
	if strings.TrimSpace(event.Host) == "" {
		return fmt.Errorf("experiment host must not be blank")
	}
	if event.EventAtMS <= 0 {
		return fmt.Errorf("experiment event time must be positive")
	}

	switch event.Kind {
	case PacketRunStarted:
		if event.Started == nil || event.Sample != nil || event.Finished != nil || event.Artifact != nil {
			return fmt.Errorf("run-started event must contain only a started payload")
		}
		return event.Started.validate()
	case PacketProcessSample:
		if event.Sample == nil || event.Started != nil || event.Finished != nil || event.Artifact != nil {
			return fmt.Errorf("process-sample event must contain only a sample payload")
		}
		return event.Sample.validate()
	case PacketRunFinished:
		if event.Finished == nil || event.Started != nil || event.Sample != nil || event.Artifact != nil {
			return fmt.Errorf("run-finished event must contain only a finished payload")
		}
		return event.Finished.validate()
	case PacketArtifact:
		if event.Artifact == nil || event.Started != nil || event.Sample != nil || event.Finished != nil {
			return fmt.Errorf("artifact event must contain only an artifact payload")
		}
		return event.Artifact.Validate(MaxArtifactBytes)
	default:
		return fmt.Errorf("unsupported experiment event kind %q", event.Kind)
	}
}

func (payload RunStartedPayload) validate() error {
	if strings.TrimSpace(payload.Command) == "" || payload.StartedAtMS <= 0 || payload.ChildPID <= 0 {
		return fmt.Errorf("run-started payload has invalid command, time, or PID")
	}
	if payload.Capture.ArtifactMaxBytes <= 0 || payload.Capture.ArtifactMaxBytes > MaxArtifactBytes {
		return fmt.Errorf("artifact max bytes must be between 1 and %d", MaxArtifactBytes)
	}
	if len(payload.Capture.PerfEvents) == 0 {
		return fmt.Errorf("capture must request at least one perf event")
	}
	return nil
}

func (payload ProcessSamplePayload) validate() error {
	if payload.SampledAtMS <= 0 || payload.ThreadCount < 0 || strings.TrimSpace(payload.ProcessState) == "" {
		return fmt.Errorf("process sample has invalid time, thread count, or state")
	}
	if payload.CPUPercent < 0 || payload.ReadBPS < 0 || payload.WriteBPS < 0 {
		return fmt.Errorf("process sample rates must not be negative")
	}
	return nil
}

func (payload RunFinishedPayload) validate() error {
	if payload.Status != "completed" && payload.Status != "failed" && payload.Status != "interrupted" {
		return fmt.Errorf("invalid terminal status %q", payload.Status)
	}
	if payload.FinishedAtMS <= 0 || payload.ElapsedNS < 0 {
		return fmt.Errorf("run-finished payload has invalid time or elapsed duration")
	}
	if payload.Status == "completed" && (payload.ExitCode == nil || *payload.ExitCode != 0) {
		return fmt.Errorf("completed run must have exit code zero")
	}
	if payload.Status != "completed" && strings.TrimSpace(payload.FailureReason) == "" {
		return fmt.Errorf("non-completed run must include a failure reason")
	}
	return nil
}

func (artifact ArtifactPayload) Validate(maxBytes int) error {
	if strings.TrimSpace(artifact.Kind) == "" {
		return fmt.Errorf("artifact kind must not be blank")
	}
	if !utf8.ValidString(artifact.Text) {
		return fmt.Errorf("artifact %q is not valid UTF-8", artifact.Kind)
	}
	if len(artifact.Text) > maxBytes {
		return fmt.Errorf("artifact %q exceeds %d bytes", artifact.Kind, maxBytes)
	}
	if artifact.OriginalBytes != int64(len(artifact.Text)) {
		return fmt.Errorf("artifact %q byte length mismatch", artifact.Kind)
	}
	want := sha256.Sum256([]byte(artifact.Text))
	if artifact.SHA256 != hex.EncodeToString(want[:]) {
		return fmt.Errorf("artifact %q checksum mismatch", artifact.Kind)
	}
	return nil
}
