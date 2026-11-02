package engine

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A speed limit is two token buckets per adapter, one per direction, shared by every flow and both
// address families: the cap is for the adapter as a whole, not per connection or per address. Caps
// are in Mbit/s everywhere (the same unit the tray shows for live speeds).
const (
	// limitChunkMin and limitChunkMax bound how many bytes one wait covers. The chunk is normally
	// rate × 5 ms, so each wait is long enough that the timer's own delay does not cap the speed;
	// at low rates the minimum keeps wake-ups rare, and at very high rates the maximum keeps a
	// single write from being split too many times.
	limitChunkMin  = 16 << 10
	limitChunkMax  = 1 << 20
	limitChunkTime = 5 * time.Millisecond
	// limitBurstMin and limitBurstTime size the bucket: rate × 50 ms (minimum 16 KB) may pass at
	// once after an idle moment, and a fresh bucket starts full so the first request on a quiet
	// link does not stall.
	limitBurstMin  = 16 << 10
	limitBurstTime = 50 * time.Millisecond
	// udpFree lets small datagrams through untouched: DNS, mDNS and calls stay responsive even
	// while a bulk QUIC download is being paced on the same adapter.
	udpFree = 512

	bytesPerMbit = 1_000_000 / 8
)

// limitPair is the installed cap: nil means unlimited. It is swapped as a whole so the data path
// reads it without a lock, and changing a limit never blocks a flow.
type limitPair struct {
	in, out    *bucket
	mbitPerSec float64
	chunk      int
}

type bucket struct {
	mu     sync.Mutex
	rate   float64 // bytes/s
	burst  float64
	tokens float64
	last   time.Time
}

func newBucket(bytesPerSec float64) *bucket {
	burst := math.Max(limitBurstMin, bytesPerSec*limitBurstTime.Seconds())
	return &bucket{rate: bytesPerSec, burst: burst, tokens: burst, last: time.Now()}
}

// take reserves n bytes and sleeps until they may pass. The reservation happens under the lock,
// so concurrent flows queue fairly instead of all waking at once; the sleep itself is outside it.
func (b *bucket) take(n int) {
	b.mu.Lock()
	now := time.Now()
	b.tokens = math.Min(b.burst, b.tokens+now.Sub(b.last).Seconds()*b.rate)
	b.last = now
	var wait time.Duration
	if need := float64(n); need > b.tokens {
		seconds := (need - b.tokens) / b.rate
		b.tokens = 0
		b.last = now.Add(time.Duration(seconds * float64(time.Second)))
		wait = time.Duration(seconds * float64(time.Second))
	} else {
		b.tokens -= need
	}
	b.mu.Unlock()
	if wait > 0 {
		time.Sleep(wait)
	}
}

// LimitMaxMbps is the largest cap accepted, in Mbit/s. It is the same ceiling limits.json accepts.
const LimitMaxMbps = 1e6

// ParseLimit reads a cap written by a person: "10", "7.5", "0", "off" or "unlimited". It returns
// 0 for no cap. Both the tray and `net-merge limit` use it, so both accept the same spellings and
// reject the same nonsense.
func ParseLimit(s string) (float64, error) {
	t := strings.TrimSpace(s)
	switch strings.ToLower(t) {
	case "unlimited", "off", "none":
		return 0, nil
	}
	mbitPerSec, err := strconv.ParseFloat(t, 64)
	if err != nil || math.IsNaN(mbitPerSec) || math.IsInf(mbitPerSec, 0) {
		return 0, fmt.Errorf("enter a speed in Mbit/s, like 50 or 7.5 (0 for unlimited)")
	}
	if mbitPerSec < 0 || mbitPerSec > LimitMaxMbps {
		return 0, fmt.Errorf("the speed must be between 0 and %.0f Mbit/s", float64(LimitMaxMbps))
	}
	return mbitPerSec, nil
}

// limitChunkFor is how many bytes one wait covers at this rate.
func limitChunkFor(bytesPerSec float64) int {
	n := bytesPerSec * limitChunkTime.Seconds()
	switch {
	case n < limitChunkMin:
		return limitChunkMin
	case n > limitChunkMax:
		return limitChunkMax
	default:
		return int(n)
	}
}

// setLimit installs the adapter's cap in Mbit/s, or clears it with 0.
func (sl *Slot) setLimit(mbitPerSec float64) {
	if mbitPerSec <= 0 {
		sl.limit.Store(nil)
		return
	}
	rate := mbitPerSec * bytesPerMbit // Mbit/s to bytes/s
	sl.limit.Store(&limitPair{
		in:         newBucket(rate),
		out:        newBucket(rate),
		mbitPerSec: mbitPerSec,
		chunk:      limitChunkFor(rate),
	})
}

func (sl *Slot) limitMbps() float64 {
	if p := sl.limit.Load(); p != nil {
		return p.mbitPerSec
	}
	return 0
}

// throttle paces n bytes in one direction (down = from the internet) on this adapter.
func (sl *Slot) throttle(down bool, n int) {
	p := sl.limit.Load()
	if p == nil || n <= 0 {
		return
	}
	b := p.out
	if down {
		b = p.in
	}
	for n > 0 {
		k := min(n, p.chunk)
		b.take(k)
		n -= k
	}
}

// throttlePacket is throttle for datagrams: small ones skip the bucket so latency-sensitive
// traffic (DNS, calls) is not stuck behind a bulk download.
func (sl *Slot) throttlePacket(down bool, n int) {
	if n <= udpFree {
		return
	}
	sl.throttle(down, n)
}

// writePaced waits for the adapter's tokens and writes p in chunks while doing so, so a large
// buffer is delivered smoothly instead of in one burst at the end of the wait.
func (sl *Slot) writePaced(dst io.Writer, p []byte, down bool) (int, error) {
	pair := sl.limit.Load()
	if pair == nil {
		return dst.Write(p)
	}
	written := 0
	for len(p) > 0 {
		k := min(len(p), pair.chunk)
		sl.throttle(down, k)
		n, err := dst.Write(p[:k])
		written += n
		if err != nil {
			return written, err
		}
		if n < k {
			return written, io.ErrShortWrite
		}
		p = p[k:]
	}
	return written, nil
}
