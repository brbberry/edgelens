package codec

import (
	"reflect"
	"testing"

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
