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

import (
	"strings"
	"syscall"
)

// sendBuf is the send buffer asked for on outgoing TCP sockets. While a VPN is the default
// network, Android applies the VPN's small TCP buffer limits (about 110 KB) to every socket,
// which caps each upload at roughly 110 KB per round trip: under 8 Mbps on a 120 ms Starlink
// link. An explicit SO_SNDBUF is limited by net.core.wmem_max instead. The receive side is left
// to autotuning: downloads measured fine, and a fixed SO_RCVBUF could end up smaller.
const sendBuf = 4 << 20

// ForceEgress pins a socket to the Android Network registered for idx (android_setsocknetwork,
// the NDK form of Network#bindSocket). The app is excluded from its own VPN, so pinned sockets
// go straight out of Wi-Fi or cellular.
func ForceEgress(idx int, _ bool) func(network, address string, c syscall.RawConn) error {
	h, ok := handle(idx)
	if idx == 0 || !ok {
		return nil
	}
	return func(network, _ string, c syscall.RawConn) error {
		var errno C.int
		if err := c.Control(func(fd uintptr) {
			errno = C.pin(C.uint64_t(h), C.int(fd))
			if errno == 0 && strings.HasPrefix(network, "tcp") {
				// Best effort: a failure here only leaves the system default.
				_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_SNDBUF, sendBuf)
			}
		}); err != nil {
			return err
		}
		if errno != 0 {
			return syscall.Errno(errno)
		}
		return nil
	}
}
