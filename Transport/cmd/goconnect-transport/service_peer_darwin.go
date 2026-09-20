package main

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation -lbsm
#include <Security/Security.h>
#include <CoreFoundation/CoreFoundation.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <bsm/libbsm.h>
#include <unistd.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>

// Use the kernel's audit token, never a PID or identity supplied in JSON.
static int servicePeer(int fd, const char *selfPath, unsigned int *uid, int *pid) {
    audit_token_t token;
    socklen_t len = sizeof(token);
    if (getsockopt(fd, SOL_LOCAL, LOCAL_PEERTOKEN, &token, &len) || len != sizeof(token)) return -1;
    *uid = audit_token_to_euid(token);
    *pid = audit_token_to_pid(token);
    if (!selfPath) return 0;
    CFDataRef audit = CFDataCreate(NULL, (const UInt8 *)&token, sizeof(token));
    const void *keys[] = {kSecGuestAttributeAudit};
    const void *values[] = {audit};
    CFDictionaryRef attrs = CFDictionaryCreate(NULL, keys, values, 1, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    SecCodeRef guest = NULL;
    SecStaticCodeRef own = NULL;
    CFDictionaryRef info = NULL;
    SecRequirementRef requirement = NULL;
    CFURLRef url = CFURLCreateFromFileSystemRepresentation(NULL, (const UInt8 *)selfPath, strlen(selfPath), false);
    OSStatus status = SecCodeCopyGuestWithAttributes(NULL, attrs, kSecCSDefaultFlags, &guest);
    if (!status) status = SecStaticCodeCreateWithPath(url, kSecCSDefaultFlags, &own);
    if (!status) status = SecCodeCopySigningInformation(own, kSecCSSigningInformation, &info);
    if (!status) {
        CFDataRef hash = CFDictionaryGetValue(info, kSecCodeInfoUnique);
        if (!hash || CFGetTypeID(hash) != CFDataGetTypeID() || CFDataGetLength(hash) != 20) status = -1;
        else {
            char hex[41];
            const UInt8 *bytes = CFDataGetBytePtr(hash);
            for (int i = 0; i < 20; i++) snprintf(hex + 2*i, 3, "%02x", bytes[i]);
            CFStringRef rule = CFStringCreateWithFormat(NULL, NULL, CFSTR("cdhash H\"%s\""), hex);
            status = SecRequirementCreateWithString(rule, kSecCSDefaultFlags, &requirement);
            CFRelease(rule);
        }
    }
    if (!status) status = SecCodeCheckValidity(guest, kSecCSStrictValidate, requirement);
    if (requirement) CFRelease(requirement);
    if (info) CFRelease(info);
    if (own) CFRelease(own);
    if (guest) CFRelease(guest);
    CFRelease(url); CFRelease(attrs); CFRelease(audit);
    return status;
}
*/
import "C"

import (
	"errors"
	"net"
	"unsafe"
)

type serviceIdentity struct {
	UID uint32
	PID int
}

func verifyServicePeer(conn *net.UnixConn, self string) (serviceIdentity, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return serviceIdentity{}, err
	}
	var path *C.char
	if self != "" {
		path = C.CString(self)
		defer C.free(unsafe.Pointer(path))
	}
	var uid C.uint
	var pid C.int
	var result C.int
	if err = raw.Control(func(fd uintptr) { result = C.servicePeer(C.int(fd), path, &uid, &pid) }); err != nil {
		return serviceIdentity{}, err
	}
	if result != 0 {
		return serviceIdentity{}, errors.New("client signature does not match installed service")
	}
	return serviceIdentity{uint32(uid), int(pid)}, nil
}
