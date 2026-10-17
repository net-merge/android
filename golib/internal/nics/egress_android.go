//go:build android

package nics

/*
#cgo LDFLAGS: -landroid
#include <android/multinetwork.h>
#include <errno.h>

static int pin(uint64_t h, int fd) {
	if (android_setsocknetwork((net_handle_t)h, fd) != 0) return errno;
	return 0;
}
*/
import "C"

import "syscall"

// ForceEgress pins a socket to the Android Network registered for idx (android_setsocknetwork,
// the NDK form of Network#bindSocket). The app is excluded from its own VPN, so pinned sockets
// go straight out of Wi-Fi or cellular.
func ForceEgress(idx int, _ bool) func(network, address string, c syscall.RawConn) error {
	h, ok := handle(idx)
	if idx == 0 || !ok {
		return nil
	}
	return func(_, _ string, c syscall.RawConn) error {
		var errno C.int
		if err := c.Control(func(fd uintptr) { errno = C.pin(C.uint64_t(h), C.int(fd)) }); err != nil {
			return err
		}
		if errno != 0 {
			return syscall.Errno(errno)
		}
		return nil
	}
}
