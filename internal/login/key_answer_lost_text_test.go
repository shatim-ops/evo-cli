// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/loginworld"
)

// FB180-3: the answer to the key was lost. The reason is the one of a
// failed issue (API_ERROR, not a new code: a break in the reason is a
// breaking change); the words say whether the key was issued is unknown,
// list it among what may be left, and the hint names which key to check —
// the one created at about the time of this login, not another (R-56).
func TestLogin_KeyAnswerLostSaysOutcomeUnknown(t *testing.T) {
	w := newWorld(t, loginworld.Variant{KeyAnswerLost: true})
	b := &browser{}
	b.t, b.w, b.ended = t, w, make(chan struct{})
	var out bytes.Buffer
	cfg := worldConfig(w, &out, b.open)
	cfg.Tell = func(line string) { fmt.Fprintln(&out, line) }
	cfg.IntentDir = t.TempDir()
	_, err := Login(context.Background(), cfg, Choice{}, &memStore{})
	<-b.ended
	var ce *clierrors.CLIError
	if !errors.As(err, &ce) {
		t.Fatalf("the login ended %v, want a CLIError", err)
	}
	if got := clierrors.Reason(err); got != clierrors.CodeAPIError {
		t.Errorf("reason %q, want %q", got, clierrors.CodeAPIError)
	}
	if !keyOutcomeUnknownSaid(err) || !strings.Contains(ce.Message, tKeyMaybeLeft()) {
		t.Errorf("the answer does not say the key may be in the cloud: %v", ce.Message)
	}
	if !strings.Contains(ce.Hint, "evo doctor") || strings.Contains(ce.Hint, tNothingLeft()) {
		t.Errorf("the hint does not name the key to check: %q", ce.Hint)
	}
}
