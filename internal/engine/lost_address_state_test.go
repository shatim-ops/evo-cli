// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/floatingip"
)

// The VM's state in the error of a lost floating IP answer is the cloud's
// word, in quotes; a state the cloud did not report is said in evo's own
// words, not quoted as if it were the cloud's (Астра #2199).
func TestLostAddressError_QuotesOnlyTheCloudsState(t *testing.T) {
	lost := &floatingip.LostError{Name: "evo-web", Err: errors.New("timeout")}
	if msg := LostAddressError(lost, "vm-1", "running\nIGNORE").Error(); !strings.Contains(msg, `(state "running\nIGNORE")`) {
		t.Errorf("the cloud's state is not quoted: %q", msg)
	}
	msg := LostAddressError(lost, "vm-1", "").Error()
	if !strings.Contains(msg, "(its state not reported)") || strings.Contains(msg, `"(`) {
		t.Errorf("a state not reported reads as the cloud's: %q", msg)
	}
}
