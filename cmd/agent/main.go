package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/brbberry/edgelens/internal/metricagg"
	"github.com/brbberry/edgelens/internal/store"
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

	database, err := store.Open("edgelens.db")
	if err != nil {
		fmt.Println("database error:", err)
		return
	}
	defer database.Close()

	if err := database.WriteMeasurement(context.Background(), m); err != nil {
		fmt.Println("write error:", err)
		return
	}

	fmt.Println("stored system measurement for", host)
}
