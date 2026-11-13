// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"net"
	"testing"
	"time"
)

// The engine the CLI builds checks port 22 of a VM before the question of
// an update (ReachSSH). In a test it connects nowhere: the worlds answer
// with documentation addresses, and a network that accepts TCP to them (a
// proxy, a VPN) would change a test's answer — the guard «no connection off
// loopback» of «Процесс · 1.9 (3)» found 32 connects of cli.test to
// 203.0.113.10:22. A port that answers here is not connected to.
func TestCLIEngine_ChecksPort22WithoutAConnect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	withCloud(t, nil)
	e, err := buildEngine(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// A connect to the listener would succeed: an error is the check that
	// made none.
	if err := e.SSHDial(ctx, ln.Addr().String()); err == nil {
		t.Error("the engine of the CLI connected to a port that answers: a test's check of port 22 goes to the network")
	}
}
