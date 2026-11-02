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
	lastJS  string             // last SetNetworks input, re-applied when the mode changes
	auto    = true
)

func init() { sched.SetAuto(true) }

// minSeedWeight is the smallest starting weight a network gets from the system's speed estimate:
// at least one new connection in three, whatever the estimate says. Android reports link speeds
// (a Wi-Fi 6 link can claim 1 Gbit/s while the router has 50), so the estimate is only a hint.
const minSeedWeight = engine.MinWeight

// network is what Kotlin reports for each usable network.
type network struct {
	Name   string  `json:"name"`
	Handle string  `json:"handle"` // Network#getNetworkHandle, as a decimal string (it is 64-bit)
	V4     bool    `json:"v4"`
	V6     bool    `json:"v6"`
	Weight float64 `json:"weight"`
	// The system's bandwidth estimate (NetworkCapabilities link bandwidth), kbit/s; 0 = unknown.
	DownKbps float64 `json:"downKbps"`
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
	var maxDown float64
	for _, n := range in {
		maxDown = max(maxDown, n.DownKbps)
	}
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
			// With speed-based balancing, start from the system's estimate (relative to the fastest
			// network). The floor keeps a network that looks slow in the game: with a lower one it
			// gets no flows, so it is never measured and never leaves its seed weight.
			if auto && maxDown > 0 && n.DownKbps > 0 {
				w = max(minSeedWeight, n.DownKbps/maxDown)
			}
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
	last, lastJS = list, js
	// Flows on a network that just went away would otherwise hang until they time out.
	sched.CloseGone()
	return nil
}

// SetAutoWeights picks how new connections are split. true (the default) follows each network's
// measured speed; false splits them evenly, which helps when the networks' upload speeds are
// closer than their download speeds.
func SetAutoWeights(on bool) {
	mu.Lock()
	auto = on
	sched.SetAuto(on)
	js, list := lastJS, last
	mu.Unlock()
	if js != "" {
		SetNetworks(js) // recompute the configured weights for the new mode
	} else {
		sched.Update(list)
	}
}

// OwnerResolver tells which app owns a TCP connection. The Kotlin side implements it with
// ConnectivityManager.getConnectionOwnerUid. A negative answer means unknown.
type OwnerResolver interface {
	OwnerUid(proto int, srcIP string, srcPort int, dstIP string, dstPort int) int
}

// SetOwnerResolver lets the engine spread one app's connections over the networks, instead of
// placing each by itself. Call it before Start; nil turns it off.
func SetOwnerResolver(r OwnerResolver) {
	mu.Lock()
	defer mu.Unlock()
	if r == nil {
		tun.OwnerFunc = nil
		return
	}
	tun.OwnerFunc = func(proto int, srcIP string, srcPort int, dstIP string, dstPort int) int {
		return r.OwnerUid(proto, srcIP, srcPort, dstIP, dstPort)
	}
}

// StartMonitor runs the internet checks and the speed sampling. They keep running while merging is
// paused, because that is how a paused engine sees a second network come back.
func StartMonitor() {
	mu.Lock()
	defer mu.Unlock()
	startMonitor()
}

func startMonitor() {
	if stop == nil {
		stop = make(chan struct{})
		go sched.RunProbes(stop)
		go tick(stop)
	}
}

// Start begins serving the VPN's TUN. fd must be a detached descriptor (ParcelFileDescriptor
// #detachFd); the engine owns and closes it.
func Start(fd int, mtu int) error {
	mu.Lock()
	defer mu.Unlock()
	startMonitor()
	return dev.Start(bal, fd, mtu)
}

// Pause closes the TUN but keeps the monitor running. Start resumes.
func Pause() {
	mu.Lock()
	defer mu.Unlock()
	dev.Stop()
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
	// The speed limit in Mbit/s, 0 = none.
	LimitMbps float64 `json:"limitMbps"`
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
			Weight: s.Weight, Present: s.Present, LimitMbps: s.LimitMbps,
		})
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// SetLimit caps a network in Mbit/s across all of its flows, both directions; 0 removes the cap.
// The cap stays with the network's name, so it survives the network going away and coming back.
func SetLimit(name string, mbitPerSec float64) { sched.SetLimit(name, mbitPerSec) }

// SetLimitsJSON replaces every cap, from what LimitsJSON returned earlier: {"Wi-Fi":10}.
func SetLimitsJSON(js string) error {
	var m map[string]float64
	if err := json.Unmarshal([]byte(js), &m); err != nil {
		return err
	}
	sched.SetLimits(m)
	return nil
}

// LimitsJSON returns the caps, to be saved.
func LimitsJSON() string {
	b, _ := json.Marshal(sched.Limits())
	return string(b)
}

// ParseLimit reads a cap typed by a person: "10", "7.5", "0", "off" or "unlimited".
func ParseLimit(s string) (float64, error) { return engine.ParseLimit(s) }

// Pausing by itself with fewer than two usable networks; the rules are in autopause.go.
var ap autoPauser

func usableAndMoving() (usable int, moving int64) {
	var rate float64
	for _, s := range sched.Snapshot() {
		if !s.Present {
			continue
		}
		if s.State == "up" || s.State6 == "up" {
			usable++
		}
		moving += s.Active
		rate += s.RateIn + s.RateOut
	}
	if rate < quietRate {
		moving = 0 // idle keep-alive connections are always open, so they don't hold back a pause
	}
	return usable, moving
}

// AutoSetEnabled is the setting; on says whether merging is on right now.
func AutoSetEnabled(enabled, on bool) {
	mu.Lock()
	defer mu.Unlock()
	ap.setEnabled(enabled, on)
}

// AutoStart begins without a tap (at boot): merging starts once two networks work, or at once if
// pausing by itself is off.
func AutoStart() {
	mu.Lock()
	defer mu.Unlock()
	ap.start(true)
}

// AutoUserSet records that the user switched merging on or off, which pausing by itself respects.
func AutoUserSet(on bool) {
	n, _ := usableAndMoving()
	mu.Lock()
	defer mu.Unlock()
	ap.userSet(on, n)
}

// AutoTick runs once a second: 0 means do nothing, 1 pause, 2 resume.
func AutoTick(on bool) int {
	n, moving := usableAndMoving()
	mu.Lock()
	defer mu.Unlock()
	return int(ap.tick(time.Now(), on, n, moving))
}

// AutoWaiting says whether a paused engine is waiting for a second network by itself.
func AutoWaiting() bool {
	mu.Lock()
	defer mu.Unlock()
	return ap.enabled && ap.waiting && !ap.manualOff
}
