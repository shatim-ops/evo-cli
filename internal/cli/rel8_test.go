// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// deadRT is a proxy that never answers: every request fails, IAM included.
type deadRT struct{ n atomic.Int32 }

func (d *deadRT) RoundTrip(*http.Request) (*http.Response, error) {
	d.n.Add(1)
	return nil, errors.New("proxyconnect tcp: dial tcp 127.0.0.1:9: connect: connection refused")
}

// REL-8: ключ есть, проекта нет, прокси мёртв. 'evo deploy' обменивал ключ
// на токен до проверки проекта и падал с API_UNREACHABLE (код 1); 'evo vm
// list' в том же положении отвечал CONFIG_INVALID (код 2). Теперь deploy —
// так же, и ни одного запроса.
func TestRel8_DeployWithoutProjectBeforeToken(t *testing.T) {
	setupCLI(t)
	rt := &deadRT{}
	oldRT, oldProxy := http.DefaultTransport, clerrors.ProxyFunc
	http.DefaultTransport = rt
	clerrors.ProxyFunc = func(*http.Request) (*url.URL, error) { return nil, nil }
	t.Cleanup(func() { http.DefaultTransport, clerrors.ProxyFunc = oldRT, oldProxy })
	saveProfile(t, "default", "")
	if err := os.WriteFile("docker-compose.yml", []byte("services:\n  web:\n    image: nginx\n    ports: [\"80:80\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := clerrors.ProjectRequired()
	for _, args := range [][]string{{"vm", "list"}, {"deploy", "--dry-run"}, {"deploy", "--yes"}} {
		code, _, stderr := runCLI(t, "", append([]string{"--json"}, args...)...)
		e := lastJSONError(t, stderr)
		if code != clerrors.ExitAuth || e.Reason != clerrors.CodeConfigInvalid || !strings.HasSuffix(e.Message, want.Message) || e.Hint != want.Hint {
			t.Errorf("%v: exit %d, %+v; want exit %d, CONFIG_INVALID", args, code, e, clerrors.ExitAuth)
		}
	}
	if n := rt.n.Load(); n != 0 {
		t.Errorf("%d requests before the refusal", n)
	}
}
