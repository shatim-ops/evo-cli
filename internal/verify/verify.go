// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package verify

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
)

// ProbeResult holds the outcome of a single health check. Its JSON is the
// output of "evo verify --json", and its keys are a contract
// (docs/exit-codes.md): snake_case like the rest of the CLI's JSON.
type ProbeResult struct {
	Port       int  `json:"port"`
	StatusCode int  `json:"status_code"` // 0 without an HTTP answer
	Healthy    bool `json:"healthy"`
	// Latency is "latency_ms" in JSON, whole milliseconds (MarshalJSON).
	Latency time.Duration `json:"-"`
	Error   string        `json:"error,omitempty"` // non-empty if probe failed
	// Protocol is how the port answered: "http", "https", or "tcp" for a
	// service that accepts connections but does not speak HTTP (a database,
	// a cache). Empty when nothing answered.
	Protocol string `json:"protocol"`
}

// MarshalJSON adds Latency as latency_ms: a number of milliseconds, not the
// nanoseconds of a bare time.Duration.
func (r ProbeResult) MarshalJSON() ([]byte, error) {
	type fields ProbeResult // without the method
	return json.Marshal(struct {
		fields
		LatencyMS int64 `json:"latency_ms"`
	}{fields(r), r.Latency.Milliseconds()})
}

// Check probes each port of ip: a TCP connect, then HTTP (or HTTPS). An HTTP
// answer is healthy for 2xx/3xx; a port that accepts connections but does
// not speak HTTP is healthy as "tcp"; a closed or hanging port is not.
// Returns one ProbeResult per port. Failures are recorded but not returned as errors
// (health checks are non-blocking warnings).
func Check(ctx context.Context, ip string, ports []int, timeout time.Duration) []ProbeResult {
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	results := make([]ProbeResult, len(ports))
	for i, port := range ports {
		results[i] = probe(ctx, ip, port, timeout)
	}
	return results
}

func probe(ctx context.Context, ip string, port int, timeout time.Duration) ProbeResult {
	// Is anything listening? A closed port is unhealthy whatever it serves.
	start := clock.Now()
	conn, err := (&net.Dialer{Timeout: timeout}).DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(port)))
	if err != nil {
		return ProbeResult{Port: port, Latency: clock.Since(start), Error: err.Error()}
	}
	tcpLatency := clock.Since(start)
	_ = conn.Close()

	result, err := httpProbe(ctx, ip, port, timeout)
	if err == nil {
		return result
	}
	// The port accepts connections but does not answer HTTP or HTTPS
	// (Postgres, Redis, MySQL close or answer in their own protocol): the
	// service is up. A timeout is not that: an HTTP service that hangs
	// stays unhealthy.
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() || ctx.Err() != nil {
		return result
	}
	return ProbeResult{Port: port, Healthy: true, Latency: tcpLatency, Protocol: "tcp"}
}

// httpProbe asks the port over HTTP, then over HTTPS when that is what it
// seems to want. The error is that of the plain HTTP attempt when neither
// produced an HTTP response.
func httpProbe(ctx context.Context, ip string, port int, timeout time.Duration) (ProbeResult, error) {
	result, err := get(ctx, "http", ip, port, timeout)
	// An HTTPS-only port answers plain HTTP with 400 (nginx, Go) or a TLS
	// alert that is not HTTP at all. Ask it over TLS; keep the plain result
	// if that does not work either. A timeout is not retried: it would
	// double the wait for a service that does not answer.
	var netErr net.Error
	timedOut := errors.As(err, &netErr) && netErr.Timeout()
	if (err != nil && !timedOut && ctx.Err() == nil) || result.StatusCode == http.StatusBadRequest {
		if tlsResult, tlsErr := get(ctx, "https", ip, port, timeout); tlsErr == nil {
			return tlsResult, nil
		}
	}
	return result, err
}

// get probes scheme://ip:port/ once. Redirects are not followed: a 3xx is
// the service answering, and following it would probe another host (often
// https:// with a certificate for a name, not for this IP). Certificates are
// not checked for the same reason — this is a liveness check, not trust.
func get(ctx context.Context, scheme, ip string, port int, timeout time.Duration) (ProbeResult, error) {
	result := ProbeResult{Port: port}

	client := &http.Client{
		Timeout:       timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	if scheme == "https" {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // liveness probe of an IP
		client.Transport = tr
		defer tr.CloseIdleConnections()
	}

	url := fmt.Sprintf("%s://%s/", scheme, net.JoinHostPort(ip, strconv.Itoa(port)))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		result.Error = err.Error()
		return result, err
	}

	start := clock.Now()
	resp, err := client.Do(req)
	result.Latency = clock.Since(start)

	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode
	result.Healthy = resp.StatusCode >= 200 && resp.StatusCode < 400
	result.Protocol = scheme

	return result, nil
}
