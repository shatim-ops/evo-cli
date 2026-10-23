// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// Experiment (not to merge): how long one state Write takes on this runner.
func TestExp_WriteTiming(t *testing.T) {
	if os.Getenv("EVO_EXP") != "1" {
		t.Skip("experiment")
	}
	path := filepath.Join(t.TempDir(), ".evo", "state.json")
	var ds []time.Duration
	for i := 0; i < 30; i++ {
		start := time.Now()
		if err := Update(path, time.Minute, func(f *File) error {
			f.UpsertDeployment(DeploymentRecord{ID: "vm", VMID: "vm", Status: "creating"})
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		ds = append(ds, time.Since(start))
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	t.Logf("state.Update ×30: min %v, median %v, p90 %v, max %v", ds[0], ds[15], ds[27], ds[29])
}
