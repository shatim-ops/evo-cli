// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// zonesCloud lists two zones and counts the lists; block holds a list
// until the call's context ends.
type zonesCloud struct {
	cancelCloud
	lists atomic.Int32
	block bool
}

func (c *zonesCloud) ListAvailabilityZones(ctx context.Context) ([]*provision.AvailabilityZone, error) {
	c.lists.Add(1)
	if c.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return []*provision.AvailabilityZone{{ID: "00000000-0000-4000-8000-0000000000a1", Name: "ru.AZ-1"},
		{ID: "00000000-0000-4000-8000-0000000000a2", Name: "ru.AZ-2"}}, nil
}

func completionRegistry(t *testing.T, cloud provision.CloudAPI, cfg config.Config) *Registry {
	t.Helper()
	t.Setenv(config.EnvConfigDir, t.TempDir())
	return New(func(map[string]any) (*engine.Engine, error) {
		return &engine.Engine{Config: cfg, Auth: staticToken{}, Cloud: cloud}, nil
	})
}

var completionCfg = config.Config{ProjectID: "p", KeyID: "k", Secret: "s"}

// Release plan 1.5, Tab: a reference is completed from its list, narrowed
// by what is typed (case aside); the list is read once for minutes, from a
// cache only the user reads, holding ids and names alone.
func TestComplete_NarrowsByPrefixAndCaches(t *testing.T) {
	cloud := &zonesCloud{cancelCloud: cancelCloud{cancel: func() {}}}
	r := completionRegistry(t, cloud, completionCfg)
	got := r.Complete(context.Background(), "provision", "region", "ru.az-2")
	if len(got) != 1 || got[0].Value != "ru.AZ-2" {
		t.Fatalf("region ru.az-2+Tab: %v, want ru.AZ-2 alone", got)
	}
	if all := r.Complete(context.Background(), "provision", "region", ""); len(all) != 2 || cloud.lists.Load() != 1 {
		t.Errorf("a second Tab: %v after %d lists, want both zones from the cache", all, cloud.lists.Load())
	}
	files, _ := filepath.Glob(filepath.Join(config.CacheDir(), "completion", "*.json"))
	if len(files) != 1 {
		t.Fatalf("cache files %v, want one", files)
	}
	if fi, _ := os.Stat(files[0]); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("cache file mode %v, want 0600", fi.Mode().Perm())
	}
	// An id argument offers ids with the name beside them.
	if ids := r.Complete(context.Background(), "provision", "availability_zone_id", "0"); len(ids) != 2 || ids[0].Note != "ru.AZ-1" {
		t.Errorf("availability_zone_id: %v, want both ids with their names", ids)
	}
}

// A cloud that does not answer: nothing within CompletionTimeout, not a
// shell that hangs. No key: nothing, without a request.
func TestComplete_SilentWithoutAnswerOrKey(t *testing.T) {
	old := CompletionTimeout
	CompletionTimeout = 50 * time.Millisecond
	t.Cleanup(func() { CompletionTimeout = old })
	cloud := &zonesCloud{cancelCloud: cancelCloud{cancel: func() {}}, block: true}
	r := completionRegistry(t, cloud, completionCfg)
	done := make(chan []Candidate, 1)
	go func() { done <- r.Complete(context.Background(), "provision", "region", "") }()
	select {
	case got := <-done:
		if len(got) != 0 {
			t.Errorf("a cloud that does not answer: %v, want none", got)
		}
	case <-time.After(5 * time.Second): // wall-clock: a hang, 100× the bound under test
		t.Fatal("Tab hangs on a cloud that does not answer")
	}

	cloud = &zonesCloud{cancelCloud: cancelCloud{cancel: func() {}}}
	r = completionRegistry(t, cloud, config.Config{ProjectID: "p"})
	if got := r.Complete(context.Background(), "provision", "region", ""); len(got) != 0 || cloud.lists.Load() != 0 {
		t.Errorf("no key: %v after %d lists, want none and no request", got, cloud.lists.Load())
	}
}

// The declarations are closed: each list completes from a read without a
// required argument, each exception names an argument the tool has.
func TestCompletion_DeclarationsHold(t *testing.T) {
	r := New(nil)
	for arg, c := range valueRefs {
		if !r.listable(c.list) {
			t.Errorf("valueRefs[%s]: %s is not a read without a required argument", arg, c.list)
		}
	}
	for tool, args := range noCompletion {
		tl, ok := r.Lookup(tool)
		if !ok {
			t.Errorf("noCompletion names %s, which is no tool", tool)
			continue
		}
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for arg := range args {
			if _, ok := props[arg]; !ok {
				t.Errorf("noCompletion[%s] names %s, which the tool does not take", tool, arg)
			}
		}
	}
	// Every kind's list is a tool (a typo would leave its ids without Tab).
	for tool, args := range idArgs {
		for _, a := range args {
			if a.kind == nil || a.kind.listTool == "" {
				continue
			}
			if _, ok := r.Lookup(a.kind.listTool); !ok {
				t.Errorf("idArgs[%s] %s: the list %s of its kind is no tool", tool, a.name, a.kind.listTool)
			}
		}
	}
	// Mutating and secret tools are never read for Tab.
	for _, tl := range r.Tools() {
		if (tl.Mutating || IsSensitive(tl)) && r.listable(tl.Name) {
			t.Errorf("%s would be called for Tab", tl.Name)
		}
	}
}

// The list Tab takes is the one whose items carry the value, not another
// list beside it in the answer (Qwen review of #478).
func TestCompletionItems_TakesTheListWithTheField(t *testing.T) {
	items := completionItems(`{"a_notes":[{"text":"x"}],"zones":[{"id":"z1","name":"ru.AZ-1"}]}`, "name")
	if len(items) != 1 || items[0]["name"] != "ru.AZ-1" {
		t.Errorf("items %v, want the zones", items)
	}
}
