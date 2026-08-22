package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/brbberry/edgelens/internal/store"
	"github.com/brbberry/edgelens/internal/transport"
	"github.com/brbberry/edgelens/internal/transport/codec"
	"github.com/brbberry/edgelens/internal/wire"
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

		packet, err := decoder.DecodePacket(buffer[:bytesRead])
		if err != nil {
			fmt.Println("decode error:", err)
			continue
		}

		var writeErr error
		switch packet.Kind {
		case wire.PacketMeasurement:
			writeErr = database.WriteMeasurement(context.Background(), *packet.Measurement)
		case wire.PacketRunStarted:
			writeErr = database.CreateRun(context.Background(), *packet.Experiment)
		case wire.PacketProcessSample:
			writeErr = database.WriteProcessSample(context.Background(), *packet.Experiment)
		case wire.PacketRunFinished:
			writeErr = database.FinalizeRun(context.Background(), *packet.Experiment)
		case wire.PacketArtifact:
			writeErr = database.WriteArtifact(context.Background(), *packet.Experiment)
		default:
			writeErr = fmt.Errorf("unsupported packet kind %q", packet.Kind)
		}
		if writeErr != nil {
			fmt.Println("database write error:", writeErr)
			continue
		}
		fmt.Printf("stored %s packet from %s\n", packet.Kind, senderAddress)
	}
}
