// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// emptyArg finds a command of evo whose argument is missing: "get  —",
// "get ." or a command at the end of the text.
var emptyArg = regexp.MustCompile(`evo (kafka|redis|pg) (get|operation)(\s{2,}|\s*[—.;,]|\s*$)`)

// The Russian notes of the creates of Kafka and Redis name commands of
// the CLI with the ids of the operation and of the cluster. An operation
// the cloud answers without the cluster's id gave «evo kafka get  — до
// статуса READY»: a command with an empty argument; the note names the
// way to the id by name instead, as pg_create does (N-07). The ids are
// the cloud's and go into evo's text quoted (quoteCloud) («Риск»,
// conclusion 243, У1).
func TestRussianNotes_DbaasCreateNamesNoEmptyArgument(t *testing.T) {
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	for _, withID := range []bool{true, false} {
		answer := `{"id":"op-1","done":false}`
		if withID {
			answer = `{"id":"op-1","resource_id":"` + auditUUID + `","done":false}`
		}
		own := func(w http.ResponseWriter, r *http.Request) bool {
			if r.Method == http.MethodPost && r.URL.Path == "/v1/clusters" {
				_, _ = io.WriteString(w, answer)
				return true
			}
			return false
		}
		for tool, args := range map[string]map[string]any{
			"kafka_create": {"name": "k", "cpu": 2, "ram": 4, "confirmed": true},
			"redis_create": {"name": "cache", "cpu": 2, "ram": 4, "confirmed": true},
		} {
			r := apiFakeRegistry(t, &apiFake{own: own})
			m, text, isErr := callNamed(t, r, tool, args)
			if isErr || m["status"] != "creating" {
				t.Fatalf("%s (cluster id %v): %s", tool, withID, text)
			}
			note, _ := m["note"].(string)
			svc := strings.TrimSuffix(tool, "_create")
			if emptyArg.MatchString(note) {
				t.Errorf("%s (cluster id %v): a command with an empty argument: %s", tool, withID, note)
			}
			if !strings.Contains(note, "evo "+svc+" operation "+strconv.Quote("op-1")) {
				t.Errorf("%s (cluster id %v): the operation's id is not quoted: %s", tool, withID, note)
			}
			switch {
			case withID && !strings.Contains(note, "evo "+svc+" get "+strconv.Quote(auditUUID)):
				t.Errorf("%s: the cluster's id is not named quoted: %s", tool, note)
			case !withID && !strings.Contains(note, "evo "+svc+" list"):
				t.Errorf("%s without the cluster's id: the note does not name the list that finds it by name: %s", tool, note)
			}
		}
	}
}
