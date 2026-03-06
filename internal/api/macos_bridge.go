//go:build darwin

package api

import (
	"unsafe"

	"github.com/ebitengine/purego"
)

var (
	libobjc uintptr
	
	objc_getClass     func(name string) uintptr
	sel_registerName func(name string) uintptr
	//nolint:unused // Symbol address required for dynamic linking
	objc_msgSend      uintptr
	
	objc_allocateClassPair func(superclass uintptr, name string, extraBytes int) uintptr
	objc_registerClassPair func(cls uintptr)
	class_addMethod        func(cls uintptr, sel uintptr, imp uintptr, types string) bool
	class_replaceMethod    func(cls uintptr, sel uintptr, imp uintptr, types string) uintptr

	// ABI-Safe Explicit Signatures for common Objective-C calls
	// We register these with explicit types to ensure ARM64 register stability.
	
	// id objc_msgSend(id self, SEL op)
	msgSend_id_void func(obj uintptr, sel uintptr) uintptr
	
	// id objc_msgSend(id self, SEL op, id arg1)
	msgSend_id_id func(obj uintptr, sel uintptr, arg1 uintptr) uintptr
	
	// id objc_msgSend(id self, SEL op, id arg1, id arg2)
	msgSend_id_id_id func(obj uintptr, sel uintptr, arg1, arg2 uintptr) uintptr

	// id objc_msgSend(id self, SEL op, id arg1, id arg2, id arg3)
	msgSend_id_id_id_id func(obj uintptr, sel uintptr, arg1, arg2, arg3 uintptr) uintptr

	// void objc_msgSend(id self, SEL op, id arg1)
	msgSend_void_id func(obj uintptr, sel uintptr, arg1 uintptr)

	// void objc_msgSend(id self, SEL op, NSUInteger options, void(^handler)(BOOL, NSError *))
	msgSend_void_uint_id func(obj uintptr, sel uintptr, options uintptr, handler uintptr)
)

func init() {
	var err error
	libobjc, err = purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_GLOBAL)
	if err != nil {
		return
	}

	purego.RegisterLibFunc(&objc_getClass, libobjc, "objc_getClass")
	purego.RegisterLibFunc(&sel_registerName, libobjc, "sel_registerName")
	
	purego.RegisterLibFunc(&objc_allocateClassPair, libobjc, "objc_allocateClassPair")
	purego.RegisterLibFunc(&objc_registerClassPair, libobjc, "objc_registerClassPair")
	purego.RegisterLibFunc(&class_addMethod, libobjc, "class_addMethod")
	purego.RegisterLibFunc(&class_replaceMethod, libobjc, "class_replaceMethod")
	
	objc_msgSend, err = purego.Dlsym(libobjc, "objc_msgSend")
	if err != nil {
		return
	}

	// Register ABI-Safe Helpers (All mapped to objc_msgSend but with Go-defined signatures)
	purego.RegisterLibFunc(&msgSend_id_void, libobjc, "objc_msgSend")
	purego.RegisterLibFunc(&msgSend_id_id, libobjc, "objc_msgSend")
	purego.RegisterLibFunc(&msgSend_id_id_id, libobjc, "objc_msgSend")
	purego.RegisterLibFunc(&msgSend_id_id_id_id, libobjc, "objc_msgSend")
	purego.RegisterLibFunc(&msgSend_void_id, libobjc, "objc_msgSend")
	purego.RegisterLibFunc(&msgSend_void_uint_id, libobjc, "objc_msgSend")
}

// msgSend0 calls [obj sel] returning id
func msgSend0(obj uintptr, sel string) uintptr {
	return msgSend_id_void(obj, sel_registerName(sel))
}

// msgSend1 calls [obj sel:arg1] returning id
func msgSend1(obj uintptr, sel string, arg1 uintptr) uintptr {
	return msgSend_id_id(obj, sel_registerName(sel), arg1)
}

// msgSend2 calls [obj sel:arg1 :arg2] returning id
func msgSend2(obj uintptr, sel string, arg1, arg2 uintptr) uintptr {
	return msgSend_id_id_id(obj, sel_registerName(sel), arg1, arg2)
}

// msgSend3 calls [obj sel:arg1 :arg2 :arg3] returning id
func msgSend3(obj uintptr, sel string, arg1, arg2, arg3 uintptr) uintptr {
	return msgSend_id_id_id_id(obj, sel_registerName(sel), arg1, arg2, arg3)
}

// msgSendVoid1 calls [obj sel:arg1] returning void
func msgSendVoid1(obj uintptr, sel string, arg1 uintptr) {
	msgSend_void_id(obj, sel_registerName(sel), arg1)
}

// nsString converts a Go string to an Objective-C NSString
func nsString(s string) uintptr {
	cls := objc_getClass("NSString")
	// #nosec G103 -- Required for purego Objective-C interop
	str := msgSend1(cls, "stringWithUTF8String:", uintptr(unsafe.Pointer(&([]byte(s + "\x00")[0]))))
	return str
}
