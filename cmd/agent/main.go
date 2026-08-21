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
	samplingConfig := metricagg.DefaultSamplingConfig()
	targetConfig := metricagg.DefaultTargetConfig()

	destination := flag.String("collector", "127.0.0.1:9000", "UDP address of the collector")
	hostOverride := flag.String("host", "", "host identity to include in measurements (defaults to the system hostname)")
	reportDelay := flag.Duration("report-delay", 5*time.Second, "delay after sending a measurement before gathering the next one")
	diskDevice := flag.String("disk-device", targetConfig.DiskDevice, "disk device to measure")
	diskMountPath := flag.String("disk-mount", targetConfig.DiskMountPath, "mount path whose disk usage to measure")
	networkInterface := flag.String("network-interface", targetConfig.NetworkInterface, "network interface to measure")
	flag.Parse()

	if *reportDelay <= 0 {
		fmt.Println("report-delay must be positive")
		return
	}
	targetConfig.DiskDevice = *diskDevice
	targetConfig.DiskMountPath = *diskMountPath
	targetConfig.NetworkInterface = *networkInterface

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

	fmt.Printf("sending measurements from %s to %s with a %s reporting delay\n", host, *destination, *reportDelay)
	encoder := codec.JSONCodec{}
	for {
		agg, err := metricagg.GatherSystemSnapshot(samplingConfig, targetConfig)
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

		time.Sleep(*reportDelay)
	}
}
