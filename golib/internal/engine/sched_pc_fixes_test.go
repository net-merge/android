package engine

import (
	"testing"
	"time"

	"netmerge-android/internal/nics"
)

// The PC version's fixes from net-merge 0.4.0 and 0.4.1, with their regression tests.

// Traffic net-merge can't tie to a busy flow (UDP: QUIC, DNS) must not lower a network's estimate
// while the other network is moving data on more flows.
func TestFewerFlowsDoNotLowerEstimate(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("A", "10.0.0.1", 1), nic("B", "10.0.0.2", 1)})
	s.SetAuto(true)
	A, B := s.slots[0], s.slots[1]
	A.peak[Fam4], B.peak[Fam4] = 10e6, 10e6
	var flows []*flowActivity
	for range 4 {
		A.actF[Fam4].Add(1)
		flows = append(flows, s.track(A, Fam4))
	}
	for range 30 {
		for _, a := range flows {
			a.bytes.Add(2_500_000)
		}
		A.bytesIn[Fam4].Add(10_000_000)
		B.bytesIn[Fam4].Add(1_000_000) // UDP only, no busy TCP flow
		s.Tick(time.Second)
	}
	if B.peak[Fam4] < 9e6 {
		t.Fatalf("B's estimate fell to %.0f from traffic on fewer flows than A", B.peak[Fam4])
	}
}

// A full network answers a probe too late. While data is still arriving it is not offline (#81).
func TestFailedProbeIgnoredWhileDataArrives(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("A", "10.0.0.1", 1)})
	a := s.slots[0]
	s.SetState(a, Fam4, true)
	a.bytesIn[Fam4].Add(1_000_000)
	s.Tick(time.Second)
	s.SetState(a, Fam4, false)
	if a.state[Fam4] != stateUp {
		t.Fatalf("busy network marked %q", a.state[Fam4])
	}
	s.Tick(time.Second) // nothing arrived: now the failure counts
	s.SetState(a, Fam4, false)
	if a.state[Fam4] != stateDown {
		t.Fatalf("quiet network left %q", a.state[Fam4])
	}
}

// A network filled by an upload receives little more than acknowledgements, and still works.
func TestFailedProbeIgnoredWhileUploading(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("A", "10.0.0.1", 1)})
	a := s.slots[0]
	s.SetState(a, Fam4, true)
	a.bytesOut[Fam4].Add(1_000_000)
	s.Tick(time.Second)
	a.linkRateIn = 25_000 // acknowledgements, from the OS's counters
	s.SetState(a, Fam4, false)
	if a.state[Fam4] != stateUp {
		t.Fatalf("uploading network marked %q", a.state[Fam4])
	}
	a.linkRateIn = 0 // sending into the void: nothing comes back
	s.SetState(a, Fam4, false)
	if a.state[Fam4] != stateDown {
		t.Fatalf("network that gets nothing back left %q", a.state[Fam4])
	}
}

// IPv4 filling the network vouches for IPv6 only until a real IPv6 connection on it times out too.
func TestFailedProbeCountsOnceItsFamilyTimesOut(t *testing.T) {
	for _, c := range []struct {
		name     string
		busy     Family
		suspect  bool
		wantDown bool
	}{
		{"other family busy", Fam4, false, false},
		{"other family busy, IPv6 timed out", Fam4, true, true},
		{"same family busy, IPv6 timed out", Fam6, true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := NewScheduler([]nics.Nic{nic("A", "10.0.0.1", 1)})
			a := s.slots[0]
			s.SetState(a, Fam6, true)
			a.bytesIn[c.busy].Add(1_000_000)
			s.Tick(time.Second)
			if c.suspect {
				s.Suspect(a, Fam6)
			}
			s.SetState(a, Fam6, false)
			if down := a.state[Fam6] == stateDown; down != c.wantDown {
				t.Fatalf("IPv6 state %q, want down=%v", a.state[Fam6], c.wantDown)
			}
		})
	}
}

// A capped network gets fewer busy flows than the others, so its falling samples are set aside. Its
// estimate must still stay at the cap, or the idle recovery lifts it and it gets new flows again.
func TestCappedLinkStaysAtItsCapUnderLoad(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("A", "10.0.0.1", 1), nic("B", "10.0.0.2", 1)})
	s.SetAuto(true)
	s.SetLimit("B", 2)
	A, B := s.slots[0], s.slots[1]
	for _, sl := range s.slots {
		s.SetState(sl, Fam4, true)
	}
	open := func(sl *Slot) *flowActivity { sl.actF[Fam4].Add(1); sl.active.Add(1); return s.track(sl, Fam4) }
	var onA []*flowActivity
	for range 4 {
		onA = append(onA, open(A))
	}
	onB := open(B)
	for i := range 120 {
		for _, a := range onA {
			a.bytes.Add(3_125_000) // 100 Mbit/s over four flows
		}
		A.bytesIn[Fam4].Add(12_500_000)
		n := int64(250_000) // B at its 2 Mbit/s cap, give or take
		if i%2 == 1 {
			n = 230_000
		}
		onB.bytes.Add(n)
		B.bytesIn[Fam4].Add(n)
		s.Tick(time.Second)
	}
	if B.peak[Fam4] > 2*bytesPerMbit {
		t.Fatalf("estimate of a network capped at 2 Mbit/s rose to %.1f Mbit/s", B.peak[Fam4]/bytesPerMbit)
	}
	got := map[string]int{}
	for range 9 {
		sl := s.AcquireFor(Fam4, nil)
		got[sl.Name]++
		s.track(sl, Fam4)
	}
	if got["B"] > 1 {
		t.Fatalf("a network capped at 2 Mbit/s got %d of 9 flows (weight %.2f)", got["B"], B.weight[Fam4])
	}
}

// The weight floor must not lift a network that is slow because it is capped.
func TestCappedLinkIsNotLiftedByTheFloor(t *testing.T) {
	s := NewScheduler([]nics.Nic{nic("A", "10.0.0.1", 1), nic("B", "10.0.0.2", 1)})
	s.SetAuto(true)
	s.SetLimit("B", 1)
	A, B := s.slots[0], s.slots[1]
	for _, sl := range s.slots {
		s.SetState(sl, Fam4, true)
	}
	A.peak[Fam4], B.peak[Fam4] = 12.5e6, 125e3
	s.Tick(time.Second)
	got := map[string]int{}
	for range 9 {
		sl := s.AcquireFor(Fam4, nil)
		got[sl.Name]++
		s.track(sl, Fam4)
	}
	if got["B"] > 1 {
		t.Fatalf("a network capped at 1 Mbit/s got %d of 9 flows (weight %.2f)", got["B"], B.weight[Fam4])
	}
}
