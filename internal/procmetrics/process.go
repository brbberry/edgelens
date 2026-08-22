package procmetrics

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrProcessExited = errors.New("process exited")

const DefaultClockTicksPerSecond = 100.0

type Sample struct {
	PID           int
	SampledAt     time.Time
	CPUTicks      uint64
	CPUPercent    float64
	RSSBytes      uint64
	VirtualBytes  uint64
	HeapDataBytes uint64
	ReadBytes     uint64
	WriteBytes    uint64
	ReadBPS       float64
	WriteBPS      float64
	ThreadCount   int
	MinorFaults   uint64
	MajorFaults   uint64
	State         string
}

type snapshot struct {
	at         time.Time
	cpuTicks   uint64
	readBytes  uint64
	writeBytes uint64
}

type Reader struct {
	ProcRoot            string
	Clock               func() time.Time
	ClockTicksPerSecond float64

	mu       sync.Mutex
	previous map[int]snapshot
}

func NewReader() *Reader {
	return &Reader{
		ProcRoot:            "/proc",
		Clock:               time.Now,
		ClockTicksPerSecond: DefaultClockTicksPerSecond,
		previous:            make(map[int]snapshot),
	}
}

func (reader *Reader) Read(pid int) (Sample, error) {
	if pid <= 0 {
		return Sample{}, fmt.Errorf("PID must be positive")
	}
	if reader.Clock == nil || reader.ClockTicksPerSecond <= 0 {
		return Sample{}, fmt.Errorf("process reader clock configuration is invalid")
	}

	root := filepath.Join(reader.ProcRoot, strconv.Itoa(pid))
	statText, err := os.ReadFile(filepath.Join(root, "stat"))
	if err != nil {
		return Sample{}, processReadError(pid, err)
	}
	statusText, err := os.ReadFile(filepath.Join(root, "status"))
	if err != nil {
		return Sample{}, processReadError(pid, err)
	}
	ioText, err := os.ReadFile(filepath.Join(root, "io"))
	if err != nil {
		return Sample{}, processReadError(pid, err)
	}

	sample, err := parse(pid, statText, statusText, ioText)
	if err != nil {
		return Sample{}, err
	}
	sample.SampledAt = reader.Clock()

	reader.mu.Lock()
	defer reader.mu.Unlock()
	if reader.previous == nil {
		reader.previous = make(map[int]snapshot)
	}
	if previous, ok := reader.previous[pid]; ok {
		seconds := sample.SampledAt.Sub(previous.at).Seconds()
		if seconds > 0 {
			sample.CPUPercent = float64(saturatingDelta(sample.CPUTicks, previous.cpuTicks)) /
				reader.ClockTicksPerSecond / seconds * 100
			sample.ReadBPS = float64(saturatingDelta(sample.ReadBytes, previous.readBytes)) / seconds
			sample.WriteBPS = float64(saturatingDelta(sample.WriteBytes, previous.writeBytes)) / seconds
		}
	}
	reader.previous[pid] = snapshot{
		at: sample.SampledAt, cpuTicks: sample.CPUTicks,
		readBytes: sample.ReadBytes, writeBytes: sample.WriteBytes,
	}
	return sample, nil
}

func parse(pid int, statText, statusText, ioText []byte) (Sample, error) {
	stat, err := parseStat(string(statText))
	if err != nil {
		return Sample{}, fmt.Errorf("parse /proc/%d/stat: %w", pid, err)
	}
	status, err := parseKeyValues(string(statusText))
	if err != nil {
		return Sample{}, fmt.Errorf("parse /proc/%d/status: %w", pid, err)
	}
	ioValues, err := parseKeyValues(string(ioText))
	if err != nil {
		return Sample{}, fmt.Errorf("parse /proc/%d/io: %w", pid, err)
	}

	rss, err := kibValue(status, "VmRSS")
	if err != nil {
		return Sample{}, err
	}
	virtual, err := kibValue(status, "VmSize")
	if err != nil {
		return Sample{}, err
	}
	heapData, err := kibValue(status, "VmData")
	if err != nil {
		return Sample{}, err
	}
	threads, err := integerValue(status, "Threads")
	if err != nil {
		return Sample{}, err
	}
	readBytes, err := unsignedValue(ioValues, "read_bytes")
	if err != nil {
		return Sample{}, err
	}
	writeBytes, err := unsignedValue(ioValues, "write_bytes")
	if err != nil {
		return Sample{}, err
	}

	return Sample{
		PID: pid, CPUTicks: stat.userTicks + stat.systemTicks,
		RSSBytes: rss, VirtualBytes: virtual, HeapDataBytes: heapData,
		ReadBytes: readBytes, WriteBytes: writeBytes, ThreadCount: threads,
		MinorFaults: stat.minorFaults, MajorFaults: stat.majorFaults, State: stat.state,
	}, nil
}

type statValues struct {
	state       string
	minorFaults uint64
	majorFaults uint64
	userTicks   uint64
	systemTicks uint64
}

func parseStat(text string) (statValues, error) {
	closingParen := strings.LastIndex(text, ")")
	if closingParen < 0 || closingParen+2 >= len(text) {
		return statValues{}, fmt.Errorf("missing process command terminator")
	}
	fields := strings.Fields(text[closingParen+2:])
	// fields starts at Linux proc_pid_stat field 3 (state).
	if len(fields) < 20 {
		return statValues{}, fmt.Errorf("got %d fields after command, want at least 20", len(fields))
	}
	minor, err := strconv.ParseUint(fields[7], 10, 64)
	if err != nil {
		return statValues{}, fmt.Errorf("minor faults: %w", err)
	}
	major, err := strconv.ParseUint(fields[9], 10, 64)
	if err != nil {
		return statValues{}, fmt.Errorf("major faults: %w", err)
	}
	user, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return statValues{}, fmt.Errorf("user ticks: %w", err)
	}
	system, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return statValues{}, fmt.Errorf("system ticks: %w", err)
	}
	return statValues{state: fields[0], minorFaults: minor, majorFaults: major, userTicks: user, systemTicks: system}, nil
}

func parseKeyValues(text string) (map[string]string, error) {
	values := make(map[string]string)
	for lineNumber, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("line %d has no colon", lineNumber+1)
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return values, nil
}

func kibValue(values map[string]string, key string) (uint64, error) {
	fields := strings.Fields(values[key])
	if len(fields) != 2 || fields[1] != "kB" {
		return 0, fmt.Errorf("%s is missing or not expressed in kB", key)
	}
	value, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return value * 1024, nil
}

func integerValue(values map[string]string, key string) (int, error) {
	value, ok := values[key]
	if !ok {
		return 0, fmt.Errorf("missing %s", key)
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}

func unsignedValue(values map[string]string, key string) (uint64, error) {
	value, ok := values[key]
	if !ok {
		return 0, fmt.Errorf("missing %s", key)
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}

func processReadError(pid int, err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: PID %d", ErrProcessExited, pid)
	}
	return fmt.Errorf("read process %d: %w", pid, err)
}

func saturatingDelta(current, previous uint64) uint64 {
	if current < previous {
		return 0
	}
	return current - previous
}
