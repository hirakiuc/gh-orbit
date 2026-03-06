//go:build darwin

package api

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

type frameworks struct {
	libobjc uintptr
	libUN   uintptr
	err     error
}

var (
	// getFrameworks returns the cached framework handles and initialization status.
	// We use sync.OnceValue to ensure error propagation to all callers.
	getFrameworks = sync.OnceValue(func() frameworks {
		objc, err := purego.Dlopen(pathLibObjC, purego.RTLD_GLOBAL)
		if err != nil {
			return frameworks{err: fmt.Errorf("failed to load %s: %w", pathLibObjC, err)}
		}

		un, err := purego.Dlopen(pathLibUN, purego.RTLD_GLOBAL)
		if err != nil {
			return frameworks{libobjc: objc, err: fmt.Errorf("failed to load %s: %w", pathLibUN, err)}
		}

		// Register FFI functions ONLY after successful library load
		purego.RegisterLibFunc(&objc_getClass, objc, "objc_getClass")
		purego.RegisterLibFunc(&sel_registerName, objc, "sel_registerName")
		purego.RegisterLibFunc(&objc_allocateClassPair, objc, "objc_allocateClassPair")
		purego.RegisterLibFunc(&objc_registerClassPair, objc, "objc_registerClassPair")
		purego.RegisterLibFunc(&class_addMethod, objc, "class_addMethod")
		purego.RegisterLibFunc(&class_replaceMethod, objc, "class_replaceMethod")
		
		objc_msgSend, _ = purego.Dlsym(objc, "objc_msgSend")

		// Register ABI-Safe Helpers (Mapped to the verified libobjc)
		purego.RegisterLibFunc(&msgSend_id_void, objc, "objc_msgSend")
		purego.RegisterLibFunc(&msgSend_id_id, objc, "objc_msgSend")
		purego.RegisterLibFunc(&msgSend_id_id_id, objc, "objc_msgSend")
		purego.RegisterLibFunc(&msgSend_id_id_id_id, objc, "objc_msgSend")
		purego.RegisterLibFunc(&msgSend_void_id, objc, "objc_msgSend")
		purego.RegisterLibFunc(&msgSend_void_uint_id, objc, "objc_msgSend")

		// Cache common selectors
		sel_new = sel_registerName("new")
		sel_setTitle = sel_registerName("setTitle:")
		sel_setSubtitle = sel_registerName("setSubtitle:")
		sel_setBody = sel_registerName("setBody:")
		sel_setThreadIdentifier = sel_registerName("setThreadIdentifier:")
		sel_setUserInfo = sel_registerName("setUserInfo:")
		sel_setInterruptionLevel = sel_registerName("setInterruptionLevel:")
		sel_setDelegate = sel_registerName("setDelegate:")
		sel_currentNotificationCenter = sel_registerName("currentNotificationCenter")
		sel_requestWithIdentifierContentTrigger = sel_registerName("requestWithIdentifier:content:trigger:")
		sel_addNotificationRequest = sel_registerName("addNotificationRequest:withCompletionHandler:")
		sel_requestAuthorization = sel_registerName("requestAuthorizationWithOptions:completionHandler:")
		sel_dictionaryWithObjectForKey = sel_registerName("dictionaryWithObject:forKey:")
		sel_objectForKey = sel_registerName("objectForKey:")
		sel_notification = sel_registerName("notification")
		sel_request = sel_registerName("request")
		sel_content = sel_registerName("content")
		sel_userInfo = sel_registerName("userInfo")
		sel_stringWithUTF8String = sel_registerName("stringWithUTF8String:")

		return frameworks{libobjc: objc, libUN: un, err: nil}
	})

	objc_getClass     func(name string) uintptr
	sel_registerName func(name string) uintptr
	//nolint:unused // Symbol address required for dynamic linking
	objc_msgSend      uintptr
	
	objc_allocateClassPair func(superclass uintptr, name string, extraBytes int) uintptr
	objc_registerClassPair func(cls uintptr)
	class_addMethod        func(cls uintptr, sel uintptr, imp uintptr, types string) bool
	class_replaceMethod    func(cls uintptr, sel uintptr, imp uintptr, types string) uintptr

	// Cached Selectors for performance
	sel_new                                uintptr
	sel_setTitle                           uintptr
	sel_setSubtitle                        uintptr
	sel_setBody                            uintptr
	sel_setThreadIdentifier                uintptr
	sel_setUserInfo                        uintptr
	sel_setInterruptionLevel               uintptr
	sel_setDelegate                        uintptr
	sel_currentNotificationCenter          uintptr
	sel_requestWithIdentifierContentTrigger uintptr
	sel_addNotificationRequest             uintptr
	sel_requestAuthorization               uintptr
	sel_dictionaryWithObjectForKey         uintptr
	//nolint:unused
	sel_objectForKey                       uintptr
	//nolint:unused
	sel_notification                       uintptr
	//nolint:unused
	sel_request                            uintptr
	//nolint:unused
	sel_content                            uintptr
	//nolint:unused
	sel_userInfo                           uintptr
	sel_stringWithUTF8String               uintptr

	// ABI-Safe Explicit Signatures for common Objective-C calls
	msgSend_id_void func(obj uintptr, sel uintptr) uintptr
	msgSend_id_id func(obj uintptr, sel uintptr, arg1 uintptr) uintptr
	msgSend_id_id_id func(obj uintptr, sel uintptr, arg1, arg2 uintptr) uintptr
	msgSend_id_id_id_id func(obj uintptr, sel uintptr, arg1, arg2, arg3 uintptr) uintptr
	msgSend_void_id func(obj uintptr, sel uintptr, arg1 uintptr)
	msgSend_void_uint_id func(obj uintptr, sel uintptr, options uintptr, handler uintptr)
)

