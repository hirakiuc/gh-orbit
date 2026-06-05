package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/engine/transport"
	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/triage"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// MCPServer wraps the MCP server logic and its UDS transport.
type MCPServer struct {
	engine      *CoreEngine
	server      *server.MCPServer
	socket      string
	insecureDev bool
	verbose     bool
}

func NewMCPServer(engine *CoreEngine, socketPath string, insecure bool, verbose bool) *MCPServer {
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
		verbose:     verbose,
	}

	m.registerTools()
	m.registerResources()

	return m
}

func (s *MCPServer) Serve(ctx context.Context) error {
	// 1. Ensure runtime directory exists for the socket
	dir := filepath.Dir(s.socket)
	if err := os.MkdirAll(dir, 0o700); err != nil {
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

	// Secure the socket file immediately
	if err := os.Chmod(s.socket, 0o600); err != nil {
		_ = l.Close()
		return fmt.Errorf("failed to secure socket file: %w", err)
	}

	// Wrap with peer verification
	verifier := transport.NewDarwinVerifier(s.insecureDev)
	secureListener := transport.NewListener(l, verifier)

	// 4. Handle Signals for clean shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	s.engine.Logger.InfoContext(ctx, "MCP server starting", "socket", s.socket)

	// 5. Internal Event Loop
	go s.eventLoop(ctx)

	// 6. Connection Loop
	done := make(chan struct{})
	var isClosing atomic.Bool

	go func() {
		defer close(done)
		for {
			conn, err := secureListener.Accept()
			if err != nil {
				if isClosing.Load() || errors.Is(err, net.ErrClosed) {
					return
				}

				select {
				case <-ctx.Done():
					return
				default:
					s.engine.Logger.ErrorContext(ctx, "failed to accept connection", "error", err)
					// Small backoff to prevent log flooding
					time.Sleep(100 * time.Millisecond)
					continue
				}
			}

			go s.handleConnection(ctx, conn)
		}
	}()

	var serveErr error
	select {
	case <-ctx.Done():
		serveErr = ctx.Err()
	case sig := <-sigChan:
		s.engine.Logger.InfoContext(ctx, "received signal, shutting down", "signal", sig)
	}

	// 7. Graceful Shutdown
	isClosing.Store(true)
	_ = l.Close()
	_ = os.Remove(s.socket)

	// Wait for connection loop to exit
	select {
	case <-done:
	case <-time.After(time.Second):
		s.engine.Logger.WarnContext(ctx, "connection loop did not exit gracefully")
	}

	return serveErr
}

func (s *MCPServer) eventLoop(ctx context.Context) {
	notifCh, unsubscribeNotif := s.engine.Bus.Subscribe(EventNotificationListChanged)
	enrichCh, unsubscribeEnrich := s.engine.Bus.Subscribe(EventNotificationEnrichmentChanged)
	defer unsubscribeNotif()
	defer unsubscribeEnrich()

	for {
		select {
		case <-ctx.Done():
			return
		case <-notifCh:
			s.notifyNotificationResourcesChanged()
		case <-enrichCh:
			s.notifyNotificationResourcesChanged()
		}
	}
}

func (s *MCPServer) notifyNotificationResourcesChanged() {
	// MCP exposes both notification-list and enrichment mutations as coarse
	// resource invalidation so clients can refetch without learning engine-only
	// event categories.
	s.server.SendNotificationToAllClients(mcp.MethodNotificationResourcesListChanged, nil)
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
func (s *udsSession) Initialize()       { s.initialized.Store(true) }
func (s *udsSession) Initialized() bool { return s.initialized.Load() }
func (s *udsSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return s.notifications
}

func (s *MCPServer) handleConnection(ctx context.Context, conn net.Conn) {
	defer func() {
		_ = conn.Close()
	}()

	s.engine.Logger.Debug("peer connected and verified via UDS")

	session, sessionCtx, cleanup, err := s.setupSession(ctx, conn)
	if err != nil {
		s.engine.Logger.ErrorContext(ctx, "failed to setup session", "error", err)
		return
	}
	defer cleanup()

	// Start notification dispatcher for this session
	go s.runNotificationDispatcher(sessionCtx, session)

	// Process incoming requests
	s.processRequestLoop(sessionCtx, session, conn)
}

func (s *MCPServer) setupSession(ctx context.Context, conn net.Conn) (*udsSession, context.Context, func(), error) {
	session := newUDSSession(conn)
	if err := s.server.RegisterSession(ctx, session); err != nil {
		return nil, nil, nil, err
	}

	sessionCtx := s.server.WithContext(ctx, session)
	cleanup := func() {
		s.server.UnregisterSession(ctx, session.SessionID())
	}

	return session, sessionCtx, cleanup, nil
}

func (s *MCPServer) runNotificationDispatcher(ctx context.Context, session *udsSession) {
	for {
		select {
		case <-ctx.Done():
			return
		case notification := <-session.notifications:
			data, err := json.Marshal(notification)
			if err == nil {
				if s.verbose {
					s.engine.Logger.Debug("MCP notification", "msg", config.RedactSecrets(string(data)))
				}
				s.sendJSONRPCMessage(session, data)
			}
		}
	}
}

func (s *MCPServer) processRequestLoop(ctx context.Context, session *udsSession, conn io.Reader) {
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				s.engine.Logger.ErrorContext(ctx, "read error", "error", err)
			}
			break
		}

		s.handleSingleMessage(ctx, session, line)
	}
}

