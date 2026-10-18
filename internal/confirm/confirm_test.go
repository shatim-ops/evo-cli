// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package confirm

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"io"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

var testPreview = Preview{
	Action:  "deploy",
	Summary: "create VM web (small) in ru.AZ-1",
	Lines:   []string{"flavor: small", "cost: about 1 RUB/month (estimate, not a bill)"},
	Details: map[string]any{"flavor": "small"},
}

type result struct {
	ok             bool
	err            error
	stdout, stderr string
}

func ask(t *testing.T, ctx context.Context, opts Options, in string, tty bool) result {
	t.Helper()
	var out, errOut bytes.Buffer
	opts.Out, opts.Err = &out, &errOut
	opts.In = strings.NewReader(in)
	opts.IsTTY = func() bool { return tty }
	ok, err := Ask(ctx, testPreview, opts)
	return result{ok, err, out.String(), errOut.String()}
}

// Четыре комбинации TTY × --yes из брифа этапа.
func TestAsk_TTYxYes(t *testing.T) {
	cases := []struct {
		name     string
		tty, yes bool
		in       string
		wantOK   bool
		wantCode int
		prompt   bool // was "[y/N]" shown
	}{
		{name: "tty, no --yes, answer y", tty: true, in: "y\n", wantOK: true, prompt: true},
		{name: "tty, no --yes, answer no", tty: true, in: "no\n", wantCode: clerrors.ExitConfirmationRequired, prompt: true},
		{name: "tty, no --yes, empty answer is no", tty: true, in: "\n", wantCode: clerrors.ExitConfirmationRequired, prompt: true},
		{name: "no tty, no --yes", tty: false, in: "y\n", wantCode: clerrors.ExitConfirmationRequired},
		{name: "tty, --yes", tty: true, yes: true, wantOK: true},
		{name: "no tty, --yes", tty: false, yes: true, wantOK: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := ask(t, context.Background(), Options{Yes: c.yes}, c.in, c.tty)
			if r.ok != c.wantOK {
				t.Fatalf("ok = %v, want %v (err %v)", r.ok, c.wantOK, r.err)
			}
			if got := clerrors.ExitCode(r.err); got != c.wantCode {
				t.Fatalf("exit code = %d, want %d (err %v)", got, c.wantCode, r.err)
			}
			if c.wantCode == clerrors.ExitConfirmationRequired && !stderrors.Is(r.err, ErrConfirmationRequired) {
				t.Errorf("error %v does not match ErrConfirmationRequired", r.err)
			}
			if r.stdout != "" {
				t.Errorf("stdout must stay clean, got %q", r.stdout)
			}
			if !strings.Contains(r.stderr, testPreview.Summary) {
				t.Errorf("preview missing from stderr: %q", r.stderr)
			}
			if got := strings.Contains(r.stderr, "[y/N]"); got != c.prompt {
				t.Errorf("prompt shown = %v, want %v: %q", got, c.prompt, r.stderr)
			}
			if c.yes && strings.Count(strings.TrimRight(r.stderr, "\n"), "\n") != 0 {
				t.Errorf("with --yes the preview must be one line: %q", r.stderr)
			}
			if !c.yes && !strings.Contains(r.stderr, "cost: about") {
				t.Errorf("full preview expected without --yes: %q", r.stderr)
			}
		})
	}
}

func TestAsk_NoTTY_JSON(t *testing.T) {
	r := ask(t, context.Background(), Options{JSON: true}, "", false)
	if got := clerrors.ExitCode(r.err); got != clerrors.ExitConfirmationRequired {
		t.Fatalf("exit code = %d, want 3", got)
	}
	var env struct {
		Status  string         `json:"status"`
		Preview map[string]any `json:"preview"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &env); err != nil {
		t.Fatalf("stdout is not JSON: %q", r.stdout)
	}
	if env.Status != "confirmation_required" {
		t.Errorf("status = %q", env.Status)
	}
	if env.Preview["action"] != "deploy" || env.Preview["summary"] != testPreview.Summary {
		t.Errorf("preview = %v", env.Preview)
	}
	if d, _ := env.Preview["details"].(map[string]any); d["flavor"] != "small" {
		t.Errorf("details lost: %v", env.Preview)
	}
}

func TestAsk_DryRun(t *testing.T) {
	// --dry-run wins over --yes: nothing is done either way.
	for _, yes := range []bool{false, true} {
		r := ask(t, context.Background(), Options{DryRun: true, Yes: yes}, "", false)
		if r.ok || r.err != nil {
			t.Fatalf("yes=%v: ok=%v err=%v, want false, nil", yes, r.ok, r.err)
		}
		if !strings.Contains(r.stdout, "Dry run") || !strings.Contains(r.stdout, "cost: about") {
			t.Errorf("yes=%v: preview missing from stdout: %q", yes, r.stdout)
		}
	}

	r := ask(t, context.Background(), Options{DryRun: true, JSON: true}, "", true)
	var env map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &env); err != nil || env["status"] != "dry_run" {
		t.Fatalf("dry-run JSON = %q (%v)", r.stdout, err)
	}
}

func TestAsk_NoPromptOnTTY(t *testing.T) {
	r := ask(t, context.Background(), Options{NoPrompt: true}, "y\n", true)
	if got := clerrors.ExitCode(r.err); got != clerrors.ExitConfirmationRequired {
		t.Fatalf("exit code = %d, want 3", got)
	}
	if strings.Contains(r.stderr, "[y/N]") {
		t.Errorf("--non-interactive must not prompt: %q", r.stderr)
	}
}

func TestAsk_InterruptAtPrompt(t *testing.T) {
	pr, pw := io.Pipe() // never written: the user does not answer
	defer pw.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ok, err := Ask(ctx, testPreview, Options{In: pr, IsTTY: func() bool { return true }})
	if ok || clerrors.ExitCode(err) != clerrors.ExitInterrupted {
		t.Fatalf("ok=%v err=%v, want interrupted", ok, err)
	}
}
