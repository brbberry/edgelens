package main

import (
	"fmt"
	"net"

	"github.com/brbberry/edgelens/internal/transport/codec"
)

func main() {

	address, err := net.ResolveUDPAddr("udp", ":9000")
	if err != nil {
		fmt.Println("address error:", err)
		return
	}

	conn, err := net.ListenUDP("udp", address)
	if err != nil {
		fmt.Println("listen error:", err)
		return
	}
	defer conn.Close()
	fmt.Println("collector listening on UDP port 9000")

	buffer := make([]byte, 64*1024)

	bytesRead, senderAddress, err := conn.ReadFromUDP(buffer)
	if err != nil {
		fmt.Println("receive error:", err)
		return
	}

	fmt.Println("received from:", senderAddress)
	fmt.Println("raw message:", string(buffer[:bytesRead]))

	measurement, err := (codec.JSONCodec{}).Decode(buffer[:bytesRead])
	if err != nil {
		fmt.Println("decode error:", err)
		return
	}

	fmt.Printf("decoded measurement: %+v\n", measurement)
}
