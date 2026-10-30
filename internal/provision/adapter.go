// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

const (
	defaultBaseURL = "https://compute.api.cloud.ru"
	flavorCacheTTL = 1 * time.Hour
	initialBackoff = 1 * time.Second
)

// CloudRuAdapter implements CloudAPI using the Cloud.ru Compute REST API.
type CloudRuAdapter struct {
	client    *http.Client
	baseURL   string
	token     string
	tokenSrc  func(context.Context) (string, error)
	mu        sync.RWMutex
	flavors   []*Flavor
	flavorExp time.Time

	// Backoff is the delay before the first retry; it doubles on each next
	// attempt. Zero means the production default (1s). Tests set milliseconds.
	Backoff time.Duration
}

// NewCloudRuAdapter creates a new adapter. If client is nil, a client with a
// 60s timeout is used (http.DefaultClient has none); the timeout covers all
// attempts of a request together, as httpretry's does.
// If baseURL is empty, the default Cloud.ru compute endpoint is used.
func NewCloudRuAdapter(client *http.Client, baseURL string) *CloudRuAdapter {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second, Transport: httpretry.UserAgent(nil)}
	}
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &CloudRuAdapter{
		client:  client,
		baseURL: baseURL,
	}
}

// SetToken updates the bearer token used for API calls.
func (a *CloudRuAdapter) SetToken(token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.token = token
}

// SetTokenSource makes every request ask src for the token, with the token
// from SetToken as the fallback when src fails. A token taken once at the
// start of a command expired during a deploy that waited ten minutes for
// its VM (Fable on r3a); the provider keeps the token and renews it five
// minutes before it expires, so asking it per request costs nothing.
func (a *CloudRuAdapter) SetTokenSource(src func(context.Context) (string, error)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tokenSrc = src
}

func (a *CloudRuAdapter) getToken(ctx context.Context) string {
	a.mu.RLock()
	src, tok := a.tokenSrc, a.token
	a.mu.RUnlock()
	if src != nil {
		if t, err := src(ctx); err == nil && t != "" {
			return t
		}
	}
	return tok
}

// CreateVM creates a new virtual machine via POST /api/v1.1/vms.
// The API expects a JSON array of VM specs and returns an array of created VMs.
func (a *CloudRuAdapter) CreateVM(ctx context.Context, req CreateVMRequest) (*VM, error) {
	body, err := json.Marshal([]CreateVMRequest{req})
	if err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, i18n.T("failed to marshal create request", "не удалось сформировать запрос на создание ВМ"), hintCheckRequest(), err)
	}

	resp, err := a.doWithRetry(ctx, http.MethodPost, "/api/v1.1/vms", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, i18n.T("failed to read create response", "не удалось прочитать ответ на создание ВМ"), hintUnexpectedFormat(), err)
	}

	// Try array first, then single object.
	var vms []VM
	if err := json.Unmarshal(respBody, &vms); err == nil && len(vms) > 0 && vms[0].ID != "" {
		return &vms[0], nil
	}

	var vm VM
	if err := json.Unmarshal(respBody, &vm); err == nil && vm.ID != "" {
		return &vm, nil
	}

	return nil, clierrors.New(clierrors.CodeAPIError, i18n.Tf("create VM returned unexpected response: %s", "неожиданный ответ API на создание ВМ: %s", respBody), i18n.T("Check API documentation", "Сверьтесь с документацией API"))
}

// GetVM retrieves a virtual machine by ID via GET /api/v1/vms/{id}.
func (a *CloudRuAdapter) GetVM(ctx context.Context, id string) (*VM, error) {
	path := fmt.Sprintf("/api/v1/vms/%s", seg(id))
	resp, err := a.doWithRetry(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var vm VM
	if err := json.NewDecoder(resp.Body).Decode(&vm); err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, i18n.T("failed to decode VM response", "не удалось разобрать ответ с данными ВМ"), hintUnexpectedFormat(), err)
	}
	return &vm, nil
}

