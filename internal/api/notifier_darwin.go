//go:build darwin

package api

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ebitengine/purego"
	"github.com/hirakiuc/gh-orbit/internal/config"
	"go.opentelemetry.io/otel/attribute"
)

var (
	once sync.Once
	delegateInstance uintptr
)

type alertRequest struct {
	title    string
	subtitle string
	body     string
	url      string
	priority int
}

type macosNotifier struct {
	logger    *slog.Logger
	executor  CommandExecutor
	queue     chan alertRequest
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	status    BridgeStatus
	mu        sync.RWMutex
	ready     chan struct{}
	readyOnce sync.Once
}

// NewPlatformNotifier returns the macOS native notifier with a background worker.
func NewPlatformNotifier(ctx context.Context, executor CommandExecutor, logger *slog.Logger) Notifier {
	n := &macosNotifier{
		logger:   logger,
		executor: executor,
		queue:    make(chan alertRequest, 100),
		status:   StatusUnknown,
		ready:    make(chan struct{}),
	}

	workerCtx, cancel := context.WithCancel(ctx)
	n.cancel = cancel

	n.wg.Add(1)
	go n.worker(workerCtx)

	return n
}

func (m *macosNotifier) Notify(ctx context.Context, title, subtitle, body, url string, priority int) error {
	req := alertRequest{
		title:    title,
		subtitle: subtitle,
		body:     body,
		url:      url,
		priority: priority,
	}

	select {
	case m.queue <- req:
	default:
		select {
		case <-m.queue:
		default:
		}
		select {
		case m.queue <- req:
		default:
		}
	}
	return nil
}

func (m *macosNotifier) Shutdown(ctx context.Context) {
	m.cancel()
	m.wg.Wait()
	m.logger.DebugContext(ctx, "macos native notifier shutdown complete")
}

func (m *macosNotifier) Status() BridgeStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

func (m *macosNotifier) Warmup() {
	m.mu.Lock()
	status := m.status
	m.mu.Unlock()

	if status == StatusUnknown {
		// Send warmup sentinel to trigger lazy initialization in worker
		m.queue <- alertRequest{priority: -1}
	}
}

func (m *macosNotifier) Ready() <-chan struct{} {
	return m.ready
}

func (m *macosNotifier) setStatus(s BridgeStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = s
}

func (m *macosNotifier) checkBundle(ctx context.Context) error {
	if os.Getenv("GH_ORBIT_NOTIFIER_FORCE_APPLE_SCRIPT") == "1" {
		return fmt.Errorf("forced AppleScript")
	}

	bundleCls := objc_getClass("NSBundle")
	if bundleCls == 0 { return fmt.Errorf("NSBundle not found") }
	
	bundle, bErr := safeMsgSend0(bundleCls, sel_mainBundle)
	if bErr != nil || bundle == 0 {
		return fmt.Errorf("could not get main bundle")
	}

	bid, idErr := safeMsgSend0(bundle, sel_bundleIdentifier)
	if idErr != nil || bid == 0 {
		return fmt.Errorf("process has no CFBundleIdentifier (standalone binary)")
	}

	return nil
}

func (m *macosNotifier) worker(ctx context.Context) {
	defer m.wg.Done()

	once.Do(func() {
		if runtime.GOOS != "darwin" {
			m.setStatus(StatusUnsupported)
			return
		}
		
		// 1. Framework Loading (MUST happen before any objc calls)
		if _, err := getFrameworks(); err != nil {
			m.logger.WarnContext(ctx, "failed to load system frameworks", "error", err)
			m.setStatus(StatusBroken)
			return
		}

		// 2. Mandatory Bundle Check
		// If we don't have a bundle, we skip native initialization to avoid panics
		if err := m.checkBundle(ctx); err != nil {
			m.logger.DebugContext(ctx, "native bridge restricted (standalone binary), using AppleScript fallback", "error", err)
			m.setStatus(StatusHealthy) // Still "Healthy" because AppleScript works on Darwin
			return
		}

		m.setupDelegate(ctx)
		m.requestAuth()
		m.setStatus(StatusHealthy)
	})

	// Signal readiness
	m.readyOnce.Do(func() { close(m.ready) })

	for {
		select {
		case <-ctx.Done():
			m.logger.DebugContext(ctx, "macos native notifier worker stopping (context canceled)")
			return
		case req := <-m.queue:
			if req.priority == -1 {
				continue // Warmup sentinel
			}

			m.deliver(ctx, req)
		}
	}
}

func (m *macosNotifier) deliver(ctx context.Context, req alertRequest) {
	// Try Native FIRST ONLY if we actually have a bundle identifier
	// We check checkBundle again or trust the initial status.
	// Actually, let's just try deliverNative and fallback if it fails.
	// BUT deliverNative panics if currentNotificationCenter is nil.
	
	err := m.deliverNative(ctx, req)
	if err != nil {
		m.logger.DebugContext(ctx, "native delivery failed, attempting osascript fallback", "error", err)
		m.deliverWithAppleScript(ctx, req)
	}
}

