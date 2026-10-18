// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Релизный аудит, A-003: ошибка без кода получала API_ERROR (код 5, «API
// Cloud.ru ответил ошибкой»), даже когда в облако ничего не уходило:
// stack_apply job_id=../../etc/passwd, helm с относительным repo_url.
// Теперь причина — по происхождению ошибки, коды — из docs/exit-codes.md.
func TestUncodedReason(t *testing.T) {
	exitErr := &exec.ExitError{ProcessState: new(os.ProcessState)}
	for _, tt := range []struct {
		name   string
		err    error
		fields map[string]any
		want   string
		exit   int
	}{
		// Ответ API — по HTTP-статусу.
		{"статус compute", fmt.Errorf("x: %w", &provision.StatusError{Status: 422, Body: "bad"}), nil, clierrors.CodeAPIError, 5},
		{"404 клиента сервиса", errors.New("nlb API 404: no such balancer"), nil, clierrors.CodeNotFound, 4},
		{"429 клиента сервиса", errors.New("secret API 429: slow down"), nil, clierrors.CodeAPIRateLimited, 5},
		{"5xx клиента сервиса", fmt.Errorf("create zone: %w", errors.New("dns API 500: internal")), nil, clierrors.CodeAPIError, 5},
		{"HTTP: 502", errors.New("HTTP: 502"), nil, clierrors.CodeAPIError, 5},
		{"API returned status", errors.New("VM v1 was created, but the create call failed (API_ERROR: API returned status 500: x)"), nil, clierrors.CodeAPIError, 5},
		{"Organization API", &adminHTTPError{Status: 503, Msg: "down"}, nil, clierrors.CodeAPIError, 5},
		{"S3", fmt.Errorf("list buckets: %w", minio.ErrorResponse{Code: "AccessDenied", StatusCode: 403}), nil, clierrors.CodeAPIError, 5},
		// Ответа нет — сеть.
		{"сеть", fmt.Errorf("vpc request failed: %w", &url.Error{Op: "Get", URL: "https://vpc.api.cloud.ru", Err: &net.OpError{Op: "dial", Err: errors.New("refused")}}), nil, clierrors.CodeAPIUnreachable, 1},
		{"DNS", &net.DNSError{Err: "no such host", Name: "x"}, nil, clierrors.CodeAPIUnreachable, 1},
		{"срок", fmt.Errorf("kubectl timed out after 1m30s: %w", context.DeadlineExceeded), nil, clierrors.CodeAPIUnreachable, 1},
		// Адрес, который не разобрать, — ошибка вызова, не сети.
		{"URL не разбирается", &url.Error{Op: "parse", URL: "::", Err: errors.New("missing protocol scheme")}, nil, clierrors.CodeUsage, 1},
		// Операция облака завершилась ошибкой.
		{"операция", errors.New("mk8s operation op-1 failed: quota exceeded"), nil, clierrors.CodeAPIError, 5},
		// kubectl / helm.
		{"kubectl: сервер отказал", exitErr, map[string]any{"output": "Error from server (Forbidden): pods is forbidden"}, clierrors.CodeAPIError, 5},
		{"kubectl: не найдено", exitErr, map[string]any{"output": `Error from server (NotFound): pods "x" not found`}, clierrors.CodeNotFound, 4},
		{"kubectl: нет связи", exitErr, map[string]any{"output": "Unable to connect to the server: dial tcp 10.0.0.1:6443: i/o timeout"}, clierrors.CodeAPIUnreachable, 1},
		{"kubectl: неизвестный флаг", exitErr, map[string]any{"output": "error: unknown flag: --nope"}, clierrors.CodeUsage, 1},
		{"helm: относительный URL", exitErr, map[string]any{"stage": "repo add", "output": `Error: could not find protocol handler for: non-absolute URLs are not supported`}, clierrors.CodeUsage, 1},
		{"helm: прочее", exitErr, map[string]any{"output": "Error: INSTALLATION FAILED: rendered manifests contain a resource that already exists"}, clierrors.CodeAPIError, 5},
		{"kubectl не установлен", &exec.Error{Name: "kubectl", Err: exec.ErrNotFound}, nil, clierrors.CodeConfigInvalid, 2},
		// ВМ создана, но не поднялась — как у deploy_apply.
		{"ВМ provision", errors.New("VM v1 created but did not become active within 5m0s"), map[string]any{"status": "timeout", "vm_id": "v1"}, clierrors.CodeVMProvisionFailed, 1},
		// Локальные ошибки: в облако ничего не уходило.
		{"job_id", fmt.Errorf("invalid job_id %q (want stk-<10 hex digits>, as stack_plan returns)", "../../etc/passwd"), nil, clierrors.CodeUsage, 1},
		{"манифест", fmt.Errorf("invalid manifest: %w", &json.SyntaxError{}), nil, clierrors.CodeUsage, 1},
		{"ответ API не JSON", fmt.Errorf("cert_versions: parse certmanager response: %w", &json.SyntaxError{}), nil, clierrors.CodeAPIError, 5},
		{"ответ mk8s не JSON", fmt.Errorf("parse cluster: %w", &json.SyntaxError{}), nil, clierrors.CodeAPIError, 5},
		{"ответ Container Apps не JSON", fmt.Errorf("parse containers: %w", &json.SyntaxError{}), nil, clierrors.CodeAPIError, 5},
		{"каталог плагинов mk8s не JSON", fmt.Errorf("parse addon releases: %w", &json.SyntaxError{}), nil, clierrors.CodeAPIError, 5},
		{"ответ OIDC не JSON", fmt.Errorf("embed_token: OIDC token exchange failed: %w", fmt.Errorf("parse oidc token: %w", &json.SyntaxError{})), nil, clierrors.CodeAPIError, 5},
		{"ответ API не той формы", fmt.Errorf("parse vpcs: %w", &json.UnmarshalTypeError{Value: "string"}), nil, clierrors.CodeAPIError, 5},
		{"план не найден", errors.New("plan p-1 not found or expired"), nil, clierrors.CodeNotFound, 4},
		{"подбор", errors.New("no matching node flavor (cpu>=64 ram>=512); see k8s_options"), nil, clierrors.CodeUsage, 1},
	} {
		var got map[string]any
		if err := json.Unmarshal([]byte(errAnswer(tt.err, tt.fields)), &got); err != nil {
			t.Fatal(err)
		}
		if got["reason"] != tt.want {
			t.Errorf("%s: reason %v, want %s (%v)", tt.name, got["reason"], tt.want, got)
		}
		if exit := clierrors.ExitCode(clierrors.New(tt.want, "", "")); exit != tt.exit {
			t.Errorf("%s: %s exits %d, docs/exit-codes.md says %d", tt.name, tt.want, exit, tt.exit)
		}
	}
}

