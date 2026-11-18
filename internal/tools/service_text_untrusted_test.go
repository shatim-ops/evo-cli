// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// IAM that refuses the roles of the key answers with its own text: whoami
// carries it in roles_note, doctor in the detail of a check — fields the
// marker names (A19-12, the release audit of 1.9 rc.1, #2432).
func TestWhoamiDoctor_TextOfTheServiceMarked(t *testing.T) {
	body, _ := json.Marshal(map[string]any{"message": hostileName})
	w := &rolesWorld{status: 422, refusal: string(body)}
	r := rolesRegistry(t, w, rolesProject, subToken{"sa-1"})
	m := whoami(t, r)
	b, _ := json.Marshal(m["roles_note"])
	if !strings.Contains(string(b), hostileMarker) {
		t.Fatalf("the world no longer brings the text of IAM to roles_note: %v", m)
	}
	if list, _ := m["_untrusted"].([]any); !slices.Contains(list, any("roles_note")) {
		t.Errorf("whoami: roles_note carries the text of IAM, \"_untrusted\" %v does not name it", list)
	}
	_, text, _ := callNamed(t, r, "doctor", map[string]any{})
	var d map[string]any
	if json.Unmarshal([]byte(text), &d) != nil || !strings.Contains(text, hostileMarker) {
		t.Fatalf("the world no longer brings the text of IAM to doctor: %.400s", text)
	}
	list, _ := d["_untrusted"].([]any)
	for f, v := range d {
		b, _ := json.Marshal(v)
		if f != "_untrusted" && strings.Contains(string(b), hostileMarker) && !slices.Contains(list, any(f)) {
			t.Errorf("doctor: %s carries the text of IAM, \"_untrusted\" %v does not name it", f, list)
		}
	}
}

// The state of an operation in a line of the wait is the cloud's text:
// quoted on one line (A19-12, #2432).
func TestOpWaitLine_StateQuoted(t *testing.T) {
	s := opState(map[string]any{"status": hostileName})
	if strings.ContainsAny(s, "\n\r") || s != quoteCloud(hostileName) {
		t.Errorf("the state of the operation is not quoted on one line: %q", s)
	}
	if s := opState(map[string]any{"done": false}); s != "done=false" {
		t.Errorf("evo's own word is quoted: %q", s)
	}
}

// hostileCatalog is catalogCloud whose image card, fields and zones are
// named by the cloud's text.
type hostileCatalog struct{ catalogCloud }

func (hostileCatalog) GetImage(_ context.Context, id string) (*provision.ImageDetail, error) {
	return &provision.ImageDetail{ID: id, Name: "astra_1.8.1", Type: "marketplace",
		MetadataFields: []provision.ImageMetadataField{
			{Name: hostileName, FieldType: hostileName, DefaultValue: hostileName, MandatoryGroup: hostileName, DisplayName: hostileName},
		},
		AvailZones: []provision.ImageAvailZone{{AvailabilityZoneID: catZone, AvailabilityZoneName: hostileName, State: hostileName}}}, nil
}

// image_get answers the fields of the catalog and the states of the image
// in its zones as the catalog has them: every field of the answer with the
// cloud's text is named in "_untrusted" (A19-12, #2432).
func TestImageGet_CatalogValuesMarked(t *testing.T) {
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{Config: config.Config{ProjectID: "00000000-0000-0000-0000-0000000000f1", KeyID: "k", Secret: "s"},
			Auth: staticToken{}, Cloud: hostileCatalog{}, Printer: output.NewWithWriter(&buf, output.ModeText, false)}, nil
	})
	r.SetStateDir(t.TempDir())
	m, text, isErr := callNamed(t, r, "image_get", map[string]any{"image_id": catImg1})
	if isErr || !strings.Contains(text, hostileMarker) {
		t.Fatalf("the world no longer brings the catalog's text to image_get: %.400s", text)
	}
	list, _ := m["_untrusted"].([]any)
	for f, v := range m {
		b, _ := json.Marshal(v)
		if f != "_untrusted" && strings.Contains(string(b), hostileMarker) && !slices.Contains(list, any(f)) {
			t.Errorf("image_get: %s carries the catalog's text, \"_untrusted\" %v does not name it", f, list)
		}
	}
}
