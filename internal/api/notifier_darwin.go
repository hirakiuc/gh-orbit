//go:build darwin

package api

import (
	"context"
	"log/slog"
	"runtime"
	"sync"

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
	logger  *slog.Logger
	queue   chan alertRequest
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewPlatformNotifier returns the macOS native notifier with a background worker.
func NewPlatformNotifier(ctx context.Context, logger *slog.Logger) Notifier {
	n := &macosNotifier{
		logger: logger,
		queue:  make(chan alertRequest, 100), // Buffer for up to 100 alerts
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
		// Queued successfully
	default:
		// Queue full, "Drop Oldest" pattern:
		// 1. Try to pull one out (the oldest)
		select {
		case <-m.queue:
		default:
		}
		// 2. Try to push the new one again
		select {
		case m.queue <- req:
		default:
			m.logger.Warn("notification queue full, dropping alert", "title", title)
		}
	}
	return nil
}

func (m *macosNotifier) Shutdown() {
	m.cancel()
	m.wg.Wait()
}

func (m *macosNotifier) worker(ctx context.Context) {
	defer m.wg.Done()

	// Lazy initialization of the frameworks and swizzling
	once.Do(func() {
		if runtime.GOOS != "darwin" {
			return
		}
		// Load UserNotifications framework
		_, _ = purego.Dlopen("/System/Library/Frameworks/UserNotifications.framework/UserNotifications", purego.RTLD_GLOBAL)
		
		m.swizzleBundleID()
		m.setupDelegate()
		m.requestAuth()
	})

	for {
		select {
		case <-ctx.Done():
			m.logger.Debug("macos notification worker stopping")
			return
		case req := <-m.queue:
			m.deliver(ctx, req)
		}
	}
}

func (m *macosNotifier) deliver(ctx context.Context, req alertRequest) {
	tracer := config.GetTracer()
	_, span := tracer.Start(ctx, "macos.notify_deliver")
	defer span.End()

	m.logger.Debug("delivering native macos notification", 
		"title", req.title, 
		"subtitle", req.subtitle,
	)

	// 1. Create content
	contentCls := objc_getClass("UNMutableNotificationContent")
	content := msgSend_id_void(contentCls, sel_new)
	
	msgSend_void_id(content, sel_setTitle, nsString(req.title))
	msgSend_void_id(content, sel_setSubtitle, nsString(req.subtitle))
	msgSend_void_id(content, sel_setBody, nsString(req.body))
	msgSend_void_id(content, sel_setThreadIdentifier, nsString(req.subtitle))

	// Store URL in userInfo for the delegate
	if req.url != "" {
		dictCls := objc_getClass("NSDictionary")
		key := nsString("url")
		val := nsString(req.url)
		userInfo := msgSend_id_id_id(dictCls, sel_dictionaryWithObjectForKey, val, key)
		msgSend_void_id(content, sel_setUserInfo, userInfo)
	}

	// 2. Set interruption level
	level := uintptr(1)
	if req.priority >= 2 {
		level = 2
	}
	msgSend_void_id(content, sel_setInterruptionLevel, level)

	// 3. Create request
	reqCls := objc_getClass("UNNotificationRequest")
	notificationReq := msgSend_id_id_id_id(reqCls, sel_requestWithIdentifierContentTrigger, nsString(""), content, 0)

	// 4. Add to center
	centerCls := objc_getClass("UNUserNotificationCenter")
	center := msgSend_id_void(centerCls, sel_currentNotificationCenter)
	msgSend_id_id_id(center, sel_addNotificationRequest, notificationReq, 0)

	span.SetAttributes(attribute.String("title", req.title))
}

func (m *macosNotifier) swizzleBundleID() {
	bundleCls := objc_getClass("NSBundle")
	if bundleCls == 0 { return }

	bundleIDCallback := purego.NewCallback(func(self, sel uintptr) uintptr {
		return nsString("com.apple.Terminal")
	})

	class_replaceMethod(
		bundleCls,
		sel_registerName("bundleIdentifier"),
		bundleIDCallback,
		"@@:",
	)
}

func (m *macosNotifier) setupDelegate() {
	// Create a runtime class for the delegate
	super := objc_getClass("NSObject")
	cls := objc_allocateClassPair(super, "OrbitNotificationDelegate", 0)
	
	callback := purego.NewCallback(func(self, sel, center, response, completion uintptr) {
		notification := msgSend_id_void(response, sel_notification)
		req := msgSend_id_void(notification, sel_request)
		content := msgSend_id_void(req, sel_content)
		userInfo := msgSend_id_void(content, sel_userInfo)
		
		urlPtr := msgSend_id_id(userInfo, sel_objectForKey, nsString("url"))
		_ = urlPtr 
		
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
	
	m.logger.Debug("native notification delegate initialized")
}

func (m *macosNotifier) requestAuth() {
	centerCls := objc_getClass("UNUserNotificationCenter")
	center := msgSend_id_void(centerCls, sel_currentNotificationCenter)
	// Use explicit signature for auth request
	msgSend_void_uint_id(center, sel_requestAuthorization, 7, 0)
}