// DeleteVM deletes a virtual machine by ID via DELETE /api/v1/vms/{id}.
func (a *CloudRuAdapter) DeleteVM(ctx context.Context, id string) error {
	path := fmt.Sprintf("/api/v1/vms/%s", seg(id))
	resp, err := a.doWithRetry(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// ListFlavors lists available flavors via GET /api/v1/flavors.
// Results are cached for 1 hour.
func (a *CloudRuAdapter) ListFlavors(ctx context.Context) ([]*Flavor, error) {
	a.mu.RLock()
	if a.flavors != nil && time.Now().Before(a.flavorExp) {
		cached := make([]*Flavor, len(a.flavors))
		copy(cached, a.flavors)
		a.mu.RUnlock()
		return cached, nil
	}
	a.mu.RUnlock()

	// Every page: the default page is 50, and a flavor named in .evo.yaml
	// past it was "not found" (api-contract §4.7).
	flavors, err := listAll[Flavor](a, ctx, "/api/v1/flavors", url.Values{}, true)
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	a.flavors = flavors
	a.flavorExp = time.Now().Add(flavorCacheTTL)
	a.mu.Unlock()

	result := make([]*Flavor, len(flavors))
	copy(result, flavors)
	return result, nil
}

// ListAvailabilityZones lists available zones via GET /api/v1/availability-zones.
func (a *CloudRuAdapter) ListAvailabilityZones(ctx context.Context) ([]*AvailabilityZone, error) {
	resp, err := a.doWithRetry(ctx, http.MethodGet, "/api/v1/availability-zones", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, i18n.T("failed to read availability zones response", "не удалось прочитать список зон доступности"), hintUnexpectedFormat(), err)
	}

	// Try paginated response first, then plain array.
	var page paginatedResponse[AvailabilityZone]
	if err := json.Unmarshal(body, &page); err == nil && len(page.Items) > 0 {
		result := make([]*AvailabilityZone, len(page.Items))
		for i := range page.Items {
			result[i] = &page.Items[i]
		}
		return result, nil
	}

	var zones []AvailabilityZone
	if err := json.Unmarshal(body, &zones); err == nil {
		result := make([]*AvailabilityZone, len(zones))
		for i := range zones {
			result[i] = &zones[i]
		}
		return result, nil
	}

	return nil, clierrors.New(clierrors.CodeAPIError, i18n.T("failed to decode availability zones response", "не удалось разобрать список зон доступности"), hintUnexpectedFormatVerbose())
}

// ListImages lists boot images via GET /api/v1/images, every page. A bare
// call returns only the shared catalogue — images built inside a project
// are invisible there. ListImagesInProject exists for those. An optional
// filter is sent as the server-side query (name, display_name, type,
// enabled, need_gpu, availability_zone_id); only the first one counts.
func (a *CloudRuAdapter) ListImages(ctx context.Context, filter ...ImageFilter) ([]*Image, error) {
	q := url.Values{}
	if len(filter) > 0 {
		q = filter[0].Query()
	}
	return listAll[Image](a, ctx, "/api/v1/images", q, true)
}

// ListImagesInProject returns the images owned by one project — the ones
// image_create makes.
func (a *CloudRuAdapter) ListImagesInProject(ctx context.Context, projectID string) ([]*Image, error) {
	if projectID == "" {
		return nil, nil
	}
	return listAll[Image](a, ctx, "/api/v1/images", url.Values{"project_id": {projectID}}, true)
}

// GetImage retrieves image details including metadata fields via GET /api/v1/images/{id}.
// The response contains an SVG icon field that can include raw control
// characters, which are invalid in JSON strings and make the strict decoder
// fail, so the body is sanitized before decoding.
func (a *CloudRuAdapter) GetImage(ctx context.Context, id string) (*ImageDetail, error) {
	path := fmt.Sprintf("/api/v1/images/%s", seg(id))
	resp, err := a.doWithRetry(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, i18n.T("failed to read image response", "не удалось прочитать ответ с данными образа"), hintUnexpectedFormat(), err)
	}

	var img ImageDetail
	if err := json.Unmarshal(sanitizeJSONControlChars(body), &img); err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, i18n.T("failed to decode image response", "не удалось разобрать ответ с данными образа"), hintUnexpectedFormat(), err)
	}
	return &img, nil
}

// sanitizeJSONControlChars replaces raw control characters (U+0000–U+001F,
// except the JSON-legal whitespace \t \n \r) with spaces, so a response whose
// string values contain literal control bytes (e.g. an embedded SVG icon) still
// parses. Structural JSON never contains other raw control bytes, so this is
// safe for decoding.
func sanitizeJSONControlChars(b []byte) []byte {
	out := make([]byte, len(b))
	for i, c := range b {
		if c < 0x20 && c != '\t' && c != '\n' && c != '\r' {
			out[i] = ' '
		} else {
			out[i] = c
		}
	}
	return out
}

