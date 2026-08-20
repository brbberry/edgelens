package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/brbberry/edgelens/internal/metricagg"
	"github.com/brbberry/edgelens/internal/transport"
	"github.com/brbberry/edgelens/internal/wire"
)

func main() {

	samplingConfig := metricagg.DefaultSamplingConfig()
	targetConfig := metricagg.DefaultTargetConfig()

	agg, err := metricagg.GatherSystemSnapshot(samplingConfig, targetConfig)
	if err != nil {
		fmt.Println("Error gathering system snapshot:", err)
		return
	}
	host_str := "192.168.1.17"
	time_stamp := time.Now().Unix()
	raw_message := wire.FromSnapshot(agg, host_str, time_stamp)
	dest_str := "Blakes-MacBook-Pro.local:9000"
	chunnel, err := transport.NewUDPSender(dest_str)
	if err != nil {
		fmt.Println("Error creating UDP sender:", err)
		return
	}
	defer chunnel.Close()
	raw_converted_message, err := json.Marshal(raw_message)
	if err != nil {
		fmt.Println("Error marshalling message:", err)
		return
	}
	err = chunnel.Send(raw_converted_message)
	if err != nil {
		fmt.Println("Error sending message:", err)
		return
	}

}
