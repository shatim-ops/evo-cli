// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"os"
	"strings"
	"testing"
)

// The fault-injection fake cloud is one file kept in two packages (engine
// and tools test their own unexported code with it). The copies must not
// drift: a fix in one goes to the other.
func TestFaultFakeCopiesMatch(t *testing.T) {
	a, err := os.ReadFile("fault_fake_test.go")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("../tools/fault_fake_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Replace(string(a), "package engine", "package tools", 1) != string(b) {
		t.Fatal("internal/engine/fault_fake_test.go and internal/tools/fault_fake_test.go differ (besides the package line); keep them identical")
	}
}
