package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

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
	// 1. Ensure runtime directory exists
	dir := os.ExpandEnv("$XDG_RUNTIME_DIR/gh-orbit")
	if dir == "" || dir == "/gh-orbit" {
		dir = "/tmp/gh-orbit"
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create runtime directory: %w", err)
	}

	// 2. Remove stale socket
	_ = os.Remove(s.socket)

	// 3. Setup UDS Listener with Peer Verification
	l, err := net.Listen("unix", s.socket)
	if err != nil {
		return fmt.Errorf("failed to listen on UDS: %w", err)
	}
	defer l.Close()
	_ = os.Chmod(s.socket, 0600)

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

			go s.handleConnection(conn)
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

func (s *MCPServer) handleConnection(conn net.Conn) {
	defer conn.Close()
	s.engine.Logger.Debug("peer connected and verified via UDS")
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

		data, _ := json.Marshal(unread)

		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      "gh-orbit://notifications/unread",
				MIMEType: "application/json",
				Text:     string(data),
			},
		}, nil
	})
}
