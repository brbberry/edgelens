package metricagg

import (
	"github.com/brbberry/edgelens/internal/sysmetrics"
)

type SystemSnapshot struct {
	CPU         sysmetrics.CPUMeasurement
	Memory      sysmetrics.MemMeasurement
	Disk        sysmetrics.DiskMeasurement
	Network     sysmetrics.NetIOMeasurement
	Temperature []sysmetrics.TempZone
}

func GatherSystemSnapshot(samplingConfig SamplingConfig, targetConfig TargetConfig) (SystemSnapshot, error) {
	cpu, err := sysmetrics.UsagePercent(samplingConfig.CPUInterval)
	if err != nil {
		return SystemSnapshot{}, err
	}

	mem, err := sysmetrics.MemUsage()
	if err != nil {
		return SystemSnapshot{}, err
	}

	disk, err := sysmetrics.ReadDiskMetrics(targetConfig.DiskDevice, targetConfig.DiskMountPath, samplingConfig.DiskInterval)
	if err != nil {
		return SystemSnapshot{}, err
	}

	net, err := sysmetrics.NetIOThroughput(targetConfig.NetworkInterface, samplingConfig.NetworkInterval)
	if err != nil {
		return SystemSnapshot{}, err
	}

	temps, err := sysmetrics.ReadTemps()
	if err != nil {
		return SystemSnapshot{}, err
	}

	return SystemSnapshot{
		CPU:         cpu,
		Memory:      mem,
		Disk:        disk,
		Network:     net,
		Temperature: temps,
	}, nil
}
