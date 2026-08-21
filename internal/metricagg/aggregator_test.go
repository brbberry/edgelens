package metricagg

import (
	"strings"
	"testing"
	"time"
)

func TestGatherSystemSnapshotRejectsNonPositiveReportInterval(t *testing.T) {
	_, err := GatherSystemSnapshot(0, MetricSources{})
	if err == nil || !strings.Contains(err.Error(), "report interval must be at least") {
		t.Fatalf("GatherSystemSnapshot() error = %v, want minimum report interval error", err)
	}
}

func TestGatherSystemSnapshotRejectsSubsecondReportInterval(t *testing.T) {
	_, err := GatherSystemSnapshot(MinimumReportInterval-time.Nanosecond, MetricSources{})
	if err == nil || !strings.Contains(err.Error(), "report interval must be at least") {
		t.Fatalf("GatherSystemSnapshot() error = %v, want minimum report interval error", err)
	}
}