// GetVMDetail retrieves full VM info including interfaces via GET /api/v1/vms/{id}.
func (a *CloudRuAdapter) GetVMDetail(ctx context.Context, id string) (*VMDetail, error) {
	path := fmt.Sprintf("/api/v1/vms/%s", seg(id))
	resp, err := a.doWithRetry(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var vm VMDetail
	if err := json.NewDecoder(resp.Body).Decode(&vm); err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, i18n.T("failed to decode VM detail response", "не удалось разобрать подробные данные ВМ"), hintUnexpectedFormat(), err)
	}
	return &vm, nil
}

// CreateFloatingIP creates a floating IP and attaches it to a VM interface.
func (a *CloudRuAdapter) CreateFloatingIP(ctx context.Context, req CreateFloatingIPRequest) (*FloatingIP, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, i18n.T("failed to marshal floating IP request", "не удалось сформировать запрос floating IP"), hintCheckRequest(), err)
	}

	resp, err := a.doWithRetry(ctx, http.MethodPost, "/api/v1/floating-ips", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var fip FloatingIP
	if err := json.NewDecoder(resp.Body).Decode(&fip); err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, i18n.T("failed to decode floating IP response", "не удалось разобрать ответ с данными floating IP"), hintUnexpectedFormat(), err)
	}
	return &fip, nil
}

// GetFloatingIP retrieves a floating IP by ID via GET /api/v1/floating-ips/{id}.
func (a *CloudRuAdapter) GetFloatingIP(ctx context.Context, id string) (*FloatingIP, error) {
	path := fmt.Sprintf("/api/v1/floating-ips/%s", seg(id))
	resp, err := a.doWithRetry(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var fip FloatingIP
	if err := json.NewDecoder(resp.Body).Decode(&fip); err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, i18n.T("failed to decode floating IP response", "не удалось разобрать ответ с данными floating IP"), hintUnexpectedFormat(), err)
	}
	return &fip, nil
}

// maxErrorBody bounds how much of an error response is read into a message.
const maxErrorBody = 64 << 10

// Texts shared by several errors of the adapter, in the language of the CLI.
func hintUnexpectedFormat() string {
	return i18n.T("API returned unexpected format", "API вернул ответ в неожиданном формате")
}

func hintUnexpectedFormatVerbose() string {
	return i18n.T("API returned unexpected format — the verbose mode of evo shows the answer",
		"API вернул ответ в неожиданном формате — подробный режим evo покажет ответ")
}

func hintCheckRequest() string {
	return i18n.T("Check request parameters", "Проверьте параметры запроса")
}

func hintCancelled() string {
	return i18n.T("The operation was cancelled.", "Операция отменена.")
}

func msgRateLimited() string {
	return i18n.T("API rate limit exceeded after retries", "превышен лимит запросов к API, повторы не помогли")
}

func hintRateLimited() string {
	return i18n.T("Wait a moment and try again", "Подождите немного и повторите")
}

func hintStatusPage() string {
	return i18n.T("Check Cloud.ru status page or try again later", "Проверьте страницу статуса Cloud.ru или повторите позже")
}

// StatusError is the HTTP status of a failed API call; it is the cause
// wrapped in the CLIError the adapter returns.
type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body) }

// HTTPStatus returns the HTTP status behind err, or 0.
func HTTPStatus(err error) int {
	var se *StatusError
	if errors.As(err, &se) {
		return se.Status
	}
	return 0
}

// NotInCurrentState reports the API's 422 for a resource that cannot be
// changed or deleted in its current state (…_from_current_state): one
// still being created, attached or deleted. Waited out, it goes through.
func NotInCurrentState(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && se.Status == http.StatusUnprocessableEntity && strings.Contains(se.Body, "_from_current_state")
}

// firstBackoff returns the delay before the first retry.
func (a *CloudRuAdapter) firstBackoff() time.Duration {
	if a.Backoff > 0 {
		return a.Backoff
	}
	return initialBackoff
}

// computeRetry is what the compute API repeats beyond httpretry's own: a
// DELETE after a dropped connection (a repeated DELETE answers 404, which
// this adapter reads as "already gone") and a read after a 500 (the API
// 500s under bursts of concurrent reads; a couple of retries clear it).
// Its Retry-After is not waited out: a 429 is retried after the backoff
// (TestFault_HTTP_CreateVM429UsesBackoffNotRetryAfter). POST and PUT are
// never repeated after a dropped connection: the server may have applied
// them.
var computeRetry = httpretry.Policy{DropMethods: []string{http.MethodDelete}, ReadStatuses: []int{http.StatusInternalServerError}, BackoffOnly: true}

