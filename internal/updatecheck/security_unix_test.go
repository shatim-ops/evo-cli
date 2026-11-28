// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package updatecheck

import (
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
)

func TestSecurity_FIFONamedPipeDoesNotHang(t *testing.T) {
	now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	setupTestEnv(t, now)

	if err := os.MkdirAll(config.CacheDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(cachePath(), 0o600); err != nil {
		t.Fatal(err)
	}

	finished := make(chan Notice, 1)
	go func() {
		finished <- ReadCached("1.9.2")
	}()
	select {
	case n := <-finished:
		if n.HasData || n.Available {
			t.Fatalf("FIFO must be refused, got %+v", n)
		}
	case <-time.After(time.Second):
		t.Fatal("ReadCached hung on FIFO named pipe")
	}
}
