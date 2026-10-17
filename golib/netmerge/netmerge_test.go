package netmerge

import (
	"encoding/json"
	"testing"
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