func (s *MCPServer) handleSingleMessage(ctx context.Context, session *udsSession, line string) {
	if s.verbose {
		s.engine.Logger.Debug("MCP request", "msg", config.RedactSecrets(line))
	}

	var rawMessage json.RawMessage
	if err := json.Unmarshal([]byte(line), &rawMessage); err != nil {
		if s.verbose {
			s.engine.Logger.Debug("MCP malformed request", "error", err, "line", config.RedactSecrets(line))
		}
		return
	}

	response := s.server.HandleMessage(ctx, rawMessage)
	if response != nil {
		data, err := json.Marshal(response)
		if err == nil {
			if s.verbose {
				s.engine.Logger.Debug("MCP response", "msg", config.RedactSecrets(string(data)))
			}
			s.sendJSONRPCMessage(session, data)
		}
	}
}

func (s *MCPServer) sendJSONRPCMessage(session *udsSession, data []byte) {
	session.writeMu.Lock()
	defer session.writeMu.Unlock()
	_, _ = fmt.Fprintf(session.writer, "%s\n", data)
}

func mutationToolSuccessResult(message string) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultText(message), nil
}

func mutationToolFailureResult(format string, err error) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError(fmt.Sprintf(format, err)), nil
}

func markReadToolResult(result types.MarkReadResult, err error) (*mcp.CallToolResult, error) {
	if err != nil {
		return mutationToolFailureResult("failed to mark read locally: %v", err)
	}
	if result.Err == nil {
		return mutationToolSuccessResult("Notification read state updated")
	}

	switch result.Status {
	case types.MarkReadRemoteFailure:
		return mutationToolFailureResult("failed to mark read on GitHub: %v", result.Err)
	case types.MarkReadLocalFailure:
		return mutationToolFailureResult("failed to mark read locally: %v", result.Err)
	default:
		return mutationToolFailureResult("failed to mark read: %v", result.Err)
	}
}

func priorityToolResult(result types.PriorityUpdateResult, err error) (*mcp.CallToolResult, error) {
	if err != nil {
		return mutationToolFailureResult("failed to set priority: %v", err)
	}
	if result.Err != nil {
		return mutationToolFailureResult("failed to set priority: %v", result.Err)
	}
	return mutationToolSuccessResult("Priority updated")
}

func persistFetchedDetailToolResult(err error) (*mcp.CallToolResult, error) {
	if err != nil {
		return mutationToolFailureResult("failed to persist fetched detail: %v", err)
	}
	return mutationToolSuccessResult("Fetched detail persisted")
}

