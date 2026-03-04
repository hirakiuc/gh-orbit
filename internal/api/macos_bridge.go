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
	objc_msgSend      uintptr
	
	objc_allocateClassPair func(superclass uintptr, name string, extraBytes int) uintptr
	objc_registerClassPair func(cls uintptr)
	class_addMethod        func(cls uintptr, sel uintptr, imp uintptr, types string) bool
	class_replaceMethod    func(cls uintptr, sel uintptr, imp uintptr, types string) uintptr
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
}

// msgSend0 calls objc_msgSend with no arguments
func msgSend0(obj uintptr, sel string) uintptr {
	r, _, _ := purego.SyscallN(objc_msgSend, obj, sel_registerName(sel))
	return r
}

// msgSend1 calls objc_msgSend with one argument
func msgSend1(obj uintptr, sel string, arg1 uintptr) uintptr {
	r, _, _ := purego.SyscallN(objc_msgSend, obj, sel_registerName(sel), arg1)
	return r
}

// msgSend2 calls objc_msgSend with two arguments
func msgSend2(obj uintptr, sel string, arg1, arg2 uintptr) uintptr {
	r, _, _ := purego.SyscallN(objc_msgSend, obj, sel_registerName(sel), arg1, arg2)
	return r
}

// msgSend3 calls objc_msgSend with three arguments
func msgSend3(obj uintptr, sel string, arg1, arg2, arg3 uintptr) uintptr {
	r, _, _ := purego.SyscallN(objc_msgSend, obj, sel_registerName(sel), arg1, arg2, arg3)
	return r
}

// nsString converts a Go string to an Objective-C NSString
func nsString(s string) uintptr {
	cls := objc_getClass("NSString")
	// #nosec G103 -- Required for purego Objective-C interop
	str := msgSend1(cls, "stringWithUTF8String:", uintptr(unsafe.Pointer(&([]byte(s + "\x00")[0]))))
	return str
}
