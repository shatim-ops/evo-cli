// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

const partialDiskID = "00000000-0000-4000-8000-0000000000d1"

type partialDiskCloud struct {
	previewCloud
	state             string
	creates, attaches int
}

func (c *partialDiskCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return []*provision.VM{{ID: parityVM, Name: "web", Region: pvZone}}, nil
}

func (c *partialDiskCloud) CreateDisk(context.Context, provision.CreateDiskReq) (map[string]any, error) {
	c.creates++
	return map[string]any{"id": partialDiskID, "name": "data", "state": "creating"}, nil
}

func (c *partialDiskCloud) ListDisks(context.Context, string) ([]map[string]any, error) {
	if c.creates == 0 {
		return nil, nil
	}
	return []map[string]any{{"id": partialDiskID, "name": "data", "state": c.state}}, nil
}

func (c *partialDiskCloud) AttachDisk(context.Context, string, string, string) (map[string]any, error) {
	c.attaches++
	return nil, &provision.StatusError{Status: http.StatusForbidden, Body: "fixture denied"}
}

// The disk already exists and is billable in both failures. A person needs
// their own language; JSON keeps English under either input locale (D6).
func TestDiskCreate_PartialLanguage(t *testing.T) {
	old := i18n.Lang()
	t.Cleanup(func() { i18n.SetLang(old) })
	for _, failure := range []struct {
		name, state, cause string
		attaches           int
	}{
		{"wait-timeout", "creating", "creating", 0},
		{"attach-forbidden", "available", "HTTP 403: fixture denied", 1},
	} {
		for _, surface := range []struct{ lang, mode string }{
			{"ru", "text"}, {"en", "text"}, {"ru", "json"}, {"en", "json"},
		} {
			t.Run(failure.name+"/"+surface.lang+"/"+surface.mode, func(t *testing.T) {
				setupCLI(t)
				saveProfile(t, "default", pvProject)
				t.Setenv("CLOUDRU_TIMEOUT", "1")
				c := &partialDiskCloud{state: failure.state}
				withCloud(t, c)
				i18n.SetLang(surface.lang)
				args := []string{"disk", "create", "data", "--size-gb", "10", "--disk-type", "SSD", "--attach-to", "web", "--yes"}
				jsonMode := surface.mode == "json"
				if jsonMode {
					args = append(args, "--json")
				}
				code, out, errOut := runCLI(t, "", args...)
				if code != clerrors.ExitAPI || out != "" || c.creates != 1 || c.attaches != failure.attaches {
					t.Fatalf("exit %d, stdout %q, creates %d, attaches %d: %s", code, out, c.creates, c.attaches, errOut)
				}

				var msg, hint string
				if jsonMode {
					var envelope struct {
						Error output.ErrorBody `json:"error"`
					}
					if err := json.Unmarshal([]byte(lastLine(errOut)), &envelope); err != nil {
						t.Fatal(err)
					}
					e := envelope.Error
					if e.Code != code || e.Reason != clerrors.CodeAPIError || e.Details["status"] != "partial" || e.Details["disk_id"] != partialDiskID {
						t.Fatalf("wrong partial result: %+v", e)
					}
					msg, hint = e.Message, e.Hint
				} else {
					msg, hint = personText(errOut)
				}

				m := strings.ToLower(msg)
				action := strings.NewReplacer("evo disk attach", "disk_attach", "evo disk list", "disk_list").Replace(strings.ToLower(hint))
				if !strings.Contains(msg, partialDiskID) || !strings.Contains(msg, failure.cause) ||
					!strings.Contains(action, "disk_attach") || !strings.Contains(action, "disk_list") {
					t.Errorf("identity, cause or next action missing: %q / %q", msg, hint)
				}
				if surface.lang == i18n.RU && !jsonMode {
					if !strings.Contains(m, "создан") || !(strings.Contains(m, "плат") || strings.Contains(m, "начисл")) ||
						!(strings.Contains(m, "не подключ") || strings.Contains(m, "не привязан") || strings.Contains(m, "не удалось")) ||
						!(strings.Contains(action, "подключ") || strings.Contains(action, "привя") || strings.Contains(action, "присоедин")) ||
						strings.Contains(m, "did not become available") || strings.Contains(action, "attach it with") {
						t.Errorf("expected a Russian explanation of the remaining billable disk, failed attachment and next action: %q / %q", msg, hint)
					}
				} else if !strings.Contains(m, "created") || !strings.Contains(m, "bill") || !strings.Contains(m, "not attached") || hasCyrillic(msg+hint) {
					t.Errorf("expected an English explanation of the remaining billable disk and failed attachment: %q / %q", msg, hint)
				}
			})
		}
	}
}
