// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/minio/minio-go/v7"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// statusClientClosed is the 499 Container Apps answers a resource that is
// not there with («… is not found»).
const statusClientClosed = 499

// uncodedReason is the reason of a tool error that carries no code (no
// CLIError in the chain, no "CODE: " prefix). Every such error used to be
// API_ERROR (exit 5, "Cloud.ru answered with an error"), local refusals as
// well: stack_apply job_id=../../etc/passwd, a bad helm repo_url (release
// audit, A-003). Now, in this order:
//   - an answer of an API, by its HTTP status (provision.StatusError, the
//     Organization API, S3, or "<service> API 404: …" / "HTTP 502: …" in the
//     message of the service clients): 404 — NOT_FOUND, 429 —
//     API_RATE_LIMITED, a 400, 422 or 499 whose answer says "not found" —
//     NOT_FOUND, else API_ERROR;
//   - no answer at all (network, DNS, a deadline) — API_UNREACHABLE;
//   - a failed operation of the cloud ("mk8s operation … failed"), or
//     kubectl/helm that ran against the cluster and failed — API_ERROR
//     (unless kubectl/helm could not reach it, or refused their arguments);
//   - kubectl/helm not installed — CONFIG_INVALID;
//   - an error about a VM provision created (vm_id in the answer) —
//     VM_PROVISION_FAILED, as deploy_apply says it;
//   - else the error is local: "not found" ("не найден" in Russian) in the
//     answer — NOT_FOUND (a plan, a job), anything else — USAGE (exit 1).
func uncodedReason(err error, fields map[string]any) string {
	answer := strings.ToLower(toJSON(fields) + " " + err.Error())
	notFound := strings.Contains(answer, "not found") || strings.Contains(answer, "не найден")
	if st := apiStatus(err); st != 0 {
		// The status decides; the words of the answer name a missing
		// resource only on the statuses the platform answers it with
		// besides 404: 400 (DBaaS, register DB-13), 422 (Compute images),
		// 499 (Container Apps). A 403, 409, 429 or 5xx whose text says
		// «not found» is not «no such resource» (cloud-e2e 24.10:
		// pg_create, 429 ResourceExhausted).
		switch {
		case st == http.StatusNotFound:
			return clierrors.CodeNotFound
		case st == http.StatusTooManyRequests:
			return clierrors.CodeAPIRateLimited
		case notFound && (st == http.StatusBadRequest || st == http.StatusUnprocessableEntity || st == statusClientClosed):
			return clierrors.CodeNotFound
		}
		return clierrors.CodeAPIError
	}
	if unreachable(err) {
		return clierrors.CodeAPIUnreachable
	}
	var ee *exec.ExitError
	switch {
	case stderrors.As(err, &ee):
		return toolExitReason(answer, notFound)
	case stderrors.Is(err, exec.ErrNotFound):
		return clierrors.CodeConfigInvalid
	case operationFailedRE.MatchString(answer):
		if notFound {
			return clierrors.CodeNotFound
		}
		return clierrors.CodeAPIError
	}
	// A service client could not read the API's answer ("parse vpcs: …",
	// "parse certmanager response: invalid character '<'" — a gateway's
	// HTML page): the cloud answered wrong, not the call (Grok review of
	// PR #89).
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	if (stderrors.As(err, &syntaxErr) || stderrors.As(err, &typeErr)) && parseAnswerRE.MatchString(answer) {
		return clierrors.CodeAPIError
	}
	// An answer that ended before its JSON or XML did: an empty 200 to
	// PUT /disks ("resize disk: EOF"), an empty S3 list. Tools read no
	// local stream that could end this way, so the cloud answered wrong
	// (break r3b, R3B-2).
	if stderrors.Is(err, io.EOF) || stderrors.Is(err, io.ErrUnexpectedEOF) {
		return clierrors.CodeAPIError
	}
	if _, ok := fields["vm_id"]; ok {
		return clierrors.CodeVMProvisionFailed
	}
	if notFound {
		return clierrors.CodeNotFound
	}
	return clierrors.CodeUsage
}

