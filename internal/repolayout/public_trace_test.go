// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package repolayout

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// The public trace (owner's decision 14.11, release plan 1.9, «Сторож
// публичного следа в CI»; audit of evo-internal #299): the repository is
// public, and what it holds about the platform is what the published
// documentation holds. Every file git would commit is read for four kinds
// of trace, by general rules — no internal name is written here:
//
//   - host: a name under the brand's domains (cloud.ru, the legacy
//     sbercloud.ru) that is neither an API of the inventories
//     (<service>.api.cloud.ru, scripts/api-contract.json and
//     scripts/probe-requests.json — published specifications) nor in
//     publicHosts with its reason;
//   - uuid: a UUID that is not of a synthetic form (syntheticUUID);
//   - ipv4: an address outside the ranges of documentation (RFC 5737),
//     private networks and the special ones (sharedOrSpecial), not a
//     network written as CIDR, and not in publicIPs with its reason;
//   - internal-path: a path inside the private repository deeper than its
//     top directories, outside the rules (CLAUDE.md, docs/RELEASING.md) and
//     the briefs (docs/internal/briefs/).
//
// Each finding fails with the file, line and rule.

// brandHost is a host under a domain of the brand: at least one label
// before it, so the brand name «Cloud.ru» alone is not a host.
var brandHost = regexp.MustCompile(`(?i)\b(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+(?:cloud|sbercloud)\.ru\b`)

// publicHosts are the hosts of the brand a text may name beyond the APIs of
// the inventories, each with why it is public; "*." names every host under
// a suffix — a name a user chooses, in a format the documentation gives.
var publicHosts = map[string]string{
	"dataplatform.api.cloud.ru":      "an API with a published specification (scripts/cloudru-specs.py), outside the inventories",
	"foundation-models.api.cloud.ru": "the API of Foundation Models in the published documentation",
	"cdn.cloud.ru":                   "the public files of the site (offers, documents)",
	"api.cloud.ru":                   "the domain of the APIs, as the documentation names it",
	"auth.iam.cloud.ru":              "the token endpoint of the published IAM specification",
	"id.cloud.ru":                    "the sign-in of the web console, which every user opens",
	"console.cloud.ru":               "the web console, which every user opens",
	"s3.cloud.ru":                    "the endpoint of Object Storage in its documentation",
	"cr.cloud.ru":                    "the domain of Artifact Registry in its documentation",
	"*.cr.cloud.ru":                  "<registry>.cr.cloud.ru — a registry the user names, the format of the documentation",
	"*.s3.cloud.ru":                  "<bucket>.s3.cloud.ru — a bucket the user names, virtual-host addressing of S3",
	"containers.cloud.ru":            "the domain of container apps in the documentation",
	"*.containers.cloud.ru":          "<app>.containers.cloud.ru — the address of a container app, the format of the documentation",
}

// inventoryServices are the services of the inventories of requests: their
// <service>.api.cloud.ru is a published API.
func inventoryServices(t *testing.T, root string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	var contract struct {
		Services []struct{ Name string } `json:"services"`
	}
	var probes struct {
		Probes []struct {
			Requests []struct{ Spec string } `json:"requests"`
		} `json:"probes"`
	}
	for file, v := range map[string]any{"api-contract.json": &contract, "probe-requests.json": &probes} {
		b, err := os.ReadFile(filepath.Join(root, "scripts", file))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, v); err != nil {
			t.Fatalf("scripts/%s: %v", file, err)
		}
	}
	for _, s := range contract.Services {
		out[s.Name] = true
	}
	for _, p := range probes.Probes {
		for _, r := range p.Requests {
			svc, _, _ := strings.Cut(r.Spec, "/")
			out[svc] = true
		}
	}
	if len(out) < 10 {
		t.Fatalf("%d services in the inventories: the reading sees nothing", len(out))
	}
	return out
}

