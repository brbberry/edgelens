package metricagg

import (
	"fmt"
	"time"

	"github.com/brbberry/edgelens/internal/sysmetrics"
	"golang.org/x/sync/errgroup"
)

type SystemSnapshot struct {
	CPU         sysmetrics.CPUMeasurement
	Memory      sysmetrics.MemMeasurement
	Disk        sysmetrics.DiskMeasurement
	Network     sysmetrics.NetIOMeasurement
	Temperature sysmetrics.TempZone
}

// GatherSystemSnapshot collects a snapshot whose rate fields are averaged over
// reportInterval. Instantaneous values are read after the rate window closes,
// so the snapshot completes at the end of that interval.
func GatherSystemSnapshot(reportInterval time.Duration, sources MetricSources) (SystemSnapshot, error) {
	if reportInterval < MinimumReportInterval {
		return SystemSnapshot{}, fmt.Errorf("report interval must be at least %s", MinimumReportInterval)
	}

	var cpu sysmetrics.CPUMeasurement
	var diskIO sysmetrics.DiskIOMeasurement
	var network sysmetrics.NetIOMeasurement

	var rateGroup errgroup.Group

	rateGroup.Go(func() error {
		measurement, err := sysmetrics.UsagePercent(reportInterval)
		if err != nil {
			return fmt.Errorf("measure cpu usage: %w", err)
		}
		cpu = measurement
		return nil
	})

	rateGroup.Go(func() error {
		measurement, err := sysmetrics.DiskIOThroughput(sources.DiskDevice, reportInterval)
		if err != nil {
			return fmt.Errorf("measure disk I/O: %w", err)
		}
		diskIO = measurement
		return nil
	})

	rateGroup.Go(func() error {
		measurement, err := sysmetrics.NetIOThroughput(sources.NetworkInterface, reportInterval)
		if err != nil {
			return fmt.Errorf("measure network I/O: %w", err)
		}
		network = measurement
		return nil
	})

	if err := rateGroup.Wait(); err != nil {
		return SystemSnapshot{}, err
	}

	var memory sysmetrics.MemMeasurement
	var diskHealth sysmetrics.DiskHealthMeasurement
	var temperature sysmetrics.TempZone
	var snapshotGroup errgroup.Group

	snapshotGroup.Go(func() error {
		measurement, err := sysmetrics.MemUsage()
		if err != nil {
			return fmt.Errorf("read memory usage: %w", err)
		}
		memory = measurement
		return nil
	})

	snapshotGroup.Go(func() error {
		measurement, err := sysmetrics.ReadDiskHealth(sources.DiskMountPath)
		if err != nil {
			return fmt.Errorf("read disk usage: %w", err)
		}
		diskHealth = measurement
		return nil
	})

	snapshotGroup.Go(func() error {
		measurement, err := sysmetrics.ReadTemps()
		if err != nil {
			return fmt.Errorf("read temperature: %w", err)
		}
		temperature = measurement
		return nil
	})

	if err := snapshotGroup.Wait(); err != nil {
		return SystemSnapshot{}, err
	}

	return SystemSnapshot{
		CPU:         cpu,
		Memory:      memory,
		Disk:        sysmetrics.DiskMeasurement{Health: diskHealth, IO: diskIO},
		Network:     network,
		Temperature: temperature,
	}, nil
}
