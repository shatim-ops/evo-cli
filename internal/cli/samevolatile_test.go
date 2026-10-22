// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import "testing"

// A plan without a name names its VM by the minute of the call
// (deployplan.Resolve): two calls a minute apart differ in that only.
// TestReadOnly_EveryTool failed on it now and then (CI 22.10, push of #188).
func TestSameVolatile_PlanNameMinute(t *testing.T) {
	a := `{"vm":{"name":"web-20261022-1159"}}`
	b := `{"vm":{"name":"web-20261022-1200"}}`
	if sameVolatile(a) != sameVolatile(b) {
		t.Fatalf("names a minute apart differ:\n%s\n%s", sameVolatile(a), sameVolatile(b))
	}
	if c := `{"vm":{"name":"web-2"}}`; sameVolatile(c) != c {
		t.Fatalf("a name without the minute changed: %s", sameVolatile(c))
	}
}
