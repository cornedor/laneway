package main

/*
#cgo LDFLAGS: -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>
#include <dlfcn.h>
#include <limits.h>
#include <stdbool.h>
#include <stdlib.h>
#include <string.h>

// Security.framework's own calls, looked up as LetsMove does: not in its headers.
typedef Boolean (*isTranslocatedFn)(CFURLRef, bool *, CFErrorRef *);
typedef CFURLRef (*originalPathFn)(CFURLRef, CFErrorRef *);

static char *translocatedFrom(const char *path) {
	void *h = dlopen("/System/Library/Frameworks/Security.framework/Security", RTLD_LAZY);
	if (!h) return NULL;
	isTranslocatedFn isTranslocated = (isTranslocatedFn)dlsym(h, "SecTranslocateIsTranslocatedURL");
	originalPathFn originalPath = (originalPathFn)dlsym(h, "SecTranslocateCreateOriginalPathForURL");
	if (!isTranslocated || !originalPath) return NULL;
	CFURLRef u = CFURLCreateFromFileSystemRepresentation(NULL, (const UInt8 *)path, strlen(path), true);
	if (!u) return NULL;
	char *out = NULL;
	bool yes = false;
	if (isTranslocated(u, &yes, NULL) && yes) {
		CFURLRef o = originalPath(u, NULL);
		if (o) {
			char buf[PATH_MAX];
			if (CFURLGetFileSystemRepresentation(o, true, (UInt8 *)buf, sizeof buf)) out = strdup(buf);
			CFRelease(o);
		}
	}
	CFRelease(u);
	return out;
}
*/
import "C"

import "unsafe"

// translocatedFrom is where the app bundle at path really is when macOS
// runs it from a temporary copy (App Translocation), else "".
func translocatedFrom(path string) string {
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	o := C.translocatedFrom(p)
	if o == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(o))
	return C.GoString(o)
}
