package metricagg

import "time"

// MetricSources identifies the operating-system sources used for disk and
// network measurements. It is independent of the report cadence.
type MetricSources struct {
	DiskDevice       string
	DiskMountPath    string
	NetworkInterface string
}

const (
	MinimumReportInterval = time.Second
	DefaultReportInterval = 5 * time.Second
)

func DefaultMetricSources() MetricSources {
	return MetricSources{
		DiskDevice:       "mmcblk0",
		DiskMountPath:    "/",
		NetworkInterface: "eth0",
	}
}
