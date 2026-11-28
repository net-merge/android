package netmerge

import (
	"testing"
	"time"
)

// run feeds the machine one tick per second for d, returning the first action and when it came.
func run(a *autoPauser, now *time.Time, d time.Duration, on bool, usable int, active int64) (autoAction, time.Duration) {
	for el := time.Duration(0); el < d; el += time.Second {
		*now = now.Add(time.Second)
		if act := a.tick(*now, on, usable, active); act != autoNone {
			return act, el + time.Second
		}
	}
	return autoNone, 0
}

func newPauser(startOn bool) (*autoPauser, *time.Time) {
	a := &autoPauser{enabled: true}
	a.start(startOn)
	now := time.Unix(1_000_000, 0)
	return a, &now
}

func TestPausesAfterTenIdleSecondsWithOneConnection(t *testing.T) {
	a, now := newPauser(false)
	a.manualOff, a.waiting = false, false // running, as if started by hand earlier and released
	if act, _ := run(a, now, 9*time.Second, true, 1, 0); act != autoNone {
		t.Fatal("paused too early")
	}
	if act, at := run(a, now, 3*time.Second, true, 1, 0); act != autoPause || at != time.Second {
		t.Fatalf("want a pause on the 10th second, got %v after %v", act, at)
	}
}

func TestBusyFlowsDelayThePause(t *testing.T) {
	a, now := newPauser(true)
	a.waiting = false
	if act, _ := run(a, now, 100*time.Second, true, 1, 5); act != autoNone {
		t.Fatal("paused while flows were open")
	}
	if act, _ := run(a, now, 30*time.Second, true, 1, 5); act != autoPause {
		t.Fatal("should pause after two minutes even with open flows")
	}
}

func TestAShortStallDoesNotPause(t *testing.T) {
	a, now := newPauser(true)
	a.waiting = false
	run(a, now, 30*time.Second, true, 1, 5) // a transfer is going
	if act, _ := run(a, now, 9*time.Second, true, 1, 0); act != autoNone {
		t.Fatal("paused during a short stall")
	}
	if act, at := run(a, now, 3*time.Second, true, 1, 0); act != autoPause || at != time.Second {
		t.Fatalf("want a pause after 10 quiet seconds, got %v after %v", act, at)
	}
}

func TestSecondConnectionResetsThePauseTimer(t *testing.T) {
	a, now := newPauser(true)
	a.waiting = false
	run(a, now, 8*time.Second, true, 1, 0)
	run(a, now, 1*time.Second, true, 2, 0) // a second link appeared for a moment
	if act, _ := run(a, now, 8*time.Second, true, 1, 0); act != autoNone {
		t.Fatal("the timer should have restarted")
	}
}

func TestStartsPausedAndResumesWithTwoConnections(t *testing.T) {
	a, now := newPauser(true) // tray started, auto on: waits for two connections
	if act, _ := run(a, now, 60*time.Second, false, 1, 0); act != autoNone {
		t.Fatal("must stay paused with one connection")
	}
	if act, at := run(a, now, 10*time.Second, false, 2, 0); act != autoResume || at != 3*time.Second {
		t.Fatalf("want a resume after 3s of two connections, got %v after %v", act, at)
	}
}

func TestManualOffIsNeverUndone(t *testing.T) {
	a, now := newPauser(true)
	a.userSet(false, 2) // the user pauses it
	if act, _ := run(a, now, 120*time.Second, false, 3, 0); act != autoNone {
		t.Fatal("resumed something the user paused")
	}
	a.userSet(true, 3) // and starts it again
	if a.manualOff {
		t.Fatal("starting it clears the manual pause")
	}
}

func TestStartedWithOffFlagStaysOff(t *testing.T) {
	a, now := newPauser(false)
	if act, _ := run(a, now, 60*time.Second, false, 2, 0); act != autoNone {
		t.Fatal("-off means off until the user says otherwise")
	}
}

func TestManualStartWithOneConnectionIsHeldUntilTwoAreSeen(t *testing.T) {
	a, now := newPauser(true)
	a.userSet(true, 1) // the user starts it on purpose with one connection
	if act, _ := run(a, now, 300*time.Second, true, 1, 0); act != autoNone {
		t.Fatal("paused something the user started on purpose")
	}
	run(a, now, 2*time.Second, true, 2, 0) // two connections seen: normal rules again
	if act, _ := run(a, now, 15*time.Second, true, 1, 0); act != autoPause {
		t.Fatal("should pause once the second connection is gone again")
	}
}

func TestSwitchesAreSpacedApart(t *testing.T) {
	a, now := newPauser(true)
	a.waiting = false
	if act, _ := run(a, now, 15*time.Second, true, 1, 0); act != autoPause {
		t.Fatal("expected the first pause")
	}
	// the second link comes back at once: resume has to wait out the 30 s gap
	act, at := run(a, now, 60*time.Second, false, 2, 0)
	if act != autoResume || at < autoGap-10*time.Second {
		t.Fatalf("resume came too soon (%v after %v)", act, at)
	}
}

func TestSleepGapCountsAsOneSecond(t *testing.T) {
	a, now := newPauser(true)
	a.waiting = false
	a.tick(*now, true, 1, 0)
	*now = now.Add(8 * time.Hour) // laptop slept
	if act := a.tick(*now, true, 1, 0); act != autoNone {
		t.Fatal("a long gap must not count as a long time with one connection")
	}
}

func TestDisabledDoesNothing(t *testing.T) {
	a, now := newPauser(true)
	a.enabled, a.waiting = false, false
	if act, _ := run(a, now, 600*time.Second, true, 0, 0); act != autoNone {
		t.Fatal("acted while disabled")
	}
	a.setEnabled(true, false) // ticking the box while paused: it will resume when two links are up
	if !a.waiting {
		t.Fatal("enabling while paused should wait for two connections")
	}
}

func TestFailedResumeIsRetriedAfterTheGap(t *testing.T) {
	a, now := newPauser(true)
	if act, _ := run(a, now, 10*time.Second, false, 2, 0); act != autoResume {
		t.Fatal("expected a resume")
	}
	a.resumeFailed()
	act, at := run(a, now, 90*time.Second, false, 2, 0)
	if act != autoResume || at < autoGap-10*time.Second {
		t.Fatalf("retry should wait for the gap, got %v after %v", act, at)
	}
}
