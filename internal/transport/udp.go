package transport

import (
	"fmt"
	"net"
)

type UDPSender struct {
	conn *net.UDPConn
}

func NewUDPSender(address string) (*UDPSender, error) {
	addr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, err
	}

	return &UDPSender{conn: conn}, nil
}

func (s *UDPSender) Send(payload []byte) error {
	written, err := s.conn.Write(payload)
	if err != nil {
		return err
	}
	if written != len(payload) {
		return fmt.Errorf("short UDP write: wrote %d of %d bytes", written, len(payload))
	}
	return nil
}

func (s *UDPSender) Close() error {
	return s.conn.Close()
}
