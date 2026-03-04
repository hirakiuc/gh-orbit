//go:build darwin

package api

import (
	"log/slog"
	"runtime"
	"sync"

	"github.com/ebitengine/purego"
)

var (
	once sync.Once
	delegateInstance uintptr
)

type macosNotifier struct {
	logger *slog.Logger
}

// NewPlatformNotifier returns the macOS native notifier.
func NewPlatformNotifier(logger *slog.Logger) Notifier {
	return &macosNotifier{logger: logger}
}

func (m *macosNotifier) Notify(title, subtitle, body, url string, priority int) error {
	once.Do(func() {
		if runtime.GOOS != "darwin" {
			return
		}
		// Load UserNotifications framework
		_, _ = purego.Dlopen("/System/Library/Frameworks/UserNotifications.framework/UserNotifications", purego.RTLD_GLOBAL)
		
		// Swizzle Bundle ID for reliability
		m.swizzleBundleID()
		
		// Setup Delegate for click-through URLs
		m.setupDelegate()
		
		// Request authorization
		m.requestAuth()
	})

	m.logger.Debug("sending native macos notification", 
		"title", title, 
		"subtitle", subtitle,
		"priority", priority,
	)

	// 1. Create content
	contentCls := objc_getClass("UNMutableNotificationContent")
	content := msgSend0(contentCls, "new")
	
	msgSend1(content, "setTitle:", nsString(title))
	msgSend1(content, "setSubtitle:", nsString(subtitle))
	msgSend1(content, "setBody:", nsString(body))
	msgSend1(content, "setThreadIdentifier:", nsString(subtitle))

	// Store URL in userInfo for the delegate
	if url != "" {
		dictCls := objc_getClass("NSDictionary")
		key := nsString("url")
		val := nsString(url)
		userInfo := msgSend2(dictCls, "dictionaryWithObject:forKey:", val, key)
		msgSend1(content, "setUserInfo:", userInfo)
	}

	// 2. Set interruption level
	level := uintptr(1)
	if priority >= 2 {
		level = 2
	}
	msgSend1(content, "setInterruptionLevel:", level)

	// 3. Create request
	reqCls := objc_getClass("UNNotificationRequest")
	req := msgSend3(reqCls, "requestWithIdentifier:content:trigger:", nsString(""), content, 0)

	// 4. Add to center
	centerCls := objc_getClass("UNUserNotificationCenter")
	center := msgSend0(centerCls, "currentNotificationCenter")
	msgSend2(center, "addNotificationRequest:withCompletionHandler:", req, 0)

	return nil
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
	
	// Implementation for didReceiveNotificationResponse:withCompletionHandler:
	// types: v@:@@@ (void return, self, sel, center, response, completionHandler)
	callback := purego.NewCallback(func(self, sel, center, response, completion uintptr) {
		notification := msgSend0(response, "notification")
		content := msgSend0(notification, "request")
		content = msgSend0(content, "content")
		userInfo := msgSend0(content, "userInfo")
		
		urlPtr := msgSend1(userInfo, "objectForKey:", nsString("url"))
		_ = urlPtr // Placeholder for future implementation
		
		// Call the completion handler
		if completion != 0 {
			purego.SyscallN(completion)
		}
	})

	class_addMethod(cls, sel_registerName("userNotificationCenter:didReceiveNotificationResponse:withCompletionHandler:"), callback, "v@:@@@")
	objc_registerClassPair(cls)
	
	delegateInstance = msgSend0(cls, "new")
	
	centerCls := objc_getClass("UNUserNotificationCenter")
	center := msgSend0(centerCls, "currentNotificationCenter")
	msgSend1(center, "setDelegate:", delegateInstance)
	
	m.logger.Debug("native notification delegate initialized")
}

func (m *macosNotifier) requestAuth() {
	centerCls := objc_getClass("UNUserNotificationCenter")
	center := msgSend0(centerCls, "currentNotificationCenter")
	msgSend2(center, "requestAuthorizationWithOptions:completionHandler:", 7, 0)
}
