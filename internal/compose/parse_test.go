// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package compose

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	clierr "github.com/shatim-ops/evo-cli/internal/errors"
)

func writeTempCompose(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParse_Minimal(t *testing.T) {
	yaml := `
services:
  web:
    image: nginx
    ports:
      - "8080:80"
`
	path := writeTempCompose(t, yaml)
	proj, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(proj.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(proj.Services))
	}

	svc := proj.Services[0]
	if svc.Name != "web" {
		t.Errorf("expected service name 'web', got %q", svc.Name)
	}
	if svc.Image != "nginx" {
		t.Errorf("expected image 'nginx', got %q", svc.Image)
	}
	if len(svc.Ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(svc.Ports))
	}
	if svc.Ports[0].Host != 8080 || svc.Ports[0].Container != 80 {
		t.Errorf("expected 8080:80, got %d:%d", svc.Ports[0].Host, svc.Ports[0].Container)
	}
	if svc.Ports[0].Protocol != "tcp" {
		t.Errorf("expected protocol 'tcp', got %q", svc.Ports[0].Protocol)
	}

	if len(proj.Ports) != 1 || proj.Ports[0] != 8080 {
		t.Errorf("expected deduplicated ports [8080], got %v", proj.Ports)
	}
}

func TestParse_MultiService(t *testing.T) {
	yaml := `
services:
  web:
    image: nginx
    ports:
      - "8080:80"
    deploy:
      resources:
        limits:
          cpus: "0.5"
          memory: "256M"
  api:
    image: node:18
    ports:
      - "3000:3000"
    deploy:
      resources:
        limits:
          cpus: "1.0"
          memory: "512M"
  db:
    image: postgres:15
    ports:
      - "5432:5432"
    deploy:
      resources:
        limits:
          cpus: "2.0"
          memory: "1G"
`
	path := writeTempCompose(t, yaml)
	proj, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(proj.Services) != 3 {
		t.Fatalf("expected 3 services, got %d", len(proj.Services))
	}

	// Aggregated resources: 0.5 + 1.0 + 2.0 = 3.5 CPUs, 256 + 512 + 1024 = 1792 MB
	expectedCPU := 3.5
	if proj.Resources.CPUCores != expectedCPU {
		t.Errorf("expected aggregated CPU %.1f, got %.1f", expectedCPU, proj.Resources.CPUCores)
	}
	expectedMem := 1792
	if proj.Resources.MemoryMB != expectedMem {
		t.Errorf("expected aggregated memory %d MB, got %d MB", expectedMem, proj.Resources.MemoryMB)
	}

	// Deduplicated ports: 3000, 5432, 8080 (sorted)
	if len(proj.Ports) != 3 {
		t.Fatalf("expected 3 ports, got %d", len(proj.Ports))
	}
	expectedPorts := []int{3000, 5432, 8080}
	for i, p := range expectedPorts {
		if proj.Ports[i] != p {
			t.Errorf("port[%d]: expected %d, got %d", i, p, proj.Ports[i])
		}
	}
}

func TestParse_NoResources(t *testing.T) {
	yaml := `
services:
  web:
    image: nginx
    ports:
      - "8080:80"
  worker:
    image: redis
`
	path := writeTempCompose(t, yaml)
	proj, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if proj.Resources.CPUCores != 0 {
		t.Errorf("expected 0 CPU, got %f", proj.Resources.CPUCores)
	}
	if proj.Resources.MemoryMB != 0 {
		t.Errorf("expected 0 memory, got %d", proj.Resources.MemoryMB)
	}

	// Check individual services also have zero resources.
	for _, svc := range proj.Services {
		if svc.Resources.CPUCores != 0 || svc.Resources.MemoryMB != 0 {
			t.Errorf("service %s: expected zero resources, got CPU=%f Mem=%d",
				svc.Name, svc.Resources.CPUCores, svc.Resources.MemoryMB)
		}
	}
}

func TestParse_Hash(t *testing.T) {
	content := `
services:
  web:
    image: nginx
`
	path1 := writeTempCompose(t, content)

	// Write identical content to a second temp file.
	dir2 := t.TempDir()
	path2 := filepath.Join(dir2, "docker-compose.yml")
	if err := os.WriteFile(path2, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	proj1, err := Parse(path1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	proj2, err := Parse(path2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if proj1.Hash != proj2.Hash {
		t.Errorf("same content should produce same hash: %s != %s", proj1.Hash, proj2.Hash)
	}

	if len(proj1.Hash) != 64 {
		t.Errorf("expected 64-char hex SHA-256 hash, got length %d", len(proj1.Hash))
	}
}

func TestParse_MissingFile(t *testing.T) {
	_, err := Parse("/nonexistent/path/docker-compose.yml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}

	var cliErr *clierr.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected CLIError, got %T: %v", err, err)
	}
	if cliErr.Code != clierr.CodeComposeNotFound {
		t.Errorf("expected code %s, got %s", clierr.CodeComposeNotFound, cliErr.Code)
	}
}

func TestParse_InvalidYAML(t *testing.T) {
	path := writeTempCompose(t, `{{{invalid yaml!!!`)
	_, err := Parse(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}

	var cliErr *clierr.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected CLIError, got %T: %v", err, err)
	}
	if cliErr.Code != clierr.CodeComposeInvalid {
		t.Errorf("expected code %s, got %s", clierr.CodeComposeInvalid, cliErr.Code)
	}
}

func TestParse_NoServices(t *testing.T) {
	path := writeTempCompose(t, `version: "3"`)
	_, err := Parse(path)
	if err == nil {
		t.Fatal("expected error for no services")
	}

	var cliErr *clierr.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected CLIError, got %T: %v", err, err)
	}
	if cliErr.Code != clierr.CodeComposeInvalid {
		t.Errorf("expected code %s, got %s", clierr.CodeComposeInvalid, cliErr.Code)
	}
}

func TestParse_PortProtocol(t *testing.T) {
	yaml := `
services:
  dns:
    image: coredns
    ports:
      - "5353:53/udp"
      - "8053:53/tcp"
`
	path := writeTempCompose(t, yaml)
	proj, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	svc := proj.Services[0]
	if len(svc.Ports) != 2 {
		t.Fatalf("expected 2 ports, got %d", len(svc.Ports))
	}

	// Ports should be in order as defined.
	if svc.Ports[0].Host != 5353 || svc.Ports[0].Container != 53 || svc.Ports[0].Protocol != "udp" {
		t.Errorf("port[0]: expected 5353:53/udp, got %d:%d/%s",
			svc.Ports[0].Host, svc.Ports[0].Container, svc.Ports[0].Protocol)
	}
	if svc.Ports[1].Host != 8053 || svc.Ports[1].Container != 53 || svc.Ports[1].Protocol != "tcp" {
		t.Errorf("port[1]: expected 8053:53/tcp, got %d:%d/%s",
			svc.Ports[1].Host, svc.Ports[1].Container, svc.Ports[1].Protocol)
	}
}
