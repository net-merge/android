package engine

import (
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"netmerge-android/internal/nics"
)

func TestParseLimit(t *testing.T) {
	for _, c := range []struct {
		in   string
		want float64
		ok   bool
	}{
		{"10", 10, true},
		{" 7.5 ", 7.5, true},
		{"0", 0, true},
		{"unlimited", 0, true},
		{"Off", 0, true},
		{"none", 0, true},
		{"", 0, false},
		{"fast", 0, false},
		{"-1", 0, false},
		{"NaN", 0, false},
		{"+Inf", 0, false},
		{"1e7", 0, false}, // over LimitMaxMbps
	} {
		got, err := ParseLimit(c.in)
		if c.ok && err != nil {
			t.Errorf("ParseLimit(%q): %v", c.in, err)
		}
		if !c.ok && err == nil {
			t.Errorf("ParseLimit(%q) = %v, want an error", c.in, got)
		}
		if c.ok && got != c.want {
			t.Errorf("ParseLimit(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestBucketPacesAfterTheBurst(t *testing.T) {
	b := newBucket(100_000) // 100 KB/s, below the minimum burst
	start := time.Now()
	b.take(limitBurstMin) // a quiet bucket starts full, so the first chunk is free
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Fatalf("the initial burst waited %s", d)
	}
	start = time.Now()
	b.take(limitChunkMin)
	if d := time.Since(start); d < 100*time.Millisecond {
		t.Fatalf("16 KB at 100 KB/s took %s, want about 164 ms", d)
	}
}

func TestThrottleWithoutLimitIsFree(t *testing.T) {
	sl := &Slot{}
	start := time.Now()
	for i := 0; i < 100; i++ {
		sl.throttle(true, 64<<10)
	}
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Fatalf("throttling an unlimited adapter took %s", d)
	}
}

func TestThrottleKeepsDirectionsSeparate(t *testing.T) {
	sl := &Slot{}
	sl.setLimit(4) // 4 Mbit/s = 500 KB/s
	sl.throttle(false, 64<<10)
	start := time.Now()
	sl.throttle(true, limitChunkMin) // the download bucket is untouched
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Fatalf("download waited %s behind upload traffic", d)
	}
}

func TestSmallDatagramsAreNotPaced(t *testing.T) {
	sl := &Slot{}
	sl.setLimit(4)
	sl.throttle(false, 64<<10)
	start := time.Now()
	sl.throttlePacket(false, 40) // a DNS query
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Fatalf("a 40-byte datagram waited %s behind a bulk flow", d)
	}
}

func TestPumpHonoursLimit(t *testing.T) {
	sl := &Slot{}
	sl.setLimit(0.8) // 0.8 Mbit/s = 100 KB/s
	srcR, srcW := net.Pipe()
	dstR, dstW := net.Pipe()
	defer srcR.Close()
	defer srcW.Close()
	defer dstR.Close()
	defer dstW.Close()

	var counted atomic.Int64
	go pump(dstW, srcR, &counted, sl, false, &flowActivity{})
	go func() {
		srcW.Write(make([]byte, 32<<10))
		srcW.Close()
	}()

	start := time.Now()
	got := make([]byte, 32<<10)
	if _, err := io.ReadFull(dstR, got); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < 100*time.Millisecond {
		t.Fatalf("32 KB at 100 KB/s took %s, want at least ~164 ms", d)
	}
	if n := counted.Load(); n != int64(len(got)) {
		t.Fatalf("counted %d bytes, want %d", n, len(got))
	}
}

func TestLimitUnits(t *testing.T) {
	sl := &Slot{}
	sl.setLimit(400) // 400 Mbit/s = 50 MB/s
	p := sl.limit.Load()
	if p == nil {
		t.Fatal("no cap installed")
	}
	const wantRate = 400 * bytesPerMbit
	if p.in.rate != wantRate || p.out.rate != wantRate {
		t.Fatalf("rate = %v/%v B/s, want %v", p.in.rate, p.out.rate, float64(wantRate))
	}
	if p.chunk != 250_000 {
		t.Fatalf("chunk = %d, want 250000 (rate x 5 ms)", p.chunk)
	}
	if p.in.burst != 2_500_000 {
		t.Fatalf("burst = %v, want 2500000 (rate x 50 ms)", p.in.burst)
	}
	if got := sl.limitMbps(); got != 400 {
		t.Fatalf("limitMbps = %v, want 400", got)
	}

	sl.setLimit(1) // 125 KB/s: the minimum chunk and burst apply
	p = sl.limit.Load()
	if p.chunk != limitChunkMin || p.in.burst != limitBurstMin {
		t.Fatalf("chunk/burst = %d/%v at 1 Mbit/s, want %d/%d", p.chunk, p.in.burst, limitChunkMin, limitBurstMin)
	}

	sl.setLimit(0)
	if sl.limit.Load() != nil || sl.limitMbps() != 0 {
		t.Fatal("0 did not clear the cap")
	}
}

