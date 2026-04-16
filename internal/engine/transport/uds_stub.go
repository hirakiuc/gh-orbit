//go:build !darwin

package transport

import (
	"fmt"
	"net"
)

type StubVerifier struct {
	InsecureDev bool
}

func NewDarwinVerifier(insecure bool) *StubVerifier {
	return &StubVerifier{InsecureDev: insecure}
}

func (v *StubVerifier) Verify(conn net.Conn) (*PeerInfo, error) {
	if !v.InsecureDev {
		return nil, fmt.Errorf("secure transport only supported on macOS")
	}
	return &PeerInfo{PID: 0, UID: 0, GID: 0}, nil
}
