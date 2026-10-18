// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package compose

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	clierr "github.com/shatim-ops/evo-cli/internal/errors"
)

func parseString(t *testing.T, content string) (*Project, error) {
	t.Helper()
	return Parse(writeTempCompose(t, content))
}

func mustParse(t *testing.T, content string) *Project {
	t.Helper()
	p, err := parseString(t, content)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return p
}

func servicePorts(lines ...string) string {
	s := "services:\n  web:\n    image: nginx\n    ports:\n"
	for _, l := range lines {
		s += "      - " + l + "\n"
	}
	return s
}

// Project.Ports are the TCP ports reachable from outside the VM: deploy
// opens exactly them in the security group and verify probes them over HTTP.
func TestParse_ExposedPortsFollowDockerSemantics(t *testing.T) {
	cases := []struct {
		name      string
		ports     []string
		wantTCP   []int
		wantUDP   []int
		wantWarns int
	}{
		{"host:container", []string{`"8080:80"`}, []int{8080}, nil, 0},
		{"bound to all interfaces", []string{`"0.0.0.0:8080:80"`}, []int{8080}, nil, 0},
		{"bound to loopback is not public", []string{`"127.0.0.1:8080:80"`}, nil, nil, 0},
		{"ipv6 loopback is not public", []string{`"[::1]:8080:80"`}, nil, nil, 0},
		{"container port only gets a random host port", []string{`"8080"`, `9090`}, nil, nil, 0},
		{"ip with random host port", []string{`"0.0.0.0::80"`}, nil, nil, 0},
		{"udp is not tcp", []string{`"5353:53/udp"`}, nil, []int{5353}, 0},
		{"explicit tcp", []string{`"8053:53/tcp"`}, []int{8053}, nil, 0},
		{"range", []string{`"8000-8002:9000-9002"`}, []int{8000, 8001, 8002}, nil, 0},
		{"unquoted", []string{`80:80`}, []int{80}, nil, 0},
		{"duplicates collapse", []string{`"80:80"`, `"80:8080/tcp"`}, []int{80}, nil, 0},
		{"interpolation with default uses the default (VM has no such env)", []string{`"${WEB_PORT:-8081}:80"`}, []int{8081}, nil, 0},
		{"interpolation without default is reported", []string{`"${WEB_PORT}:80"`}, nil, nil, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := mustParse(t, servicePorts(tc.ports...))
			if !reflect.DeepEqual(p.Ports, tc.wantTCP) && !(len(p.Ports) == 0 && len(tc.wantTCP) == 0) {
				t.Errorf("Ports = %v, want %v", p.Ports, tc.wantTCP)
			}
			if !reflect.DeepEqual(p.UDPPorts, tc.wantUDP) && !(len(p.UDPPorts) == 0 && len(tc.wantUDP) == 0) {
				t.Errorf("UDPPorts = %v, want %v", p.UDPPorts, tc.wantUDP)
			}
			if len(p.Warnings) != tc.wantWarns {
				t.Errorf("Warnings = %q, want %d", p.Warnings, tc.wantWarns)
			}
		})
	}
}

func TestParse_LongSyntaxPorts(t *testing.T) {
	p := mustParse(t, `services:
  web:
    image: nginx
    ports:
      - target: 80
        published: "8080"
      - target: 443
        published: 8443
        protocol: tcp
      - target: 53
        published: 5353
        protocol: udp
      - target: 9000
        published: 9000
        host_ip: 127.0.0.1
      - target: 7000
`)
	if want := []int{8080, 8443}; !reflect.DeepEqual(p.Ports, want) {
		t.Errorf("Ports = %v, want %v", p.Ports, want)
	}
	if want := []int{5353}; !reflect.DeepEqual(p.UDPPorts, want) {
		t.Errorf("UDPPorts = %v, want %v", p.UDPPorts, want)
	}
}

