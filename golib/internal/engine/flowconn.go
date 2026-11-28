package engine

import (
	"context"
	"errors"
	"net"
	"sync"

	"netmerge-android/internal/nics"
)

// slotConn counts a pinned TCP flow's bytes and releases its slot on Close. It exposes
// CloseRead/CloseWrite because the TUN relay half-closes each direction separately.
type slotConn struct {
	*net.TCPConn
	sl       *Slot
	fam      Family
	s        *Scheduler
	once     sync.Once
	activity *flowActivity
	dest     string // its group (see GroupKey)
}

func (c *slotConn) Read(p []byte) (int, error) {
	if pair := c.sl.limit.Load(); pair != nil && len(p) > pair.chunk {
		p = p[:pair.chunk] // hand out a chunk at a time, so a limited flow stays smooth
	}
	n, err := c.TCPConn.Read(p)
	if n > 0 {
		c.sl.bytesIn[c.fam].Add(int64(n))
		if c.activity != nil {
			c.activity.bytes.Add(int64(n))
		}
		c.sl.throttle(true, n)
	}
	return n, err
}

func (c *slotConn) Write(p []byte) (int, error) {
	n, err := c.sl.writePaced(c.TCPConn, p, false)
	c.sl.bytesOut[c.fam].Add(int64(n))
	if c.activity != nil {
		c.activity.bytes.Add(int64(n))
	}
	return n, err
}

// release frees the slot's balancing count. It happens at the first half-close, not at the final
// Close: the relay can keep a finished flow open for up to a minute waiting for the peer's FIN,
// and counting those as active would steer new flows away from the NIC that just finished.
func (c *slotConn) release() {
	c.once.Do(func() {
		c.s.untrack(c.sl, c.fam, c.activity)
		c.s.ReleaseDest(c.sl, c.fam, c.dest)
	})
}

func (c *slotConn) CloseRead() error  { c.release(); return c.TCPConn.CloseRead() }
func (c *slotConn) CloseWrite() error { c.release(); return c.TCPConn.CloseWrite() }
func (c *slotConn) Close() error {
	c.release()
	c.sl.conns.Delete(c)
	return c.TCPConn.Close()
}

// DialPinned is DialTCP for front-ends that hand the conn to someone else to pump.
func (b *Balancer) DialPinned(ctx context.Context, host string, port int) (net.Conn, error) {
	return b.DialPinnedGroup(ctx, host, port, "")
}

// DialPinnedGroup is DialPinned for a flow whose owner is known: group (see GroupKey) is the app.
func (b *Balancer) DialPinnedGroup(ctx context.Context, host string, port int, group string) (net.Conn, error) {
	c, sl, f, err := b.dialTCP(ctx, host, port, group)
	if err != nil {
		return nil, err
	}
	key := GroupKey(group, host)
	sc := &slotConn{TCPConn: c.(*net.TCPConn), sl: sl, fam: f, s: b.S, dest: key, activity: b.S.trackKey(sl, f, key)}
	sl.conns.Store(sc, struct{}{})
	return sc, nil
}

type slotPacketConn struct {
	net.PacketConn
	sl  *Slot
	fam Family

	// Set for a socket that is a real flow (see ListenUDPFlow), which holds its slot until closed.
	s        *Scheduler
	dest     string
	activity *flowActivity
	once     sync.Once
}

func (c *slotPacketConn) Close() error {
	c.sl.conns.Delete(c)
	if c.activity != nil {
		c.once.Do(func() {
			c.s.untrack(c.sl, c.fam, c.activity)
			c.s.ReleaseDest(c.sl, c.fam, c.dest)
		})
	}
	return c.PacketConn.Close()
}

func (c *slotPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, a, err := c.PacketConn.ReadFrom(p)
	if n > 0 {
		c.sl.bytesIn[c.fam].Add(int64(n))
		if c.activity != nil {
			c.activity.bytes.Add(int64(n))
		}
		c.sl.throttlePacket(true, n)
	}
	return n, a, err
}

func (c *slotPacketConn) WriteTo(p []byte, a net.Addr) (int, error) {
	c.sl.throttlePacket(false, len(p))
	n, err := c.PacketConn.WriteTo(p, a)
	c.sl.bytesOut[c.fam].Add(int64(n))
	if c.activity != nil {
		c.activity.bytes.Add(int64(n))
	}
	return n, err
}

// ListenUDP opens a UDP socket pinned to one NIC (a UDP "flow" = the socket, so every datagram
// of that flow leaves the same way). UDP flows are short and numerous (DNS), so they don't hold
// a balancing slot: the pick still spreads them via the scheduler's tie-breaking.
func (b *Balancer) ListenUDP(ctx context.Context, f Family) (net.PacketConn, error) {
	return b.listenUDP(ctx, f, "")
}

// ListenUDPFlow is ListenUDP for a socket that is a real flow, such as QUIC, where one socket can
// carry a whole download: it counts in the balancing and holds its slot until it is closed.
func (b *Balancer) ListenUDPFlow(ctx context.Context, f Family, group, host string) (net.PacketConn, error) {
	return b.listenUDP(ctx, f, GroupKey(group, host))
}

func (b *Balancer) listenUDP(ctx context.Context, f Family, key string) (net.PacketConn, error) {
	network := "udp4"
	if f == Fam6 {
		network = "udp6"
	}
	tried := map[string]struct{}{}
	lastErr := errors.New("no usable network adapter")
	for {
		sl := b.S.AcquireDest(f, tried, key)
		if sl == nil {
			return nil, lastErr
		}
		tried[sl.key] = struct{}{}
		lc := net.ListenConfig{Control: nics.ForceEgress(sl.Index, f == Fam6)}
		pc, err := lc.ListenPacket(ctx, network, net.JoinHostPort(sl.ip(f).String(), "0"))
		if err != nil {
			b.S.ReleaseDest(sl, f, key)
			if classify(err).linkDown {
				b.S.Fail(sl, f, b.FailCooldown)
			}
			lastErr = err
			continue
		}
		spc := &slotPacketConn{PacketConn: pc, sl: sl, fam: f}
		if key == "" {
			b.S.ReleaseFor(sl, f)
		} else {
			spc.s, spc.dest, spc.activity = b.S, key, b.S.trackKey(sl, f, key)
		}
		sl.conns.Store(spc, struct{}{})
		return spc, nil
	}
}
