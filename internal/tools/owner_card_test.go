// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"
)

// A kind with a read by id — a tool «…_get» that takes one id of it —
// reads its resource by id (card): the preview of a change of another
// project's resource names that project, not «not determined», and evo
// doctor checks a cut delete by the id (FB19I-20, the class of line 8 of the
// journal of 1.9: backup_delete and k8s_nodepool_delete had no card though
// backup_get and k8s_nodepool_get read by id).
// cardPending are the reads by id whose kind has no card yet: "tool" →
// whose it is. It only shrinks; a new kind with a read by id comes with
// its card. The class is «Процесс · 1.9 (инварианты)»'s (FB19I-20). Closes by 1.10.
var cardPending = map[string]string{
	"kafka_get":      "«разработка B»: Kafka — the list of the project tells it",
	"redis_get":      "«разработка B»: Redis — the list of the project tells it",
	"vm_get":         "«разработка A»: a VM by id or by name",
	"dns_zone_get":   "«разработка C»: DNS — public and private zones",
	"dns_record_get": "«разработка C»: DNS — a record of a zone",
}

func TestOwnerKinds_AReadByIDDeclaresTheCard(t *testing.T) {
	seen := 0
	for tool, args := range idArgs {
		if !strings.HasSuffix(tool, "_get") || len(args) != 1 || args[0].kind == nil {
			continue
		}
		seen++
		pending := cardPending[tool] != ""
		if args[0].kind.card != nil && pending {
			t.Errorf("cardPending names %s, whose kind has its card now: remove the line (the list only shrinks)", tool)
		}
		if args[0].kind.card == nil && !pending {
			t.Errorf("%s reads a %s by id, and the kind declares no card: the preview of its change and evo doctor cannot read it by id", tool, args[0].kind.what)
		}
	}
	if seen == 0 {
		t.Fatal("no read by id in idArgs: the test reads nothing")
	}
}
