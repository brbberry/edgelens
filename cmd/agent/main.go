package main

import (
	"fmt"

	"github.com/brbberry/edgelens/internal/sysmetrics"
)

type SystemMetrics struct {
	CPU         sysmetrics.CPUStats
	Memory      sysmetrics.MemStats
	Network     sysmetrics.NetIOStats
	Temperature []sysmetrics.TempZone
}

func main() {

	curRead := SystemMetrics{}
	cpuStats, err := sysmetrics.ReadCPUStats()
	if err != nil {
		fmt.Println("Error reading CPU stats:", err)
		return
	}
	curRead.CPU = cpuStats

	memStats, err := sysmetrics.ReadMemStats()
	if err != nil {
		fmt.Println("Error reading memory stats:", err)
		return
	}
	curRead.Memory = memStats

	netStats, err := sysmetrics.ReadNetIOStats("eth0")
	if err != nil {
		fmt.Println("Error reading network stats:", err)
		return
	}
	curRead.Network = netStats

	tempZones, err := sysmetrics.ReadTemps()
	if err != nil {
		fmt.Println("Error reading temperature zones:", err)
		return
	}
	curRead.Temperature = tempZones

	fmt.Printf("Current System Metrics: %+v\n", curRead)
}
