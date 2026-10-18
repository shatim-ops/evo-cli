// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package compose

import (
	"strings"
	"testing"
)

// guide §5: build:, env_file: and local volumes do not reach the VM — Parse
// warns, so that the deploy preview shows it (QA r4 Q2, test T-C1).
func TestParse_WarnsAboutLocalOnlyKeys(t *testing.T) {
	path := writeTempCompose(t, `
services:
  web:
    image: nginx
    build: .
    env_file: .env
    volumes:
      - ./data:/data
    ports:
      - "8080:80"
`)
	proj, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(proj.Warnings, "\n")
	for _, key := range []string{"build", "env_file", "volumes"} {
		if !strings.Contains(all, key) {
			t.Errorf("no warning about %s; warnings: %q", key, proj.Warnings)
		}
	}
}

func TestParse_LocalOnlyWarnings(t *testing.T) {
	path := writeTempCompose(t, `
services:
  app:
    image: app:1
    env_file:
      - common.env
      - path: ./local.env
        required: false
    volumes:
      - data:/var/lib/data
      - /var/run/docker.sock:/var/run/docker.sock
      - /cache
      - ~/certs:/certs:ro
      - type: bind
        source: ../shared
        target: /shared
      - type: volume
        source: logs
        target: /logs
  db:
    image: postgres:16
secrets:
  db_password:
    file: ./db_password.txt
  api_key:
    environment: API_KEY
configs:
  nginx:
    file: ./nginx.conf
  inline:
    content: "x"
volumes:
  data:
  logs:
`)
	proj, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(proj.Warnings, "\n")
	for _, want := range []string{"common.env, ./local.env", "~/certs:/certs:ro", "../shared",
		"configs.nginx", "./nginx.conf", "secrets.db_password", "./db_password.txt"} {
		if !strings.Contains(all, want) {
			t.Errorf("warnings lack %q:\n%s", want, all)
		}
	}
	for _, unwanted := range []string{"data:/var/lib/data", "docker.sock", "/cache", "logs", "api_key", "inline", `"db"`, "build"} {
		if strings.Contains(all, unwanted) {
			t.Errorf("warnings mention %q:\n%s", unwanted, all)
		}
	}
	if len(proj.Warnings) != 5 {
		t.Errorf("want 5 warnings (env_file, 2 volumes, config, secret), got %d:\n%s", len(proj.Warnings), all)
	}
}

func TestParse_NoWarningsForPlainImages(t *testing.T) {
	path := writeTempCompose(t, `
services:
  web:
    image: nginx
    environment:
      MODE: prod
    volumes:
      - html:/usr/share/nginx/html
    ports:
      - "80:80"
volumes:
  html:
`)
	proj, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(proj.Warnings) != 0 {
		t.Errorf("warnings = %q, want none", proj.Warnings)
	}
}