func (s *MCPServer) registerTools() {
	// 1. Sync Tool
	syncTool := mcp.NewTool(
		"sync",
		mcp.WithDescription("Trigger a background synchronization with GitHub"),
		mcp.WithBoolean(
			"force",
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

		rl, err := s.engine.Backend.Sync(ctx, force)
		if err != nil {
			if errors.Is(err, types.ErrSyncIntervalNotReached) {
				payload := syncToolResult{
					Status:    syncToolStatusIntervalNotReached,
					RateLimit: rl,
				}
				return mcp.NewToolResultStructured(payload, "sync interval not reached"), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("sync failed: %v", err)), nil
		}

		payload := syncToolResult{
			Status:    syncToolStatusOK,
			RateLimit: rl,
		}
		data, _ := json.Marshal(rl)
		return mcp.NewToolResultStructured(payload, string(data)), nil
	})

	// 2. Mark Read Tool
	markReadTool := mcp.NewTool(
		"mark_read",
		mcp.WithDescription("Mark a notification thread as read"),
		mcp.WithString(
			"id",
			mcp.Description("The GitHub notification ID"),
		),
		mcp.WithBoolean(
			"read",
			mcp.Description("Whether to mark as read or unread"),
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

		read := true
		if r, ok := args["read"].(bool); ok {
			read = r
		}

		result, err := s.engine.Backend.MarkRead(ctx, id, read)
		return markReadToolResult(result, err)
	})

	// 3. Set Priority Tool
	setPriorityTool := mcp.NewTool(
		"set_priority",
		mcp.WithDescription("Update the priority of a notification"),
		mcp.WithString("id", mcp.Description("The GitHub notification ID")),
		mcp.WithNumber("level", mcp.Description("Priority level (0-3)")),
	)

	s.server.AddTool(setPriorityTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]any)
		if !ok {
			return mcp.NewToolResultError("invalid arguments format"), nil
		}

		id, _ := args["id"].(string)
		levelVal, _ := args["level"].(float64)
		level := int(levelVal)

		result, err := s.engine.Backend.SetPriority(ctx, id, level)
		return priorityToolResult(result, err)
	})

	// 4. Fetch Detail Tool
	// Temporary explicit exception: already transport-thin and no matching
	// backend seam expansion is needed for this slice.
	fetchDetailTool := mcp.NewTool(
		"fetch_detail",
		mcp.WithDescription("Fetch enriched detail for a single notification subject"),
		mcp.WithString("url", mcp.Required(), mcp.Description("The GitHub API subject URL")),
		mcp.WithString("subject_type", mcp.Required(), mcp.Description("The GitHub subject type")),
		mcp.WithBoolean("force", mcp.Description("Whether to bypass freshness checks")),
	)

	s.server.AddTool(fetchDetailTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]any)
		if !ok {
			return mcp.NewToolResultError("invalid arguments format"), nil
		}

		url, _ := args["url"].(string)
		subjectType, _ := args["subject_type"].(string)
		if url == "" || subjectType == "" {
			return mcp.NewToolResultError("url and subject_type are required"), nil
		}

		force := false
		if f, ok := args["force"].(bool); ok {
			force = f
		}

		res, err := s.engine.Enrich.FetchDetail(ctx, url, subjectType, force)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to fetch detail: %v", err)), nil
		}

		data, _ := json.Marshal(res)
		return mcp.NewToolResultText(string(data)), nil
	})

	// 5. Batch Enrichment Tool
	// Temporary explicit exception: already transport-thin and no matching
	// backend seam expansion is needed for this slice.
	batchEnrichTool := mcp.NewTool(
		"fetch_hybrid_batch",
		mcp.WithDescription("Fetch enrichment state for a batch of notifications"),
		mcp.WithArray(
			"notifications",
			mcp.Required(),
			mcp.Description("Notification records to enrich; response is keyed by subject_node_id"),
			mcp.Items(map[string]any{"type": "object"}),
		),
		mcp.WithBoolean("force", mcp.Description("Whether to bypass freshness checks")),
	)

	s.server.AddTool(batchEnrichTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]any)
		if !ok {
			return mcp.NewToolResultError("invalid arguments format"), nil
		}

		rawNotifications, ok := args["notifications"]
		if !ok {
			return mcp.NewToolResultError("notifications is required"), nil
		}

		payload, err := json.Marshal(rawNotifications)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to parse notifications: %v", err)), nil
		}

		var notifications []triage.NotificationWithState
		if err := json.Unmarshal(payload, &notifications); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to decode notifications: %v", err)), nil
		}

		force := false
		if f, ok := args["force"].(bool); ok {
			force = f
		}

		// Keep the response keyed by SubjectNodeID to match the TUI batch update contract.
		results := s.engine.Enrich.FetchHybridBatch(ctx, notifications, force)
		data, _ := json.Marshal(results)
		return mcp.NewToolResultText(string(data)), nil
	})

	// 6. Enrichment Persistence Tool
	persistFetchedDetailTool := mcp.NewTool(
		"persist_fetched_detail",
		mcp.WithDescription("Persist freshly fetched detail through the enrichment owner without self-evicting the cache"),
		mcp.WithString("id", mcp.Required(), mcp.Description("The GitHub notification ID")),
		mcp.WithString("source_url", mcp.Required(), mcp.Description("The source URL used for the fetch cache key")),
		mcp.WithString("node_id", mcp.Description("The GitHub subject node ID")),
		mcp.WithString("body", mcp.Description("The enriched body text")),
		mcp.WithString("author", mcp.Description("The enriched author login")),
		mcp.WithString("html_url", mcp.Description("The canonical GitHub HTML URL")),
		mcp.WithString("resource_state", mcp.Description("The enriched resource state")),
		mcp.WithString("resource_sub_state", mcp.Description("The enriched resource sub state")),
	)

	s.server.AddTool(persistFetchedDetailTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]any)
		if !ok {
			return mcp.NewToolResultError("invalid arguments format"), nil
		}

		id, _ := args["id"].(string)
		sourceURL, _ := args["source_url"].(string)
		if id == "" || sourceURL == "" {
			return mcp.NewToolResultError("id and source_url are required"), nil
		}

		nodeID, _ := args["node_id"].(string)
		body, _ := args["body"].(string)
		author, _ := args["author"].(string)
		htmlURL, _ := args["html_url"].(string)
		resourceState, _ := args["resource_state"].(string)
		resourceSubState, _ := args["resource_sub_state"].(string)

		err := s.engine.Backend.PersistFetchedDetail(ctx, id, sourceURL, models.EnrichmentResult{
			SubjectNodeID:    nodeID,
			Body:             body,
			Author:           author,
			HTMLURL:          htmlURL,
			ResourceState:    resourceState,
			ResourceSubState: resourceSubState,
			FetchedAt:        time.Now(),
		})
		return persistFetchedDetailToolResult(err)
	})

	// Temporary explicit exception: there is not yet a matching backend seam
	// operation for independent enrichment persistence.
	enrichNotificationTool := mcp.NewTool(
		"enrich_notification",
		mcp.WithDescription("Persist enriched notification fields through the engine-backed repository"),
		mcp.WithString("id", mcp.Required(), mcp.Description("The GitHub notification ID")),
		mcp.WithString("node_id", mcp.Description("The GitHub subject node ID")),
		mcp.WithString("body", mcp.Description("The enriched body text")),
		mcp.WithString("author", mcp.Description("The enriched author login")),
		mcp.WithString("html_url", mcp.Description("The canonical GitHub HTML URL")),
		mcp.WithString("resource_state", mcp.Description("The enriched resource state")),
		mcp.WithString("resource_sub_state", mcp.Description("The enriched resource sub state")),
	)

	s.server.AddTool(enrichNotificationTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := request.Params.Arguments.(map[string]any)
		if !ok {
			return mcp.NewToolResultError("invalid arguments format"), nil
		}

		id, _ := args["id"].(string)
		if id == "" {
			return mcp.NewToolResultError("id is required"), nil
		}

		nodeID, _ := args["node_id"].(string)
		body, _ := args["body"].(string)
		author, _ := args["author"].(string)
		htmlURL, _ := args["html_url"].(string)
		resourceState, _ := args["resource_state"].(string)
		resourceSubState, _ := args["resource_sub_state"].(string)

		if err := s.engine.Enrich.PersistIndependentDetail(ctx, id, nodeID, body, author, htmlURL, resourceState, resourceSubState); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to persist enrichment: %v", err)), nil
		}

		return mcp.NewToolResultText("Notification enrichment persisted"), nil
	})
}