// Ports Docker would reject fail before a paid VM is created.
func TestParse_InvalidPortsAreComposeInvalid(t *testing.T) {
	for _, port := range []string{`"0:80"`, `"70000:80"`, `"80:0"`, `"-1:80"`, `"abc:80"`, `"90-80:90-80"`, `"80-82:80-81"`, `"80:80/icmp"`, `"1.2.3.4:5:6:7"`} {
		t.Run(port, func(t *testing.T) {
			_, err := parseString(t, servicePorts(port))
			var ce *clierr.CLIError
			if !errors.As(err, &ce) || ce.Code != clierr.CodeComposeInvalid {
				t.Fatalf("err = %v, want COMPOSE_INVALID", err)
			}
			if !strings.Contains(ce.Message, "web") {
				t.Errorf("message %q does not name the service", ce.Message)
			}
		})
	}
}

func TestParse_MemoryUnits(t *testing.T) {
	cases := map[string]int{
		`"512m"`:        512,
		`"512M"`:        512,
		`"512mb"`:       512,
		`"1g"`:          1024,
		`"1gb"`:         1024,
		`"1.5G"`:        1536,
		`"2048k"`:       2,
		`"2048kb"`:      2,
		`536870912`:     512, // a bare number is bytes, as in Docker
		`"536870912"`:   512,
		`"1073741824b"`: 1024,
	}
	for mem, want := range cases {
		t.Run(mem, func(t *testing.T) {
			p := mustParse(t, "services:\n  web:\n    image: nginx\n    deploy:\n      resources:\n        limits:\n          memory: "+mem+"\n")
			if got := p.Services[0].Resources.MemoryMB; got != want {
				t.Errorf("MemoryMB = %d, want %d", got, want)
			}
		})
	}
}

// A negative, NaN or infinite limit used to reach the sizer, where the
// overflowing int conversion picked the smallest flavor.
func TestParse_InvalidResourcesAreComposeInvalid(t *testing.T) {
	for _, lim := range []string{`cpus: "-1"`, `cpus: .nan`, `cpus: "NaN"`, `cpus: "Inf"`, `cpus: 1e300`, `memory: "-512m"`, `memory: "1e300g"`, `cpus: "two"`, `memory: "lots"`} {
		t.Run(lim, func(t *testing.T) {
			_, err := parseString(t, "services:\n  web:\n    image: nginx\n    deploy:\n      resources:\n        limits:\n          "+lim+"\n")
			var ce *clierr.CLIError
			if !errors.As(err, &ce) || ce.Code != clierr.CodeComposeInvalid {
				t.Fatalf("err = %v, want COMPOSE_INVALID", err)
			}
		})
	}
}

func TestParse_CRLFSameAsLF(t *testing.T) {
	lf := servicePorts(`"8080:80"`)
	crlf := strings.ReplaceAll(lf, "\n", "\r\n")
	a, b := mustParse(t, lf), mustParse(t, crlf)
	if !reflect.DeepEqual(a.Ports, b.Ports) || len(a.Services) != len(b.Services) {
		t.Fatalf("CRLF parse differs: %v vs %v", a.Ports, b.Ports)
	}
}

func TestParse_AliasBombRejected(t *testing.T) {
	bomb := "a: &a [x,x,x,x,x,x,x,x,x,x]\n"
	prev := "a"
	for i := 0; i < 9; i++ {
		n := string(rune('b' + i))
		bomb += n + ": &" + n + " [" + strings.Repeat("*"+prev+",", 9) + "*" + prev + "]\n"
		prev = n
	}
	bomb += "services:\n  web:\n    image: nginx\n"
	_, err := parseString(t, bomb)
	var ce *clierr.CLIError
	if !errors.As(err, &ce) || ce.Code != clierr.CodeComposeInvalid {
		t.Fatalf("err = %v, want COMPOSE_INVALID", err)
	}
}

func TestParse_EmptyFile(t *testing.T) {
	for _, content := range []string{"", "\n", "---\n", "services:\n", "services: []\n", "services:\n  web:\n"} {
		_, err := parseString(t, content)
		var ce *clierr.CLIError
		if !errors.As(err, &ce) || ce.Code != clierr.CodeComposeInvalid {
			t.Errorf("%q: err = %v, want COMPOSE_INVALID", content, err)
		}
	}
}
