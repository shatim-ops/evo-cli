// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// worldDrift are the ways the worlds are known to answer otherwise than the
// platform: "answer id: what" → the work item that makes the world follow
// the platform. The list only shrinks: a drift not here fails the test, and
// so does a line the worlds no longer drift by.
// Closes by 1.8: a line lives at most two versions (release plan 1.6,
// «храповики со сроком»; TestMisses_HaveADeadline).
var worldDrift = map[string]string{}

// Release plan 1.6, «Миры из живых ответов»: ≈50 % of the defects that
// escaped the tests were a wrong model of the platform in the worlds. For
// every line of testdata/platform-answers.json the JSON world answers the
// same request, and its answer keeps to the live form: no path the
// platform does not send (under a path the probe walked), the same kind
// where the probe saw one (a name is no id), and no value of a field of
// state the platform was not seen with — or a line of worldDrift.
func TestPlatformAnswers_WorldsKeepTheLiveForm(t *testing.T) {
	answers := loadCatalogue(t)
	w, _ := withJSONWorld(t)
	found := map[string]bool{}
	ids := map[string]bool{}
	for _, a := range answers {
		if ids[a.ID] {
			t.Errorf("%s: twice", a.ID)
		}
		ids[a.ID] = true
		if a.Source == "" || a.Recorded == "" || len(a.Paths) == 0 {
			t.Errorf("%s: a line without its source, date or paths", a.ID)
			continue
		}
		if a.Skip != "" {
			continue
		}
		method, url, _ := strings.Cut(a.Request, " ")
		url = strings.NewReplacer("{project}", pvProject, "{id}", parityVM).Replace(url)
		req, err := http.NewRequest(method, url, nil)
		if err != nil {
			t.Fatalf("%s: %v", a.ID, err)
		}
		resp, err := w.rt.RoundTrip(req)
		if err != nil {
			t.Fatalf("%s: the world does not answer %s: %v", a.ID, a.Request, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != a.Status {
			t.Errorf("%s: the world answers %d, the platform %d", a.ID, resp.StatusCode, a.Status)
			continue
		}
		kinds, strs, err := answerShape(body)
		if err != nil {
			t.Errorf("%s: the world's answer is not JSON: %v", a.ID, err)
			continue
		}
		checkLiveForm(a, a.ID, kinds, strs, found)
	}
	holdDrift(t, found, worldDrift, func(k string) bool { return !strings.Contains(k, " (go ") })
}

// goWorlds are the worlds that answer a line of the catalogue through
// provision.CloudAPI rather than over HTTP: answer id → world name → its
// answer, as the body the platform sends (a list under "items").
var goWorlds = map[string]map[string]func() (any, error){
	"compute-flavors": {
		"previewCloud": func() (any, error) { return previewCloud{}.ListFlavors(context.Background()) },
	},
	"compute-floating-ips": {
		"freeFIPCloud": func() (any, error) { return freeFIPCloud{}.ListFloatingIPs(context.Background(), pvProject) },
		"busyFIPCloud": func() (any, error) { return busyFIPCloud{}.ListFloatingIPs(context.Background(), pvProject) },
	},
}

// Release plan 1.6, «Миры из живых ответов», step 2: the worlds that answer
// through provision.CloudAPI keep to the live form too. A world's answer is
// marshalled by the tags evo decodes the API with, its zero values dropped
// (a field the world does not set is not one it sends), and held against
// the line as TestPlatformAnswers_WorldsKeepTheLiveForm holds the HTTP
// worlds. A line skipped for provision.CloudAPI has its worlds here.
func TestPlatformAnswers_GoWorldsKeepTheLiveForm(t *testing.T) {
	answers := loadCatalogue(t)
	for _, a := range answers {
		_, ok := goWorlds[a.ID]
		if strings.Contains(a.Skip, "provision.CloudAPI") != ok {
			t.Errorf("%s: a line skipped for provision.CloudAPI has its worlds in goWorlds, and only such a line", a.ID)
		}
	}
	found := goWorldsFound(t, goWorlds)
	holdDrift(t, found, worldDrift, func(k string) bool { return strings.Contains(k, " (go ") })
}
