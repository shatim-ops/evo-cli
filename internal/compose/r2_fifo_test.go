// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package compose

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	clierr "github.com/shatim-ops/evo-cli/internal/errors"
)

// Cursor r2, R2-5: a FIFO as the compose file (or compose_file of an MCP
// deploy) blocked os.ReadFile until a writer appeared, whatever the
// caller's context: a cancelled HTTP tools/call kept its slot. Now it is
// COMPOSE_INVALID at once.
func TestParse_FIFORefused(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "docker-compose.yml")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := Parse(fifo)
		done <- err
	}()
	select {
	case err := <-done:
		var ce *clierr.CLIError
		if !errors.As(err, &ce) || ce.Code != clierr.CodeComposeInvalid {
			t.Fatalf("FIFO: want COMPOSE_INVALID, got %v", err)
		}
	case <-time.After(80 * time.Millisecond):
		w, werr := os.OpenFile(fifo, os.O_WRONLY, 0)
		if werr == nil {
			_, _ = w.Write([]byte("services: {}\n"))
			_ = w.Close()
		}
		select {
		case <-done:
		case <-time.After(time.Second):
		}
		t.Fatal("Parse blocked in os.ReadFile on a FIFO; cancellation cannot release an MCP tool slot")
	}
}
