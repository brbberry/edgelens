package store

import (
	"context"
	"path/filepath"
	"testing"

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
