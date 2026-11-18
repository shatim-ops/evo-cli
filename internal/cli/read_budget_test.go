// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// listBudget is the budget of a list of a read answer: n, the objects the
// world of TestParity_JSONEqualsTool answers in it, and perObject, the
// bytes of compact JSON one of them may take on average.
type listBudget struct{ n, perObject int }

// answerBudget is the budget of every list of objects a read tool answers
// on the world, "tool key" (the key of the list in the answer; "tool" for
// an answer that is the list). The numbers are measured on the world at
// 17.11 (1.9) plus about a quarter, rounded up to ten bytes: an answer that
// outgrows its line fails the build, and the line is raised in the PR that
// grows it, with a line «бюджет ответа: <list> — <why>» in its description
// — as maxToolsListBytes (internal/mcp/limits_test.go), never by cutting
// fields or rows without a word («Риск», conclusion 49, Р3).
var answerBudget = map[string]listBudget{
	// "tool key": {n, budget}, // measured bytes per object
	"backup_list backups":                  {2, 330}, // 263
	"disk_list disks":                      {1, 140}, // 112
	"dns_zones zones":                      {1, 220}, // 172
	"doctor checks":                        {7, 120}, // 94
	"history lines":                        {3, 330}, // 264
	"history live":                         {1, 360}, // 284
	"image_get fields":                     {2, 170}, // 131
	"image_list images":                    {1, 110}, // 84
	"k8s_addon_versions releases":          {2, 240}, // 190
	"k8s_addons addons":                    {1, 230}, // 182
	"k8s_nodepools node_pools":             {1, 80},  // 64
	"k8s_options flavors":                  {1, 110}, // 85
	"kafka_specs specifications":           {1, 130}, // 99
	"kafka_versions versions":              {1, 30},  // 22
	"list_disk_types disk_types":           {1, 80},  // 58
	"list_flavors flavors":                 {3, 190}, // 147
	"list_floating_ips floating_ips":       {1, 180}, // 140
	"list_images images":                   {1, 110}, // 87
	"list_security_groups security_groups": {2, 210}, // 163
	"list_subnets subnets":                 {1, 210}, // 167
	"list_zones zones":                     {1, 80},  // 62
	"marketplace_list items":               {4, 260}, // 208
	"pg_databases databases":               {1, 20},  // 14
	"pg_specs specifications":              {1, 180}, // 137
	"pg_users users":                       {1, 20},  // 14
	"pg_versions versions":                 {1, 30},  // 24
	"redis_specs specifications":           {1, 130}, // 99
	"redis_versions versions":              {1, 30},  // 22
	"s3_buckets buckets":                   {2, 60},  // 41
	"s3_ls objects":                        {1, 70},  // 49
	"snat_list nat_gateways":               {1, 190}, // 145
	"vm_get interfaces":                    {1, 170}, // 134
	"vm_get not_checked":                   {1, 200}, // 160
	"vm_list vms":                          {1, 110}, // 86
	"vpc_list vpcs":                        {1, 120}, // 89
	"vpc_routes routes":                    {1, 140}, // 107
}

// answerUnmeasured are the lists of read answers the world answers empty:
// their objects are not measured, and nothing holds their size. The list
// only shrinks: the world answers objects in the list (parityAnswers,
// parityEmptyLists), the line goes and a line of answerBudget comes. A line
// whose list the world answers with objects, or answers no more, fails the
// test. Closes by 1.10.
var answerUnmeasured = map[string]string{
	"ar_list registries":                    "в проекте мира нет реестра",
	"billing_consumption by_service":        "в периоде мира нет расхода",
	"billing_finops by_service":             "в периоде мира нет расхода",
	"ca_list containers":                    "в проекте мира нет контейнерного приложения",
	"ca_logs logs":                          "у приложения мира нет строк журнала",
	"cert_usage usage":                      "сертификат мира нигде не используется",
	"cert_versions versions":                "у сертификата мира нет версий",
	"dns_records records":                   "в зоне мира нет записей",
	"iam_service_accounts service_accounts": "в проекте мира нет сервисного аккаунта",
	"image_get fields_required":             "у образа мира нет полей без значения (список строк, не объектов)",
	"image_get zones_ready":                 "образ мира не готов ни в одной зоне (список строк, не объектов)",
	"k8s_list clusters":                     "в проекте мира нет кластера Kubernetes",
	"kafka_list clusters":                   "в проекте мира нет кластера Kafka",
	"lb_list load_balancers":                "в проекте мира нет балансировщика",
	"list deployments":                      "в каталоге мира нет записанного развёртывания",
	"org_projects projects":                 "ключ мира не видит других проектов",
	"org_quotas quota_usages":               "у проекта мира нет строк квот",
	"pg_list clusters":                      "в проекте мира нет кластера PostgreSQL",
	"redis_list clusters":                   "в проекте мира нет кластера Redis",
	"secret_list secrets":                   "в проекте мира нет секрета",
	"tg_list target_groups":                 "в проекте мира нет целевой группы",
}

