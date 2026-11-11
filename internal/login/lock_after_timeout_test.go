// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/loginworld"
)

// Gate rc.1 of 1.8, Grok F3 (findings ledger № 192): after LOGIN_TIMEOUT
// the configuration directory still holds login.lock. The file is the
// place of the lock, not the lock: the login that ended no longer holds
// it, and the next login starts — it is not refused STATE_LOCKED.
func TestLogin_LockFileAfterTimeoutHoldsNoLogin(t *testing.T) {
	w := newWorld(t, loginworld.Variant{})
	var out bytes.Buffer
	cfg := worldConfig(w, &out, func(string) error { return nil })
	cfg.Wait = 300 * time.Millisecond
	_, err := Login(context.Background(), cfg, Choice{}, &memStore{})
	var ce *clierrors.CLIError
	if !errors.As(err, &ce) || ce.Code != clierrors.CodeLoginTimeout {
		t.Fatalf("the first login: %v, want LOGIN_TIMEOUT", err)
	}
	if _, err := os.Stat(cfg.LockPath); err != nil {
		t.Logf("no lock file after the timeout (%v): nothing left, nothing to hold", err)
	}
	_, err = Login(context.Background(), cfg, Choice{}, &memStore{})
	if errors.As(err, &ce) && ce.Code == clierrors.CodeStateLocked {
		t.Fatalf("the login after a timed-out one is refused STATE_LOCKED: the file left holds the lock: %v", err)
	}
	if !errors.As(err, &ce) || ce.Code != clierrors.CodeLoginTimeout {
		t.Errorf("the second login: %v, want it to start and time out as the first", err)
	}
}
