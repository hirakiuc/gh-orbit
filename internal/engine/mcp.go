package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/google/uuid"
	"github.com/hirakiuc/gh-orbit/internal/engine/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// MCPServer wraps the MCP server logic and its UDS transport.
type MCPServer struct {
	engine      *CoreEngine
	server      *server.MCPServer
	socket      string
	insecureDev bool
}

func NewMCPServer(engine *CoreEngine, socketPath string, insecure bool) *MCPServer {
	s := server.NewMCPServer(
		"gh-orbit",
		"0.1.0",
		server.WithResourceCapabilities(true, false),
		server.WithToolCapabilities(true),
	)

	m := &MCPServer{
		engine:      engine,
		server:      s,
		socket:      socketPath,
		insecureDev: insecure,
	}

	m.registerTools()
	m.registerResources()

	return m
}

func (s *MCPServer) Serve(ctx context.Context) error {
	// 1. Ensure runtime directory exists for the socket
	dir := filepath.Dir(s.socket)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create runtime directory %s: %w", dir, err)
	}

	// 2. Handle stale socket via Probe and Purge
	if err := s.handleStaleSocket(); err != nil {
		return err
	}

	// 3. Setup UDS Listener with Peer Verification
	l, err := net.Listen("unix", s.socket)
	if err != nil {
		return fmt.Errorf("failed to listen on UDS %s: %w", s.socket, err)
	}
	defer func() {
		_ = l.Close()
		_ = os.Remove(s.socket)
	}()

	// Secure the socket file immediately
	if err := os.Chmod(s.socket, 0600); err != nil {
		return fmt.Errorf("failed to secure socket file: %w", err)
	}

	// Wrap with peer verification
	verifier := transport.NewDarwinVerifier(s.insecureDev)
	secureListener := transport.NewListener(l, verifier)

	// 4. Handle Signals for clean shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	s.engine.Logger.InfoContext(ctx, "MCP server starting", "socket", s.socket)

	// 5. Connection Loop
	go func() {
		for {
			conn, err := secureListener.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					s.engine.Logger.ErrorContext(ctx, "failed to accept connection", "error", err)
					continue
				}
			}

			go s.handleConnection(ctx, conn)
		}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case sig := <-sigChan:
		s.engine.Logger.InfoContext(ctx, "received signal, shutting down", "signal", sig)
		return nil
	}
}

func (s *MCPServer) handleStaleSocket() error {
	if _, err := os.Stat(s.socket); os.IsNotExist(err) {
		return nil
	}

	// Attempt connection to see if it's alive
	conn, err := net.Dial("unix", s.socket)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("socket %s is already in use by another process", s.socket)
	}

	// Socket exists but not reachable, purge it
	s.engine.Logger.Warn("purging stale socket file", "path", s.socket)
	return os.Remove(s.socket)
}

// udsSession implements server.ClientSession for UDS transport.
type udsSession struct {
	id            string
	notifications chan mcp.JSONRPCNotification
	initialized   atomic.Bool
	writer        io.Writer
	writeMu       sync.Mutex
}

func newUDSSession(writer io.Writer) *udsSession {
	return &udsSession{
		id:            uuid.New().String(),
		notifications: make(chan mcp.JSONRPCNotification, 100),
		writer:        writer,
	}
}

func (s *udsSession) SessionID() string { return s.id }
func (s *udsSession) Initialize()      { s.initialized.Store(true) }
func (s *udsSession) Initialized() bool { return s.initialized.Load() }
func (s *udsSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return s.notifications
}

func (s *MCPServer) handleConnection(ctx context.Context, conn net.Conn) {
	defer func() {
		_ = conn.Close()
	}()

	s.engine.Logger.Debug("peer connected and verified via UDS")

	session := newUDSSession(conn)
	if err := s.server.RegisterSession(ctx, session); err != nil {
		s.engine.Logger.ErrorContext(ctx, "failed to register session", "error", err)
		return
	}
	defer s.server.UnregisterSession(ctx, session.SessionID())

	sessionCtx := s.server.WithContext(ctx, session)
	reader := bufio.NewReader(conn)

	// Start notification dispatcher for this session
	go func() {
		for {
			select {
			case <-sessionCtx.Done():
				return
			case notification := <-session.notifications:
				data, err := json.Marshal(notification)
				if err == nil {
					session.writeMu.Lock()
					_, _ = fmt.Fprintf(session.writer, "%s\n", data)
					session.writeMu.Unlock()
				}
			}
		}
	}()

	// Request/Response loop
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				s.engine.Logger.ErrorContext(ctx, "read error", "error", err)
			}
			break
		}

		var rawMessage json.RawMessage
		if err := json.Unmarshal([]byte(line), &rawMessage); err != nil {
			continue
		}

		response := s.server.HandleMessage(sessionCtx, rawMessage)
		if response != nil {
			data, err := json.Marshal(response)
			if err == nil {
				session.writeMu.Lock()
				_, _ = fmt.Fprintf(session.writer, "%s\n", data)
				session.writeMu.Unlock()
			}
		}
	}
}

func (s *MCPServer) registerTools() {
	// 1. Sync Tool
	syncTool := mcp.NewTool("sync",
		mcp.WithDescription("Trigger a background synchronization with GitHub"),
		mcp.WithBoolean("force",
			mcp.Description("Force a cold sync even if interval not reached"),
		),
	)

	s.server.AddTool(syncTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]any)
		if !ok {
			return mcp.NewToolResultError("invalid arguments format"), nil
		}

		force := false
		if f, ok := args["force"].(bool); ok {
			force = f
		}

		user, err := s.engine.Client.CurrentUser(ctx)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get current user: %v", err)), nil
		}

		rl, err := s.engine.Sync.Sync(ctx, user.Login, force)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("sync failed: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Sync complete. Quota remaining: %d/%d", rl.Remaining, rl.Limit)), nil
	})

	// 2. Mark Read Tool
	markReadTool := mcp.NewTool("mark_read",
		mcp.WithDescription("Mark a notification thread as read"),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description("The GitHub notification ID"),
		),
	)

	s.server.AddTool(markReadTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]any)
		if !ok {
			return mcp.NewToolResultError("invalid arguments format"), nil
		}

		id, _ := args["id"].(string)
		if id == "" {
			return mcp.NewToolResultError("id is required"), nil
		}

		if err := s.engine.DB.MarkReadLocally(ctx, id, true); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to mark read locally: %v", err)), nil
		}
		if err := s.engine.Client.MarkThreadAsRead(ctx, id); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to mark read on GitHub: %v", err)), nil
		}
		return mcp.NewToolResultText("Notification marked as read"), nil
	})
}

func (s *MCPServer) registerResources() {
	// Notifications Resource
	res := mcp.NewResource("gh-orbit://notifications/unread", "Unread Notifications",
		mcp.WithResourceDescription("List of unread notifications from the local database"),
		mcp.WithMIMEType("application/json"),
	)

	s.server.AddResource(res, func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		notifs, err := s.engine.DB.ListNotifications(ctx)
		if err != nil {
			return nil, err
		}

		var unread []any
		for _, n := range notifs {
			if !n.IsReadLocally {
				unread = append(unread, n)
			}
		}

		data, err := json.Marshal(unread)
		if err != nil {
			return nil, err
		}

		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      "gh-orbit://notifications/unread",
				MIMEType: "application/json",
				Text:     string(data),
			},
		}, nil
	})
}
