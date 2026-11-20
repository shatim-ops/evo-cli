// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"path/filepath"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/history"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/intent"
)

// journalProvenance uses the project independently resolved for this preview.
// The configuration journal and other state directories are outside its scope.
func (r *Registry) journalProvenance(del, project, id string) engine.Provenance {
	unknown := engine.Provenance{CreatedByEvo: engine.ProvenanceUnknown, Note: i18n.T("this folder's journal does not confirm creation of this resource in its project", "журнал этой папки не подтверждает создание ресурса в его проекте")}
	root, ok := r.stateDir()
	if !ok || project == "" || id == "" {
		return unknown
	}
	j, err := history.ReadAll(history.DirOf(intent.Dir(root)), r.historyCheck)
	if err != nil || j.Skipped > 0 {
		return unknown
	}
	for _, l := range history.Live(j.Lines) {
		if l.Outcome != history.Created || l.Kind != resourceKind(del) || l.ProjectID != project || l.ResourceID != id || l.Parent != "" || l.StateDir == "" || filepath.Clean(l.StateDir) != filepath.Clean(root) {
			continue
		}
		// Redis already recorded both IDs from CreateCluster in 1.9.0.
		// Recovery by name carries no operation ID. Older subnet lines
		// cannot distinguish the POST response from that recovery.
		if (del == "redis_delete" && l.Tool == "redis_create" && l.OperationID != "") ||
			(del == "subnet_delete" && l.Tool == "subnet_create" && l.CreateReceipt) {
			return engine.Provenance{CreatedByEvo: engine.ProvenanceYes, Note: i18n.T("this folder's journal records the cloud ID returned by creation in this project", "журнал этой папки содержит облачный ID из ответа на создание ресурса в этом проекте")}
		}
	}
	return unknown
}
