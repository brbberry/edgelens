package perf

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	path    string
	outputs [][]byte
	errors  []error
	calls   [][]string
}

func TestWorkloadDescendantSkipsPerfWrappers(t *testing.T) {
	root := t.TempDir()
	writeProcessTreeNode(t, root, 10, "perf", 11)
	writeProcessTreeNode(t, root, 11, "perf", 12)
	writeProcessTreeNode(t, root, 12, "matrix-multiply")
	if got := workloadDescendant(root, 10); got != 12 {
		t.Fatalf("workloadDescendant() = %d, want 12", got)
	}
}

func writeProcessTreeNode(t *testing.T, root string, pid int, name string, children ...int) {
	t.Helper()
	directory := filepath.Join(root, fmt.Sprint(pid))
	taskDirectory := filepath.Join(directory, "task", fmt.Sprint(pid))
	if err := os.MkdirAll(taskDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	childText := ""
	for _, child := range children {
		childText += fmt.Sprintf("%d ", child)
	}
	if err := os.WriteFile(filepath.Join(taskDirectory, "children"), []byte(childText), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "comm"), []byte(name+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (runner *fakeRunner) LookPath(string) (string, error) {
	if runner.path == "" {
		return "", errors.New("not found")
	}
	return runner.path, nil
}

func (runner *fakeRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	runner.calls = append(runner.calls, append([]string{name}, args...))
	output, err := runner.outputs[0], runner.errors[0]
	runner.outputs, runner.errors = runner.outputs[1:], runner.errors[1:]
	return output, err
}

func TestPreflightChecksVersionAndCounters(t *testing.T) {
	runner := &fakeRunner{path: "/usr/bin/perf", outputs: [][]byte{[]byte("perf version 6.1"), nil}, errors: []error{nil, nil}}
	result, err := Preflight(context.Background(), runner, []string{"cycles"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != "perf version 6.1" || len(runner.calls) != 2 {
		t.Fatalf("result = %+v, calls = %v", result, runner.calls)
	}
}

func TestPreflightFailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		runner *fakeRunner
	}{
		{"missing executable", &fakeRunner{}},
		{"counter permission denied", &fakeRunner{path: "perf", outputs: [][]byte{[]byte("perf version"), []byte("permission denied")}, errors: []error{nil, errors.New("exit 255")}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Preflight(context.Background(), test.runner, []string{"cycles"}, false); !errors.Is(err, ErrPreflight) {
				t.Fatalf("Preflight() error = %v, want ErrPreflight", err)
			}
		})
	}
}

func TestFoldPerfScript(t *testing.T) {
	script := "work 1 [000] 1.0: cycles:\n\tffff leaf+0x0 (/bin/work)\n\tffff parent+0x0 (/bin/work)\n\n" +
		"work 1 [000] 1.1: cycles:\n\tffff leaf+0x0 (/bin/work)\n\tffff parent+0x0 (/bin/work)\n\n"
	folded, err := FoldPerfScript(script, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if folded != "parent;leaf 2\n" {
		t.Fatalf("folded stacks = %q", folded)
	}
}

func TestFoldPerfScriptEnforcesBound(t *testing.T) {
	_, err := FoldPerfScript("work:\n\tffff long-symbol (/bin/work)\n\n", 2)
	if !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("FoldPerfScript() error = %v, want ErrOutputTooLarge", err)
	}
}

func TestFoldPerfScriptTopAccountsForOmittedSamples(t *testing.T) {
	script := "work:\n\tffff very-long-leaf-one (/bin/work)\n\n" +
		"work:\n\tffff very-long-leaf-two (/bin/work)\n\n"
	folded, err := FoldPerfScriptTop(script, 65)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(folded, "[other] 1") {
		t.Fatalf("bounded folded stacks do not account for omitted samples: %q", folded)
	}
}

func TestValidateEventsRejectsDelimiterInjection(t *testing.T) {
	if err := validateEvents([]string{"cycles,instructions"}); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("validateEvents() error = %v", err)
	}
}

func TestAnalyzeGoHeapRejectsMissingProfile(t *testing.T) {
	if _, err := AnalyzeGoHeap(context.Background(), "/does/not/exist.pb.gz", 1024); err == nil {
		t.Fatal("AnalyzeGoHeap() accepted a missing profile")
	}
}
