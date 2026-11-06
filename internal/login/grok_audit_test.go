// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/loginworld"
)

// Release plan 1.8, «B-5, дефект: тайм-аут согласия» (Grok audit 06.11,
// #1087): when the wait ends, evo said «прервано» with code 130, as for
// Ctrl+C — the end of the wait is not the person's interrupt. It has its
// own words («время ожидания истекло») and its own reason and code, and
// nothing is made.
func TestLogin_WaitEndIsNoInterrupt(t *testing.T) {
	prev := i18n.Lang()
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(prev) })
	w := newWorld(t, loginworld.Variant{})
	var out bytes.Buffer
	// The browser opens nothing: the person never comes back.
	cfg := worldConfig(w, &out, func(string) error { return nil })
	// The wait of the call, short: the test bounds a real timer of the
	// login, it reads no wall clock.
	cfg.Wait = 50 * time.Millisecond
	_, err := Login(context.Background(), cfg, Choice{}, &memStore{})
	if err == nil {
		t.Fatal("the wait ended and the login answered no error")
	}
	if r := clierrors.Reason(err); r == clierrors.ReasonInterrupted {
		t.Errorf("the end of the wait is %s, as Ctrl+C: it has a reason of its own", r)
	}
	if c := clierrors.ExitCode(err); c == clierrors.ExitInterrupted {
		t.Errorf("the end of the wait exits %d, as Ctrl+C: it has a code of its own", c)
	}
	if !strings.Contains(err.Error(), "истекло") {
		t.Errorf("the end of the wait does not say so («время ожидания истекло»): %v", err)
	}
	if len(w.Accounts()) != 0 || len(w.Keys()) != 0 {
		t.Errorf("the wait ended and something was made: %d accounts, %d keys", len(w.Accounts()), len(w.Keys()))
	}
}

// Release plan 1.8, «B-5, дефекты флагов и параллельного входа» (в) (Grok
// audit 06.11, #1087): two evo login lived at once on two ports — the
// person may finish the one they did not mean (R-47). While one waits for
// the person, a second refuses before it opens a browser or a port, and
// says that a sign-in is already open; the first goes on.
func TestLogin_SecondLoginRefusedWhileOneWaits(t *testing.T) {
	w := newWorld(t, loginworld.Variant{})
	var out1, out2 bytes.Buffer
	opened := make(chan struct{})
	var once atomic.Bool
	cfg1 := worldConfig(w, &out1, func(string) error {
		if once.CompareAndSwap(false, true) {
			close(opened)
		}
		return nil
	})
	ctx1, cancel1 := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() {
		_, err := Login(ctx1, cfg1, Choice{}, &memStore{})
		first <- err
	}()
	select {
	case <-opened:
	case err := <-first:
		t.Fatalf("the first login ended before it waited: %v", err)
	case <-time.After(10 * time.Second):
		cancel1()
		t.Fatal("the first login never opened its page")
	}

	var secondOpened atomic.Bool
	cfg2 := worldConfig(w, &out2, func(string) error { secondOpened.Store(true); return nil })
	cfg2.Wait = 2 * time.Second
	_, err := Login(context.Background(), cfg2, Choice{}, &memStore{})
	cancel1()
	<-first
	if err == nil {
		t.Fatal("a second evo login went on while the first waited (R-47)")
	}
	if secondOpened.Load() {
		t.Errorf("the second login opened a browser before it refused")
	}
	if errors.Is(err, context.Canceled) || clierrors.Reason(err) == clierrors.ReasonInterrupted {
		t.Errorf("the second login ended by its wait or an interrupt, not by a refusal: %v", err)
	}
}
