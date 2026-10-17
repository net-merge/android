// Package nics is the Android replacement for upstream net-merge's adapter layer. On Android the
// app does not scan interfaces itself: the Kotlin side tracks Wi-Fi and cellular through
// ConnectivityManager and hands each Network to the engine. Index is a small id assigned here and
// mapped to the Network's handle, which is what sockets are pinned with.
package nics

import (
	"net"
	"sync"
)

// Nic is one network (Wi-Fi, cellular) with the source addresses flows are pinned to. On Android
// the addresses are usually the unspecified ones (0.0.0.0 / ::): binding the socket to the
// Network picks the right source, and it also works on IPv6-only cellular with 464XLAT, where
// the IPv4 address lives on a stacked interface.
type Nic struct {
	Name   string
	Index  int // id registered with SetHandle; 0 = don't pin (tests)
	IP     net.IP
	IP6    net.IP
	Weight float64
}

var handles sync.Map // int -> uint64 (android.net.Network#getNetworkHandle)

// SetHandle maps an adapter index to an Android network handle.
func SetHandle(index int, handle uint64) { handles.Store(index, handle) }

// DropHandle forgets an index once its network is gone.
func DropHandle(index int) { handles.Delete(index) }

func handle(index int) (uint64, bool) {
	v, ok := handles.Load(index)
	if !ok {
		return 0, false
	}
	return v.(uint64), true
}

// Octets would report the adapter's total traffic. Android gives apps no per-network counters
// (and /sys/class/net is blocked by SELinux), so link rates stay at zero; the scheduler then
// relies only on the traffic it carries itself.
func Octets(int) (in, out uint64, ok bool) { return 0, 0, false }
