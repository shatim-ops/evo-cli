// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

func writeCompose(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// UDP ports are opened as UDP, loopback-bound ports are not opened at all,
// and a UDP rule on a port does not count as the TCP port being open.
func TestDeploy_OpensPortsByProtocolAndBinding(t *testing.T) {
	cloud := &mockCloudAPI{
		listSecurityGroupsFn: func(context.Context, string) ([]*provision.SecurityGroup, error) {
			return []*provision.SecurityGroup{{ID: "sg-1", Name: "SSH-access_ru-moscow-1", Rules: []provision.SGRule{
				{Direction: "ingress", IPProtocol: "udp", PortRange: "8080:8080"},
				{Direction: "ingress", IPProtocol: "tcp", PortRange: "443:443"},
			}}}, nil
		},
	}
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	var errBuf bytes.Buffer
	eng.Printer = output.NewWithWriters(&bytes.Buffer{}, &errBuf, output.ModePlain, false)
	opts := newDeployOpts(t, dir)
	opts.ComposeFile = writeCompose(t, dir, `services:
  web:
    image: nginx
    ports:
      - "8080:80"
      - "443:443"
      - "127.0.0.1:9000:9000"
      - "${ADMIN_PORT}:9001"
  dns:
    image: coredns
    ports:
      - "53:53/udp"
`)
	probe := &stubProbe{}
	eng.Probe = probe.Check
	if err := eng.Deploy(context.Background(), opts); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	var got []string
	for _, r := range cloud.addedRules {
		got = append(got, r.IPProtocol+" "+r.PortRange)
	}
	sort.Strings(got)
	want := []string{"tcp 8080:8080", "udp 53:53"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("rules added = %v, want %v", got, want)
	}
	// Only public TCP ports are probed over HTTP.
	if len(probe.ports) != 2 || probe.ports[0] != 443 || probe.ports[1] != 8080 {
		t.Errorf("probed %v, want [443 8080]", probe.ports)
	}
	if !strings.Contains(errBuf.String(), "ADMIN_PORT") {
		t.Errorf("no warning about the unresolved port variable:\n%s", errBuf.String())
	}
}