// Release plan 1.9, phase 0, И9 (бюджет ответа; «Риск», conclusion 49, Р3):
// an agent calls lists in a loop, and every byte of an object is paid in its
// context. For every read tool of the registry on the world of
// TestParity_JSONEqualsTool — its answer, which --json prints as it is
// (TestParity_JSONEqualsTool): one measure for CLI and MCP — each list of
// objects in the answer (a key whose value is an array of objects, or the
// answer itself) has a line of answerBudget: the world answers n objects in
// it, and their compact JSON is at most perObject bytes each on average.
// Measured, not estimated: the bytes of the answer, the paths of the
// directory of the test read as one.
func TestRead_AnswerWithinBudget(t *testing.T) {
	w, fake := withJSONWorld(t)
	reg := newRegistry()
	measured, empty := map[string]bool{}, map[string]bool{}
	var bad, table []string
	for _, tl := range reg.Tools() {
		args, ok := parityArgs[tl.Name]
		if tl.Mutating || !ok || textAnswer[tl.Name] != "" {
			continue
		}
		args = copyArgs(args)
		done := func() {}
		if setup := paritySetup[tl.Name]; setup != nil {
			extra, d := setup(t, w)
			for k, v := range extra {
				args[k] = v
			}
			done = d
		}
		// A read that asks first (secret_value) answers what it read.
		args["confirmed"] = true
		fake.reset()
		newProcess()
		flagJSON, flagDryRun, flagYes = false, false, false
		text, isErr := tl.Handler(context.Background(), args)
		done()
		forgetRun(w)
		if isErr {
			bad = append(bad, tl.Name+": the tool fails on the world: "+clipLine(text))
			continue
		}
		var answer any
		if json.Unmarshal([]byte(budgetVolatile(text)), &answer) != nil {
			bad = append(bad, tl.Name+": the answer is no JSON: "+clipLine(text))
			continue
		}
		for key, objects := range objectLists(tl.Name, answer) {
			if len(objects) == 0 {
				empty[key] = true
				if _, ok := answerUnmeasured[key]; !ok {
					bad = append(bad, key+": the world answers the list empty, and it is not in answerUnmeasured — give the world objects in it, or a line with why")
				}
				continue
			}
			if _, ok := answerUnmeasured[key]; ok {
				bad = append(bad, key+": in answerUnmeasured, but the world answers objects in it: remove the line, measure it in answerBudget")
			}
			total := 0
			for _, o := range objects {
				var b bytes.Buffer
				enc := json.NewEncoder(&b)
				enc.SetEscapeHTML(false)
				_ = enc.Encode(o)
				total += len(bytes.TrimSpace(b.Bytes()))
			}
			per := (total + len(objects) - 1) / len(objects)
			measured[key] = true
			b, ok := answerBudget[key]
			table = append(table, fmt.Sprintf("%-40s n=%-3d %4d B/object, budget %d", key, len(objects), per, b.perObject))
			switch {
			case !ok:
				bad = append(bad, fmt.Sprintf("%s: a list of %d object(s), %d bytes each, without a line of answerBudget — add one (measured plus about a quarter)", key, len(objects), per))
			case b.n != len(objects):
				bad = append(bad, fmt.Sprintf("%s: the world answers %d object(s), the budget is set at %d — measure again and set the line", key, len(objects), b.n))
			case per > b.perObject:
				bad = append(bad, fmt.Sprintf("%s: %d bytes per object, budget %d — the answer grew: raise the line in the PR with «бюджет ответа: %s — <why>», or keep the object within it", key, per, b.perObject, key))
			}
		}
	}
	for key := range answerBudget {
		if !measured[key] {
			bad = append(bad, key+": a line of answerBudget, but no read tool answers that list on the world: remove the line")
		}
	}
	for key, why := range answerUnmeasured {
		if !empty[key] && !measured[key] {
			bad = append(bad, key+": in answerUnmeasured, but no read tool answers that list on the world: remove the line")
		}
		if why == "" {
			bad = append(bad, key+": a line of answerUnmeasured without why")
		}
	}
	t.Logf("lists measured: %d; answered empty (answerUnmeasured): %d", len(measured), len(answerUnmeasured))
	sort.Strings(table)
	t.Logf("lists of read answers on the world:\n%s", strings.Join(table, "\n"))
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatal(strings.Join(bad, "\n"))
	}
}

// objectLists are the lists of objects of a read answer by their line of
// answerBudget: "tool key" for an array of objects under key, "tool" for an
// answer that is an array. An empty array is a list too: its objects are
// not known.
func objectLists(tool string, answer any) map[string][]any {
	out := map[string][]any{}
	isObjects := func(a []any) bool {
		for _, e := range a {
			if _, ok := e.(map[string]any); !ok {
				return false
			}
		}
		return true
	}
	switch x := answer.(type) {
	case []any:
		if isObjects(x) {
			out[tool] = x
		}
	case map[string]any:
		for k, e := range x {
			if a, ok := e.([]any); ok && isObjects(a) {
				out[tool+" "+k] = a
			}
		}
	}
	return out
}

// budgetVolatile reads as one what is not the answer's but the test's: a
// path in a directory of the test (doctor names the config directory and
// kubectl), whose length differs from one machine to another.
func budgetVolatile(s string) string {
	return testDir.ReplaceAllString(humanVolatile(s), "<dir>")
}

// The parts of the path are split by / or by \ — doubled in JSON, single
// in the human output on Windows.
var testDir = regexp.MustCompile(`[^"\s]*?Test\w+?\d+(/|\\+)+\d{3}`)
