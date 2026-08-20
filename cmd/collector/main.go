package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/brbberry/edgelens/internal/store"
	"github.com/brbberry/edgelens/internal/transport"
	"github.com/brbberry/edgelens/internal/transport/codec"
)

func main() {
	databasePath := flag.String("database", "measurements.db", "path to the SQLite measurements database")
	udpAddress := flag.String("udp-address", ":9000", "UDP listen address for agent measurements")
	flag.Parse()

	database, err := store.Open(*databasePath)
	if err != nil {
		fmt.Println("database error:", err)
		return
	}
	defer database.Close()

	receiver, err := transport.NewUDPReceiver(*udpAddress)
	if err != nil {
		fmt.Println("receiver error:", err)
		return
	}
	defer receiver.Close()
	fmt.Printf("collector listening for UDP measurements on %s\n", *udpAddress)

	buffer := make([]byte, 64*1024)
	decoder := codec.JSONCodec{}
	for {
		bytesRead, senderAddress, err := receiver.Receive(buffer)
		if err != nil {
			fmt.Println("receive error:", err)
			continue
		}

		fmt.Println("received from:", senderAddress)
		fmt.Println("raw message:", string(buffer[:bytesRead]))

		measurement, err := decoder.Decode(buffer[:bytesRead])

		if err != nil {
			fmt.Println("decode error:", err)
			continue
		}

		if err := database.WriteMeasurement(context.Background(), measurement); err != nil {
			fmt.Println("database write error:", err)
			continue
		}

		fmt.Printf("stored measurement from %s: host=%s timestamp=%d\n",
			senderAddress,
			measurement.Host,
			measurement.Timestamp,
		)

		fmt.Printf("decoded measurement: %+v\n", measurement)
	}
}
