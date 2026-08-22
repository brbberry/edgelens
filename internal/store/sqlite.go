package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

var (
	ErrNotFound          = errors.New("store record not found")
	ErrInvalidTransition = errors.New("invalid run lifecycle transition")
)

var experimentSchema = []string{
	`CREATE TABLE IF NOT EXISTS experiment_runs (
		run_id TEXT PRIMARY KEY,
		host TEXT NOT NULL,
		command TEXT NOT NULL,
		args_json TEXT NOT NULL,
		status TEXT NOT NULL,
		started_at_ms INTEGER NOT NULL,
		finished_at_ms INTEGER,
		elapsed_ns INTEGER,
		child_pid INTEGER NOT NULL,
		exit_code INTEGER,
		signal TEXT NOT NULL DEFAULT '',
		capture_json TEXT NOT NULL,
		failure_reason TEXT NOT NULL DEFAULT '',
		perf_version TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE TABLE IF NOT EXISTS process_samples (
		run_id TEXT NOT NULL REFERENCES experiment_runs(run_id),
		sampled_at_ms INTEGER NOT NULL,
		cpu_ticks INTEGER NOT NULL,
		cpu_percent REAL NOT NULL,
		rss_bytes INTEGER NOT NULL,
		virtual_bytes INTEGER NOT NULL,
		heap_data_bytes INTEGER NOT NULL,
		read_bytes INTEGER NOT NULL,
		write_bytes INTEGER NOT NULL,
		read_bps REAL NOT NULL,
		write_bps REAL NOT NULL,
		thread_count INTEGER NOT NULL,
		minor_faults INTEGER NOT NULL,
		major_faults INTEGER NOT NULL,
		process_state TEXT NOT NULL,
		PRIMARY KEY (run_id, sampled_at_ms)
	)`,
	`CREATE TABLE IF NOT EXISTS artifacts (
		run_id TEXT NOT NULL REFERENCES experiment_runs(run_id),
		artifact_kind TEXT NOT NULL,
		text TEXT NOT NULL,
		sha256 TEXT NOT NULL,
		original_bytes INTEGER NOT NULL,
		PRIMARY KEY (run_id, artifact_kind)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_experiment_runs_host_started ON experiment_runs(host, started_at_ms DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_process_samples_run_time ON process_samples(run_id, sampled_at_ms)`,
	`CREATE INDEX IF NOT EXISTS idx_artifacts_run ON artifacts(run_id)`,
}

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

	db.SetMaxOpenConns(1)
	store := &DB{db: db}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable sqlite foreign keys: %w", err)
	}
	if _, err := db.Exec(createMeasurementsTable); err != nil {
		db.Close()
		return nil, fmt.Errorf("create measurements table: %w", err)
	}
	for _, statement := range experimentSchema {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			return nil, fmt.Errorf("create experiment schema: %w", err)
		}
	}

	return store, nil
}

type RunRecord struct {
	ID            string           `json:"id"`
	Host          string           `json:"host"`
	Command       string           `json:"command"`
	Args          []string         `json:"args"`
	Status        string           `json:"status"`
	StartedAtMS   int64            `json:"started_at_ms"`
	FinishedAtMS  *int64           `json:"finished_at_ms,omitempty"`
	ElapsedNS     *int64           `json:"elapsed_ns,omitempty"`
	ChildPID      int              `json:"child_pid"`
	ExitCode      *int             `json:"exit_code,omitempty"`
	Signal        string           `json:"signal,omitempty"`
	Capture       wire.CaptureSpec `json:"capture"`
	FailureReason string           `json:"failure_reason,omitempty"`
	PerfVersion   string           `json:"perf_version,omitempty"`
}

type ProcessSampleRecord struct {
	RunID string `json:"run_id"`
	wire.ProcessSamplePayload
}

type ArtifactRecord struct {
	RunID string `json:"run_id"`
	wire.ArtifactPayload
}

