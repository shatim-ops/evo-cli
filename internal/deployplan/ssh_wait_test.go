// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package deployplan

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/output"
)

// CLAUDE.md §6, axis 2 (#457): the wait for SSH says the state of port 22
// — refused while sshd is not up, no answer while the VM boots — once per
// change, not only that time passes.
func TestWaitForSSHPort_SaysThePortState(t *testing.T) {
	var buf bytes.Buffer
	show := &Progress{P: output.NewWithWriter(&buf, output.ModePlain, false)}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// Port 22 of 127.0.0.2: refused where no sshd runs, no answer where
	// the address is not routed (macOS) — either is a state said once.
	if waitForSSHPort(ctx, "127.0.0.2", 300*time.Millisecond, show) {
		t.Skip("an sshd answers on 127.0.0.2:22 here")
	}
	if n := strings.Count(buf.String(), "127.0.0.2: "); n != 1 {
		t.Errorf("the state of port 22 said %d times, want once per change:\n%s", n, buf.String())
	}
}
