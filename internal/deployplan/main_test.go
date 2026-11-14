// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package deployplan

import (
	"context"
	"os"
	"testing"
	"time"
)

// Fake clouds answer with documentation addresses (203.0.113.0/24): the
// wait for SSH is cut to a moment, and its check gets no answer without a
// connect — a network that answers those addresses (a proxy, a VPN) does
// not decide a test (#1707).
func TestMain(m *testing.M) {
	SSHPortWait = 10 * time.Millisecond
	DialSSH = func(context.Context, string, time.Duration) error { return timeoutErr{} }
	os.Exit(m.Run())
}
