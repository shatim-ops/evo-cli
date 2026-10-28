// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var updateContractMap = flag.Bool("update-contract-map", false, "rewrite docs/internal/contract-map.json from the declarations")

const contractMapPath = "../../docs/internal/contract-map.json"

// axisEntry is one tool of the map: what the declarations of the axes of
// CLAUDE.md §6 say of it.
type axisEntry struct {
	// Every tool here asks for consent (Mutating). Destructive: it may
	// delete or overwrite data. Axis 1: a create's name is checked against
	// what was there before (taken: "checked", "adopts: <item>", "not
	// reached: <why>").
	Destructive bool   `json:"destructive"`
	Taken       string `json:"taken,omitempty"`
	// Axis 3: what a create makes and where it is found and deleted; what
	// a delete names and in which states the platform refuses it.
	Creates *mapCreate `json:"creates,omitempty"`
	Deletes *mapDelete `json:"deletes,omitempty"`
}

type mapCreate struct {
	ID     string `json:"id,omitempty"`
	List   string `json:"list,omitempty"`
	Delete string `json:"delete,omitempty"`
	None   string `json:"none,omitempty"`
}

type mapDelete struct {
	Arg      string   `json:"arg"`
	Parent   string   `json:"parent,omitempty"`
	Refusals []string `json:"refusals,omitempty"`
	NoRefuse string   `json:"no_refusal,omitempty"`
}

// contractMap is the map of every tool that changes the cloud.
func contractMap() map[string]axisEntry {
	r := New(nil)
	out := map[string]axisEntry{}
	for _, tl := range r.Tools() {
		if !tl.Mutating {
			continue
		}
		e := axisEntry{Destructive: IsDestructive(tl)}
		if c, ok := createdIDs[tl.Name]; ok {
			id := c.id
			if id == "" {
				id = c.arg
			}
			e.Creates = &mapCreate{ID: id, List: c.list, Delete: c.delete, None: c.none}
		}
		if _, ok := takenArgs[tl.Name]; ok {
			switch {
			case takenAdopts[tl.Name] != "":
				e.Taken = "adopts: " + takenAdopts[tl.Name]
			case takenNotReached[tl.Name] != "":
				e.Taken = "not reached: " + takenNotReached[tl.Name]
			default:
				e.Taken = "checked"
			}
		}
		if d, ok := deleteTargets[tl.Name]; ok {
			md := &mapDelete{Arg: d.arg, Parent: d.parent, NoRefuse: deleteRefusals[tl.Name].none}
			for _, s := range deleteRefusals[tl.Name].states {
				md.Refusals = append(md.Refusals, s.state+" ("+s.register+")")
			}
			e.Deletes = md
		}
		out[tl.Name] = e
	}
	return out
}

// Release plan 1.5, the map of the contract of CLAUDE.md §6, tool × axis:
// docs/internal/contract-map.json is what the declarations of axes 1 and 3
// say of every tool that changes the cloud (destructive, the taken name,
// createdIDs, deleteTargets, deleteRefusals), so an audit reads one file
// and a PR that changes an axis shows it in its diff. The file is built
// from the code: a stale one fails here. Axis 2 is a matter of the waits
// of functions, not of tools (TestWaits_SayWhatTheyWaitFor).
func TestContractMap_IsTheDeclarations(t *testing.T) {
	b, err := json.MarshalIndent(contractMap(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	if *updateContractMap {
		if err := os.WriteFile(filepath.FromSlash(contractMapPath), b, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	old, err := os.ReadFile(filepath.FromSlash(contractMapPath))
	// A Windows checkout may hold the file with CRLF (as docs_test does).
	old = bytes.ReplaceAll(old, []byte("\r\n"), []byte("\n"))
	if err != nil || !bytes.Equal(old, b) {
		t.Errorf("docs/internal/contract-map.json is not what the declarations say: go test ./internal/tools -run TestContractMap -update-contract-map")
	}
}
