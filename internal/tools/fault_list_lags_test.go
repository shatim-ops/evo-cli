// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
)

// The request was taken, the resource is not listed yet (release plan 1.9,
// «Находки промежуточного дельта-аудита», class 1; backup_restore, #1580):
// a create whose answer is lost looks for what it made in the list, and the
// repeat checks the name there. The fault matrix lost the answer and kept
// the list true to the moment — the resource was there at the next read. A
// list that shows it some reads later is the same call seen by another
// cloud: the repeat must not make a second billable resource of the name.
//
// Whether a service of Cloud.ru lists a resource later than it takes the
// request is known for one (the disk of a restore, two runs of Г-16); for
// the others it is a hypothesis, and so is each line found here.

// listLagsPending are the creates whose repeat, after a lost answer and a
// list that lags, leaves more billable resources than one clean call —
// each with what is known. Only shrinks. Closes by 1.10; the six that cost
// a cluster close by 1.9, each in its line (release plan 1.9, «Повтор
// платного создания после потерянного ответа спрашивает заново»).
// listLagsUnknown is what is known of every line today.
const listLagsUnknown = "a second of the name after a lost answer: whether the service lists later than it takes the request is not known (no run)"

var listLagsPending = map[string]string{
	"ar_create":         listLagsUnknown,
	"cert_import":       listLagsUnknown,
	"dns_record_create": listLagsUnknown,
	"k8s_add_nodepool":  "Closes by 1.9 (owner's decision 13.11, #1590, question 20): the price of a cluster — a blocker of rc.1; " + listLagsUnknown,
	"k8s_create":        "Closes by 1.9 (owner's decision 13.11, #1590, question 20): the price of a cluster — a blocker of rc.1; " + listLagsUnknown,
	"kafka_create":      "Closes by 1.9 (owner's decision 13.11, #1590, question 20): the price of a cluster — a blocker of rc.1; " + listLagsUnknown,
	"lb_create":         "Closes by 1.9 (owner's decision 13.11, #1590, question 20): the price of a cluster — a blocker of rc.1; " + listLagsUnknown,
	"pg_backup_create":  listLagsUnknown,
	"pg_create":         "Closes by 1.9 (owner's decision 13.11, #1590, question 20): the price of a cluster — a blocker of rc.1; " + listLagsUnknown,
	"redis_create":      "Closes by 1.9 (owner's decision 13.11, #1590, question 20): the price of a cluster — a blocker of rc.1; " + listLagsUnknown,
	"snat_create":       listLagsUnknown,
	"tg_create":         listLagsUnknown,
	"vpc_create":        listLagsUnknown,
	"vpc_route_create":  listLagsUnknown,
}

func TestFault_Matrix_ListBehindALostCreate(t *testing.T) {
	const lag = 3
	found := map[string]string{}
	ran := 0
	for _, c := range faultMatrixCases {
		happy, _, _, _ := faultRun(t, c, 0, "")
		if _, ok := happy.(*faultAPI); !ok {
			continue // the worlds of compute and of the backups list in their own code
		}
		calls := happy.callLog()
		clean := len(happy.billed())
		for k := 1; k <= len(calls); k++ {
			if !strings.HasPrefix(calls[k-1], http.MethodPost+" ") && !strings.HasPrefix(calls[k-1], http.MethodPut+" ") {
				continue
			}
			ctx, cancel := context.WithCancel(context.Background())
			w, r, _ := c.world(t, cancel)
			api := w.(*faultAPI)
			api.lag = lag
			w.failAt(k, faultLost)
			first, _ := r.handlers[c.tool](ctx, c.args)
			w.disarm()
			again, _ := r.handlers[c.tool](context.Background(), c.args)
			cancel()
			ran++
			if n := len(w.billed()); n > clean && found[c.tool] == "" {
				found[c.tool] = fmt.Sprintf("the answer of %s lost and the list %d reads behind: the repeat left %d billable, one clean call leaves %d\nfirst: %.300s\nagain: %.300s",
					calls[k-1], lag, n, clean, first, again)
			}
		}
	}
	var tools []string
	for tool := range found {
		tools = append(tools, tool)
	}
	sort.Strings(tools)
	for _, tool := range tools {
		if listLagsPending[tool] == "" {
			t.Errorf("%s: %s", tool, found[tool])
		}
	}
	for tool := range listLagsPending {
		if found[tool] == "" {
			t.Errorf("listLagsPending names %s, whose repeat makes no second resource now: remove the line (the list only shrinks)", tool)
		}
	}
	if ran < 15 {
		t.Errorf("%d lost writes were repeated: the matrix no longer reaches the creates of faultAPI", ran)
	}
	t.Logf("lost writes repeated behind a lagging list: %d; creates that made a second: %d (%s)", ran, len(tools), strings.Join(tools, ", "))
}
