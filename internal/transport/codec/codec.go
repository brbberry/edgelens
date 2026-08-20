package codec

import "github.com/brbberry/edgelens/internal/wire"

// Codec converts a Measurement to and from its wire representation.
type Codec interface {
	Encode(wire.Measurement) ([]byte, error)
	Decode([]byte) (wire.Measurement, error)
}
