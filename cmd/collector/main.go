package main

import (
	"context"
	"fmt"

	"github.com/brbberry/edgelens/internal/store"
	"github.com/brbberry/edgelens/internal/transport"
	"github.com/brbberry/edgelens/internal/transport/codec"
)

func main() {
	// later make the database path configurable via command line flags or environment variables
	database, err := store.Open("measurements.db")
	if err != nil {
		fmt.Println("database error:", err)
		return
	}
	defer database.Close()

	// later make the UDP port configurable via command line flags or environment variables
	receiver, err := transport.NewUDPReceiver(":9000")
	if err != nil {
		fmt.Println("receiver error:", err)
		return
	}
	defer receiver.Close()
	fmt.Println("collector listening on UDP port 9000")

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
