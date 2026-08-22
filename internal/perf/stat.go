package perf

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

var (
	ErrPreflight      = errors.New("perf capture preflight failed")
	ErrOutputTooLarge = errors.New("capture output exceeds configured limit")
)

var DefaultEvents = []string{
	"task-clock", "cycles", "instructions", "branches", "branch-misses",
	"cache-references", "cache-misses", "context-switches", "cpu-migrations", "page-faults",
}

type CommandRunner interface {
	LookPath(file string) (string, error)
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

type OSCommandRunner struct{}

func (OSCommandRunner) LookPath(file string) (string, error) { return exec.LookPath(file) }

func (OSCommandRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type PreflightResult struct {
	Executable string
	Version    string
}

func Preflight(ctx context.Context, runner CommandRunner, events []string, collectFlame bool) (PreflightResult, error) {
	if runner == nil {
		return PreflightResult{}, fmt.Errorf("%w: command runner is nil", ErrPreflight)
	}
	if err := validateEvents(events); err != nil {
		return PreflightResult{}, fmt.Errorf("%w: %v", ErrPreflight, err)
	}
	executable, err := runner.LookPath("perf")
	if err != nil {
		return PreflightResult{}, fmt.Errorf("%w: find perf: %v", ErrPreflight, err)
	}
	versionOutput, err := runner.Output(ctx, executable, "--version")
	if err != nil {
		return PreflightResult{}, fmt.Errorf("%w: read perf version: %v", ErrPreflight, err)
	}
	paranoid := readParanoid()

	args := []string{"stat", "-x,", "-e", strings.Join(events, ","), "--", "true"}
	if output, err := runner.Output(ctx, executable, args...); err != nil {
		return PreflightResult{}, fmt.Errorf("%w: counters unavailable (perf_event_paranoid=%d): %v: %s",
			ErrPreflight, paranoid, err, strings.TrimSpace(string(output)))
	}
	if collectFlame {
		outputPath := filepath.Join(os.TempDir(), fmt.Sprintf("edgelens-preflight-%d.data", time.Now().UnixNano()))
		defer os.Remove(outputPath)
		if output, err := runner.Output(ctx, executable, "record", "-q", "-o", outputPath, "-g", "--call-graph", "dwarf", "--", "true"); err != nil {
			return PreflightResult{}, fmt.Errorf("%w: stack sampling unavailable: %v: %s", ErrPreflight, err, strings.TrimSpace(string(output)))
		}
	}
	return PreflightResult{Executable: executable, Version: strings.TrimSpace(string(versionOutput))}, nil
}

type Session struct {
	cmd          *exec.Cmd
	trackedPID   int
	directory    string
	perfPath     string
	statPath     string
	recordPath   string
	collectFlame bool
	startedAt    time.Time
	maxBytes     int
}

type Result struct {
	FinishedAt   time.Time
	Elapsed      time.Duration
	ExitCode     *int
	Signal       string
	StatText     string
	FoldedStacks string
	Err          error
}

func Start(ctx context.Context, preflight PreflightResult, command string, args, events []string, collectFlame bool, maxBytes int) (*Session, error) {
	if strings.TrimSpace(preflight.Executable) == "" {
		return nil, fmt.Errorf("perf preflight result has no executable")
	}
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("workload command must not be blank")
	}
	if maxBytes <= 0 {
		return nil, fmt.Errorf("artifact byte limit must be positive")
	}
	if err := validateEvents(events); err != nil {
		return nil, err
	}

	directory, err := os.MkdirTemp("", "edgelens-perf-")
	if err != nil {
		return nil, fmt.Errorf("create perf workspace: %w", err)
	}
	statPath := filepath.Join(directory, "perf-stat.csv")
	recordPath := filepath.Join(directory, "perf.data")
	perfArgs := []string{"stat", "-x,", "-o", statPath, "-e", strings.Join(events, ","), "--"}
	if collectFlame {
		perfArgs = append(perfArgs, preflight.Executable, "record", "-q", "-o", recordPath, "-g", "--call-graph", "dwarf", "--")
	}
	perfArgs = append(perfArgs, command)
	perfArgs = append(perfArgs, args...)

	cmd := exec.CommandContext(ctx, preflight.Executable, perfArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = 2 * time.Second
	startedAt := time.Now()
	if err := cmd.Start(); err != nil {
		os.RemoveAll(directory)
		return nil, fmt.Errorf("start perf workload: %w", err)
	}
	trackedPID := waitForWorkloadPID("/proc", cmd.Process.Pid, 250*time.Millisecond)
	return &Session{
		cmd: cmd, trackedPID: trackedPID, directory: directory, perfPath: preflight.Executable,
		statPath: statPath, recordPath: recordPath, collectFlame: collectFlame,
		startedAt: startedAt, maxBytes: maxBytes,
	}, nil
}

func (session *Session) PID() int { return session.trackedPID }

func (session *Session) Wait() Result {
	waitErr := session.cmd.Wait()
	finishedAt := time.Now()
	result := Result{FinishedAt: finishedAt.UTC(), Elapsed: finishedAt.Sub(session.startedAt), Err: waitErr}
	if state := session.cmd.ProcessState; state != nil {
		exitCode := state.ExitCode()
		if exitCode >= 0 {
			result.ExitCode = &exitCode
		} else {
			result.Signal = state.String()
		}
	}
	defer os.RemoveAll(session.directory)

	statText, err := readBoundedFile(session.statPath, session.maxBytes)
	if err != nil {
		result.Err = errors.Join(result.Err, fmt.Errorf("read perf stat artifact: %w", err))
	} else {
		result.StatText = statText
	}
	if session.collectFlame {
		script, err := runBounded(session.maxBytes*16, session.perfPath, "script", "-i", session.recordPath)
		if err != nil {
			result.Err = errors.Join(result.Err, fmt.Errorf("render perf stacks: %w", err))
		} else {
			result.FoldedStacks, err = FoldPerfScript(string(script), session.maxBytes)
			if errors.Is(err, ErrOutputTooLarge) {
				result.FoldedStacks, err = FoldPerfScriptTop(string(script), session.maxBytes)
			}
			if err != nil {
				result.Err = errors.Join(result.Err, err)
			}
		}
	}
	return result
}

func AnalyzeGoHeap(ctx context.Context, profilePath string, maxBytes int) (string, error) {
	if strings.TrimSpace(profilePath) == "" {
		return "", nil
	}
	if _, err := os.Stat(profilePath); err != nil {
		return "", fmt.Errorf("read Go heap profile: %w", err)
	}
	output, err := runBoundedContext(ctx, maxBytes, "go", "tool", "pprof", "-top", "-nodecount=80", "-sample_index=inuse_space", profilePath)
	if err != nil {
		return "", fmt.Errorf("analyze Go heap profile: %w", err)
	}
	return string(output), nil
}

func FoldPerfScript(script string, maxBytes int) (string, error) {
	counts := make(map[string]int)
	var stack []string
	flush := func() {
		if len(stack) == 0 {
			return
		}
		for left, right := 0, len(stack)-1; left < right; left, right = left+1, right-1 {
			stack[left], stack[right] = stack[right], stack[left]
		}
		counts[strings.Join(stack, ";")]++
		stack = nil
	}

	scanner := bufio.NewScanner(strings.NewReader(script))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		if len(line) == 0 || (line[0] != ' ' && line[0] != '\t') {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			symbol := strings.TrimSuffix(fields[1], "+0x0")
			stack = append(stack, symbol)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan perf script: %w", err)
	}
	flush()

	keys := make([]string, 0, len(counts))
	for stack := range counts {
		keys = append(keys, stack)
	}
	sort.Strings(keys)
	var folded strings.Builder
	for _, stack := range keys {
		fmt.Fprintf(&folded, "%s %d\n", stack, counts[stack])
		if folded.Len() > maxBytes {
			return "", ErrOutputTooLarge
		}
	}
	return folded.String(), nil
}

// FoldPerfScriptTop produces a bounded derived stack summary. It keeps stacks
// with the most samples and accounts for every omitted sample in [other].
func FoldPerfScriptTop(script string, maxBytes int) (string, error) {
	complete, err := FoldPerfScript(script, len(script)*2+1024)
	if err != nil {
		return "", err
	}
	type entry struct {
		stack string
		count int
	}
	entries := make([]entry, 0)
	for _, line := range strings.Split(strings.TrimSpace(complete), "\n") {
		split := strings.LastIndex(line, " ")
		if split <= 0 {
			continue
		}
		count, err := strconv.Atoi(line[split+1:])
		if err != nil {
			return "", fmt.Errorf("parse folded stack count: %w", err)
		}
		entries = append(entries, entry{stack: line[:split], count: count})
	}
	sort.Slice(entries, func(left, right int) bool {
		if entries[left].count == entries[right].count {
			return entries[left].stack < entries[right].stack
		}
		return entries[left].count > entries[right].count
	})

	var output strings.Builder
	omitted := 0
	for index, item := range entries {
		line := fmt.Sprintf("%s %d\n", item.stack, item.count)
		// Reserve enough space for the explicit omitted-sample bucket.
		if output.Len()+len(line)+32 > maxBytes {
			for _, remaining := range entries[index:] {
				omitted += remaining.count
			}
			break
		}
		output.WriteString(line)
	}
	if omitted > 0 {
		fmt.Fprintf(&output, "[other] %d\n", omitted)
	}
	if output.Len() > maxBytes {
		return "", ErrOutputTooLarge
	}
	return output.String(), nil
}

func waitForWorkloadPID(procRoot string, wrapperPID int, maximumWait time.Duration) int {
	deadline := time.Now().Add(maximumWait)
	for {
		if pid := workloadDescendant(procRoot, wrapperPID); pid != wrapperPID {
			return pid
		}
		if time.Now().After(deadline) {
			return wrapperPID
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func workloadDescendant(procRoot string, wrapperPID int) int {
	current := wrapperPID
	for depth := 0; depth < 4; depth++ {
		childrenPath := filepath.Join(procRoot, strconv.Itoa(current), "task", strconv.Itoa(current), "children")
		data, err := os.ReadFile(childrenPath)
		if err != nil {
			return current
		}
		fields := strings.Fields(string(data))
		if len(fields) == 0 {
			return current
		}
		child, err := strconv.Atoi(fields[0])
		if err != nil || child <= 0 {
			return current
		}
		current = child
		name, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(current), "comm"))
		if err != nil || strings.TrimSpace(string(name)) != "perf" {
			return current
		}
	}
	return current
}

func validateEvents(events []string) error {
	if len(events) == 0 {
		return fmt.Errorf("at least one perf event is required")
	}
	seen := make(map[string]struct{}, len(events))
	for _, event := range events {
		if strings.TrimSpace(event) == "" || strings.ContainsAny(event, "\r\n,") {
			return fmt.Errorf("invalid perf event %q", event)
		}
		if _, exists := seen[event]; exists {
			return fmt.Errorf("duplicate perf event %q", event)
		}
		seen[event] = struct{}{}
	}
	return nil
}

func readParanoid() int {
	data, err := os.ReadFile("/proc/sys/kernel/perf_event_paranoid")
	if err != nil {
		return -1
	}
	value, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return -1
	}
	return value
}

func readBoundedFile(path string, maxBytes int) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(maxBytes)+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxBytes {
		return "", ErrOutputTooLarge
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("capture output is not UTF-8")
	}
	return string(data), nil
}

func runBounded(maxBytes int, name string, args ...string) ([]byte, error) {
	return runBoundedContext(context.Background(), maxBytes, name, args...)
}

func runBoundedContext(ctx context.Context, maxBytes int, name string, args ...string) ([]byte, error) {
	buffer := &boundedBuffer{maximum: maxBytes}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = buffer
	cmd.Stderr = buffer
	err := cmd.Run()
	if errors.Is(buffer.err, ErrOutputTooLarge) {
		return nil, ErrOutputTooLarge
	}
	if err != nil {
		return nil, fmt.Errorf("%v: %s", err, strings.TrimSpace(buffer.String()))
	}
	return bytes.Clone(buffer.Bytes()), nil
}

type boundedBuffer struct {
	bytes.Buffer
	maximum int
	err     error
}

func (buffer *boundedBuffer) Write(data []byte) (int, error) {
	if buffer.err != nil {
		return 0, buffer.err
	}
	remaining := buffer.maximum - buffer.Len()
	if len(data) > remaining {
		if remaining > 0 {
			_, _ = buffer.Buffer.Write(data[:remaining])
		}
		buffer.err = ErrOutputTooLarge
		return len(data), buffer.err
	}
	return buffer.Buffer.Write(data)
}
