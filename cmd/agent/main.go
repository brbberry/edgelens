package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/brbberry/edgelens/internal/metricagg"
	"github.com/brbberry/edgelens/internal/transport"
	"github.com/brbberry/edgelens/internal/transport/codec"
	"github.com/brbberry/edgelens/internal/wire"
)

func main() {
	sources := metricagg.DefaultMetricSources()

	destination := flag.String("collector", "127.0.0.1:9000", "UDP address of the collector")
	hostOverride := flag.String("host", "", "host identity to include in measurements (defaults to the system hostname)")
	reportInterval := flag.Duration("report-interval", metricagg.DefaultReportInterval, "interval covered by each rate measurement and between reports")
	diskDevice := flag.String("disk-device", sources.DiskDevice, "disk device to measure")
	diskMountPath := flag.String("disk-mount", sources.DiskMountPath, "mount path whose disk usage to measure")
	networkInterface := flag.String("network-interface", sources.NetworkInterface, "network interface to measure")
	flag.Parse()

	if *reportInterval < metricagg.MinimumReportInterval {
		fmt.Printf("report-interval must be at least %s\n", metricagg.MinimumReportInterval)
		return
	}
	sources.DiskDevice = *diskDevice
	sources.DiskMountPath = *diskMountPath
	sources.NetworkInterface = *networkInterface

	host := *hostOverride
	if host == "" {
		var err error
		host, err = os.Hostname()
		if err != nil {
			fmt.Println("get hostname:", err)
			return
		}
	}

	sender, err := transport.NewUDPSender(*destination)
	if err != nil {
		fmt.Println("create UDP sender:", err)
		return
	}
	defer sender.Close()

	fmt.Printf("sending measurements from %s to %s every %s\n", host, *destination, *reportInterval)
	encoder := codec.JSONCodec{}
	for {
		cycleStartedAt := time.Now()
		agg, err := metricagg.GatherSystemSnapshot(*reportInterval, sources)
		if err != nil {
			fmt.Println("gather system snapshot:", err)
		} else {
			message := wire.FromSnapshot(agg, host, time.Now().Unix())
			payload, err := encoder.Encode(message)
			if err != nil {
				fmt.Println("encode measurement:", err)
			} else if err := sender.Send(payload); err != nil {
				fmt.Println("send measurement:", err)
			}
		}

		if delay := time.Until(cycleStartedAt.Add(*reportInterval)); delay > 0 {
			time.Sleep(delay)
		}
	}
}
