package metricagg

import "time"

type SamplingConfig struct {
	CPUInterval     time.Duration
	DiskInterval    time.Duration
	NetworkInterval time.Duration
	TempInterval    time.Duration
}

type TargetConfig struct {
	DiskDevice       string
	DiskMountPath    string
	NetworkInterface string
}

func DefaultSamplingConfig() SamplingConfig {
	return SamplingConfig{
		CPUInterval:     1 * time.Second,
		DiskInterval:    1 * time.Second,
		NetworkInterval: 1 * time.Second,
		TempInterval:    0 * time.Second, // not used yet, but could be used for future sampling of temperature readings
	}
}

func DefaultTargetConfig() TargetConfig {
	return TargetConfig{
		DiskDevice:       "mmcblk0",
		DiskMountPath:    "/",
		NetworkInterface: "eth0",
	}
}
