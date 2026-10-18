// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package compose

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

func breakIsolate(t *testing.T) {
	t.Helper()
	t.Setenv("CLOUDRU_CONFIG_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, k := range []string{
		"CLOUDRU_KEY_ID", "CLOUDRU_SECRET", "CLOUDRU_KEY_SECRET", "CLOUDRU_PROJECT_ID",
		"CLOUDRU_API_KEY", "EVOLUTION_PROJECT_ID", "EVOLUTION_SERVICE_ACCOUNT_KEY_ID",
		"EVOLUTION_SERVICE_ACCOUNT_KEY_SECRET", "MCP_BEARER_TOKEN", "NO_COLOR", "EVO_LANG",
	} {
		t.Setenv(k, "")
	}
}

// Parse uses os.ReadFile. A FIFO in the project (or compose_file of an MCP
// deploy) blocks until a writer appears. The caller's context is not
// consulted, so cancelling the HTTP tools/call does not return the slot.
func TestBreak_Compose_FIFOBlocksRead(t *testing.T) {
	breakIsolate(t)
	if runtime.GOOS == "windows" {
		t.Skip("FIFOs are not supported")
	}
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
		if err == nil {
			t.Fatal("FIFO was parsed as a compose file")
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
