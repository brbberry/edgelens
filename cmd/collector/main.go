package main

import (
	"fmt"

	"github.com/brbberry/edgelens/internal/transport"
	"github.com/brbberry/edgelens/internal/transport/codec"
)

func main() {

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

		fmt.Printf("decoded measurement: %+v\n", measurement)
	}
}
