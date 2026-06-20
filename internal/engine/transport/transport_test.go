package transport

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

type mockVerifier struct {
	shouldFail bool
}

func (v *mockVerifier) Verify(conn net.Conn) (*PeerInfo, error) {
	if v.shouldFail {
		return nil, os.ErrPermission
	}
	// #nosec G115: Safe UID conversion for mock
	return &PeerInfo{PID: os.Getpid(), UID: uint32(os.Getuid())}, nil
}

func TestListener_Accept(t *testing.T) {
	cwd, _ := os.Getwd()
	// Use project-local tmp for sandbox compatibility
	tmpDir := filepath.Join(cwd, "../../../tmp")
	_ = os.MkdirAll(tmpDir, 0o700)
	socket := filepath.Join(tmpDir, "test-orbit.sock")

	_ = os.Remove(socket)
	defer func() {
		_ = os.Remove(socket)
	}()

	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Logf("Skipping test: cannot listen on %s: %v", socket, err)
		return
	}
	defer func() {
		_ = l.Close()
	}()

	t.Run("Successful Verification", func(t *testing.T) {
		verifier := &mockVerifier{shouldFail: false}
		secureListener := NewListener(l, verifier)

		done := make(chan struct{})
		go func() {
			conn, err := net.Dial("unix", socket)
			assert.NoError(t, err)
			if err == nil {
				_ = conn.Close()
			}
			close(done)
		}()

		conn, err := secureListener.Accept()
		assert.NoError(t, err)
		assert.NotNil(t, conn)
		if conn != nil {
			_ = conn.Close()
		}
		<-done
	})

	t.Run("Failed Verification Disconnects", func(t *testing.T) {
		socket2 := socket + ".2"
		_ = os.Remove(socket2)
		l2, err := net.Listen("unix", socket2)
		if err != nil {
			t.Logf("Skipping subtest: cannot listen on %s: %v", socket2, err)
			return
		}
		defer func() {
			_ = os.Remove(socket2)
		}()
		defer func() {
			_ = l2.Close()
		}()

		verifier := &mockVerifier{shouldFail: true}
		secureListener := NewListener(l2, verifier)

		go func() {
			conn, err := net.Dial("unix", socket2)
			if err == nil {
				// We expect the server to close the connection
				buf := make([]byte, 1)
				_, _ = conn.Read(buf)
				_ = conn.Close()
			}
		}()

		conn, err := secureListener.Accept()
		assert.Error(t, err)
		assert.Nil(t, conn)
	})
}