func publicHost(host string, services map[string]bool) bool {
	host = strings.ToLower(host)
	if svc, ok := strings.CutSuffix(host, ".api.cloud.ru"); ok && services[svc] {
		return true
	}
	if publicHosts[host] != "" {
		return true
	}
	for pattern := range publicHosts {
		if suffix, ok := strings.CutPrefix(pattern, "*."); ok && strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

var uuidRe = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)

// syntheticUUID: a UUID written by hand, not given by a system — a run of
// six equal digits (0000…, 1111…) or a last group of at most four distinct
// digits (444455556666). A random one is either with a chance under 1e-4.
func syntheticUUID(u string) bool {
	hex := strings.ToLower(strings.ReplaceAll(u, "-", ""))
	run := 1
	for i := 1; i < len(hex); i++ {
		if hex[i] == hex[i-1] {
			run++
			if run >= 6 {
				return true
			}
		} else {
			run = 1
		}
	}
	distinct := map[rune]bool{}
	for _, c := range strings.ToLower(u[len(u)-12:]) {
		distinct[c] = true
	}
	return len(distinct) <= 4
}

// ipv4Re is a dotted quad with what follows it: a CIDR prefix length.
var ipv4Re = regexp.MustCompile(`\b((?:25[0-5]|2[0-4]\d|1?\d?\d)(?:\.(?:25[0-5]|2[0-4]\d|1?\d?\d)){3})\b(/\d{1,2})?`)

// sharedOrSpecial are the ranges no host of the internet answers from:
// documentation (RFC 5737), private (RFC 1918), shared (RFC 6598),
// benchmarking (RFC 2544),
// loopback, link-local, this network, multicast and reserved.
var sharedOrSpecial = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{"192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24", "10.0.0.0/8", "172.16.0.0/12",
		"192.168.0.0/16", "100.64.0.0/10", "198.18.0.0/15", "127.0.0.0/8", "169.254.0.0/16", "0.0.0.0/8", "224.0.0.0/3"} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

// publicIPs are the addresses a text may name outside those ranges, each
// with why: an address everybody knows, or digits that only read as one.
var publicIPs = map[string]string{
	"1.2.3.4":       "the placeholder of examples (1.2.3.4–1.2.3.6, 5.6.7.8), no host of the platform",
	"1.2.3.5":       "the placeholder of examples",
	"1.2.3.6":       "the placeholder of examples",
	"5.6.7.8":       "the placeholder of examples",
	"8.8.8.8":       "a public resolver everybody knows",
	"4.1.2.1":       "a section of RFC 6749, not an address",
	"3.1.2.6":       "a section of OpenID Connect Core, not an address",
	"93.184.216.34": "the published address of example.com (RFC 2606)",
}

func publicIP(quad, prefix string) bool {
	a, err := netip.ParseAddr(quad)
	if err != nil {
		return true
	}
	for _, p := range sharedOrSpecial {
		if p.Contains(a) {
			return true
		}
	}
	if prefix != "" {
		// A network written as CIDR (128.0.0.0/1), or as IPv4 inside an
		// IPv6 prefix (::ffff:128.0.0.0/97): no host is named.
		bits := 0
		fmt.Sscanf(prefix, "/%d", &bits)
		if bits > 32 {
			bits -= 96
		}
		if p, err := a.Prefix(bits); bits >= 0 && err == nil && p.Addr() == a {
			return true
		}
	}
	return publicIPs[quad] != ""
}

// internalPath is a path inside the private repository: its top directory
// (runs/, risk/ — named by the rules) and anything below it.
var internalPath = regexp.MustCompile("evo-internal/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-][^\\s)\\]`'\"«»,;]*")

// pathRulesAndBriefs may name paths inside the private repository: they
// tell a session where to write.
func pathRulesAndBriefs(file string) bool {
	return file == "CLAUDE.md" || file == "docs/RELEASING.md" || strings.HasPrefix(file, "docs/internal/briefs/")
}

// publicTraceSkip are files the walk does not read, with why.
var publicTraceSkip = map[string]string{
	"specs/": "the published specifications of Cloud.ru: what they name is public",
	"go.sum": "hashes of modules",
	// The cases of TestPublicTrace_Rules are traces by design.
	"internal/repolayout/public_trace_test.go": "this test: the cases of its rules",
}

// traceFinding is one trace: file, line, rule, the text matched.
type traceFinding struct {
	file  string
	line  int
	rule  string
	match string
}

func publicTraceFindings(t *testing.T, root string, paths []string) []traceFinding {
	t.Helper()
	services := inventoryServices(t, root)
	var out []traceFinding
	for _, p := range paths {
		skip := false
		for prefix := range publicTraceSkip {
			if strings.HasPrefix(p, prefix) {
				skip = true
			}
		}
		if skip {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		if !utf8.Valid(b) {
			continue // binary
		}
		out = append(out, traceIn(p, string(b), services)...)
	}
	return out
}

// These probe templates contain shell variable names. Check their finite
// source before treating the URL as an inventory endpoint. A change to the
// source or a different literal host on the same line is still reported.
var dbProbeBindings = regexp.MustCompile(`^[ \t]*case "\$kind" in
[ \t]*pg\) g=Г-13 host=([a-z0-9-]+) ;;
[ \t]*kafka\) g=Г-14 host=([a-z0-9-]+) ;;
[ \t]*redis\) g=Г-15 host=([a-z0-9-]+) ;;
[ \t]*\*\) bad dbchange "DB_KIND «\$kind» — не pg, kafka, redis"; return 1 ;;
[ \t]*esac
[ \t]*\[ -n "\$id" \] \|\| \{ bad dbchange "нет DB_CLUSTER_ID"; return 1; \}
[ \t]*mask "\$id"
[ \t]*iam_fresh \|\| \{ gap "\$g" "нет токена IAM \(KEY_ID, SECRET\)"; return 0; \}\z`)

func probeLoopServices(header, variable string, services map[string]bool) bool {
	names, ok := strings.CutPrefix(strings.TrimSpace(header), "for "+variable+" in ")
	if !ok {
		return false
	}
	names, ok = strings.CutSuffix(names, "; do")
	if !ok || len(strings.Fields(names)) == 0 {
		return false
	}
	for _, name := range strings.Fields(names) {
		if !services[name] {
			return false
		}
	}
	return true
}

func publicProbeTemplate(file string, lines []string, row, start int, host string, services map[string]bool) bool {
	line := lines[row]
	if start == 0 || line[start-1] != '$' {
		return false
	}
	variable, ok := strings.CutSuffix(host, ".api.cloud.ru")
	if !ok {
		return false
	}
	switch file {
	case "internal/workflows/probe_hosts_test.go":
		// A Go map key declaring the shell template used by cloud-live.sh.
		return variable == "host" && strings.TrimSpace(line[:start]) == `"$` &&
			strings.HasPrefix(line[start+len(host):], `":`)
	case ".github/scripts/cloud-live.sh":
		if variable != "host" || row < 9 || strings.TrimSpace(line) != `url="https://$host.api.cloud.ru/v1/clusters/$id"` {
			return false
		}
		values := dbProbeBindings.FindStringSubmatch(strings.Join(lines[row-9:row], "\n"))
		if values == nil {
			return false
		}
		for _, value := range values[1:] {
			if !services[value] {
				return false
			}
		}
		return true
	case ".github/workflows/api-probe.yml":
		switch strings.TrimSpace(line) {
		case `D="https://$svc.api.cloud.ru/v1"`, `h=https://$svc.api.cloud.ru`:
			return variable == "svc" && row > 0 && probeLoopServices(lines[row-1], variable, services)
		case probeTLS12, probeTLS13:
			init := row - 1
			if strings.TrimSpace(line) == probeTLS12 {
				if init < 0 || strings.TrimSpace(lines[init]) != probeTLS13 {
					return false
				}
				init--
			}
			return variable == "h19" && init > 0 && strings.TrimSpace(lines[init]) == "t13=no; t12=no" &&
				probeLoopServices(lines[init-1], variable, services)
		}
	}
	return false
}

const (
	probeTLS13 = `curl -sS -o /dev/null --tlsv1.3 -m 15 "https://$h19.api.cloud.ru/" 2>/dev/null && t13=yes`
	probeTLS12 = `curl -sS -o /dev/null --tlsv1.2 --tls-max 1.2 -m 15 "https://$h19.api.cloud.ru/" 2>/dev/null && t12=yes`
)

// Four negative inputs exercise redaction or rejection. Only the complete
// test statement, including its expected output, has this provenance.
func negativeHostFixture(file string, lines []string, row int) bool {
	switch file {
	case ".github/scripts/redact_test.sh":
		switch lines[row] {
		case `check "server: https://abc123.mk8s.ru-central-1.cloud.ru:6443" "server: https://<host>:6443"`,
			`check "db pg-123.private.cloud.ru:5432" "db <host>.cloud.ru:5432"`,
			`check "хост x.compute.api.cloud.ru" "хост <host>.cloud.ru"`:
			return true
		}
	case ".github/scripts/answers_catalogue_test.sh":
		if row == 0 || lines[row-1] != `cat >"$work/hostile.jsonl" <<'J'` || lines[row] != `{"request":"GET https://SECRET_MARKER\uff1abad.api.cloud.ru/api/v1/vms/id","status":200,"paths":{}}` {
			return false
		}
		// The inline fixture is read by the generator; it must produce an
		// empty catalogue and keep the marker out of stderr.
		block, _, _ := strings.Cut(strings.Join(lines[row:], "\n"), "\n\n")
		return strings.Contains(block, `"$work/hostile.jsonl" 2>"$work/herr"`) &&
			strings.Contains(block, `[ "$h" = "[]" ] ||`) &&
			strings.Contains(block, `if grep -q SECRET_MARKER "$work/herr"; then`)
	}
	return false
}

// traceIn is the trace of one text: a file of the repository, or a message
// of a commit or the description of a PR (file names it).
func traceIn(file, text string, services map[string]bool) []traceFinding {
	var out []traceFinding
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		add := func(rule, m string) { out = append(out, traceFinding{file, i + 1, rule, m}) }
		for _, pos := range brandHost.FindAllStringIndex(line, -1) {
			h := line[pos[0]:pos[1]]
			if !publicHost(h, services) && !publicProbeTemplate(file, lines, i, pos[0], h, services) && !negativeHostFixture(file, lines, i) {
				add("host", strings.ToLower(h))
			}
		}
		for _, u := range uuidRe.FindAllString(line, -1) {
			if !syntheticUUID(u) {
				add("uuid", u)
			}
		}
		for _, m := range ipv4Re.FindAllStringSubmatch(line, -1) {
			if !publicIP(m[1], m[2]) {
				add("ipv4", m[1])
			}
		}
		if !pathRulesAndBriefs(file) {
			for _, m := range internalPath.FindAllString(line, -1) {
				add("internal-path", strings.TrimRight(m, ".:"))
			}
		}
	}
	return out
}

func TestPublicTrace_HEAD(t *testing.T) {
	root := filepath.Join("..", "..")
	paths := files(t)
	if len(paths) < 1000 {
		t.Fatalf("%d files: the walk sees nothing", len(paths))
	}
	found := publicTraceFindings(t, root, paths)
	t.Logf("files read: %d; traces: %d", len(paths), len(found))
	for _, f := range found {
		t.Errorf("%s:%d: %s %q", f.file, f.line, f.rule, f.match)
	}
}

// Exercise the same inputs the HEAD walk reads. Replacing a host inside
// an otherwise valid test or probe must leave a finding, even in that file.
func TestPublicTrace_ProbeAndFixtureProvenance(t *testing.T) {
	root := filepath.Join("..", "..")
	services := inventoryServices(t, root)
	const foreign = "other-private.api.cloud.ru"
	read := func(file string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	hasHost := func(file, text, host string) bool {
		for _, f := range traceIn(file, text, services) {
			if f.rule == "host" && f.match == host {
				return true
			}
		}
		return false
	}
	for _, c := range []struct {
		file, host string
		count      int
	}{
		{".github/scripts/redact_test.sh", "abc123.mk8s.ru-central-1.cloud.ru", 1},
		{".github/scripts/redact_test.sh", "pg-123.private.cloud.ru", 1},
		{".github/scripts/redact_test.sh", "x.compute.api.cloud.ru", 1},
		{".github/scripts/answers_catalogue_test.sh", "uff1abad.api.cloud.ru", 1},
		{".github/scripts/cloud-live.sh", "host.api.cloud.ru", 1},
		{".github/workflows/api-probe.yml", "svc.api.cloud.ru", 2},
		{".github/workflows/api-probe.yml", "h19.api.cloud.ru", 2},
		{"internal/workflows/probe_hosts_test.go", "host.api.cloud.ru", 1},
	} {
		t.Run(c.file+"/"+c.host, func(t *testing.T) {
			text := read(c.file)
			if strings.Count(text, c.host) != c.count || hasHost(c.file, text, c.host) {
				t.Fatal("expected the reviewed occurrences only")
			}
			if !hasHost("unrelated.md", text, c.host) {
				t.Fatal("the fixture was accepted outside its source")
			}
			replaced := strings.ReplaceAll(text, c.host, foreign)
			if !hasHost(c.file, replaced, foreign) {
				t.Fatal("an unrelated host passed in the same statement")
			}
			if !hasHost(c.file, text+"\n"+c.host, c.host) {
				t.Fatal("a literal in the same file passed")
			}
		})
	}
	for _, c := range []struct{ file, old, replacement, host string }{
		{".github/scripts/cloud-live.sh", "redis) g=Г-15 host=redis", "redis) g=Г-15 host=unlisted", "host.api.cloud.ru"},
		{".github/workflows/api-probe.yml", "for svc in kafka redis; do", "for svc in kafka unlisted; do", "svc.api.cloud.ru"},
		{".github/workflows/api-probe.yml", "for h19 in compute vpc", "for h19 in unlisted vpc", "h19.api.cloud.ru"},
		{".github/scripts/redact_test.sh", `"server: https://<host>:6443"`, `"unexpected output"`, "abc123.mk8s.ru-central-1.cloud.ru"},
		{".github/scripts/answers_catalogue_test.sh", `[ "$h" = "[]" ]`, `[ "$h" = "other" ]`, "uff1abad.api.cloud.ru"},
	} {
		text := read(c.file)
		changed := strings.Replace(text, c.old, c.replacement, 1)
		if changed == text || !hasHost(c.file, changed, c.host) {
			t.Errorf("%s: a change to the source or its assertion passed: %s", c.file, c.old)
		}
	}
}

// Each rule finds what it names and leaves what is public or synthetic.
func TestPublicTrace_Rules(t *testing.T) {
	services := map[string]bool{"compute": true}
	for _, c := range []struct {
		file, text string
		want       []string // rule:match
	}{
		{"a.md", "https://compute.api.cloud.ru/api/v1/vms", nil},
		{"a.md", "the brand Cloud.ru, the site cloud.ru", nil},
		{"a.md", "a bucket b.s3.cloud.ru, a registry r.cr.cloud.ru", nil},
		{"a.md", "a mirror m-1.ar.cloud.ru and an API other.api.cloud.ru", []string{"host:m-1.ar.cloud.ru", "host:other.api.cloud.ru"}},
		{"a.md", "the old x.y.sbercloud.ru", []string{"host:x.y.sbercloud.ru"}},
		{"a.md", "00000000-0000-4000-8000-000000000001 0b5e9a6c-1111-2222-3333-444455556666", nil},
		{"a.md", "id 3f6c2b9e-7d41-4a8f-9c2e-5b1d8e7a4c90", []string{"uuid:3f6c2b9e-7d41-4a8f-9c2e-5b1d8e7a4c90"}},
		{"a.md", "192.0.2.5 10.1.2.3 100.64.0.1 198.18.255.10 0.0.0.0/0 128.0.0.0/1 ::ffff:128.0.0.0/97 1.2.3.4", nil},
		{"a.md", "the host 77.88.21.3 and 128.0.0.0", []string{"ipv4:77.88.21.3", "ipv4:128.0.0.0"}},
		{"a.md", "see evo-internal/runs/ and evo-internal/runs/rc-1.9.md", []string{"internal-path:evo-internal/runs/rc-1.9.md"}},
		{"docs/internal/briefs/x.md", "write to evo-internal/runs/rc-1.9.md", nil},
		{"a.md", "evo-internal/audits/2026/november/report.md", []string{"internal-path:evo-internal/audits/2026/november/report.md"}},
		{"a.md", "the site www.mycloud.ru", nil},
	} {
		var got []string
		for _, f := range traceIn(c.file, c.text, services) {
			got = append(got, f.rule+":"+f.match)
		}
		if strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("%s %q: found %v, want %v", c.file, c.text, got, c.want)
		}
	}
}

// modelCoauthor is the line of co-authorship that names a model (release
// plan 1.9, «Название модели — ни в коммиты, ни в squash main»): a word
// between «Claude» and the address. «Claude <address>» names no model and
// stays; a message keeps the link to the session, not the model.
var modelCoauthor = regexp.MustCompile(`(?im)^co-authored-by:[ \t]*claude[ \t]+[^<\s][^<\n]*<.*$`)

// TestPublicTrace_Text reads the texts that are not files of the
// repository: the title and description of a PR, the messages of its
// commits, the message of a commit on main (workflow public-trace). Each
// file of EVO_PUBLIC_TRACE_TEXT (separated by the list separator of the
// system) is one text; a trace in it fails, as a line of co-authorship that
// names a model does — with EVO_PUBLIC_TRACE_MODEL=warn that line is a
// warning: the commits of a PR made before the rule keep it, and the
// release squashes them with a message of its own (the plan line). Unset:
// nothing to read.
func TestPublicTrace_Text(t *testing.T) {
	list := os.Getenv("EVO_PUBLIC_TRACE_TEXT")
	if list == "" {
		t.Skip("EVO_PUBLIC_TRACE_TEXT is unset: no text of a PR or a commit to read")
	}
	services := inventoryServices(t, filepath.Join("..", ".."))
	var bad []string
	for _, path := range filepath.SplitList(list) {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(path)
		for _, f := range traceIn(name, string(b), services) {
			bad = append(bad, fmt.Sprintf("%s:%d: %s %q", f.file, f.line, f.rule, f.match))
		}
		for _, m := range modelCoauthor.FindAllString(string(b), -1) {
			msg := fmt.Sprintf("%s: a line of co-authorship names a model: %q — keep the link to the session only", name, strings.TrimSpace(m))
			if os.Getenv("EVO_PUBLIC_TRACE_MODEL") == "warn" {
				fmt.Printf("::warning title=Название модели в коммите::%s; squash — с сообщением без неё, автослияние не включать\n", msg)
				continue
			}
			bad = append(bad, msg)
		}
	}
	if len(bad) > 0 {
		t.Fatal("the public trace of a PR or a commit (remove it from the text — the description, a message; a message already pushed is rewritten by squash):\n" + strings.Join(bad, "\n"))
	}
}

func TestPublicTrace_ModelCoauthor(t *testing.T) {
	for line, want := range map[string]bool{
		"Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>": true,
		"co-authored-by: claude sonnet <a@b>":                     true,
		"Co-authored-by: Claude <noreply@anthropic.com>":          false,
		"Claude-Session: https://claude.ai/code/session_x":        false,
		"Co-Authored-By: Alice <a@b>":                             false,
	} {
		if got := modelCoauthor.MatchString("msg\n\n" + line); got != want {
			t.Errorf("%q: names a model %v, want %v", line, got, want)
		}
	}
}