func (m *macosNotifier) deliverNative(ctx context.Context, req alertRequest) error {
	// Safety: check if we actually have a bundle ID before calling UNUserNotificationCenter
	if err := m.checkBundle(ctx); err != nil {
		return err // Fallback to AppleScript
	}

	tracer := config.GetTracer()
	ctx, span := tracer.Start(ctx, "macos.notify_deliver")
	defer span.End()

	// 1. Create content safely
	contentCls := objc_getClass("UNMutableNotificationContent")
	if contentCls == 0 { return fmt.Errorf("UNMutableNotificationContent class not found") }
	
	content, err := safeMsgSend0(contentCls, sel_new)
	if err != nil { return err }
	
	t, tErr := nsString(req.title)
	s, sErr := nsString(req.subtitle)
	b, bErr := nsString(req.body)

	if tErr != nil || sErr != nil || bErr != nil {
		m.logger.WarnContext(ctx, "failed to convert notification strings to NSString", "title_err", tErr, "body_err", bErr)
	}
	
	_ = safeMsgSendVoid1(content, sel_setTitle, t)
	_ = safeMsgSendVoid1(content, sel_setSubtitle, s)
	_ = safeMsgSendVoid1(content, sel_setBody, b)
	_ = safeMsgSendVoid1(content, sel_setThreadIdentifier, s)

	// Store URL in userInfo for the delegate
	if req.url != "" {
		dictCls := objc_getClass("NSDictionary")
		key, _ := nsString("url")
		val, _ := nsString(req.url)
		userInfo, err := safeMsgSend2(dictCls, sel_dictionaryWithObjectForKey, val, key)
		if err == nil {
			_ = safeMsgSendVoid1(content, sel_setUserInfo, userInfo)
		}
	}

	// 2. Set interruption level
	level := uintptr(1)
	if req.priority >= 2 { level = 2 }
	_ = safeMsgSendVoid1(content, sel_setInterruptionLevel, level)

	// 3. Create request
	reqCls := objc_getClass("UNNotificationRequest")
	if reqCls == 0 { return fmt.Errorf("UNNotificationRequest class not found") }
	
	emptyStr, _ := nsString("")
	notificationReq, err := safeMsgSend2(reqCls, sel_requestWithIdentifierContentTrigger, emptyStr, content)
	if err != nil { return err }

	// 4. Add to center
	centerCls := objc_getClass("UNUserNotificationCenter")
	if centerCls == 0 { return fmt.Errorf("UNUserNotificationCenter class not found") }
	
	center, err := safeMsgSend0(centerCls, sel_currentNotificationCenter)
	if err != nil { return err }
	
	span.SetAttributes(attribute.String("title", req.title))

	_, err = safeMsgSend2(center, sel_addNotificationRequest, notificationReq, 0)
	return err
}

var appleScriptReplacer = strings.NewReplacer(
	"\\", "\\\\",
	"\"", "\\\"",
	"`", "\\`",
	"$", "\\$",
)

func escapeAppleScript(s string) string {
	return appleScriptReplacer.Replace(s)
}

func (m *macosNotifier) deliverWithAppleScript(ctx context.Context, req alertRequest) {
	m.logger.DebugContext(ctx, "delivering notification via osascript fallback")
	
	script := fmt.Sprintf(
		"display notification \"%s\" with title \"%s\" subtitle \"%s\"",
		escapeAppleScript(req.body),
		escapeAppleScript(req.title),
		escapeAppleScript(req.subtitle),
	)

	// Execute asynchronously with worker's lifecycle-managed context
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		
		cmdCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := m.executor.Run(cmdCtx, "osascript", "-e", script); err != nil {
			m.logger.WarnContext(context.Background(), "osascript fallback failed", "error", err)
		}
	}()
}

func (m *macosNotifier) setupDelegate(ctx context.Context) {
	super := objc_getClass("NSObject")
	cls := objc_allocateClassPair(super, "OrbitNotificationDelegate", 0)
	
	callback := purego.NewCallback(func(self, sel, center, response, completion uintptr) {
		if completion != 0 {
			purego.SyscallN(completion)
		}
	})

	class_addMethod(cls, sel_registerName("userNotificationCenter:didReceiveNotificationResponse:withCompletionHandler:"), callback, "v@:@@@")
	objc_registerClassPair(cls)
	
	delegateInstance = msgSend_id_void(cls, sel_new)
	centerCls := objc_getClass("UNUserNotificationCenter")
	center := msgSend_id_void(centerCls, sel_currentNotificationCenter)
	msgSend_void_id(center, sel_setDelegate, delegateInstance)
	
	m.logger.DebugContext(ctx, "native notification delegate initialized")
}

func (m *macosNotifier) requestAuth() {
	centerCls := objc_getClass("UNUserNotificationCenter")
	center := msgSend_id_void(centerCls, sel_currentNotificationCenter)
	msgSend_void_uint_id(center, sel_requestAuthorization, 7, 0)
}

// CheckFocusMode performs a soft-failure probe for active macOS Focus modes.
func CheckFocusMode(executor CommandExecutor) string {
	// NSStatusItem Visible FocusModes is a reliable indicator in modern macOS
	out, err := executor.Execute(context.Background(), "defaults", "read", "com.apple.controlcenter", "NSStatusItem Visible FocusModes")
	if err != nil {
		return "Unknown (Permissions restricted)"
	}
	
	val := strings.TrimSpace(string(out))
	if val == "1" {
		return "Active (Notifications may be suppressed)"
	}
	return "Inactive"
}
