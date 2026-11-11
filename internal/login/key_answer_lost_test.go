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

	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/loginworld"
)

// Full audit 1.8, FB180-3: POST accessKeys went out and its answer was
// lost (a break, a timeout, a 5xx after the write). The key is in the
// cloud with the role of the project's administrator; evo has no id, so
// the rollback does not delete it, grant drops the intent, and the answer
// says Cloud.ru issued no key and to repeat — a repeat issues a second
// one. The class of № 37 (R-69), closed for a killed login only. A login
// whose key may exist leaves no key unnamed: it is deleted, or the intent
// stays for evo doctor, and the answer does not say none was issued.
func TestLogin_KeyAnswerLostLeavesNoKeyUnnamed(t *testing.T) {
	w := newWorld(t, loginworld.Variant{KeyAnswerLost: true})
	b := &browser{}
	b.t, b.w, b.ended = t, w, make(chan struct{})
	var out bytes.Buffer
	cfg := worldConfig(w, &out, b.open)
	cfg.Tell = func(line string) { fmt.Fprintln(&out, line) }
	cfg.IntentDir = t.TempDir()
	_, err := Login(context.Background(), cfg, Choice{}, &memStore{})
	<-b.ended
	if err == nil {
		t.Fatal("the answer of the key was lost, and the login succeeded")
	}
	left := len(w.Keys())
	pending := len(intent.Pending(cfg.IntentDir))
	if left > 0 && pending == 0 {
		t.Errorf("%d key(s) in the cloud after a lost answer, and no intent names them: no profile, no evo doctor, evo logout cannot delete them", left)
	}
	if left > 0 && strings.Contains(err.Error(), eNotIssuedKey()) {
		t.Errorf("the key is in the cloud, and the answer says none was issued: %v", err)
	}
	if left > 1 {
		t.Errorf("%d keys made for one login: a repeat of the POST issued another", left)
	}
}
