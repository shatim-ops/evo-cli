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

type diskWaitReadCloud struct {
	partialDiskCloud
	rows           []map[string]any
	readError      bool
	hasStatusAlias bool
}

func (c *diskWaitReadCloud) CreateDisk(ctx context.Context, req provision.CreateDiskReq) (map[string]any, error) {
	disk, err := c.partialDiskCloud.CreateDisk(ctx, req)
	if c.hasStatusAlias {
		disk["status"] = "creating"
	}
	return disk, err
}

func (c *diskWaitReadCloud) ListDisks(context.Context, string) ([]map[string]any, error) {
	if c.creates == 0 {
		return nil, nil
	}
	if c.readError {
		return nil, &provision.StatusError{Status: http.StatusForbidden}
	}
	return c.rows, nil
}

func TestDiskCreate_PartialLastRead(t *testing.T) {
	ru(t)
	const otherDisk = "00000000-0000-4000-8000-0000000000d2"
	for _, tc := range []struct {
		name           string
		rows           []map[string]any
		readError      bool
		wantState      string
		wantAttaches   int
		hasStatusAlias bool
	}{
		{name: "creation-failed", rows: []map[string]any{{"id": partialDiskID, "name": "data", "state": "error_creating"}}, wantState: "error_creating"},
		{name: "attach-refused", rows: []map[string]any{{"id": partialDiskID, "name": "data", "state": "available"}}, wantState: "available", wantAttaches: 1},
		{name: "status-alias", rows: []map[string]any{{"id": partialDiskID, "name": "data", "status": "available"}}, wantState: "available", wantAttaches: 1, hasStatusAlias: true},
		{name: "list-refused", readError: true, hasStatusAlias: true},
		{name: "no-state-for-this-id", rows: []map[string]any{{"id": otherDisk, "name": "other", "state": "available"}, {"id": partialDiskID, "name": "data"}}},
	} {
		for _, mode := range []string{"json", "text"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				setupCLI(t)
				saveProfile(t, "default", pvProject)
				t.Setenv("CLOUDRU_TIMEOUT", "1")
				i18n.SetLang(i18n.RU)
				c := &diskWaitReadCloud{rows: tc.rows, readError: tc.readError, hasStatusAlias: tc.hasStatusAlias}
				withCloud(t, c)
				args := []string{"disk", "create", "data", "--size-gb", "10", "--disk-type", "SSD", "--attach-to", "web", "--yes"}
				if mode == "json" {
					args = append(args, "--json")
				}
				code, stdout, stderr := runCLI(t, "", args...)
				if code != clerrors.ExitAPI || stdout != "" || c.creates != 1 || c.attaches != tc.wantAttaches {
					t.Fatalf("exit %d, stdout %q, creates %d, attaches %d: %s", code, stdout, c.creates, c.attaches, stderr)
				}
				message, _ := personText(stderr)
				if mode == "json" {
					var envelope struct {
						Error output.ErrorBody `json:"error"`
					}
					if err := json.Unmarshal([]byte(lastLine(stderr)), &envelope); err != nil {
						t.Fatal(err)
					}
					e := envelope.Error
					if e.Reason != clerrors.CodeAPIError || e.Details["status"] != "partial" || e.Details["disk_id"] != partialDiskID {
						t.Fatalf("wrong partial: %+v", e)
					}
					disk, ok := e.Details["disk"].(map[string]any)
					if !ok || disk["id"] != partialDiskID {
						t.Fatalf("wrong disk identity: %#v", disk)
					}
					state, hasState := disk["state"]
					alias, hasAlias := disk["status"]
					wantState := tc.wantState != ""
					if hasState != wantState || (hasState && state != tc.wantState) {
						t.Errorf("disk state %v (present %t), want %q: %#v", state, hasState, tc.wantState, disk)
					}
					if hasAlias != (wantState && tc.hasStatusAlias) || (hasAlias && alias != tc.wantState) {
						t.Errorf("disk status alias disagrees with last read or receipt: %#v", disk)
					}
					message = e.Message
				}
				var want string
				switch {
				case tc.wantState != "":
					want = `"` + tc.wantState + `"`
				case mode == "json":
					want = "disk state could not be read"
				default:
					want = "состояние диска прочитать не удалось"
				}
				if !strings.Contains(strings.ToLower(message), want) || !strings.Contains(message, partialDiskID) {
					t.Errorf("message %q lacks %q or disk ID", message, want)
				}
				if tc.wantState != "" {
					code, _, stderr = runCLI(t, "", args...)
					if code == 0 || c.creates != 1 || c.attaches != tc.wantAttaches {
						t.Fatalf("repeat: exit %d, creates %d, attaches %d: %s", code, c.creates, c.attaches, stderr)
					}
				}
			})
		}
	}
}
