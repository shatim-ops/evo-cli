// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"net"
	"net/http"
	"sort"
	"sync"
)

// noNetwork is the transport under every client of evo in the tests of
// this package (httpretry.DefaultBase, TestMain): a request to loopback —
// a world, an httptest server — goes to base; any other is refused and
// noted. CLAUDE.md §5: the worlds of parity go to no network. On 06.11
// the tests of vm create, provision and deploy read the quotas of
// organization.api.cloud.ru (tools.QuotaProbe) and the names of
// compute.api.cloud.ru over the network of the runner, and one of them
// passed only while the real API answered (found by «1.8 — разработка
// (2)» behind the proxy of its session; «Процесс · 1.8 (2)» traced it).
//
// What a test that sets no world of its own meets off this machine is
// worldAnswers: each one the answer of the platform for the keys of the
// tests, so that no test passes on what the real API happened to say.
type noNetwork struct {
	base http.RoundTripper
	mu   sync.Mutex
	seen map[string]bool
}

func (n *noNetwork) RoundTrip(r *http.Request) (*http.Response, error) {
	host := r.URL.Hostname()
	if host == "localhost" {
		return n.base.RoundTrip(r)
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return n.base.RoundTrip(r)
	}
	if answer, ok := worldAnswers[r.Method+" "+host+r.URL.Path]; ok {
		return contractRT{status: answer.status, body: answer.body}.RoundTrip(r)
	}
	n.mu.Lock()
	n.seen[r.Method+" "+host+r.URL.Path] = true
	n.mu.Unlock()
	return nil, fmt.Errorf("test world: no network to %s (CLAUDE.md §5)", host)
}

// worldAnswers are the answers of the default world: "METHOD host/path" →
// the answer. Each is the platform's for the keys of the tests, not a
// guess about what a test needs.
var worldAnswers = map[string]struct {
	status int
	body   string
}{
	// The preview of a VM reads the project's quotas (D60, tools.QuotaProbe):
	// a key of a project reads none — 403, «quota: not checked (project key)».
	"GET organization.api.cloud.ru/v3/quotas/usages": {http.StatusForbidden, `{"code":"PERMISSION_DENIED","message":"a project key reads no quotas"}`},
}

// leaks are the requests that tried to leave the machine, sorted.
func (n *noNetwork) leaks() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]string, 0, len(n.seen))
	for k := range n.seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
