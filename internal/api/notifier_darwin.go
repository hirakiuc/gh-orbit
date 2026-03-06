//go:build darwin

package api

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ebitengine/purego"
	"github.com/hirakiuc/gh-orbit/internal/config"
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
	logger  *slog.Logger
	queue   chan alertRequest
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	status  BridgeStatus
	mu      sync.RWMutex
}

// NewPlatformNotifier returns the macOS native notifier with a background worker.
func NewPlatformNotifier(ctx context.Context, logger *slog.Logger) Notifier {
	n := &macosNotifier{
		logger: logger,
		queue:  make(chan alertRequest, 100),
		status: StatusUnknown,
	}

	workerCtx, cancel := context.WithCancel(ctx)
	n.cancel = cancel

	n.wg.Add(1)
	go n.worker(workerCtx)

	return n
}

func (m *macosNotifier) Notify(title, subtitle, body, url string, priority int) error {
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

func (m *macosNotifier) Shutdown() {
	m.cancel()
	m.wg.Wait()
}

func (m *macosNotifier) Status() BridgeStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

func (m *macosNotifier) setStatus(s BridgeStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = s
}

func (m *macosNotifier) worker(ctx context.Context) {
	defer m.wg.Done()

	once.Do(func() {
		if runtime.GOOS != "darwin" {
			m.setStatus(StatusUnsupported)
			return
		}
		
		_, err := purego.Dlopen("/System/Library/Frameworks/UserNotifications.framework/UserNotifications", purego.RTLD_GLOBAL)
		if err != nil {
			m.logger.Warn("failed to load UserNotifications framework, using osascript fallback", "error", err)
			m.setStatus(StatusBroken)
			return
		}
		
		// Probe bridge
		probes := ProbeBridge()
		allPassed := true
		for _, p := range probes {
			if !p.Passed {
				allPassed = false
				break
			}
		}

		if !allPassed {
			m.setStatus(StatusBroken)
			return
		}

		m.swizzleBundleID()
		m.setupDelegate()
		m.requestAuth()
		m.setStatus(StatusHealthy)
	})

	for {
		select {
		case <-ctx.Done():
			return
		case req := <-m.queue:
			if m.Status() == StatusHealthy {
				err := m.deliver(ctx, req)
				if err != nil {
					m.logger.Warn("native delivery failed, attempting osascript fallback", "error", err)
					m.deliverWithAppleScript(ctx, req)
				}
			} else {
				m.deliverWithAppleScript(ctx, req)
			}
		}
	}
}

func (m *macosNotifier) deliver(ctx context.Context, req alertRequest) error {
	tracer := config.GetTracer()
	_, span := tracer.Start(ctx, "macos.notify_deliver")
	defer span.End()

	// 1. Create content safely
	contentCls := objc_getClass("UNMutableNotificationContent")
	content, err := safeMsgSend0(contentCls, sel_new)
	if err != nil { return err }
	
	_ = safeMsgSendVoid1(content, sel_setTitle, nsString(req.title))
	_ = safeMsgSendVoid1(content, sel_setSubtitle, nsString(req.subtitle))
	_ = safeMsgSendVoid1(content, sel_setBody, nsString(req.body))
	_ = safeMsgSendVoid1(content, sel_setThreadIdentifier, nsString(req.subtitle))

	// Store URL in userInfo for the delegate
	if req.url != "" {
		dictCls := objc_getClass("NSDictionary")
		key := nsString("url")
		val := nsString(req.url)
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
	notificationReq, err := safeMsgSend2(reqCls, sel_requestWithIdentifierContentTrigger, nsString(""), content)
	if err != nil { return err }

	// 4. Add to center
	centerCls := objc_getClass("UNUserNotificationCenter")
	center, err := safeMsgSend0(centerCls, sel_currentNotificationCenter)
	if err != nil { return err }
	
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
	m.logger.Debug("delivering notification via osascript fallback")
	
	script := fmt.Sprintf(
		"display notification \"%s\" with title \"%s\" subtitle \"%s\"",
		escapeAppleScript(req.body),
		escapeAppleScript(req.title),
		escapeAppleScript(req.subtitle),
	)

	// Execute asynchronously with Go 1.26 WaitDelay and process group isolation
	go func() {
		cmdCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// #nosec G204 -- script is sanitized via escapeAppleScript
		cmd := exec.CommandContext(cmdCtx, "osascript", "-e", script)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		
		if err := cmd.Run(); err != nil {
			m.logger.Warn("osascript fallback failed", "error", err)
		}
	}()
}

func (m *macosNotifier) swizzleBundleID() {
	bundleCls := objc_getClass("NSBundle")
	if bundleCls == 0 { return }

	bundleIDCallback := purego.NewCallback(func(self, sel uintptr) uintptr {
		return nsString("com.apple.Terminal")
	})

	class_replaceMethod(bundleCls, sel_registerName("bundleIdentifier"), bundleIDCallback, "@@:")
}

func (m *macosNotifier) setupDelegate() {
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
}

func (m *macosNotifier) requestAuth() {
	centerCls := objc_getClass("UNUserNotificationCenter")
	center := msgSend_id_void(centerCls, sel_currentNotificationCenter)
	msgSend_void_uint_id(center, sel_requestAuthorization, 7, 0)
}
