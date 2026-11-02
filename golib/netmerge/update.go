package netmerge

import (
	"strconv"
	"strings"
)

// Newer reports whether release tag latest is newer than current. Anything after the number is
// ignored, so a dev build of 0.3.3 still hears about 0.3.4. The same rule as the PC version.
func Newer(current, latest string) bool {
	c, ok := numbers(current)
	if !ok {
		return false
	}
	l, ok := numbers(latest)
	if !ok {
		return false
	}
	for i := range c {
		if c[i] != l[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// numbers reads the leading dotted numbers, up to three: "v0.3.3-dev+1" -> 0.3.3.
func numbers(v string) ([3]int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	var out [3]int
	n := 0
	for n < len(out) {
		i := 0
		for i < len(v) && v[i] >= '0' && v[i] <= '9' {
			i++
		}
		if i == 0 {
			break
		}
		x, err := strconv.Atoi(v[:i])
		if err != nil {
			return out, false
		}
		out[n] = x
		n++
		v = strings.TrimPrefix(v[i:], ".")
	}
	return out, n > 0
}
