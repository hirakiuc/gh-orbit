package transport

import (
	"net"
)

// PeerInfo represents the identity of a connected client.
type PeerInfo struct {
	PID int
	UID uint32
	GID uint32
}

// PeerVerifier defines the interface for validating connected clients.
type PeerVerifier interface {
	Verify(conn net.Conn) (*PeerInfo, error)
}

// Listener wraps a net.Listener with mandatory peer verification.
type Listener struct {
	net.Listener
	verifier PeerVerifier
}

func NewListener(l net.Listener, v PeerVerifier) *Listener {
	return &Listener{
		Listener: l,
		verifier: v,
	}
}

func (l *Listener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}

	if _, err := l.verifier.Verify(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}

	return conn, nil
}