func TestHighCapIsReached(t *testing.T) {
	sl := &Slot{}
	sl.setLimit(400) // 50 MB/s
	const size = 8 << 20
	start := time.Now()
	if _, err := sl.writePaced(io.Discard, make([]byte, size), false); err != nil {
		t.Fatal(err)
	}
	d := time.Since(start)
	// 8 MB at 50 MB/s is 160 ms; the first 2.5 MB (50 ms worth) passes as burst, so ~110 ms is right.
	// 16 KB chunks would spend most of their time in timer overshoot and land far below the cap.
	if d < 70*time.Millisecond {
		t.Fatalf("8 MB at 400 Mbit/s took only %s: the cap was not applied", d)
	}
	if d > 400*time.Millisecond {
		t.Fatalf("8 MB at 400 Mbit/s took %s, want about 110-200 ms", d)
	}
}

func TestSlotConnPacesWrites(t *testing.T) {
	sl := &Slot{}
	sl.setLimit(80) // 10 MB/s
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		io.Copy(io.Discard, c)
		c.Close()
	}()

	raw, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	c := &slotConn{TCPConn: raw.(*net.TCPConn), sl: sl, fam: Fam4, s: NewScheduler(nil)}
	defer c.Close()

	const size = 2 << 20 // 2 MB at 10 MB/s is 200 ms, minus the 500 KB burst
	start := time.Now()
	if _, err := c.Write(make([]byte, size)); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < 60*time.Millisecond || d > 2*time.Second {
		t.Fatalf("2 MB through a limited slotConn took %s, want about 150-300 ms", d)
	}
}

func TestSlotConnPacesReads(t *testing.T) {
	sl := &Slot{}
	sl.setLimit(80) // 10 MB/s
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		c.Write(make([]byte, 2<<20))
		c.Close()
	}()

	raw, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.(*net.TCPConn).SetReadBuffer(32 << 10); err != nil {
		t.Fatal(err)
	}
	c := &slotConn{TCPConn: raw.(*net.TCPConn), sl: sl, fam: Fam4, s: NewScheduler(nil)}
	defer c.Close()

	const size = 2 << 20
	buf := make([]byte, 64<<10)
	start := time.Now()
	read := 0
	for read < size {
		n, err := c.Read(buf)
		read += n
		if err != nil {
			t.Fatalf("after %d bytes: %v", read, err)
		}
	}
	if d := time.Since(start); d < 60*time.Millisecond || d > 2*time.Second {
		t.Fatalf("2 MB through a limited slotConn.Read took %s, want about 150-300 ms", d)
	}
}

func TestLimitsFollowAdapters(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("A", "10.0.0.1", 1)})
	s.SetLimit("A", 5)
	s.SetLimit("B", 10) // not seen yet: saved for when it appears

	limitOf := func(name string) float64 {
		for _, st := range s.Snapshot() {
			if st.Name == name {
				return st.LimitMbps
			}
		}
		t.Fatalf("adapter %q not in the snapshot", name)
		return 0
	}
	if got := limitOf("A"); got != 5 {
		t.Fatalf("A has limit %v, want 5", got)
	}

	s.Update([]nics.Nic{nic("A", "10.0.0.1", 1), nic("B", "10.0.0.2", 1)})
	if got := limitOf("B"); got != 10 {
		t.Fatalf("B came back with limit %v, want 10", got)
	}
	if limits := s.Limits(); len(limits) != 2 || limits["A"] != 5 || limits["B"] != 10 {
		t.Fatalf("Limits() = %v, want A=5 B=10", limits)
	}

	s.SetLimits(map[string]float64{"C": 20})
	if got := limitOf("A"); got != 0 {
		t.Fatalf("A still has limit %v after SetLimits replaced the map", got)
	}
	if limits := s.Limits(); len(limits) != 1 || limits["C"] != 20 {
		t.Fatalf("Limits() = %v, want only C=20", limits)
	}
	s.SetLimit("C", 0)
	if limits := s.Limits(); len(limits) != 0 {
		t.Fatalf("Limits() = %v after clearing C, want empty", limits)
	}
}
