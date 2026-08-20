package codec

import (
	"encoding/json"

	"github.com/brbberry/edgelens/internal/wire"
)

// JSONCodec is the readable-but-fat format; packets can be inspected with `nc -u -l`.
type JSONCodec struct{}

var _ Codec = JSONCodec{}

func (JSONCodec) Encode(m wire.Measurement) ([]byte, error) {
	// json.Marshal
	encoding, err := json.Marshal(m)
	if err != nil {
		//fmt.Println("json.Marshal error:", err)
		return nil, err
	}
	return encoding, nil
}

func (JSONCodec) Decode(b []byte) (wire.Measurement, error) {
	var m wire.Measurement
	err := json.Unmarshal(b, &m)
	if err != nil {
		//fmt.Println("json.Unmarshal error:", err)
		return wire.Measurement{}, err
	}
	return m, nil
}
