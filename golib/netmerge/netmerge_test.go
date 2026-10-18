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
	if w["Wi-Fi"] != 1 || w["Mobile data"] != 0.125 {
		t.Fatalf("weights %v, want Wi-Fi 1 and mobile 0.125", w)
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