const (
	pathLibObjC = "/usr/lib/libobjc.A.dylib"
	pathLibUN   = "/System/Library/Frameworks/UserNotifications.framework/UserNotifications"
)

// BridgeProbe represents the result of a single bridge diagnostic check.
type BridgeProbe struct {
	Name    string
	Passed  bool
	Message string
}

// ProbeBridge deep-probes the Objective-C runtime to ensure compatibility.
func ProbeBridge() []BridgeProbe {
	f := getFrameworks()
	
	var probes []BridgeProbe

	probes = append(probes, BridgeProbe{
		Name:    "Framework: libobjc",
		Passed:  f.libobjc != 0,
		Message: func() string { if f.err != nil && f.libobjc == 0 { return f.err.Error() }; return "" }(),
	})
	probes = append(probes, BridgeProbe{
		Name:    "Framework: UserNotifications",
		Passed:  f.libUN != 0,
		Message: func() string { if f.err != nil && f.libUN == 0 { return f.err.Error() }; return "" }(),
	})

	if f.libobjc == 0 {
		return probes
	}

	classes := []string{
		"NSString", "NSBundle", "NSObject", "NSDictionary",
		"UNMutableNotificationContent", "UNNotificationRequest", "UNUserNotificationCenter",
	}

	for _, name := range classes {
		cls := objc_getClass(name)
		probes = append(probes, BridgeProbe{
			Name:   fmt.Sprintf("Class: %s", name),
			Passed: cls != 0,
		})
	}

	return probes
}

// nsString converts a Go string to an Objective-C NSString safely.
func nsString(s string) uintptr {
	if f := getFrameworks(); f.err != nil && f.libobjc == 0 {
		return 0
	}
	
	cls := objc_getClass("NSString")
	if cls == 0 || sel_stringWithUTF8String == 0 {
		return 0
	}
	// #nosec G103 -- Required for purego Objective-C interop
	return msgSend_id_id(cls, sel_stringWithUTF8String, uintptr(unsafe.Pointer(&([]byte(s + "\x00")[0]))))
}

// safeMsgSend0 verifies pointers before calling [obj sel]
func safeMsgSend0(obj uintptr, sel uintptr) (uintptr, error) {
	if obj == 0 || sel == 0 {
		return 0, errors.New("nil receiver or selector")
	}
	return msgSend_id_void(obj, sel), nil
}

//nolint:unused
// safeMsgSend1 verifies pointers before calling [obj sel:arg1]
func safeMsgSend1(obj uintptr, sel uintptr, arg1 uintptr) (uintptr, error) {
	if obj == 0 || sel == 0 {
		return 0, errors.New("nil receiver or selector")
	}
	return msgSend_id_id(obj, sel, arg1), nil
}

// safeMsgSend2 verifies pointers before calling [obj sel:arg1 :arg2]
func safeMsgSend2(obj uintptr, sel uintptr, arg1, arg2 uintptr) (uintptr, error) {
	if obj == 0 || sel == 0 {
		return 0, errors.New("nil receiver or selector")
	}
	return msgSend_id_id_id(obj, sel, arg1, arg2), nil
}

// safeMsgSendVoid1 verifies pointers before calling [obj sel:arg1] returning void
func safeMsgSendVoid1(obj uintptr, sel uintptr, arg1 uintptr) error {
	if obj == 0 || sel == 0 {
		return errors.New("nil receiver or selector")
	}
	msgSend_void_id(obj, sel, arg1)
	return nil
}