func (s *DB) CreateRun(ctx context.Context, event wire.ExperimentEvent) error {
	if err := event.Validate(); err != nil || event.Kind != wire.PacketRunStarted {
		return fmt.Errorf("invalid run-started event: %v", err)
	}
	argsJSON, err := json.Marshal(event.Started.Args)
	if err != nil {
		return fmt.Errorf("encode run arguments: %w", err)
	}
	captureJSON, err := json.Marshal(event.Started.Capture)
	if err != nil {
		return fmt.Errorf("encode capture specification: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO experiment_runs (
	run_id, host, command, args_json, status, started_at_ms, child_pid, capture_json
) VALUES (?, ?, ?, ?, 'running', ?, ?, ?)
ON CONFLICT(run_id) DO NOTHING`, event.RunID, event.Host, event.Started.Command, string(argsJSON),
		event.Started.StartedAtMS, event.Started.ChildPID, string(captureJSON))
	if err != nil {
		return fmt.Errorf("create experiment run: %w", err)
	}
	return nil
}

func (s *DB) WriteProcessSample(ctx context.Context, event wire.ExperimentEvent) error {
	if err := event.Validate(); err != nil || event.Kind != wire.PacketProcessSample {
		return fmt.Errorf("invalid process-sample event: %v", err)
	}
	sample := event.Sample
	result, err := s.db.ExecContext(ctx, `
INSERT INTO process_samples (
	run_id, sampled_at_ms, cpu_ticks, cpu_percent, rss_bytes, virtual_bytes, heap_data_bytes,
	read_bytes, write_bytes, read_bps, write_bps, thread_count, minor_faults, major_faults, process_state
) SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
WHERE EXISTS (SELECT 1 FROM experiment_runs WHERE run_id = ?)
ON CONFLICT(run_id, sampled_at_ms) DO NOTHING`,
		event.RunID, sample.SampledAtMS, sample.CPUTicks, sample.CPUPercent, sample.RSSBytes,
		sample.VirtualBytes, sample.HeapDataBytes, sample.ReadBytes, sample.WriteBytes,
		sample.ReadBPS, sample.WriteBPS, sample.ThreadCount, sample.MinorFaults, sample.MajorFaults,
		sample.ProcessState, event.RunID)
	if err != nil {
		return fmt.Errorf("write process sample: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM experiment_runs WHERE run_id = ?`, event.RunID).Scan(&exists); err != nil {
			return fmt.Errorf("%w: run %q", ErrNotFound, event.RunID)
		}
	}
	return nil
}

func (s *DB) FinalizeRun(ctx context.Context, event wire.ExperimentEvent) error {
	if err := event.Validate(); err != nil || event.Kind != wire.PacketRunFinished {
		return fmt.Errorf("invalid run-finished event: %v", err)
	}
	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin finalize run: %w", err)
	}
	defer transaction.Rollback()

	var currentStatus string
	if err := transaction.QueryRowContext(ctx, `SELECT status FROM experiment_runs WHERE run_id = ?`, event.RunID).Scan(&currentStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: run %q", ErrNotFound, event.RunID)
		}
		return fmt.Errorf("read run status: %w", err)
	}
	if currentStatus != "running" {
		if currentStatus == event.Finished.Status {
			return nil
		}
		return fmt.Errorf("%w: run %q is already %s", ErrInvalidTransition, event.RunID, currentStatus)
	}

	finished := event.Finished
	if _, err := transaction.ExecContext(ctx, `
UPDATE experiment_runs SET
	status = ?, finished_at_ms = ?, elapsed_ns = ?, exit_code = ?, signal = ?, failure_reason = ?, perf_version = ?
WHERE run_id = ? AND status = 'running'`, finished.Status, finished.FinishedAtMS, finished.ElapsedNS,
		finished.ExitCode, finished.Signal, finished.FailureReason, finished.PerfVersion, event.RunID); err != nil {
		return fmt.Errorf("finalize experiment run: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit finalized run: %w", err)
	}
	return nil
}

func (s *DB) WriteArtifact(ctx context.Context, event wire.ExperimentEvent) error {
	if err := event.Validate(); err != nil || event.Kind != wire.PacketArtifact {
		return fmt.Errorf("invalid artifact event: %v", err)
	}
	result, err := s.db.ExecContext(ctx, `
INSERT INTO artifacts (run_id, artifact_kind, text, sha256, original_bytes)
SELECT ?, ?, ?, ?, ?
WHERE EXISTS (SELECT 1 FROM experiment_runs WHERE run_id = ?)
ON CONFLICT(run_id, artifact_kind) DO NOTHING`, event.RunID, event.Artifact.Kind, event.Artifact.Text,
		event.Artifact.SHA256, event.Artifact.OriginalBytes, event.RunID)
	if err != nil {
		return fmt.Errorf("write experiment artifact: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM experiment_runs WHERE run_id = ?`, event.RunID).Scan(&exists); err != nil {
			return fmt.Errorf("%w: run %q", ErrNotFound, event.RunID)
		}
	}
	return nil
}

func (s *DB) ListRuns(ctx context.Context, host string, limit int) ([]RunRecord, error) {
	if limit <= 0 || limit > 1000 {
		return nil, fmt.Errorf("run limit must be between 1 and 1000")
	}
	query := `
SELECT run_id, host, command, args_json, status, started_at_ms, finished_at_ms,
	elapsed_ns, child_pid, exit_code, signal, capture_json, failure_reason, perf_version
FROM experiment_runs`
	arguments := []any{}
	if host != "" {
		query += ` WHERE host = ?`
		arguments = append(arguments, host)
	}
	query += ` ORDER BY started_at_ms DESC LIMIT ?`
	arguments = append(arguments, limit)
	rows, err := s.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list experiment runs: %w", err)
	}
	defer rows.Close()

	runs := make([]RunRecord, 0, limit)
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate experiment runs: %w", err)
	}
	return runs, nil
}

