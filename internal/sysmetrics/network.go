package sysmetrics

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// NetIOStats holds cumulative byte counters for one network interface,
// parsed from /proc/net/dev.
type NetIOStats struct {
	Interface string
	BytesRecv uint64
	BytesSent uint64
}

// NetIOMeasurement holds network throughput in bytes/sec.
type NetIOMeasurement struct {
	RecvBps float64
	SentBps float64
}

// ReadNetIOStats reads /proc/net/dev and returns counters for the named
// interface (e.g. "eth0", "wlan0").
func ReadNetIOStats(iface string) (NetIOStats, error) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return NetIOStats{}, fmt.Errorf("opening /proc/net/dev: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// first two lines are headers; interface lines look like "  eth0: 123 4 0 0 ... 456 7 0 0 ..."
	for scanner.Scan() {
		name, rest, found := strings.Cut(scanner.Text(), ":")
		if !found {
			continue
		}
		name = strings.TrimSpace(name)
		if name != iface {
			continue
		}

		fields := strings.Fields(rest)
		if len(fields) < 9 {
			return NetIOStats{}, fmt.Errorf("unexpected /proc/net/dev format for %s", iface)
		}
		// fields[0] = receive bytes, fields[8] = transmit bytes
		recv, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return NetIOStats{}, fmt.Errorf("parsing received bytes: %w", err)
		}
		sent, err := strconv.ParseUint(fields[8], 10, 64)
		if err != nil {
			return NetIOStats{}, fmt.Errorf("parsing sent bytes: %w", err)
		}
		return NetIOStats{Interface: iface, BytesRecv: recv, BytesSent: sent}, nil
	}
	if err := scanner.Err(); err != nil {
		return NetIOStats{}, fmt.Errorf("reading /proc/net/dev: %w", err)
	}
	return NetIOStats{}, fmt.Errorf("interface %q not found in /proc/net/dev", iface)
}

// ThroughputBps samples an interface's counters twice, separated by
// interval, and returns the average receive/send throughput in bytes/sec.
func ThroughputBps(iface string, interval time.Duration) (recvBps, sentBps float64, err error) {
	first, err := ReadNetIOStats(iface)
	if err != nil {
		return 0, 0, err
	}

	time.Sleep(interval)

	second, err := ReadNetIOStats(iface)
	if err != nil {
		return 0, 0, err
	}

	seconds := interval.Seconds()
	if seconds == 0 {
		return 0, 0, nil
	}
	recvBps = float64(second.BytesRecv-first.BytesRecv) / seconds
	sentBps = float64(second.BytesSent-first.BytesSent) / seconds
	return recvBps, sentBps, nil
}

// NetIOThroughput samples an interface's counters twice, separated by
// interval, and returns the average receive/send throughput in bytes/sec.
func NetIOThroughput(iface string, interval time.Duration) (NetIOMeasurement, error) {
	first, err := ReadNetIOStats(iface)
	if err != nil {
		return NetIOMeasurement{}, err
	}

	time.Sleep(interval)

	second, err := ReadNetIOStats(iface)
	if err != nil {
		return NetIOMeasurement{}, err
	}

	seconds := interval.Seconds()
	if seconds == 0 {
		return NetIOMeasurement{}, nil
	}
	return NetIOMeasurement{
		RecvBps: float64(second.BytesRecv-first.BytesRecv) / seconds,
		SentBps: float64(second.BytesSent-first.BytesSent) / seconds,
	}, nil
}
