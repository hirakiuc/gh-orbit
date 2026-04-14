package transport

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockVerifier struct {
	shouldFail bool
}

func (v *mockVerifier) Verify(conn net.Conn) (*PeerInfo, error) {
	if v.shouldFail {
		return nil, os.ErrPermission
	}
	return &PeerInfo{PID: os.Getpid(), UID: uint32(os.Getuid())}, nil
}

func TestListener_Accept(t *testing.T) {
	cwd, _ := os.Getwd()
	// Use project-local tmp for sandbox compatibility
	tmpDir := filepath.Join(cwd, "../../../tmp")
	_ = os.MkdirAll(tmpDir, 0700)
	socket := filepath.Join(tmpDir, "test-orbit.sock")

	_ = os.Remove(socket)
	defer os.Remove(socket)

	l, err := net.Listen("unix", socket)
	require.NoError(t, err)
	defer l.Close()

	t.Run("Successful Verification", func(t *testing.T) {
		verifier := &mockVerifier{shouldFail: false}
		secureListener := NewListener(l, verifier)

		done := make(chan struct{})
		go func() {
			conn, err := net.Dial("unix", socket)
			assert.NoError(t, err)
			if err == nil {
				conn.Close()
			}
			close(done)
		}()

		conn, err := secureListener.Accept()
		assert.NoError(t, err)
		assert.NotNil(t, conn)
		if conn != nil {
			conn.Close()
		}
		<-done
	})

	t.Run("Failed Verification Disconnects", func(t *testing.T) {
		socket2 := socket + ".2"
		l2, err := net.Listen("unix", socket2)
		require.NoError(t, err)
		defer os.Remove(socket2)
		defer l2.Close()

		verifier := &mockVerifier{shouldFail: true}
		secureListener := NewListener(l2, verifier)

		go func() {
			conn, err := net.Dial("unix", socket2)
			if err == nil {
				// We expect the server to close the connection
				buf := make([]byte, 1)
				_, _ = conn.Read(buf)
				conn.Close()
			}
		}()

		conn, err := secureListener.Accept()
		assert.Error(t, err)
		assert.Nil(t, conn)
	})
}