func (s *DB) ReadRun(ctx context.Context, runID string) (RunRecord, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT run_id, host, command, args_json, status, started_at_ms, finished_at_ms,
	elapsed_ns, child_pid, exit_code, signal, capture_json, failure_reason, perf_version
FROM experiment_runs WHERE run_id = ?`, runID)
	run, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return RunRecord{}, fmt.Errorf("%w: run %q", ErrNotFound, runID)
	}
	return run, err
}

func (s *DB) ReadProcessSamples(ctx context.Context, runID string, limit int) ([]ProcessSampleRecord, error) {
	if limit <= 0 || limit > 10000 {
		return nil, fmt.Errorf("process sample limit must be between 1 and 10000")
	}
	if _, err := s.ReadRun(ctx, runID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT sampled_at_ms, cpu_ticks, cpu_percent, rss_bytes, virtual_bytes, heap_data_bytes,
	read_bytes, write_bytes, read_bps, write_bps, thread_count, minor_faults, major_faults, process_state
FROM process_samples WHERE run_id = ? ORDER BY sampled_at_ms ASC LIMIT ?`, runID, limit)
	if err != nil {
		return nil, fmt.Errorf("read process samples: %w", err)
	}
	defer rows.Close()

	samples := make([]ProcessSampleRecord, 0, limit)
	for rows.Next() {
		record := ProcessSampleRecord{RunID: runID}
		sample := &record.ProcessSamplePayload
		if err := rows.Scan(&sample.SampledAtMS, &sample.CPUTicks, &sample.CPUPercent,
			&sample.RSSBytes, &sample.VirtualBytes, &sample.HeapDataBytes,
			&sample.ReadBytes, &sample.WriteBytes, &sample.ReadBPS, &sample.WriteBPS,
			&sample.ThreadCount, &sample.MinorFaults, &sample.MajorFaults, &sample.ProcessState); err != nil {
			return nil, fmt.Errorf("scan process sample: %w", err)
		}
		samples = append(samples, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate process samples: %w", err)
	}
	return samples, nil
}

func (s *DB) ReadArtifact(ctx context.Context, runID, kind string) (ArtifactRecord, error) {
	var artifact ArtifactRecord
	artifact.RunID = runID
	err := s.db.QueryRowContext(ctx, `
SELECT artifact_kind, text, sha256, original_bytes
FROM artifacts WHERE run_id = ? AND artifact_kind = ?`, runID, kind).Scan(
		&artifact.Kind, &artifact.Text, &artifact.SHA256, &artifact.OriginalBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return ArtifactRecord{}, fmt.Errorf("%w: artifact %q for run %q", ErrNotFound, kind, runID)
	}
	if err != nil {
		return ArtifactRecord{}, fmt.Errorf("read artifact: %w", err)
	}
	if err := artifact.ArtifactPayload.Validate(wire.MaxArtifactBytes); err != nil {
		return ArtifactRecord{}, fmt.Errorf("stored artifact failed integrity check: %w", err)
	}
	return artifact, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRun(scanner rowScanner) (RunRecord, error) {
	var run RunRecord
	var argsJSON, captureJSON string
	var finishedAt, elapsed, exitCode sql.NullInt64
	if err := scanner.Scan(
		&run.ID, &run.Host, &run.Command, &argsJSON, &run.Status, &run.StartedAtMS,
		&finishedAt, &elapsed, &run.ChildPID, &exitCode, &run.Signal, &captureJSON,
		&run.FailureReason, &run.PerfVersion,
	); err != nil {
		return RunRecord{}, err
	}
	if err := json.Unmarshal([]byte(argsJSON), &run.Args); err != nil {
		return RunRecord{}, fmt.Errorf("decode stored run arguments: %w", err)
	}
	if err := json.Unmarshal([]byte(captureJSON), &run.Capture); err != nil {
		return RunRecord{}, fmt.Errorf("decode stored capture specification: %w", err)
	}
	if finishedAt.Valid {
		value := finishedAt.Int64
		run.FinishedAtMS = &value
	}
	if elapsed.Valid {
		value := elapsed.Int64
		run.ElapsedNS = &value
	}
	if exitCode.Valid {
		value := int(exitCode.Int64)
		run.ExitCode = &value
	}
	return run, nil
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
