package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/brbberry/edgelens/internal/metricagg"
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

	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}

	m := wire.FromSnapshot(agg, host, time.Now().Unix())

	b, err := json.Marshal(m)

	fmt.Printf("System Snapshot: %+v\n", b)

	if err != nil {
		fmt.Println("marshal error:", err)
		return
	}
	// fmt.Println("--- sizing probe ---")
	// fmt.Println("payload bytes:", len(b))
	// fmt.Println(string(b))
}
