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
	select {
	case m.queue <- alertRequest{
		title:    title,
		subtitle: subtitle,
		body:     body,
		url:      url,
		priority: priority,
	}:
		// Queued successfully
	default:
		// Queue full, drop oldest or ignore
		m.logger.Warn("notification queue full, dropping alert", "title", title)
	}
	return nil
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
	content := msgSend0(contentCls, "new")
	
	msgSendVoid1(content, "setTitle:", nsString(req.title))
	msgSendVoid1(content, "setSubtitle:", nsString(req.subtitle))
	msgSendVoid1(content, "setBody:", nsString(req.body))
	msgSendVoid1(content, "setThreadIdentifier:", nsString(req.subtitle))

	// Store URL in userInfo for the delegate
	if req.url != "" {
		dictCls := objc_getClass("NSDictionary")
		key := nsString("url")
		val := nsString(req.url)
		userInfo := msgSend2(dictCls, "dictionaryWithObject:forKey:", val, key)
		msgSendVoid1(content, "setUserInfo:", userInfo)
	}

	// 2. Set interruption level
	level := uintptr(1)
	if req.priority >= 2 {
		level = 2
	}
	msgSend_void_id(content, sel_registerName("setInterruptionLevel:"), level)

	// 3. Create request
	reqCls := objc_getClass("UNNotificationRequest")
	notificationReq := msgSend3(reqCls, "requestWithIdentifier:content:trigger:", nsString(""), content, 0)

	// 4. Add to center
	centerCls := objc_getClass("UNUserNotificationCenter")
	center := msgSend0(centerCls, "currentNotificationCenter")
	msgSend2(center, "addNotificationRequest:withCompletionHandler:", notificationReq, 0)

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
		notification := msgSend0(response, "notification")
		req := msgSend0(notification, "request")
		content := msgSend0(req, "content")
		userInfo := msgSend0(content, "userInfo")
		
		urlPtr := msgSend1(userInfo, "objectForKey:", nsString("url"))
		_ = urlPtr 
		
		if completion != 0 {
			purego.SyscallN(completion)
		}
	})

	class_addMethod(cls, sel_registerName("userNotificationCenter:didReceiveNotificationResponse:withCompletionHandler:"), callback, "v@:@@@")
	objc_registerClassPair(cls)
	
	delegateInstance = msgSend0(cls, "new")
	
	centerCls := objc_getClass("UNUserNotificationCenter")
	center := msgSend0(centerCls, "currentNotificationCenter")
	msgSendVoid1(center, "setDelegate:", delegateInstance)
	
	m.logger.Debug("native notification delegate initialized")
}

func (m *macosNotifier) requestAuth() {
	centerCls := objc_getClass("UNUserNotificationCenter")
	center := msgSend0(centerCls, "currentNotificationCenter")
	// Use explicit signature for auth request
	msgSend_void_uint_id(center, sel_registerName("requestAuthorizationWithOptions:completionHandler:"), 7, 0)
}
