// Package tun terminates the VpnService's TUN in a userland TCP/IP stack (gVisor via tun2socks)
// and re-dials every flow through the Balancer. Unlike upstream, Android creates the adapter and
// its routes (VpnService.Builder); this side only receives the file descriptor.
package tun

import (
	"context"
	"net"
	"strconv"
	"sync"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip/stack"

	"github.com/xjasonlyu/tun2socks/v2/core"
	"github.com/xjasonlyu/tun2socks/v2/core/device"
	"github.com/xjasonlyu/tun2socks/v2/core/device/fdbased"
	"github.com/xjasonlyu/tun2socks/v2/core/option"
	tlog "github.com/xjasonlyu/tun2socks/v2/log"
	M "github.com/xjasonlyu/tun2socks/v2/metadata"
	"github.com/xjasonlyu/tun2socks/v2/proxy/reject"
	"github.com/xjasonlyu/tun2socks/v2/tunnel"

	"netmerge-android/internal/engine"
)

// udpSessionTimeout ends idle UDP sessions sooner than tun2socks' 60s (see upstream).
const udpSessionTimeout = 30 * time.Second

type tunProxy struct{ b *engine.Balancer }

// OwnerFunc, when set, tells which app owns a TCP flow (its uid), or a negative number if unknown.
// Android implements it with ConnectivityManager.getConnectionOwnerUid. Set it before Start.
var OwnerFunc func(proto int, srcIP string, srcPort int, dstIP string, dstPort int) int

// group names the app that owns the flow, or nothing if that is unknown.
func group(proto int, m *M.Metadata, dst string) string {
	if own := OwnerFunc; own != nil {
		if uid := own(proto, m.SrcIP.Unmap().String(), int(m.SrcPort), dst, int(m.DstPort)); uid >= 0 {
			return "app:" + strconv.Itoa(uid)
		}
	}
	return ""
}

func (p tunProxy) DialContext(ctx context.Context, m *M.Metadata) (net.Conn, error) {
	dst := m.DstIP.Unmap().String()
	conn, err := p.b.DialPinnedGroup(ctx, dst, int(m.DstPort), group(6, m, dst))
	if err != nil {
		return nil, err
	}
	return keepReading{conn}, nil
}

func (p tunProxy) DialUDP(m *M.Metadata) (net.PacketConn, error) {
	d := m.DstIP.Unmap()
	f := engine.Fam4
	if d.Is6() {
		f = engine.Fam6
	}
	// UDP to port 443 is QUIC, where one socket can carry a whole download. It has to count in the
	// balancing like a TCP connection, or such downloads pile onto one network by chance.
	if m.DstPort == 443 {
		return p.b.ListenUDPFlow(context.Background(), f, group(17, m, d.String()), d.String())
	}
	return p.b.ListenUDP(context.Background(), f)
}

// Tun owns the stack built on the VPN's file descriptor.
type Tun struct {
	mu  sync.Mutex
	dev device.Device
	stk *stack.Stack
}

// Start takes ownership of fd (it is closed by Stop).
func (t *Tun) Start(b *engine.Balancer, fd, mtu int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.teardown()
	if lvl, e := tlog.ParseLevel("error"); e == nil {
		tlog.SetLogger(tlog.Must(tlog.NewLeveled(lvl)))
	}
	tunnel.T().SetUDPTimeout(udpSessionTimeout)
	dev, err := fdbased.Open(strconv.Itoa(fd), uint32(mtu), 0)
	if err != nil {
		return err
	}
	stk, err := core.CreateStack(&core.Config{
		LinkEndpoint:     dev,
		TransportHandler: tunnel.T(),
		Options:          []option.Option{option.WithTCPModerateReceiveBuffer(true)},
	})
	if err != nil {
		dev.Close()
		return err
	}
	installForwarders(stk, b.V6Allowed)
	tunnel.T().SetProxy(tunProxy{b})
	t.dev, t.stk = dev, stk
	return nil
}

func (t *Tun) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.teardown()
}

func (t *Tun) teardown() {
	tunnel.T().SetProxy(&reject.Reject{})
	if t.dev != nil {
		t.dev.Close()
	}
	if t.stk != nil {
		done := make(chan struct{})
		go func() { t.stk.Close(); t.stk.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}
	t.dev, t.stk = nil, nil
}
