// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"debug/buildinfo"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/product"
)

func TestResolveVersion_LdflagsWin(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{Version: "v9.9.9"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "fromvcs"},
			{Key: "vcs.time", Value: "2000-01-01T00:00:00Z"},
		},
	}
	v, c, d := resolveVersion("1.0.0-rc.1", "abc123", "2026-10-14T00:00:00Z", info)
	if v != "1.0.0-rc.1" || c != "abc123" || d != "2026-10-14T00:00:00Z" {
		t.Errorf("ldflags values overwritten: %q %q %q", v, c, d)
	}
}

func TestResolveVersion_FromBuildInfo(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{Version: "v1.2.3"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "deadbeef"},
			{Key: "vcs.time", Value: "2026-10-01T10:00:00Z"},
		},
	}
	v, c, d := resolveVersion("dev", "none", "unknown", info)
	if v != "1.2.3" || c != "deadbeef" || d != "2026-10-01T10:00:00Z" {
		t.Errorf("got %q %q %q", v, c, d)
	}
}

func TestResolveVersion_DevelAndNoInfoKeepDefaults(t *testing.T) {
	for _, info := range []*debug.BuildInfo{nil, {Main: debug.Module{Version: "(devel)"}}} {
		v, c, d := resolveVersion("dev", "none", "unknown", info)
		if v != "dev" || c != "none" || d != "unknown" {
			t.Errorf("info %+v: got %q %q %q", info, v, c, d)
		}
	}
}

var testVersion = versionInfo{
	Product: product.Name, Author: product.AuthorEN, Version: "1.0.0-rc.1", Commit: "abc123", Date: "2026-10-14T12:00:00Z",
	Go: "go1.26.9", OS: "linux", Arch: "amd64",
}

func TestPrintVersion_Text(t *testing.T) {
	var buf bytes.Buffer
	if err := printVersion(&buf, false, testVersion); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{"evo CMCP 1.0.0-rc.1\n", "abc123", "2026-10-14T12:00:00Z", "go1.26.9", "linux/amd64"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func TestPrintVersion_JSON(t *testing.T) {
	var buf bytes.Buffer
	if err := printVersion(&buf, true, testVersion); err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not one JSON object: %v: %q", err, buf.String())
	}
	want := map[string]string{
		"product": "evo CMCP", "author": "Timur Shakirbekov, Cloud.ru", "version": "1.0.0-rc.1", "commit": "abc123", "date": "2026-10-14T12:00:00Z",
		"go": "go1.26.9", "os": "linux", "arch": "amd64",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("unexpected keys: %v", got)
	}
}

func TestRun_VersionExitsZero(t *testing.T) {
	if got := run([]string{"version", "--json"}); got != 0 {
		t.Errorf("run(version --json) = %d, want 0", got)
	}
}

func TestSetBuildIdentity_KeepsLdflags(t *testing.T) {
	v, c, d := version, commit, date
	t.Cleanup(func() { version, commit, date = v, c, d })
	setBuildIdentity("1.0.0-rc.4", "abc123", "2026-10-16T00:00:00Z")
	if version != "1.0.0-rc.4" || commit != "abc123" || date != "2026-10-16T00:00:00Z" {
		t.Errorf("got %q %q %q", version, commit, date)
	}
}

// docs/install.md: "go build ./cmd/evo" from a checkout reports the commit
// of the VCS data; Main used to overwrite it with "none" (QA r4 Q4, T-V1).
func TestBuiltBinaryReportsVCSCommit(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go in PATH")
	}
	name := "evo"
	if runtime.GOOS == "windows" {
		name += ".exe" // exec does not find a binary without it there
	}
	bin := filepath.Join(t.TempDir(), name)
	if out, err := exec.Command(goBin, "build", "-o", bin, "../../cmd/evo").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	info, err := buildinfo.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	var revision string
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			revision = s.Value
		}
	}
	if revision == "" {
		t.Skip("built without VCS data (not a checkout)")
	}
	cmd := exec.Command(bin, "version", "--json")
	cmd.Env = append(os.Environ(), "EVO_LANG=en")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var got versionInfo
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %q", err, out)
	}
	if got.Commit != revision || got.Date == "unknown" || got.Version == "dev" {
		t.Errorf("version --json = %s; want commit %s, the VCS time and the module version", out, revision)
	}
}
