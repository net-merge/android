package netmerge

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSetNetworksAndStats(t *testing.T) {
	js := `[{"name":"Wi-Fi","handle":"432902426637","v4":true,"v6":true},{"name":"Mobile data","handle":"437197393933","v4":true}]`
	if err := SetNetworks(js); err != nil {
		t.Fatal(err)
	}
	var st []stat
	if err := json.Unmarshal([]byte(Stats()), &st); err != nil {
		t.Fatal(err)
	}
	if len(st) != 2 || st[0].Name != "Wi-Fi" || st[1].State6 != "-" {
		t.Fatalf("unexpected stats: %+v", st)
	}
	// The same name keeps its index when its handle changes (Wi-Fi reconnect).
	if err := SetNetworks(`[{"name":"Wi-Fi","handle":"1","v4":true}]`); err != nil {
		t.Fatal(err)
	}
	if indexes["Wi-Fi"] != 1 || indexes["Mobile data"] != 2 {
		t.Fatalf("indexes changed: %v", indexes)
	}
	if err := SetNetworks(`[{"name":"x","handle":"nope"}]`); err == nil {
		t.Fatal("want error for a bad handle")
	}
}

func TestSetAutoWeightsRestoresEvenSplit(t *testing.T) {
	if err := SetNetworks(`[{"name":"A","handle":"1","v4":true},{"name":"B","handle":"2","v4":true}]`); err != nil {
		t.Fatal(err)
	}
	SetAutoWeights(false)
	defer SetAutoWeights(true)
	var st []stat
	if err := json.Unmarshal([]byte(Stats()), &st); err != nil {
		t.Fatal(err)
	}
	for _, s := range st {
		if s.Present && s.Weight != 1 {
			t.Fatalf("%s weight %v, want 1", s.Name, s.Weight)
		}
	}
}

func TestSystemEstimateSeedsWeights(t *testing.T) {
	SetAutoWeights(true)
	if err := SetNetworks(`[{"name":"Wi-Fi","handle":"1","v4":true,"downKbps":400000},{"name":"Mobile data","handle":"2","v4":true,"downKbps":50000}]`); err != nil {
		t.Fatal(err)
	}
	sched.Tick(time.Second) // nothing measured yet: weights come from the estimates
	w := map[string]float64{}
	var st []stat
	json.Unmarshal([]byte(Stats()), &st)
	for _, s := range st {
		w[s.Name] = s.Weight
	}
	// 50000/400000 is 0.125, below the floor.
	if w["Wi-Fi"] != 1 || w["Mobile data"] != minSeedWeight {
		t.Fatalf("weights %v, want Wi-Fi 1 and mobile %v", w, minSeedWeight)
	}
	SetAutoWeights(false) // even split ignores the estimates
	defer SetAutoWeights(true)
	json.Unmarshal([]byte(Stats()), &st)
	for _, s := range st {
		if s.Present && s.Weight != 1 {
			t.Fatalf("even split: %s weight %v", s.Name, s.Weight)
		}
	}
}

// A network the system rates far slower must still carry some of a handful of connections, or it is
// never measured and the user sees mobile data unused next to Wi-Fi.
func TestSlowLookingNetworkStillGetsFlows(t *testing.T) {
	SetAutoWeights(true)
	if err := SetNetworks(`[{"name":"Wi-Fi","handle":"1","v4":true,"downKbps":900000},{"name":"Mobile data","handle":"2","v4":true,"downKbps":30000}]`); err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for i := 0; i < 6; i++ {
		sl := sched.Acquire(nil)
		if sl == nil {
			t.Fatal("no network")
		}
		got[sl.Name]++
		defer sched.Release(sl)
	}
	if got["Mobile data"] < 2 || got["Wi-Fi"] < 2 {
		t.Fatalf("6 connections went %v, want both networks to carry at least 2", got)
	}
}
