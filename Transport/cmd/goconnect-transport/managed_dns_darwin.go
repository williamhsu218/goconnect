package main

/*
#cgo LDFLAGS: -framework SystemConfiguration -framework CoreFoundation
#include <SystemConfiguration/SystemConfiguration.h>
#include <stdlib.h>

typedef struct {
    SCDynamicStoreRef store;
    CFStringRef key;
    CFDictionaryRef value;
} GoConnectDNSLease;

static void releaseDNSLease(GoConnectDNSLease *lease) {
    if (!lease) return;
    // Only remove our own value. Never overwrite a later owner's changes.
    CFPropertyListRef current = SCDynamicStoreCopyValue(lease->store, lease->key);
    if (current) {
        if (CFEqual(current, lease->value)) SCDynamicStoreRemoveValue(lease->store, lease->key);
        CFRelease(current);
    }
    CFRelease(lease->value);
    CFRelease(lease->key);
    CFRelease(lease->store);
    free(lease);
}

static GoConnectDNSLease *acquireDNSLease(int port, const char *scope, int *error) {
    SCDynamicStoreRef store = SCDynamicStoreCreate(NULL, CFSTR("GoConnect DNS session"), NULL, NULL);
    if (!store) { *error = SCError(); return NULL; }
    CFUUIDRef uuid = CFUUIDCreate(NULL);
    CFStringRef id = CFUUIDCreateString(NULL, uuid);
    CFStringRef key = CFStringCreateWithFormat(NULL, NULL, CFSTR("State:/Network/Service/GoConnect-DNS-%@/DNS"), id);
    CFRelease(id); CFRelease(uuid);
    CFStringRef address = CFSTR("127.0.0.1");
    CFStringRef domain = CFStringCreateWithCString(NULL, scope, kCFStringEncodingUTF8);
    CFArrayRef addresses = CFArrayCreate(NULL, (const void **)&address, 1, &kCFTypeArrayCallBacks);
    CFArrayRef domains = CFArrayCreate(NULL, (const void **)&domain, 1, &kCFTypeArrayCallBacks);
    int order = 1000;
    CFNumberRef portValue = CFNumberCreate(NULL, kCFNumberIntType, &port);
    CFNumberRef orderValue = CFNumberCreate(NULL, kCFNumberIntType, &order);
    const void *keys[] = {kSCPropNetDNSServerAddresses, kSCPropNetDNSServerPort, kSCPropNetDNSSupplementalMatchDomains, kSCPropNetDNSSearchOrder};
    const void *values[] = {addresses, portValue, domains, orderValue};
    CFDictionaryRef value = CFDictionaryCreate(NULL, keys, values, 4, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    CFRelease(addresses); CFRelease(domains); CFRelease(domain); CFRelease(portValue); CFRelease(orderValue);
    if (!SCDynamicStoreAddTemporaryValue(store, key, value)) {
        *error = SCError(); CFRelease(value); CFRelease(key); CFRelease(store); return NULL;
    }
    GoConnectDNSLease *lease = calloc(1, sizeof(GoConnectDNSLease));
    lease->store = store; lease->key = key; lease->value = value;
    return lease;
}

static int dnsLeaseIntact(GoConnectDNSLease *lease) {
    CFPropertyListRef current = SCDynamicStoreCopyValue(lease->store, lease->key);
    if (!current) return 0;
    int result = CFEqual(current, lease->value);
    CFRelease(current);
    return result;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

type managedDNSLease interface {
	intact() bool
	close() error
}

type nativeDNSLease struct {
	value *C.GoConnectDNSLease
	port  uint16
	scope string
}

// The service owns a new temporary dynamic-store key, never the user's network
// preferences. configd removes it even if the worker is killed. A domain scope
// is also pre-armed for explicitly approved company domains by the unified controller.
func acquireManagedDNS(port uint16, scope string) (managedDNSLease, error) {
	if port == 0 || (scope != "" && !validDirectDomain(scope)) {
		return nil, fmt.Errorf("invalid managed DNS scope")
	}
	domain := C.CString(scope)
	defer C.free(unsafe.Pointer(domain))
	var code C.int
	value := C.acquireDNSLease(C.int(port), domain, &code)
	if value == nil {
		return nil, fmt.Errorf("无法启用系统 DNS 接管（SystemConfiguration %d）", int(code))
	}
	return &nativeDNSLease{value: value, port: port, scope: scope}, nil
}

func (l *nativeDNSLease) intact() bool { return l.value != nil && C.dnsLeaseIntact(l.value) != 0 }
func (l *nativeDNSLease) close() error {
	if l.value != nil {
		C.releaseDNSLease(l.value)
		l.value = nil
		return waitManagedDNS(l.port, l.scope, false)
	}
	return nil
}
