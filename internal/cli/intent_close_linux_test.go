// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/state"
	"golang.org/x/sys/unix"
)

func openIntentPTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { master.Close() })
	fd := int(master.Fd())
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { slave.Close() })
	return master, slave
}

// Use real Linux terminal descriptors for run(), so the normal Go test
// coverage counters also see the CLI's eligibility and confirmation path.
func runIntentClosePTY(t *testing.T, args []string, input string, terminal string) (int, string) {
	t.Helper()
	master, slave := openIntentPTY(t)
	in, out := slave, slave
	if terminal == "split terminals" {
		_, out = openIntentPTY(t)
	}
	if terminal == "pipe" {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		if _, err := io.WriteString(writer, input+"\n"); err != nil {
			writer.Close()
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		in = reader
	} else if _, err := io.WriteString(master, input+"\n"); err != nil {
		t.Fatal(err)
	}
	code := func() int {
		oldIn, oldOut, oldErr := os.Stdin, os.Stdout, os.Stderr
		defer func() { os.Stdin, os.Stdout, os.Stderr = oldIn, oldOut, oldErr }()
		os.Stdin, os.Stdout, os.Stderr = in, out, slave
		return run(args)
	}()
	if err := slave.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(master)
	if err != nil && !errors.Is(err, unix.EIO) {
		t.Fatal(err)
	}
	return code, string(raw)
}

func TestIntentCloseLinuxTerminal(t *testing.T) {
	setupCLI(t)
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer i18n.SetLang(i18n.Lang())
	i18n.SetLang(i18n.EN)
	for _, name := range []string{"CI", "GITHUB_ACTIONS", "GITLAB_CI", "JENKINS_URL", "TF_BUILD", "BUILDKITE", "CIRCLECI", "TEAMCITY_VERSION"} {
		t.Setenv(name, "")
	}
	if err := config.SaveProfile("default", config.Profile{ProjectID: pvProject}, config.Credentials{KeyID: fakeKeyID, Secret: fakeSecret, Login: &config.LoginKey{ServiceAccountID: "receipt-account"}}); err != nil {
		t.Fatal(err)
	}
	key := &state.Maker{ServiceAccountID: "receipt-account", KeyID: config.MaskKeyID(fakeKeyID)}
	rec, err := intent.Open(intent.Dir(root), intent.Intent{Tool: "redis_create", Name: "review-me", StartedAt: time.Now(), Surface: "cli", Counts: true, Ended: true, Sent: true, Key: key, ProjectID: pvProject})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(rec.Dir, rec.ID+".json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"intent", "close", rec.ID}
	const prompt = "Enter the full receipt ID:"
	const refusal = "Closing a receipt requires a new confirmation in a local terminal."
	checkReceipt := func(t *testing.T, want int) {
		t.Helper()
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(after, original) {
			t.Fatalf("receipt changed: %v", err)
		}
		files, err := filepath.Glob(filepath.Join(config.Dir(), "intent-decisions", "*", "decision-*", "decision.json"))
		if err != nil || len(files) != want {
			t.Fatalf("decision files=%v want %d: %v", files, want, err)
		}
		s, err := intent.PreviewManual(root, rec.ID, "default")
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		marked, err := s.Marked()
		if err != nil || marked != (want == 1) {
			t.Fatalf("marked=%v decisions=%d err=%v", marked, want, err)
		}
	}
	for _, tc := range []struct{ name, flag string }{
		{"pipe", ""}, {"split terminals", ""}, {"CI", ""}, {"yes", "--yes"}, {"json", "--json"}, {"dry run", "--dry-run"}, {"non-interactive", "--non-interactive"}, {"wrong ID", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			line := args
			if tc.flag != "" {
				line = append(line, tc.flag)
			}
			if tc.name == "CI" {
				t.Setenv("GITHUB_ACTIONS", "true")
			}
			input := rec.ID
			if tc.name == "wrong ID" {
				input = rec.ID[:8]
			}
			code, transcript := runIntentClosePTY(t, line, input, tc.name)
			if code != 3 || !strings.Contains(transcript, refusal) || strings.Contains(transcript, prompt) != (tc.name == "wrong ID") {
				t.Fatalf("exit %d: %q", code, transcript)
			}
			checkReceipt(t, 0)
		})
	}
	t.Run("other project", func(t *testing.T) {
		code, transcript := runIntentClosePTY(t, append(args, "--project-id", parityVM), rec.ID, "same terminal")
		if code != 1 || !strings.Contains(transcript, "Cannot verify the owner and project") || strings.Contains(transcript, prompt) {
			t.Fatalf("exit %d: %q", code, transcript)
		}
		checkReceipt(t, 0)
	})
	// Close once with the full ID. A second invocation must report the
	// existing decision without asking again or writing a second decision.
	t.Run("full ID and repeat", func(t *testing.T) {
		code, transcript := runIntentClosePTY(t, args, rec.ID, "same terminal")
		if code != 0 || !strings.Contains(transcript, prompt) || !strings.Contains(transcript, "Local review closed. The cloud outcome is unknown.") {
			t.Fatalf("close exit=%d: %q", code, transcript)
		}
		checkReceipt(t, 1)
		code, transcript = runIntentClosePTY(t, args, "", "same terminal")
		if code != 0 || strings.Contains(transcript, prompt) || !strings.Contains(transcript, "This version was already closed locally. The cloud outcome is unknown.") {
			t.Fatalf("repeat exit=%d: %q", code, transcript)
		}
		checkReceipt(t, 1)
	})
}
