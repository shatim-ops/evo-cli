// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Zone and image names come from the API and go into the YAML init writes.
// A newline in one must not add a key to the user's project file.
func TestInit_CloudNamesCannotAddConfigKeys(t *testing.T) {
	withPromptStdin(t, strings.NewReader(""), false)
	eng, path, _ := initEngine(t)
	cloud := eng.Cloud.(*mockCloudAPI)
	cloud.listAvailabilityZonesFn = func(context.Context) ([]*provision.AvailabilityZone, error) {
		return []*provision.AvailabilityZone{{ID: "az-1-id", Name: "ru.AZ-1\ncompose_file: /etc/passwd"}}, nil
	}
	if err := eng.Init(context.Background(), InitOptions{Path: path, NonInteractive: true}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fc struct {
		ComposeFile string `yaml:"compose_file"`
	}
	if err := yaml.Unmarshal(data, &fc); err != nil {
		t.Fatalf("generated config is not YAML: %v\n%s", err, data)
	}
	if fc.ComposeFile != "docker-compose.yml" {
		t.Errorf("compose_file = %q, injected through a zone name:\n%s", fc.ComposeFile, data)
	}
}
