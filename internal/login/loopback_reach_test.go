// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/loginworld"
)

// Plan 1.8, P1 (release audit 1.8, findings appendix): a browser that
// cannot reach 127.0.0.1 of the computer evo runs on — over SSH, in a
// container or a sandboxed browser — ends at «cannot connect» with no word
// of why. The wait names it; a terminal over SSH is told before the
// browser, with the way that works: the port forwarded by ssh -L.
func TestLogin_LoopbackOutOfReachIsNamed(t *testing.T) {
	for _, remote := range []bool{false, true} {
		w := newWorld(t, loginworld.Variant{})
		var out bytes.Buffer
		cfg := worldConfig(w, &out, func(string) error { return nil }) // the person never comes
		cfg.Tell = func(line string) { fmt.Fprintln(&out, line) }
		cfg.Wait = 50 * time.Millisecond // wall-clock: a share of the production wait, to end the test
		cfg.Remote = remote
		_, _ = Login(context.Background(), cfg, Choice{}, &memStore{})
		text := out.String()
		if !strings.Contains(text, tWaiting(cfg.Wait)) || !strings.Contains(text, tCannotReach()) {
			t.Errorf("remote %v: the wait does not name a browser that cannot reach 127.0.0.1:\n%s", remote, text)
		}
		if over := strings.Contains(text, "ssh -L"); over != remote {
			t.Errorf("remote %v: the line of SSH is there: %v\n%s", remote, over, text)
		}
	}
}
