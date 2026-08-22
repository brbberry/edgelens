package codec

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/brbberry/edgelens/internal/wire"
)

func TestJSONCodecRoundTrip(t *testing.T) {
	// Every field non-zero and distinct, so a dropped or transposed field fails.
	want := wire.Measurement{
		Version:   wire.Version,
		Host:      "test-host",
		Timestamp: 1786736345,

		CPUUsagePct: 11.5,

		MemUsedPct:    22.5,
		MemUsedBytes:  1681276928,
		MemTotalBytes: 17006788608,
		SwapUsedPct:   33.5,

		DiskUsagePct: 44.5,
		DiskReadBps:  55.5,
		DiskWriteBps: 66.5,

		NetRecvBps: 77.5,
		NetSentBps: 88.5,

		TempZone:    "thermal_zone9",
		TempType:    "test-thermal",
		TempCelsius: 99.5,
	}

	c := JSONCodec{}

	b, err := c.Encode(want)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	got, err := c.Decode(b)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if !reflect.DeepEqual(want, got) {
		t.Errorf("round trip mismatch\nwant %+v\ngot  %+v\njson %s", want, got, b)
	}
}

func TestJSONCodecDecodePacketPreservesLegacyMeasurement(t *testing.T) {
	want := wire.Measurement{Version: wire.Version, Host: "legacy", Timestamp: 123}
	payload, err := JSONCodec{}.Encode(want)
	if err != nil {
		t.Fatal(err)
	}

	packet, err := (JSONCodec{}).DecodePacket(payload)
	if err != nil {
		t.Fatal(err)
	}
	if packet.Kind != wire.PacketMeasurement || packet.Measurement == nil || !reflect.DeepEqual(*packet.Measurement, want) {
		t.Fatalf("decoded packet = %+v, want legacy measurement %+v", packet, want)
	}
}

func TestJSONCodecExperimentPackets(t *testing.T) {
	exitCode := 0
	artifact := wire.NewTextArtifact("perf-stat", "1,234,,cycles\n")
	events := []wire.ExperimentEvent{
		{
			SchemaVersion: wire.ExperimentVersion, MessageID: "message-start", Kind: wire.PacketRunStarted,
			RunID: "run-1", Host: "node-1", EventAtMS: time.Now().UnixMilli(),
			Started: &wire.RunStartedPayload{Command: "true", StartedAtMS: time.Now().UnixMilli(), ChildPID: 42,
				Capture: wire.CaptureSpec{PerfEvents: []string{"cycles"}, ArtifactMaxBytes: 1024}},
		},
		{
			SchemaVersion: wire.ExperimentVersion, MessageID: "message-sample", Kind: wire.PacketProcessSample,
			RunID: "run-1", Host: "node-1", EventAtMS: time.Now().UnixMilli(),
			Sample: &wire.ProcessSamplePayload{SampledAtMS: time.Now().UnixMilli(), ThreadCount: 1, ProcessState: "R"},
		},
		{
			SchemaVersion: wire.ExperimentVersion, MessageID: "message-artifact", Kind: wire.PacketArtifact,
			RunID: "run-1", Host: "node-1", EventAtMS: time.Now().UnixMilli(), Artifact: &artifact,
		},
		{
			SchemaVersion: wire.ExperimentVersion, MessageID: "message-finish", Kind: wire.PacketRunFinished,
			RunID: "run-1", Host: "node-1", EventAtMS: time.Now().UnixMilli(),
			Finished: &wire.RunFinishedPayload{Status: "completed", FinishedAtMS: time.Now().UnixMilli(), ExitCode: &exitCode},
		},
	}

	for _, want := range events {
		t.Run(string(want.Kind), func(t *testing.T) {
			payload, err := (JSONCodec{}).EncodePacket(wire.Packet{Kind: want.Kind, Experiment: &want})
			if err != nil {
				t.Fatal(err)
			}
			packet, err := (JSONCodec{}).DecodePacket(payload)
			if err != nil {
				t.Fatal(err)
			}
			if packet.Kind != want.Kind || packet.Experiment == nil || !reflect.DeepEqual(*packet.Experiment, want) {
				t.Fatalf("round trip mismatch\nwant %+v\ngot  %+v", want, packet.Experiment)
			}
		})
	}
}

func TestJSONCodecRejectsInvalidExperimentArtifact(t *testing.T) {
	event := wire.ExperimentEvent{
		SchemaVersion: wire.ExperimentVersion, MessageID: "message-artifact", Kind: wire.PacketArtifact,
		RunID: "run-1", Host: "node-1", EventAtMS: time.Now().UnixMilli(),
		Artifact: &wire.ArtifactPayload{Kind: "perf-stat", Text: "changed", SHA256: "wrong", OriginalBytes: 7},
	}
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := (JSONCodec{}).DecodePacket(payload); err == nil {
		t.Fatal("DecodePacket() accepted an invalid artifact checksum")
	}
}

func TestJSONCodecRejectsOversizedPacket(t *testing.T) {
	payload := make([]byte, wire.MaxExperimentPacketBytes+1)
	if _, err := (JSONCodec{}).DecodePacket(payload); err == nil {
		t.Fatal("DecodePacket() accepted an oversized datagram")
	}
}

func TestJSONCodecRejectsInvalidExperimentIdentityAndSchema(t *testing.T) {
	now := time.Now().UnixMilli()
	base := wire.ExperimentEvent{
		SchemaVersion: wire.ExperimentVersion, MessageID: "message", Kind: wire.PacketProcessSample,
		RunID: "run", Host: "node", EventAtMS: now,
		Sample: &wire.ProcessSamplePayload{SampledAtMS: now, ThreadCount: 1, ProcessState: "R"},
	}
	for _, mutate := range []func(*wire.ExperimentEvent){
		func(event *wire.ExperimentEvent) { event.SchemaVersion = 999 },
		func(event *wire.ExperimentEvent) { event.MessageID = "" },
		func(event *wire.ExperimentEvent) { event.RunID = "" },
	} {
		event := base
		mutate(&event)
		payload, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := (JSONCodec{}).DecodePacket(payload); err == nil {
			t.Fatalf("DecodePacket() accepted invalid event %+v", event)
		}
	}
}
