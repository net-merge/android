package netmerge

import "time"

// Pausing by itself: with fewer than two usable connections there is nothing to merge, so merging
// pauses, and it resumes once a second connection works. A switch cuts the connections that are
// open at that moment, so both directions wait for things to settle, and automatic switches are
// spaced apart so a flapping link can't toggle it all the time.
const (
	pauseAfterIdle = 10 * time.Second // one connection and nothing moving through net-merge
	pauseAfterBusy = 2 * time.Minute  // one connection but data is still moving: give it time to end
	resumeAfter    = 3 * time.Second  // two or more connections
	autoGap        = 30 * time.Second // least time between two automatic switches
	quietRate      = 50_000           // bytes/s through net-merge under which it counts as quiet (about 0.4 Mbit/s)
)

type autoAction int

const (
	autoNone autoAction = iota
	autoPause
	autoResume
)

type autoPauser struct {
	enabled    bool
	manualOff  bool // the user paused it: it never starts by itself
	held       bool // the user started it with one connection: no auto-pause until two have been seen
	waiting    bool // paused by us, or not started yet: resumes when two connections are usable
	low, high  time.Duration
	quiet      time.Duration // how long nothing has been moving
	lastTick   time.Time
	lastSwitch time.Time
}

// start sets the first state: startOn false means the user asked for it to stay off.
func (a *autoPauser) start(startOn bool) {
	if !startOn {
		a.manualOff = true
	} else if a.enabled {
		a.waiting = true
	}
}

// tick runs about once a second. on is whether merging is on, usable how many connections reach
// the internet, active how many flows are moving data through net-merge (0 when it is quiet).
func (a *autoPauser) tick(now time.Time, on bool, usable int, active int64) autoAction {
	dt := time.Second
	if d := now.Sub(a.lastTick); !a.lastTick.IsZero() && d > 0 && d < 2*time.Second {
		dt = d // after a sleep one second counts, not the whole gap
	}
	a.lastTick = now
	if active == 0 {
		a.quiet += dt
	} else {
		a.quiet = 0
	}
	if !a.enabled {
		a.low, a.high = 0, 0
		return autoNone
	}
	if usable >= 2 {
		a.held = false
	}
	cooling := !a.lastSwitch.IsZero() && now.Sub(a.lastSwitch) < autoGap
	switch {
	case on && usable < 2 && !a.held:
		a.low, a.high = a.low+dt, 0
		settled := a.low >= pauseAfterIdle && a.quiet >= pauseAfterIdle
		if (settled || a.low >= pauseAfterBusy) && !cooling {
			a.low, a.waiting, a.lastSwitch = 0, true, now
			return autoPause
		}
	case !on && a.waiting && !a.manualOff && usable >= 2:
		a.high, a.low = a.high+dt, 0
		if a.high >= resumeAfter && !cooling {
			a.high, a.waiting, a.lastSwitch = 0, false, now
			return autoResume
		}
	default:
		a.low, a.high = 0, 0
	}
	return autoNone
}

// userSet records that the user switched merging on or off (menu or `net-merge on|off`).
func (a *autoPauser) userSet(on bool, usable int) {
	a.low, a.high = 0, 0
	a.waiting = false
	a.manualOff = !on
	a.held = on && usable < 2
}

// setEnabled is the menu checkbox.
func (a *autoPauser) setEnabled(enabled, on bool) {
	a.enabled = enabled
	a.low, a.high = 0, 0
	if enabled && !on && !a.manualOff {
		a.waiting = true
	}
}

// resumeFailed puts back the wait after an automatic switch-on that did not work; it tries again
// after the gap.
func (a *autoPauser) resumeFailed() { a.waiting = true }
