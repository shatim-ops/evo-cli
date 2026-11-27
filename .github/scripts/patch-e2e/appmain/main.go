// Narrow network boundary for the candidate's ordinary CLI and MCP commands.
package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/shatim-ops/evo-cli/internal/cli"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
)

const candidate = "d7645da5efea04122f984cfd8cb9a28408d08177"

var refused = errors.New("request outside the verified Container Apps operation")

type fence struct {
	base     http.RoundTripper
	name     string
	project  string
	evidence string
	until    time.Time
}

func (f fence) RoundTrip(r *http.Request) (*http.Response, error) {
	u := r.URL
	if u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.RawPath != "" || !time.Now().Before(f.until) {
		return nil, refused
	}
	if u.Host == "iam.api.cloud.ru" && u.Path == "/api/v1/auth/token" && u.RawQuery == "" && r.Method == http.MethodPost {
		return f.base.RoundTrip(r)
	}
	if u.Host != "containers.api.cloud.ru" || u.Path != "/v2/containers/"+f.name || u.RawQuery != "projectId="+f.project {
		return nil, refused
	}
	if r.Method == http.MethodGet {
		return f.base.RoundTrip(r)
	}
	if r.Method != http.MethodDelete || f.evidence == "" || r.ContentLength != 0 {
		return nil, refused
	}
	// The outer retry transport may ask again after 401/429. Exactly one
	// attempt may acquire this file. Never remove it to retry a failed send.
	out, err := os.OpenFile(f.evidence, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, refused
	}
	defer out.Close()
	if _, err := fmt.Fprintln(out, "attempt", time.Now().UnixMilli()); err != nil || out.Sync() != nil {
		return nil, refused
	}
	resp, err := f.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintln(out, resp.StatusCode, time.Now().UnixMilli()); err != nil || out.Sync() != nil {
		resp.Body.Close()
		return nil, refused
	}
	return resp, nil
}

func main() {
	until, err := strconv.ParseInt(os.Getenv("P90_CA_DEADLINE"), 10, 64)
	if err != nil {
		os.Exit(1)
	}
	base := httpretry.NewBaseTransport()
	base.Protocols = new(http.Protocols)
	base.Protocols.SetHTTP1(true)
	base.DisableKeepAlives = true
	base.Proxy = nil
	httpretry.DefaultBase = fence{base, os.Getenv("P90_CA_NAME"), os.Getenv("CLOUDRU_PROJECT_ID"),
		os.Getenv("P90_CA_WIRE"), time.Unix(until, 0)}
	cli.Main("1.9.1-preview", candidate, "unknown")
}
