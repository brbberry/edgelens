package transport

import "net"

// UDPReceiver listens for UDP datagrams on a local address.
type UDPReceiver struct {
	conn *net.UDPConn
}

// NewUDPReceiver binds a UDP receiver to address.
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

// Receive waits for one datagram and returns its size and sender address.
func (r *UDPReceiver) Receive(buffer []byte) (int, *net.UDPAddr, error) {
	bytesRead, senderAddress, err := r.conn.ReadFromUDP(buffer)
	return bytesRead, senderAddress, err
}

// Close releases the listening socket.
func (r *UDPReceiver) Close() error {
	return r.conn.Close()
}