// doWithRetry sends one request through httpretry with computeRetry — the
// one retry loop of evo (process audit 1.2.1, 6.1) — and turns the final
// answer into evo's errors.
func (a *CloudRuAdapter) doWithRetry(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, bodyReader)
	if err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, i18n.T("failed to create HTTP request", "не удалось сформировать HTTP-запрос"), i18n.T("Check API configuration", "Проверьте настройки адреса API"), err)
	}
	req.Header.Set("Authorization", "Bearer "+a.getToken(ctx))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := *a.client
	client.Transport = &httpretry.Transport{Base: a.client.Transport, Backoff: a.firstBackoff(), Policy: computeRetry}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, clierrors.Wrap(clierrors.CodeAPIUnreachable, i18n.T("request cancelled", "запрос отменён"), hintCancelled(), err)
		}
		// An untrusted certificate stays untrusted; the request never left.
		untrusted := clierrors.TLSUntrusted(err)
		hint := clierrors.NetworkHint(err, i18n.T("Check your network connection and try again", "Проверьте подключение к сети и повторите"))
		if method != http.MethodGet && method != http.MethodDelete && !untrusted {
			hint = i18n.T("The request may have reached Cloud.ru before the connection broke: check the Cloud.ru console for a resource it created before retrying.",
				"Запрос мог дойти до Cloud.ru до обрыва соединения: прежде чем повторять, проверьте в консоли Cloud.ru, не создан ли ресурс.")
		}
		return nil, clierrors.Wrap(clierrors.CodeAPIUnreachable, i18n.T("cannot reach Cloud.ru API", "API Cloud.ru недоступен"), hint, err)
	}

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return resp, nil

	case resp.StatusCode == http.StatusTooManyRequests:
		resp.Body.Close()
		return nil, clierrors.New(clierrors.CodeAPIRateLimited, msgRateLimited(), hintRateLimited())

	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		resp.Body.Close()
		cause := &StatusError{Status: resp.StatusCode, Body: string(respBody)}
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, clierrors.Wrap(clierrors.CodeAuthExpired, i18n.T("Cloud.ru API rejected the token (401)", "API Cloud.ru отклонил токен (401)"),
				clierrors.TokenRejectedHint(), cause)
		}
		return nil, clierrors.Wrap(clierrors.CodeAuthFailed, i18n.Tf("access denied (403) for %s %s: %s", "доступ запрещён (403): %s %s: %s", method, path, respBody),
			clierrors.NoRoleHint(), cause)

	case resp.StatusCode == http.StatusNotFound:
		resp.Body.Close()
		return nil, clierrors.New(notFoundCode(path), i18n.Tf("resource not found: %s %s", "ресурс не найден: %s %s", method, path),
			i18n.T("Verify the resource ID and API endpoint", "Проверьте ID ресурса и адрес API"))

	case resp.StatusCode >= 500 && resp.StatusCode < 600 && method == http.MethodGet:
		// A read the transport repeated (computeRetry) and still failed.
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		msg := i18n.Tf("API returned status %d after %d retries: %s", "API вернул статус %d после %d повторов: %s", resp.StatusCode, httpretry.MaxRetries, string(respBody))
		return nil, clierrors.New(clierrors.CodeAPIError, msg, hintStatusPage())

	default:
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		resp.Body.Close()
		msg := i18n.Tf("API returned status %d: %s", "API вернул статус %d: %s", resp.StatusCode, string(respBody))
		hint := hintStatusPage()
		if resp.StatusCode < 500 {
			// 400/409/422: this request is refused; waiting will not help.
			hint = i18n.T("Cloud.ru rejected the request; the message above says why.", "Cloud.ru отклонил запрос; причина — в сообщении выше.")
		}
		return nil, clierrors.Wrap(clierrors.CodeAPIError, msg, hint, &StatusError{Status: resp.StatusCode, Body: string(respBody)})
	}
}

// notFoundCode is the code of a 404 on path: VM_NOT_FOUND for a VM
// (/api/v1/vms, /api/v1.1/vms/{id}/…), NOT_FOUND for anything else — a
// disk, a floating IP, a rule (QA-10: "delete disk: VM_NOT_FOUND"). Both
// exit with 4.
func notFoundCode(path string) string {
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	// "/api/<version>/<collection>/…"
	parts := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 4)
	if len(parts) >= 3 && parts[0] == "api" && parts[2] == "vms" {
		return clierrors.CodeVMNotFound
	}
	return clierrors.CodeNotFound
}
