package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// UDSClientTransport implements transport.Interface for Unix Domain Sockets.
type UDSClientTransport struct {
	socketPath string
	conn       net.Conn
	sessionID  string

	notificationHandler func(mcp.JSONRPCNotification)
	pendingRequests     sync.Map // map[RequestId]chan *transport.JSONRPCResponse

	initialized atomic.Bool
	closed      chan struct{}
	wg          sync.WaitGroup
}

func NewUDSClientTransport(socketPath string) *UDSClientTransport {
	return &UDSClientTransport{
		socketPath: socketPath,
		sessionID:  uuid.New().String(),
		closed:     make(chan struct{}),
	}
}

func (t *UDSClientTransport) Start(ctx context.Context) error {
	conn, err := net.Dial("unix", t.socketPath)
	if err != nil {
		return fmt.Errorf("failed to connect to UDS: %w", err)
	}
	t.conn = conn
	t.initialized.Store(true)

	t.wg.Add(1)
	go t.readLoop()

	return nil
}

func (t *UDSClientTransport) readLoop() {
	defer t.wg.Done()
	reader := bufio.NewReader(t.conn)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}

		var raw json.RawMessage
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}

		// Try to parse as response
		var resp transport.JSONRPCResponse
		if err := json.Unmarshal(raw, &resp); err == nil && resp.ID.Value() != nil {
			if ch, ok := t.pendingRequests.Load(resp.ID); ok {
				ch.(chan *transport.JSONRPCResponse) <- &resp
				t.pendingRequests.Delete(resp.ID)
				continue
			}
		}

		// Try to parse as notification
		var notif mcp.JSONRPCNotification
		if err := json.Unmarshal(raw, &notif); err == nil && notif.Method != "" {
			if t.notificationHandler != nil {
				t.notificationHandler(notif)
			}
		}
	}
}

func (t *UDSClientTransport) SendRequest(ctx context.Context, request transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	if !t.initialized.Load() {
		return nil, fmt.Errorf("transport not started")
	}

	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	respCh := make(chan *transport.JSONRPCResponse, 1)
	t.pendingRequests.Store(request.ID, respCh)

	_, err = fmt.Fprintf(t.conn, "%s\n", data)
	if err != nil {
		t.pendingRequests.Delete(request.ID)
		return nil, err
	}

	select {
	case <-ctx.Done():
		t.pendingRequests.Delete(request.ID)
		return nil, ctx.Err()
	case resp := <-respCh:
		return resp, nil
	}
}

func (t *UDSClientTransport) SendNotification(ctx context.Context, notification mcp.JSONRPCNotification) error {
	if !t.initialized.Load() {
		return fmt.Errorf("transport not started")
	}

	data, err := json.Marshal(notification)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(t.conn, "%s\n", data)
	return err
}

func (t *UDSClientTransport) SetNotificationHandler(handler func(notification mcp.JSONRPCNotification)) {
	t.notificationHandler = handler
}

func (t *UDSClientTransport) Close() error {
	if t.conn != nil {
		_ = t.conn.Close()
	}
	close(t.closed)
	t.wg.Wait()
	return nil
}

func (t *UDSClientTransport) GetSessionId() string {
	return t.sessionID
}

// Ensure UDSClientTransport implements transport.Interface
var _ transport.Interface = (*UDSClientTransport)(nil)
