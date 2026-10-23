// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"net/http"
	"os"
	"sort"
	"testing"
	"time"
)

// Every tariff_url a preview gives answers 200 to HEAD, without a
// redirect: Cloud.ru may move the pages, and a preview would then send the
// person to a 404 or to the front page (review Qwen #170). The pages are
// on the network, so the test runs only with EVO_LIVE_TARIFFS=1 — in the
// readonly job of cloud-e2e (TestWorkflows_ReadonlyChecksTariffPages).
func TestLive_TariffPagesAnswer(t *testing.T) {
	if os.Getenv("EVO_LIVE_TARIFFS") != "1" {
		t.Skip("network: EVO_LIVE_TARIFFS=1 (cloud-e2e readonly)")
	}
	byURL := map[string][]string{}
	for res, u := range tariffs {
		byURL[u] = append(byURL[u], res)
	}
	urls := make([]string, 0, len(byURL))
	for u := range byURL {
		urls = append(urls, u)
	}
	sort.Strings(urls)
	client := &http.Client{
		Timeout:       20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	for _, u := range urls {
		code, err := headStatus(client, u)
		if err != nil || code >= 500 {
			// One more try: the site drops a request now and then.
			code, err = headStatus(client, u)
		}
		switch {
		case err != nil:
			t.Errorf("%s (%v): %v", u, byURL[u], err)
		case code != http.StatusOK:
			t.Errorf("%s (%v): HEAD answers %d, want 200 — the tariff page moved? (tariffs, create_preview.go)", u, byURL[u], code)
		default:
			t.Logf("%s: 200", u)
		}
	}
}

func headStatus(client *http.Client, u string) (int, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodHead, u, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}