// uncodedHint is the hint of an error answer that has none: the service
// clients (vpc aside, which is compute) return plain errors, and a 401, a
// 403 or a network failure reached the person as "mk8s API 403: …" or a Go
// transport error without a word on what to fix. The exit code stays the
// reason's (403 from these services is API_ERROR, docs/exit-codes.md).
func uncodedHint(err error, reason string) string {
	switch reason {
	case clierrors.CodeAPIUnreachable:
		return clierrors.NetworkHint(err, i18n.T("Check your network connection and try again", "Проверьте подключение к сети и повторите"))
	case clierrors.CodeAPIError:
		switch apiStatus(err) {
		case http.StatusUnauthorized:
			return clierrors.TokenRejectedHint()
		case http.StatusForbidden:
			// Object Storage is reached with keys of its own (Ц-3).
			var se minio.ErrorResponse
			if stderrors.As(err, &se) {
				return clierrors.S3AccessHint()
			}
			return clierrors.NoRoleHint()
		}
	}
	return ""
}

// apiStatusRE finds the HTTP status in the message of a service client's
// error: "vpc API 404: …", "HTTP 502: …", "HTTP: 502", "OIDC token HTTP
// 401: …", "API returned status 422" (and its Russian text).
var apiStatusRE = regexp.MustCompile(`\b(?:API|HTTP):? ([1-5][0-9]{2})\b|\b(?:API returned status|API вернул статус) ([1-5][0-9]{2})\b`)

// parseAnswerRE is how the service clients wrap a JSON error of the API's
// answer: "parse <what>: …", or "разбор <что>: …" in the Russian CLI
// (i18n.T of the clients; RE2's \b does not see Cyrillic). The reason must
// not depend on the CLI's language (review of #354). A local manifest
// says "invalid manifest", a job file "parse job <id>".
var parseAnswerRE = regexp.MustCompile(`\bparse [a-z_ ]+: |(?:^|[\s(:])разбор [\p{Cyrillic}a-z_ ]+: `)

// operationFailedRE is an asynchronous operation the cloud reported failed.
var operationFailedRE = regexp.MustCompile(`\boperation\b[^"]*\bfailed\b`)

// apiStatus is the HTTP status of the API answer behind err, 0 without one.
func apiStatus(err error) int {
	if st := provision.HTTPStatus(err); st != 0 {
		return st
	}
	var he *adminHTTPError
	if stderrors.As(err, &he) {
		return he.Status
	}
	var se minio.ErrorResponse
	if stderrors.As(err, &se) && se.StatusCode != 0 {
		return se.StatusCode
	}
	if m := apiStatusRE.FindStringSubmatch(err.Error()); m != nil {
		n, _ := strconv.Atoi(m[1] + m[2])
		return n
	}
	return 0
}

// unreachable reports an error without an answer from the API: the
// connection, DNS, TLS or a deadline. A URL that does not parse is the
// caller's, not the network's.
func unreachable(err error) bool {
	var ue *url.Error
	if stderrors.As(err, &ue) {
		return ue.Op != "parse"
	}
	var oe *net.OpError
	var de *net.DNSError
	return stderrors.As(err, &oe) || stderrors.As(err, &de) ||
		stderrors.Is(err, context.DeadlineExceeded) || stderrors.Is(err, context.Canceled)
}

// kubectl and helm print these when they could not reach the cluster, or
// refused their own arguments before asking it.
var (
	toolUnreachable = []string{"unable to connect to the server", "dial tcp", "i/o timeout", "no such host", "connection refused", "tls handshake timeout"}
	toolUsage       = []string{"unknown flag", "unknown shorthand flag", "unknown command", "required flag", "accepts ", "requires at least", "non-absolute", "unsupported protocol scheme", "invalid argument"}
)

// toolExitReason classifies a kubectl or helm run that exited non-zero
// from its output (in answer, lowercased).
func toolExitReason(answer string, notFound bool) string {
	switch {
	case notFound:
		return clierrors.CodeNotFound
	case strings.Contains(answer, "error from server"):
		return clierrors.CodeAPIError
	}
	for _, s := range toolUnreachable {
		if strings.Contains(answer, s) {
			return clierrors.CodeAPIUnreachable
		}
	}
	for _, s := range toolUsage {
		if strings.Contains(answer, s) {
			return clierrors.CodeUsage
		}
	}
	return clierrors.CodeAPIError
}
