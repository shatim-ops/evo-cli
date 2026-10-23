// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"net/http"
	"strings"
	"testing"
)

// A request the parity world has no fixture for is named, not answered
// silently with an empty list (delta audit 1.2.3): a tool that starts to
// read a new API would otherwise pass parity without a model of it.
func TestParityWorld_UnknownRequestIsNamed(t *testing.T) {
	rt := &parityRT{addon: &addonAPI{}}
	for _, u := range []string{
		"https://nlb.api.cloud.ru/v2/targetGroups",                                // an empty list on purpose
		"https://vpc.api.cloud.ru/v1/vpcs/" + parityVM,                            // an answer
		"https://nlb.api.cloud.ru/v2/healthChecks/" + parityVM,                    // nothing
		"https://dataplatform.api.cloud.ru/v1/clusters?project=" + pvProject,      // nothing
		"https://postgresql.api.cloud.ru/v1/clusters/" + parityVM + "/extensions", // the fake's 404
		"https://redis.api.cloud.ru/v1/specifications",                            // the fake's answer
	} {
		r, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := rt.RoundTrip(r); err != nil {
			t.Fatal(err)
		}
	}
	got := strings.Join(rt.takeUnknown(), "; ")
	want := "GET dataplatform.api.cloud.ru/v1/clusters; GET nlb.api.cloud.ru/v2/healthChecks/{id}; GET postgresql.api.cloud.ru/v1/clusters/{id}/extensions"
	if got != want {
		t.Errorf("unknown %q, want %q", got, want)
	}
	if again := rt.takeUnknown(); len(again) != 0 {
		t.Errorf("unknown after take: %q", again)
	}
}

// Only a list may stay empty on purpose: a read of one resource or a write
// answered with {"items":[]} is no model of the API. A list may end in the
// ID of its owner after a «list» segment (audit: /events/list/{project}).
func TestParityWorld_EmptyListsAreLists(t *testing.T) {
	for key, why := range parityEmptyLists {
		method, path, _ := strings.Cut(key, " ")
		last := path[strings.LastIndex(path, "/")+1:]
		byOwner := strings.HasSuffix(path, "/list/{id}")
		if method != http.MethodGet || (last == "{id}" && !byOwner) || strings.Contains(last, ":") || why == "" {
			t.Errorf("parityEmptyLists %q (%q): only a GET of a list, with the reason", key, why)
		}
		if _, ok := parityAnswers[key]; ok {
			t.Errorf("%q is both an empty list and an answer", key)
		}
	}
}
