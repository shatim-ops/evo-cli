// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"net/http"
	"path"
	"strings"
	"testing"
)

func (f *ownerFake) serveUpgradeCard(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != "GET" || r.URL.Host != "mk8s.api.cloud.ru" {
		return false
	}
	id := path.Base(r.URL.Path)
	project, ok := projectOf[id]
	if !ok {
		return false
	}
	card := map[string]any{"id": id, "projectId": project, "status": f.upgradeStatus, "versionUpgrade": map[string]any{"upgradeAvailable": true, "availableVersions": []string{"v1.36.5"}}}
	switch path.Dir(r.URL.Path) {
	case "/v3/clusters":
		v := "v1.35.9"
		if f.upgradeTool == "k8s_nodepool_upgrade" {
			v = "v1.36.5"
			card["status"] = "OBJECT_STATUS_RUNNING"
		}
		card["controlPlane"] = map[string]any{"version": v}
	case "/v3/nodePools":
		card["version"] = "v1.35.9"
		card["clusterId"] = id
	default:
		return false
	}
	_ = json.NewEncoder(w).Encode(card)
	return true
}

func upgradeStatePreview(t *testing.T, tool, status string) string {
	t.Helper()
	t.Setenv("CLOUDRU_CONFIG_DIR", t.TempDir())
	f := &ownerFake{upgradeTool: tool, upgradeStatus: status}
	r := ownerRegistry(t, f, false, ownProject)
	arg := "cluster_id"
	if tool == "k8s_nodepool_upgrade" {
		arg = "nodepool_id"
	}
	_, text, _ := callOwned(t, r, tool, map[string]any{arg: resMine, "version": "v1.36.5"}, false)
	for _, req := range f.log() {
		if !strings.HasPrefix(req, "GET ") {
			t.Fatalf("preview wrote: %s", req)
		}
	}
	return text
}

var upgradeStates = map[string]string{
	"OBJECT_STATUS_RUNNING": "confirmation_required",
	"OBJECT_STATUS_PENDING": "error", "OBJECT_STATUS_PROVISIONING": "error", "OBJECT_STATUS_RESUMING": "error", "OBJECT_STATUS_DELETING": "error", "OBJECT_STATUS_ERROR": "error",
	"OBJECT_STATUS_SCALING_UP": "error", "OBJECT_STATUS_SCALING_DOWN": "error", "OBJECT_STATUS_STOPPING": "error", "OBJECT_STATUS_STOPPED": "error", "OBJECT_STATUS_SUSPENDING": "error",
	"OBJECT_STATUS_SUSPENDED": "error", "OBJECT_STATUS_UPGRADING": "error", "OBJECT_STATUS_EDITING": "error", "OBJECT_STATUS_ADDING": "error", "OBJECT_STATUS_DELETED": "error",
}

func init() {
	addStateRows(stateRowsOf, map[string]map[string]string{"k8s_upgrade": upgradeStates, "k8s_nodepool_upgrade": upgradeStates})
}
func TestUpgradeStateBeforeConsent(t *testing.T) {
	for _, tool := range []string{"k8s_upgrade", "k8s_nodepool_upgrade"} {
		for state, want := range upgradeStates {
			t.Run(tool+"/"+state, func(t *testing.T) {
				text := upgradeStatePreview(t, tool, state)
				var got struct{ Status string }
				if json.Unmarshal([]byte(text), &got) != nil || got.Status != want {
					t.Fatalf("want %s: %s", want, text)
				}
			})
		}
	}
}
