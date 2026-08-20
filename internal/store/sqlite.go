package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/brbberry/edgelens/internal/wire"
	_ "modernc.org/sqlite"
)

const createMeasurementsTable = `
CREATE TABLE IF NOT EXISTS measurements (
	version INTEGER NOT NULL,
	host TEXT NOT NULL,
	timestamp INTEGER NOT NULL,
	cpu_usage_pct REAL NOT NULL,
	mem_used_pct REAL NOT NULL,
	mem_used_bytes INTEGER NOT NULL,
	mem_total_bytes INTEGER NOT NULL,
	swap_used_pct REAL NOT NULL,
	disk_usage_pct REAL NOT NULL,
	disk_read_bps REAL NOT NULL,
	disk_write_bps REAL NOT NULL,
	net_recv_bps REAL NOT NULL,
	net_sent_bps REAL NOT NULL,
	temp_zone TEXT NOT NULL,
	temp_type TEXT NOT NULL,
	temp_celsius REAL NOT NULL,
	PRIMARY KEY (host, timestamp)
);`

// DB stores measurements in a local SQLite database.
type DB struct {
	db *sql.DB
}

// Open opens or creates a SQLite database at path and ensures its schema exists.
func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	store := &DB{db: db}
	if _, err := db.Exec(createMeasurementsTable); err != nil {
		db.Close()
		return nil, fmt.Errorf("create measurements table: %w", err)
	}

	return store, nil
}

// WriteMeasurement inserts one measurement. Rewriting the same host and
// timestamp is treated as a duplicate and leaves the original row unchanged.
func (s *DB) WriteMeasurement(ctx context.Context, measurement wire.Measurement) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO measurements (
	version, host, timestamp, cpu_usage_pct,
	mem_used_pct, mem_used_bytes, mem_total_bytes, swap_used_pct,
	disk_usage_pct, disk_read_bps, disk_write_bps,
	net_recv_bps, net_sent_bps, temp_zone, temp_type, temp_celsius
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (host, timestamp) DO NOTHING`,
		measurement.Version,
		measurement.Host,
		measurement.Timestamp,
		measurement.CPUUsagePct,
		measurement.MemUsedPct,
		measurement.MemUsedBytes,
		measurement.MemTotalBytes,
		measurement.SwapUsedPct,
		measurement.DiskUsagePct,
		measurement.DiskReadBps,
		measurement.DiskWriteBps,
		measurement.NetRecvBps,
		measurement.NetSentBps,
		measurement.TempZone,
		measurement.TempType,
		measurement.TempCelsius,
	)
	if err != nil {
		return fmt.Errorf("write measurement: %w", err)
	}

	return nil
}

// ReadMeasurements returns up to limit of the newest measurements ordered by
// timestamp so callers can render them as a time series.
func (s *DB) ReadMeasurements(ctx context.Context, limit int) ([]wire.Measurement, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("measurement limit must be positive")
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT
	version, host, timestamp, cpu_usage_pct,
	mem_used_pct, mem_used_bytes, mem_total_bytes, swap_used_pct,
	disk_usage_pct, disk_read_bps, disk_write_bps,
	net_recv_bps, net_sent_bps, temp_zone, temp_type, temp_celsius
FROM (
	SELECT
		version, host, timestamp, cpu_usage_pct,
		mem_used_pct, mem_used_bytes, mem_total_bytes, swap_used_pct,
		disk_usage_pct, disk_read_bps, disk_write_bps,
		net_recv_bps, net_sent_bps, temp_zone, temp_type, temp_celsius
	FROM measurements
	ORDER BY timestamp DESC
	LIMIT ?
)
ORDER BY timestamp ASC`, limit)
	if err != nil {
		return nil, fmt.Errorf("read measurements: %w", err)
	}
	defer rows.Close()

	measurements := make([]wire.Measurement, 0, limit)
	for rows.Next() {
		var measurement wire.Measurement
		if err := rows.Scan(
			&measurement.Version,
			&measurement.Host,
			&measurement.Timestamp,
			&measurement.CPUUsagePct,
			&measurement.MemUsedPct,
			&measurement.MemUsedBytes,
			&measurement.MemTotalBytes,
			&measurement.SwapUsedPct,
			&measurement.DiskUsagePct,
			&measurement.DiskReadBps,
			&measurement.DiskWriteBps,
			&measurement.NetRecvBps,
			&measurement.NetSentBps,
			&measurement.TempZone,
			&measurement.TempType,
			&measurement.TempCelsius,
		); err != nil {
			return nil, fmt.Errorf("scan measurement: %w", err)
		}
		measurements = append(measurements, measurement)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate measurements: %w", err)
	}

	return measurements, nil
}

// Close releases the database connection.
func (s *DB) Close() error {
	return s.db.Close()
}
