// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package compose

import (
	"errors"
	"math"
	"testing"

	clierr "github.com/shatim-ops/evo-cli/internal/errors"
)

// FuzzParse: any input either parses into a sane Project or fails with a
// COMPOSE_INVALID CLIError — never a panic, and never a port or resource
// value that would reach the security group or the sizer out of range.
// CI runs the seed corpus; locally: go test -fuzz=FuzzParse ./internal/compose
func FuzzParse(f *testing.F) {
	seeds := []string{
		"",
		"services:\n  web:\n    image: nginx\n    ports:\n      - \"8080:80\"\n",
		"services:\r\n  web:\r\n    image: nginx\r\n    ports:\r\n      - \"80:80\"\r\n",
		"services:\n  web:\n    image: nginx\n    ports: [\"127.0.0.1:80:80\", \"[::1]:81:81\", \"8000-8002:9000-9002\", \"53:53/udp\", 9000, \"${P:-8081}:80\", \"$P:80\"]\n",
		"services:\n  web:\n    ports:\n      - target: 80\n        published: \"8080-8081\"\n        host_ip: \"::\"\n        protocol: udp\n",
		"services:\n  web:\n    deploy:\n      resources:\n        limits:\n          cpus: \"0.5\"\n          memory: 512M\n",
		"services:\n  web:\n    deploy:\n      resources:\n        limits:\n          cpus: .nan\n          memory: 1e300g\n",
		"x: &a [1,2]\ny: *a\nservices: {web: {image: redis, depends_on: {db: {condition: service_healthy}}}}\n",
		"services:\n  web:\n    ports: \"80:80\"\n",
		"services: [web]\n",
		"- just\n- a list\n",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := parseData(data)
		if err != nil {
			var ce *clierr.CLIError
			if !errors.As(err, &ce) || ce.Code != clierr.CodeComposeInvalid {
				t.Fatalf("error is not COMPOSE_INVALID: %v", err)
			}
			return
		}
		if len(p.Services) == 0 {
			t.Fatal("parsed without services")
		}
		for _, ports := range [][]int{p.Ports, p.UDPPorts} {
			for i, port := range ports {
				if port < 1 || port > 65535 {
					t.Fatalf("port %d out of range", port)
				}
				if i > 0 && ports[i-1] >= port {
					t.Fatalf("ports not sorted and unique: %v", ports)
				}
			}
		}
		r := p.Resources
		if math.IsNaN(r.CPUCores) || math.IsInf(r.CPUCores, 0) || r.CPUCores < 0 || r.MemoryMB < 0 {
			t.Fatalf("bad resources %+v", r)
		}
		for _, s := range p.Services {
			if s.Resources.CPUCores < 0 || s.Resources.MemoryMB < 0 {
				t.Fatalf("service %s: bad resources %+v", s.Name, s.Resources)
			}
			for _, m := range s.Ports {
				if m.Container < 1 || m.Container > 65535 || m.Host < 0 || m.Host > 65535 {
					t.Fatalf("service %s: bad mapping %+v", s.Name, m)
				}
			}
		}
	})
}
