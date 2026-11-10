// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/loginworld"
)

// The intent of the key is a local write before POST accessKeys (№ 37).
// When it fails — the directory of intents cannot be made, a file is in
// its place — the login stops before the key, undoes what it made, and
// says a local write failed: not API_ERROR «access not granted», which
// sends the person to the cloud and to an administrator (sweep of #1198;
// «Риск», conclusion 75; ledger 1.8, № 36).
func TestLogin_IntentNotWrittenIsALocalError(t *testing.T) {
	w := newWorld(t, loginworld.Variant{})
	b := &browser{}
	b.t, b.w, b.ended = t, w, make(chan struct{})
	var out bytes.Buffer
	cfg := worldConfig(w, &out, b.open)
	cfg.Tell = func(line string) { fmt.Fprintln(&out, line) }
	root := t.TempDir()
	// A file where the directory of intents goes: mkdir fails for root
	// too, unlike a mode.
	if err := os.WriteFile(filepath.Join(root, ".evo"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.IntentDir = filepath.Join(root, ".evo", "intents")
	st := &memStore{}
	_, err := Login(context.Background(), cfg, Choice{}, st)
	<-b.ended
	if err == nil {
		t.Fatal("the login went on without the intent of its key")
	}
	if len(w.Keys()) != 0 || len(w.Permissions()) != 0 || len(st.saved) != 0 {
		t.Errorf("left keys %d, permissions %d, profiles %d", len(w.Keys()), len(w.Permissions()), len(st.saved))
	}
	var ce *clierrors.CLIError
	if errors.As(err, &ce) && ce.Code == clierrors.CodeAPIError || clierrors.ExitCode(err) == clierrors.ExitAPI {
		t.Errorf("a local write is answered as the cloud's: %v", err)
	}
	for _, s := range []string{"доступ не выдан", "access not granted", "access was not granted"} {
		if strings.Contains(strings.ToLower(err.Error()), s) {
			t.Errorf("the answer sends the person to the cloud's access (%q): %v", s, err)
		}
	}
}
