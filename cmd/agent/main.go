package main

import (
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

	host, err := os.Hostname()
	if err != nil {
		fmt.Println("Error getting hostname:", err)
		return
	}
	destination := "Blakes-MacBook-Pro.local:9000"
	sender, err := transport.NewUDPSender(destination)
	if err != nil {
		fmt.Println("Error creating UDP sender:", err)
		return
	}
	defer sender.Close()
	encoder := codec.JSONCodec{}
	for {
		agg, err := metricagg.GatherSystemSnapshot(samplingConfig, targetConfig)
		if err != nil {
			fmt.Println("Error gathering system snapshot:", err)
			continue
		}
		timestamp := time.Now().Unix()

		message := wire.FromSnapshot(agg, host, timestamp)
		payload, err := encoder.Encode(message)
		if err != nil {
			fmt.Println("Error marshalling message:", err)
			continue
		}
		if err := sender.Send(payload); err != nil {
			fmt.Println("Error sending message:", err)
			continue
		}
	}
}
