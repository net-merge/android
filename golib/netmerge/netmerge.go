// Package netmerge is the gomobile-facing API of the Android port. The Kotlin side (VpnService)
// owns the TUN, tracks the available networks and calls in here; everything about picking a
// network for each flow lives in the engine copied from upstream net-merge.
package netmerge

import (
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"sync"
	"time"

	"netmerge-android/internal/engine"
	"netmerge-android/internal/nics"
	"netmerge-android/internal/tun"
)

var (
	mu      sync.Mutex
	sched   = engine.NewScheduler(nil)
	bal     = engine.NewBalancer(sched)
	dev     tun.Tun
	stop    chan struct{}
	indexes = map[string]int{} // network name -> stable index
	last    []nics.Nic         // last list given to the scheduler, to re-apply weights
)

func init() { sched.SetAuto(false) } // even split by default; the app turns auto on if asked

// network is what Kotlin reports for each usable network.
type network struct {
	Name   string  `json:"name"`
	Handle string  `json:"handle"` // Network#getNetworkHandle, as a decimal string (it is 64-bit)
	V4     bool    `json:"v4"`
	V6     bool    `json:"v6"`
	Weight float64 `json:"weight"`
}

// SetNetworks replaces the set of networks flows may use. It may be called at any time, before
// or after Start. The argument is a JSON array of {name, handle, v4, v6, weight}.
func SetNetworks(js string) error {
	var in []network
	if err := json.Unmarshal([]byte(js), &in); err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	list := make([]nics.Nic, 0, len(in))
	for _, n := range in {
		h, err := strconv.ParseUint(n.Handle, 10, 64)
		if err != nil {
			return errors.New("bad network handle " + n.Handle)
		}
		idx, ok := indexes[n.Name]
		if !ok {
			idx = len(indexes) + 1
			indexes[n.Name] = idx
		}
		nics.SetHandle(idx, h)
		w := n.Weight
		if w <= 0 {
			w = 1
		}
		nic := nics.Nic{Name: n.Name, Index: idx, Weight: w}
		// Unspecified source addresses: the Network binding chooses the real source.
		if n.V4 {
			nic.IP = net.IPv4zero
		}
		if n.V6 {
			nic.IP6 = net.IPv6unspecified
		}
		list = append(list, nic)
	}
	sched.Update(list)
	last = list
	// Flows on a network that just went away would otherwise hang until they time out.
	sched.CloseGone()
	return nil
}

// SetAutoWeights picks how new connections are split. true (the default) follows each network's
// measured speed; false splits them evenly, which helps when the networks' upload speeds are
// closer than their download speeds.
func SetAutoWeights(on bool) {
	mu.Lock()
	defer mu.Unlock()
	sched.SetAuto(on)
	sched.Update(last) // with auto off this restores the even static weights
}

// Start begins serving the VPN's TUN. fd must be a detached descriptor (ParcelFileDescriptor
// #detachFd); the engine owns and closes it.
func Start(fd int, mtu int) error {
	mu.Lock()
	defer mu.Unlock()
	if stop != nil {
		close(stop)
		stop = nil
	}
	if err := dev.Start(bal, fd, mtu); err != nil {
		return err
	}
	stop = make(chan struct{})
	go sched.RunProbes(stop)
	go tick(stop)
	return nil
}

func tick(stop <-chan struct{}) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	last := time.Now()
	for {
		select {
		case <-stop:
			return
		case now := <-t.C:
			sched.Tick(now.Sub(last))
			last = now
		}
	}
}

// Stop tears the stack down and closes the TUN descriptor.
func Stop() {
	mu.Lock()
	defer mu.Unlock()
	if stop != nil {
		close(stop)
		stop = nil
	}
	dev.Stop()
}

// IsRunning reports whether the engine is serving a TUN.
func IsRunning() bool {
	mu.Lock()
	defer mu.Unlock()
	return stop != nil
}

// SetIPv6Mode sets how IPv6 flows are handled: "auto", "on" or "off" (see upstream).
func SetIPv6Mode(mode string) error {
	m, ok := engine.ParseV6Mode(mode)
	if !ok {
		return errors.New("IPv6 mode must be auto, on or off")
	}
	bal.SetV6Mode(m)
	return nil
}

type stat struct {
	Name     string  `json:"name"`
	State    string  `json:"state"`
	State6   string  `json:"state6"`
	Active   int64   `json:"active"`
	Total    int64   `json:"total"`
	Failed   int64   `json:"failed"`
	BytesIn  int64   `json:"bytesIn"`
	BytesOut int64   `json:"bytesOut"`
	RateIn   float64 `json:"rateIn"`
	RateOut  float64 `json:"rateOut"`
	Weight   float64 `json:"weight"`
	Present  bool    `json:"present"`
}

// Stats returns a JSON array with one entry per network: probe state, flows and throughput.
func Stats() string {
	snap := sched.Snapshot()
	out := make([]stat, 0, len(snap))
	for _, s := range snap {
		out = append(out, stat{
			Name: s.Name, State: s.State, State6: s.State6,
			Active: s.Active, Total: s.Total, Failed: s.Failed,
			BytesIn: s.BytesIn, BytesOut: s.BytesOut, RateIn: s.RateIn, RateOut: s.RateOut,
			Weight: s.Weight, Present: s.Present,
		})
	}
	b, _ := json.Marshal(out)
	return string(b)
}
