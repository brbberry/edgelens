package reciever

import "net"

type UDPReceiver struct {
	conn *net.UDPConn
}

func NewUDPReceiver(address string) (*UDPReceiver, error) {
	addr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return nil, err
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}

	return &UDPReceiver{conn: conn}, nil
}

func (r *UDPReceiver) Receive(buffer []byte) (int, error) {
	n, _, err := r.conn.ReadFromUDP(buffer)
	return n, err
}

func (r *UDPReceiver) Close() error {
	return r.conn.Close()
}
