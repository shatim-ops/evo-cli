// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// tariffSnapshot is a public Cloud.ru tariff as evo prices by it: the page,
// its version, the date it came in force, and its lines.
type tariffSnapshot struct {
	URL     string       `json:"url"`
	Version string       `json:"version"`
	InForce string       `json:"in_force"`
	Rates   []tariffRate `json:"rates"`
}

// tariffRate is a line of a tariff: what is billed, by which unit and
// period, at which price with and without VAT (as the page writes them).
type tariffRate struct {
	Name       string `json:"name"`
	Unit       string `json:"unit"`
	Period     string `json:"period"`
	WithVAT    string `json:"with_vat"`
	WithoutVAT string `json:"without_vat"`
}

const pgTariffSnapshot = "testdata/tariff-managed-postgresql.json"

func loadTariffSnapshot(t *testing.T, path string) tariffSnapshot {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var s tariffSnapshot
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return s
}

var (
	tariffTag     = regexp.MustCompile(`(?s)<script.*?</script>|<style.*?</style>|<[^>]+>`)
	tariffVersion = regexp.MustCompile(`^Версия (\S+)$`)
	tariffInForce = regexp.MustCompile(`^Вступление в силу: (.+)$`)
	tariffRowNo   = regexp.MustCompile(`^\d+\.$`)
	tariffPrice   = regexp.MustCompile(`^\d+(,\d+)?$`)
)

// parseTariffPage reads a tariff page of cloud.ru/documents/tariffs: its
// text lines carry «Версия N», «Вступление в силу: …» and a table whose
// rows are «N.», name, unit, period, price with VAT, without VAT, VAT.
func parseTariffPage(page string) (tariffSnapshot, error) {
	var s tariffSnapshot
	var lines []string
	for _, l := range strings.Split(html.UnescapeString(tariffTag.ReplaceAllString(page, "\n")), "\n") {
		if l = strings.Join(strings.Fields(l), " "); l != "" {
			lines = append(lines, l)
		}
	}
	for i, l := range lines {
		if m := tariffVersion.FindStringSubmatch(l); m != nil && s.Version == "" {
			s.Version = m[1]
		}
		if m := tariffInForce.FindStringSubmatch(l); m != nil && s.InForce == "" {
			s.InForce = m[1]
		}
		if !tariffRowNo.MatchString(l) || i+5 >= len(lines) {
			continue
		}
		r := tariffRate{Name: lines[i+1], Unit: lines[i+2], Period: lines[i+3], WithVAT: lines[i+4], WithoutVAT: lines[i+5]}
		if !tariffPrice.MatchString(r.WithVAT) || !tariffPrice.MatchString(r.WithoutVAT) {
			return s, fmt.Errorf("row %s %q: the prices are %q and %q — the table changed its form", l, r.Name, r.WithVAT, r.WithoutVAT)
		}
		s.Rates = append(s.Rates, r)
	}
	if s.Version == "" || s.InForce == "" || len(s.Rates) == 0 {
		return s, fmt.Errorf("no version (%q), date in force (%q) or rates (%d) — the page changed its form", s.Version, s.InForce, len(s.Rates))
	}
	return s, nil
}

// The price of PostgreSQL before consent is reckoned from a tariff card
// (release plan 1.6, «Цена PostgreSQL до согласия»; owner's decision 29.10,
// #570): an old card is a wrong price the person agrees to. The public
// page is checked against the snapshot the card is made from — its
// version, date in force and every line: a new version or another rate
// fails, and the card and the snapshot change together. The page is on
// the network: EVO_LIVE_TARIFFS=1, in job contract once a week and by hand
// before a release candidate (TestWorkflows_ContractChecksTheTariff).
func TestLive_TariffPageMatchesSnapshot(t *testing.T) {
	if os.Getenv("EVO_LIVE_TARIFFS") != "1" {
		t.Skip("network: EVO_LIVE_TARIFFS=1 (job contract)")
	}
	want := loadTariffSnapshot(t, pgTariffSnapshot)
	page, err := fetchTariffPage(want.URL)
	if err != nil {
		// One more try: the site drops a request now and then.
		page, err = fetchTariffPage(want.URL)
	}
	if err != nil {
		t.Fatalf("%s: %v", want.URL, err)
	}
	got, err := parseTariffPage(page)
	if err != nil {
		t.Fatalf("%s: %v", want.URL, err)
	}
	got.URL = want.URL
	if got.Version != want.Version || got.InForce != want.InForce {
		t.Errorf("%s: version %s in force %s, the snapshot has %s in force %s — a new tariff: update the card of pricing and %s together", want.URL, got.Version, got.InForce, want.Version, want.InForce, pgTariffSnapshot)
	}
	if fmt.Sprint(got.Rates) != fmt.Sprint(want.Rates) {
		t.Errorf("%s: the rates differ from %s\npage:     %v\nsnapshot: %v", want.URL, pgTariffSnapshot, got.Rates, want.Rates)
	}
}

func fetchTariffPage(u string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	// Without a User-Agent the site answers 403 (register, documentation
	// portal).
	req.Header.Set("User-Agent", "evo-tariff-check")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET answers %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return string(b), err
}

// The parse of a tariff page, offline: the snapshot is what the page of
// its form gives, and a page whose table lost its prices is an error, not
// an empty tariff.
func TestTariffPage_Parse(t *testing.T) {
	want := loadTariffSnapshot(t, pgTariffSnapshot)
	var b strings.Builder
	b.WriteString("<html><body><h1>Тарифы</h1><div>Версия " + want.Version + "</div><p>Вступление в силу: " + want.InForce + "</p><table>")
	for i, r := range want.Rates {
		fmt.Fprintf(&b, "<tr><td>%d.</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>0</td></tr>",
			i+1, html.EscapeString(r.Name), r.Unit, r.Period, r.WithVAT, r.WithoutVAT)
	}
	b.WriteString("</table><script>var x = '1.';</script></body></html>")
	got, err := parseTariffPage(b.String())
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != want.Version || got.InForce != want.InForce || fmt.Sprint(got.Rates) != fmt.Sprint(want.Rates) {
		t.Errorf("parsed %+v, want %+v", got, want)
	}
	if _, err := parseTariffPage(strings.Replace(b.String(), want.Rates[0].WithVAT, "по запросу", 1)); err == nil {
		t.Error("a row without its price parsed without an error")
	}
	if _, err := parseTariffPage("<html>moved</html>"); err == nil {
		t.Error("a page without a tariff parsed without an error")
	}
}
