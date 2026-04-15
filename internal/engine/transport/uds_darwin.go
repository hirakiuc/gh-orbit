//go:build darwin

package transport

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

type DarwinVerifier struct {
	InsecureDev bool
}

func NewDarwinVerifier(insecure bool) *DarwinVerifier {
	return &DarwinVerifier{InsecureDev: insecure}
}

func (v *DarwinVerifier) Verify(conn net.Conn) (*PeerInfo, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return nil, fmt.Errorf("not a unix connection")
	}

	rawConn, err := unixConn.File()
	if err != nil {
		return nil, fmt.Errorf("failed to get raw connection: %w", err)
	}
	defer func() {
		_ = rawConn.Close()
	}()

	// #nosec G115: Fd is guaranteed to be a valid file descriptor index
	fd := int(rawConn.Fd())

	// 1. Get Peer PID
	pid, err := unix.GetsockoptInt(fd, unix.SOL_LOCAL, unix.LOCAL_PEERPID)
	if err != nil {
		return nil, fmt.Errorf("failed to get peer pid: %w", err)
	}

	// 2. Get Peer UID/GID
	uid, gid, err := getpeereid(fd)
	if err != nil {
		return nil, fmt.Errorf("failed to get peer eid: %w", err)
	}

	// 3. UID Check (Must be same user)
	// #nosec G115: UID comparison is safe on macOS
	if uint32(os.Getuid()) != uid {
		return nil, fmt.Errorf("unauthorized user: peer uid %d != engine uid %d", uid, os.Getuid())
	}

	info := &PeerInfo{
		PID: pid,
		UID: uid,
		GID: gid,
	}

	if v.InsecureDev {
		return info, nil
	}

	// 4. Code Signature Verification (macOS)
	if err := verifyCodeSignature(pid); err != nil {
		return nil, fmt.Errorf("code signature verification failed: %w", err)
	}

	return info, nil
}

// getpeereid is a manual implementation for macOS since it's not in x/sys/unix directly
func getpeereid(fd int) (uint32, uint32, error) {
	cr, err := getsockoptPeerCred(fd)
	if err != nil {
		return 0, 0, err
	}
	// On macOS, Xucred has Uid and Groups [16]uint32
	var gid uint32
	if cr.Ngroups > 0 {
		gid = cr.Groups[0]
	}
	return cr.Uid, gid, nil
}

func getsockoptPeerCred(fd int) (*unix.Xucred, error) {
	return unix.GetsockoptXucred(fd, unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
}

func verifyCodeSignature(pid int) error {
	path, err := getPidPath(pid)
	if err != nil {
		return err
	}

	// Security Hardening: Enforce specific trust requirement.
	// We require the peer to be signed by Apple or a valid Developer,
	// AND have an identifier that starts with 'gh-orbit-'.
	// This prevents any generic Apple app from talking to the socket.
	// #nosec G204: Intentional security check of peer binary identity
	req := `anchor apple generic and identifier prefix "gh-orbit-"`
	cmd := exec.Command("codesign", "-v", "-R", req, path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("binary at %s is not validly signed or fails requirement: %s", path, string(out))
	}

	return nil
}

func getPidPath(pid int) (string, error) {
	// #nosec G204: Intentional PID lookup for security verification
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get path for pid %d: %w", pid, err)
	}
	return strings.TrimSpace(string(out)), nil
}