// Живые примеры аудита: stack_apply с чужим job_id и helm с относительным
// repo_url — USAGE до любого обращения к облаку.
func TestUncodedReason_AuditExamples(t *testing.T) {
	r := New(nilFactory)
	_, text, isErr := callNamed(t, r, "stack_apply", map[string]any{"job_id": "../../etc/passwd", "confirmed": true})
	if !isErr || !strings.Contains(text, `"reason": "USAGE"`) || !strings.Contains(text, "invalid job_id") {
		t.Errorf("stack_apply job_id=../../etc/passwd: %s", text)
	}
	_, text, isErr = callNamed(t, r, "k8s_helm", map[string]any{"cluster_id": "c", "action": "install", "release": "r", "chart": "repo/app", "repo_url": "charts/local", "confirmed": true})
	if !isErr || !strings.Contains(text, `"reason": "USAGE"`) || !strings.Contains(text, "repo_url") {
		t.Errorf("k8s_helm repo_url=charts/local: %s", text)
	}
	_, text, isErr = callNamed(t, r, "image_create", map[string]any{"name": "img1", "disk_id": "d", "image_metadata": "nope", "confirmed": true})
	if !isErr || !strings.Contains(text, `"reason": "USAGE"`) || !strings.Contains(text, "image_metadata") {
		t.Errorf("image_create image_metadata=nope: %s", text)
	}
}
