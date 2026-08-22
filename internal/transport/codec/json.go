package codec

import (
	"encoding/json"
	"fmt"

	"github.com/brbberry/edgelens/internal/wire"
)

// JSONCodec is the readable-but-fat format; packets can be inspected with `nc -u -l`.
type JSONCodec struct{}

func (JSONCodec) Encode(m wire.Measurement) ([]byte, error) {
	encoding, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return encoding, nil
}

func (JSONCodec) Decode(b []byte) (wire.Measurement, error) {
	var m wire.Measurement
	err := json.Unmarshal(b, &m)
	if err != nil {
		return wire.Measurement{}, err
	}
	return m, nil
}

func (codec JSONCodec) EncodePacket(packet wire.Packet) ([]byte, error) {
	switch packet.Kind {
	case wire.PacketMeasurement:
		if packet.Measurement == nil || packet.Experiment != nil {
			return nil, fmt.Errorf("measurement packet must contain only a measurement")
		}
		return codec.Encode(*packet.Measurement)
	case wire.PacketRunStarted, wire.PacketProcessSample, wire.PacketRunFinished, wire.PacketArtifact:
		if packet.Experiment == nil || packet.Measurement != nil || packet.Experiment.Kind != packet.Kind {
			return nil, fmt.Errorf("experiment packet kind and payload do not match")
		}
		if err := packet.Experiment.Validate(); err != nil {
			return nil, fmt.Errorf("validate experiment packet: %w", err)
		}
		encoded, err := json.Marshal(packet.Experiment)
		if err != nil {
			return nil, err
		}
		if len(encoded) > wire.MaxExperimentPacketBytes {
			return nil, fmt.Errorf("experiment packet exceeds %d bytes", wire.MaxExperimentPacketBytes)
		}
		return encoded, nil
	default:
		return nil, fmt.Errorf("unsupported packet kind %q", packet.Kind)
	}
}

func (codec JSONCodec) DecodePacket(payload []byte) (wire.Packet, error) {
	if len(payload) > wire.MaxExperimentPacketBytes {
		return wire.Packet{}, fmt.Errorf("packet exceeds %d bytes", wire.MaxExperimentPacketBytes)
	}
	var discriminator struct {
		SchemaVersion *int            `json:"schema_version"`
		Kind          wire.PacketKind `json:"kind"`
	}
	if err := json.Unmarshal(payload, &discriminator); err != nil {
		return wire.Packet{}, fmt.Errorf("decode packet discriminator: %w", err)
	}

	if discriminator.SchemaVersion == nil {
		measurement, err := codec.Decode(payload)
		if err != nil {
			return wire.Packet{}, err
		}
		if measurement.Version != wire.Version {
			return wire.Packet{}, fmt.Errorf("unsupported measurement version %d", measurement.Version)
		}
		return wire.Packet{Kind: wire.PacketMeasurement, Measurement: &measurement}, nil
	}

	var event wire.ExperimentEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return wire.Packet{}, fmt.Errorf("decode experiment event: %w", err)
	}
	if err := event.Validate(); err != nil {
		return wire.Packet{}, fmt.Errorf("validate experiment event: %w", err)
	}
	return wire.Packet{Kind: event.Kind, Experiment: &event}, nil
}
