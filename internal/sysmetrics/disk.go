package sysmetrics

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
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

// DiskIOMeasurement holds disk throughput in bytes/sec.
type DiskIOMeasurement struct {
	ReadBps  float64
	WriteBps float64
}

type DiskHealthMeasurement struct {
	UsagePercent float64
}

type DiskMeasurement struct {
	Health DiskHealthMeasurement
	IO     DiskIOMeasurement
}

// ReadDiskHealth reads the disk usage percentage for the filesystem mounted at path.
func ReadDiskHealth(path string) (DiskHealthMeasurement, error) {
	diskSpace, err := ReadDiskSpace(path)
	if err != nil {
		return DiskHealthMeasurement{}, err
	}
	return DiskHealthMeasurement{UsagePercent: diskSpace.UsedPercent()}, nil
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

// DiskIOThroughput samples a device's counters twice, separated by interval,
// and returns average read/write throughput in bytes/sec.
func DiskIOThroughput(device string, interval time.Duration) (DiskIOMeasurement, error) {
	startedAt := time.Now()
	first, err := ReadDiskIOStats(device)
	if err != nil {
		return DiskIOMeasurement{}, err
	}
	time.Sleep(interval)
	second, err := ReadDiskIOStats(device)
	if err != nil {
		return DiskIOMeasurement{}, err
	}

	seconds := time.Since(startedAt).Seconds()
	if seconds <= 0 {
		return DiskIOMeasurement{}, nil
	}

	const sectorSize = 512
	readBps := float64(second.SectorsRead-first.SectorsRead) * sectorSize / seconds
	writeBps := float64(second.SectorsWrite-first.SectorsWrite) * sectorSize / seconds
	return DiskIOMeasurement{ReadBps: readBps,
			WriteBps: writeBps},
		nil
}

func ReadDiskMetrics(device, mountPath string, interval time.Duration) (DiskMeasurement, error) {
	io, err := DiskIOThroughput(device, interval)
	if err != nil {
		return DiskMeasurement{}, err
	}
	space, err := ReadDiskSpace(mountPath)
	if err != nil {
		return DiskMeasurement{}, err
	}
	return DiskMeasurement{Health: DiskHealthMeasurement{UsagePercent: space.UsedPercent()}, IO: io}, nil
}
