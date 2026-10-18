// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The VM and hostname are named after the project directory. "My App
// (копия)" gave the hostname "evo-My App (копия)", which is not a
// hostname (RFC 1123: letters, digits, hyphens, up to 63).
func TestDeploy_VMNameFromDirectoryIsAHostname(t *testing.T) {
	hostname := regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	for _, dirName := range []string{"My App (копия)", "проект", "a_b.c", strings.Repeat("x", 80), "-x-"} {
		t.Run(dirName, func(t *testing.T) {
			cloud := &mockCloudAPI{}
			eng, base := newTestEngine(t, cloud, newMockDeployer())
			eng.StateDir = filepath.Join(base, dirName)
			if err := os.MkdirAll(eng.StateDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := eng.Deploy(context.Background(), newDeployOpts(t, eng.StateDir)); err != nil {
				t.Fatalf("Deploy: %v", err)
			}
			req := cloud.createReq
			if !hostname.MatchString(req.Name) {
				t.Errorf("VM name %q is not a hostname", req.Name)
			}
			if h := req.ImageMetadata["hostname"]; !hostname.MatchString(h) {
				t.Errorf("hostname %q is not a hostname", h)
			}
		})
	}
}