func (s *MCPServer) registerResources() {
	s.server.AddResource(
		mcp.NewResource(
			"gh-orbit://session/user",
			"Current User",
			mcp.WithResourceDescription("Effective GitHub login for the connected engine session"),
			mcp.WithMIMEType("application/json"),
		),
		func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			user, err := s.engine.Client.CurrentUser(ctx)
			if err != nil {
				return nil, err
			}

			data, err := json.Marshal(map[string]string{"login": user.Login})
			if err != nil {
				return nil, err
			}

			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      "gh-orbit://session/user",
					MIMEType: "application/json",
					Text:     string(data),
				},
			}, nil
		},
	)

	// Notifications Resource Template
	tpl := mcp.NewResourceTemplate(
		"gh-orbit://notifications/{category}", "Notifications List",
		mcp.WithTemplateDescription("List of notifications by category (unread, all)"),
	)

	s.server.AddResourceTemplate(tpl, func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		uri := request.Params.URI

		notifs, err := s.engine.DB.ListNotifications(ctx)
		if err != nil {
			return nil, err
		}

		var items []any
		for _, n := range notifs {
			if strings.Contains(uri, "unread") && n.IsReadLocally {
				continue
			}
			items = append(items, n)
		}

		data, err := json.Marshal(items)
		if err != nil {
			return nil, err
		}

		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      request.Params.URI,
				MIMEType: "application/json",
				Text:     string(data),
			},
		}, nil
	})
}
