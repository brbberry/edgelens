package sysmetrics

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// DiskSpace holds filesystem space usage in bytes.
type DiskSpace struct {
	Total uint64
	Free  uint64
	Used  uint64
}

// UsedPercent returns the percentage of the filesystem's space in use.
func (d DiskSpace) UsedPercent() float64 {
	if d.Total == 0 {
		return 0
	}
	return (float64(d.Used) / float64(d.Total)) * 100
}

// ReadDiskSpace calls the statfs syscall to get space usage for the
// filesystem mounted at path (e.g. "/").
func ReadDiskSpace(path string) (DiskSpace, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return DiskSpace{}, fmt.Errorf("statfs %s: %w", path, err)
	}

	blockSize := uint64(stat.Bsize)
	total := stat.Blocks * blockSize
	free := stat.Bfree * blockSize
	return DiskSpace{Total: total, Free: free, Used: total - free}, nil
}

// DiskIOStats holds cumulative sector counters for one block device, read
// from /proc/diskstats. Multiply sectors by 512 to get bytes.
type DiskIOStats struct {
	Device       string
	SectorsRead  uint64
	SectorsWrite uint64
}

// ReadDiskIOStats reads /proc/diskstats and returns counters for the named
// device (e.g. "sda", "nvme0n1" — no "/dev/" prefix).
func ReadDiskIOStats(device string) (DiskIOStats, error) {
	f, err := os.Open("/proc/diskstats")
	if err != nil {
		return DiskIOStats{}, fmt.Errorf("opening /proc/diskstats: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		// columns: major minor name reads_completed ... sectors_read ... sectors_written ...
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 || fields[2] != device {
			continue
		}

		sectorsRead, err := strconv.ParseUint(fields[5], 10, 64)
		if err != nil {
			return DiskIOStats{}, fmt.Errorf("parsing sectors read: %w", err)
		}
		sectorsWrite, err := strconv.ParseUint(fields[9], 10, 64)
		if err != nil {
			return DiskIOStats{}, fmt.Errorf("parsing sectors written: %w", err)
		}
		return DiskIOStats{Device: device, SectorsRead: sectorsRead, SectorsWrite: sectorsWrite}, nil
	}
	if err := scanner.Err(); err != nil {
		return DiskIOStats{}, fmt.Errorf("reading /proc/diskstats: %w", err)
	}
	return DiskIOStats{}, fmt.Errorf("device %q not found in /proc/diskstats", device)
}
