package sysmetrics

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// CPUStats holds the raw jiffie counters parsed from the first line of /proc/stat.
type CPUStats struct {
	User    uint64
	Nice    uint64
	System  uint64
	Idle    uint64
	IOWait  uint64
	IRQ     uint64
	SoftIRQ uint64
	Steal   uint64
}

type CPUMeasurement struct {
	UsagePercent float64
}

// Total returns the sum of all counters, i.e. all CPU time accounted for since boot.
func (c CPUStats) Total() uint64 {
	return c.User + c.Nice + c.System + c.Idle + c.IOWait + c.IRQ + c.SoftIRQ + c.Steal
}

// IdleTotal returns the counters that represent time the CPU was not doing work.
func (c CPUStats) IdleTotal() uint64 {
	return c.Idle + c.IOWait
}

// readCPUStats reads and parses the aggregate "cpu" line from /proc/stat.
func ReadCPUStats() (CPUStats, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return CPUStats{}, fmt.Errorf("opening /proc/stat: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}

		fields := strings.Fields(line)
		// fields[0] is the "cpu" label, the rest are the jiffie counters in a fixed order.
		values := make([]uint64, len(fields)-1)
		for i, field := range fields[1:] {
			v, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return CPUStats{}, fmt.Errorf("parsing /proc/stat field %q: %w", field, err)
			}
			values[i] = v
		}

		stats := CPUStats{}
		// Older kernels may omit trailing fields, so guard each index.
		set := func(i int, dst *uint64) {
			if i < len(values) {
				*dst = values[i]
			}
		}
		set(0, &stats.User)
		set(1, &stats.Nice)
		set(2, &stats.System)
		set(3, &stats.Idle)
		set(4, &stats.IOWait)
		set(5, &stats.IRQ)
		set(6, &stats.SoftIRQ)
		set(7, &stats.Steal)

		return stats, nil
	}

	if err := scanner.Err(); err != nil {
		return CPUStats{}, fmt.Errorf("reading /proc/stat: %w", err)
	}
	return CPUStats{}, fmt.Errorf("no cpu line found in /proc/stat")
}

// UsagePercent samples /proc/stat twice, separated by interval, and returns the
// percentage of CPU time spent doing work (i.e. 100 - idle%) over that window.
func UsagePercent(interval time.Duration) (CPUMeasurement, error) {
	first, err := ReadCPUStats()
	if err != nil {
		return CPUMeasurement{}, err
	}

	time.Sleep(interval)

	second, err := ReadCPUStats()
	if err != nil {
		return CPUMeasurement{}, err
	}

	totalDelta := second.Total() - first.Total()
	idleDelta := second.IdleTotal() - first.IdleTotal()
	if totalDelta == 0 {
		return CPUMeasurement{UsagePercent: 0}, nil
	}

	usage := (float64(totalDelta-idleDelta) / float64(totalDelta)) * 100
	return CPUMeasurement{UsagePercent: usage}, nil
}
