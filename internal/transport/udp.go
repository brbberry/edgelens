package transport

import (
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
	_, err := s.conn.Write(payload)
	return err
}

func (s *UDPSender) Close() error {
	return s.conn.Close()
}
