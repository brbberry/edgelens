package sysmetrics

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// MemStats holds selected fields from /proc/meminfo, all in kilobytes.
type MemStats struct {
	Total     uint64
	Free      uint64
	Available uint64
	Buffers   uint64
	Cached    uint64
	SwapTotal uint64
	SwapFree  uint64
}

type MemMeasurement struct {
	UsedPercent     float64
	SwapUsedPercent float64
	UsedBytes       uint64
	TotalBytes      uint64
}

// MemUsage reads /proc/meminfo and returns the derived usage measurement.
func MemUsage() (MemMeasurement, error) {
	stats, err := ReadMemStats()
	if err != nil {
		return MemMeasurement{}, err
	}
	return MemMeasurement{
		UsedPercent:     stats.UsedPercent(),
		SwapUsedPercent: stats.SwapUsedPercent(),
		UsedBytes:       (stats.Total - stats.Available) * 1024,
		TotalBytes:      stats.Total * 1024,
	}, nil
}

// UsedPercent uses MemAvailable (the kernel's estimate of memory reclaimable
// for new allocations) rather than MemFree, which undercounts reclaimable cache.
func (m MemStats) UsedPercent() float64 {
	if m.Total == 0 {
		return 0
	}
	used := m.Total - m.Available
	return (float64(used) / float64(m.Total)) * 100
}

// SwapUsedPercent returns the percentage of swap space in use.
func (m MemStats) SwapUsedPercent() float64 {
	if m.SwapTotal == 0 {
		return 0
	}
	used := m.SwapTotal - m.SwapFree
	return (float64(used) / float64(m.SwapTotal)) * 100
}

// ReadMemStats reads and parses /proc/meminfo.
func ReadMemStats() (MemStats, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return MemStats{}, fmt.Errorf("opening /proc/meminfo: %w", err)
	}
	defer f.Close()

	stats := MemStats{}
	wanted := map[string]*uint64{
		"MemTotal":     &stats.Total,
		"MemFree":      &stats.Free,
		"MemAvailable": &stats.Available,
		"Buffers":      &stats.Buffers,
		"Cached":       &stats.Cached,
		"SwapTotal":    &stats.SwapTotal,
		"SwapFree":     &stats.SwapFree,
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		// each line looks like "MemTotal:       16332828 kB"
		key, rest, found := strings.Cut(scanner.Text(), ":")
		if !found {
			continue
		}
		dst, ok := wanted[key]
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		v, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return MemStats{}, fmt.Errorf("parsing %s: %w", key, err)
		}
		*dst = v
	}
	if err := scanner.Err(); err != nil {
		return MemStats{}, fmt.Errorf("reading /proc/meminfo: %w", err)
	}
	return stats, nil
}
