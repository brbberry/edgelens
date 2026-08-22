package procmetrics

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReaderCalculatesRatesAcrossSamples(t *testing.T) {
	root := t.TempDir()
	pid := 42
	writeFixture(t, root, pid, 100, 20, 1000, 2000)
	times := []time.Time{time.Unix(10, 0), time.Unix(12, 0)}
	reader := &Reader{
		ProcRoot: root, ClockTicksPerSecond: 100,
		Clock: func() time.Time { current := times[0]; times = times[1:]; return current },
	}

	first, err := reader.Read(pid)
	if err != nil {
		t.Fatal(err)
	}
	if first.CPUPercent != 0 || first.ReadBPS != 0 {
		t.Fatalf("first sample rates = CPU %.1f read %.1f, want zero baseline", first.CPUPercent, first.ReadBPS)
	}

	writeFixture(t, root, pid, 130, 30, 1400, 2600)
	second, err := reader.Read(pid)
	if err != nil {
		t.Fatal(err)
	}
	if second.CPUPercent != 20 || second.ReadBPS != 200 || second.WriteBPS != 300 {
		t.Fatalf("rates = CPU %.1f read %.1f write %.1f, want 20, 200, 300", second.CPUPercent, second.ReadBPS, second.WriteBPS)
	}
	if second.RSSBytes != 1024*1024 || second.HeapDataBytes != 512*1024 || second.ThreadCount != 3 {
		t.Fatalf("memory/thread sample = %+v", second)
	}
}

func TestReaderTreatsDisappearedPIDAsExit(t *testing.T) {
	reader := NewReader()
	reader.ProcRoot = t.TempDir()
	_, err := reader.Read(999)
	if !errors.Is(err, ErrProcessExited) {
		t.Fatalf("Read() error = %v, want ErrProcessExited", err)
	}
}

func TestParseStatHandlesSpacesAndParenthesesInCommand(t *testing.T) {
	text := statFixture("worker (phase one)", 10, 4)
	values, err := parseStat(text)
	if err != nil {
		t.Fatal(err)
	}
	if values.state != "R" || values.userTicks != 10 || values.systemTicks != 4 {
		t.Fatalf("parsed stat = %+v", values)
	}
}

func TestParseRejectsMalformedInput(t *testing.T) {
	if _, err := parseStat("42 malformed"); err == nil {
		t.Fatal("parseStat accepted malformed input")
	}
	if _, err := parseKeyValues("missing colon"); err == nil {
		t.Fatal("parseKeyValues accepted malformed input")
	}
}

func writeFixture(t *testing.T, root string, pid int, userTicks, systemTicks, readBytes, writeBytes uint64) {
	t.Helper()
	dir := filepath.Join(root, fmt.Sprint(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"stat":   statFixture("test process", userTicks, systemTicks),
		"status": "Name:\ttest\nVmSize:\t2048 kB\nVmRSS:\t1024 kB\nVmData:\t512 kB\nThreads:\t3\n",
		"io":     fmt.Sprintf("read_bytes: %d\nwrite_bytes: %d\n", readBytes, writeBytes),
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func statFixture(command string, userTicks, systemTicks uint64) string {
	// Fields after command start at field 3: state, ppid ... minflt (10),
	// majflt (12), utime (14), stime (15), then through rss (24).
	return fmt.Sprintf("42 (%s) R 1 1 1 0 0 0 5 0 2 0 %d %d 0 0 20 0 3 0 100 0 0", command, userTicks, systemTicks)
}
