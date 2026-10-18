package provision

import (
	"strings"
	"testing"
)

func TestSortFlavors(t *testing.T) {
	fs := []*Flavor{
		{Name: "gpu-8-64-a100", VCPUs: 8, RAMGB: 64},
		{Name: "gen-4-16", VCPUs: 4, RAMGB: 16},
		{Name: "lowcost-1-1", VCPUs: 1, RAMGB: 1},
		{Name: "gen-2-8", VCPUs: 2, RAMGB: 8},
		{Name: "GPU-2-16", VCPUs: 2, RAMGB: 16},
		{Name: "gen-1-1", VCPUs: 1, RAMGB: 1},
		{Name: "gen-2-4", VCPUs: 2, RAMGB: 4},
	}
	SortFlavors(fs)
	names := make([]string, len(fs))
	for i, f := range fs {
		names[i] = f.Name
	}
	want := "gen-1-1 lowcost-1-1 gen-2-4 gen-2-8 gen-4-16 GPU-2-16 gpu-8-64-a100"
	if got := strings.Join(names, " "); got != want {
		t.Errorf("order:\n got %s\nwant %s", got, want)
	}
}
