package wire

import "github.com/brbberry/edgelens/internal/metricagg"

// Version is the current wire format version. Bump on any breaking change.
const Version = 1

// Measurement is the contract between agent and collector.
// Changing a json tag here is a protocol change — treat it as such.
type Measurement struct {
	Version   int    `json:"v"`
	Host      string `json:"host"`
	Timestamp int64  `json:"ts"` // unix seconds, UTC

	CPUUsagePct float64 `json:"cpu_pct"`

	MemUsedPct    float64 `json:"mem_used_pct"`
	MemUsedBytes  uint64  `json:"mem_used_b"`
	MemTotalBytes uint64  `json:"mem_total_b"`
	SwapUsedPct   float64 `json:"swap_used_pct"`

	DiskUsagePct float64 `json:"disk_used_pct"`
	DiskReadBps  float64 `json:"disk_read_bps"`
	DiskWriteBps float64 `json:"disk_write_bps"`

	NetRecvBps float64 `json:"net_recv_bps"`
	NetSentBps float64 `json:"net_sent_bps"`

	TempZone    string  `json:"temp_zone"`
	TempType    string  `json:"temp_type"`
	TempCelsius float64 `json:"temp_c"`
}

// FromSnapshot translates the agent's internal representation into the wire
// contract. This is the ONLY place the two shapes are coupled — every field
// mapping lives here, so a refactor of SystemSnapshot fails to compile here
// rather than silently changing the protocol.
//
// host and ts are passed in rather than read here so that this package stays
// pure: no syscalls, no clock, trivially testable.
func FromSnapshot(s metricagg.SystemSnapshot, host string, ts int64) Measurement {
	return Measurement{
		Version:   Version,
		Host:      host,
		Timestamp: ts,

		CPUUsagePct: s.CPU.UsagePercent,

		MemUsedPct:    s.Memory.UsedPercent,
		MemUsedBytes:  s.Memory.UsedBytes,
		MemTotalBytes: s.Memory.TotalBytes,
		SwapUsedPct:   s.Memory.SwapUsedPercent,

		DiskUsagePct: s.Disk.Health.UsagePercent,
		DiskReadBps:  s.Disk.IO.ReadBps,
		DiskWriteBps: s.Disk.IO.WriteBps,

		NetRecvBps: s.Network.RecvBps,
		NetSentBps: s.Network.SentBps,

		TempZone:    s.Temperature.Zone,
		TempType:    s.Temperature.Type,
		TempCelsius: s.Temperature.Celsius,
	}
}
