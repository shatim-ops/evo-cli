// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/stack"
)

// Регрессии внешнего аудита релиза (#90): управляемые БД, сертификаты,
// DNS, мониторинг, клиенты сервисов. UUID выдуманы.

const auditUUID = "00000000-0000-0000-0000-0000000000e1"

// apiFake — поддельный Cloud.ru: сначала свой обработчик (по хосту и
// пути), остальное — fakeDbaas (зоны, подсети, Kafka/Redis). Пишет
// метод, хост, путь и тело каждого запроса.
type apiFake struct {
	fakeDbaas
	mu     sync.Mutex
	calls  []string
	bodies map[string]string // "METHOD host/path" → тело
	own    func(w http.ResponseWriter, r *http.Request) bool
}

func (f *apiFake) RoundTrip(r *http.Request) (*http.Response, error) {
	var body []byte
	if r.Body != nil {
		body, _ = io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
	}
	key := r.Method + " " + r.URL.Host + r.URL.Path
	f.mu.Lock()
	f.calls = append(f.calls, key)
	if f.bodies == nil {
		f.bodies = map[string]string{}
	}
	f.bodies[key] = string(body)
	f.mu.Unlock()
	rec := httptest.NewRecorder()
	if f.own == nil || !f.own(rec, r) {
		f.serve(rec, r)
	}
	return rec.Result(), nil
}

func (f *apiFake) posted(prefix string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k, v := range f.bodies {
		if strings.HasPrefix(k, "POST "+prefix) {
			return v, true
		}
	}
	return "", false
}

func (f *apiFake) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func apiFakeRegistry(t *testing.T, f *apiFake) *Registry {
	t.Helper()
	old := http.DefaultTransport
	http.DefaultTransport = f
	t.Cleanup(func() { http.DefaultTransport = old })
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:  config.Config{ProjectID: dbaasProject, KeyID: "k", Secret: "s"},
			Auth:    staticToken{},
			Cloud:   provision.NewCloudRuAdapter(&http.Client{Transport: f}, ""),
			Printer: output.NewWithWriter(&buf, output.ModeText, false),
		}, nil
	})
}

// dbaasCreateOK отвечает операцией на POST /v1/clusters Kafka и Redis.
func dbaasCreateOK(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodPost && r.URL.Path == "/v1/clusters" {
		_, _ = io.WriteString(w, `{"id":"op-1","resource_id":"`+auditUUID+`","done":false}`)
		return true
	}
	return false
}

// L-34: replicas в POST — реплики без мастера. evo поднимал до 3, и API
// отказывал любому кластеру («range [3:3], requested - 4»). По умолчанию
// теперь 2 (3 хоста), без подъёма; превью говорит, что GET покажет 3.
func TestRedisCreate_ReplicasWithoutMaster(t *testing.T) {
	f := &apiFake{own: dbaasCreateOK}
	r := apiFakeRegistry(t, f)
	m, text, isErr := callNamed(t, r, "redis_create", map[string]any{"name": "cache", "cpu": 2, "ram": 4})
	wc, _ := m["will_create"].(map[string]any)
	if !isErr || m["status"] != "confirmation_required" || wc["replicas"] != float64(2) || wc["hosts_per_shard"] != float64(3) {
		t.Fatalf("preview: %s", text)
	}
	m, text, isErr = callNamed(t, r, "redis_create", map[string]any{"name": "cache", "cpu": 2, "ram": 4, "confirmed": true})
	if isErr || m["status"] != "creating" || !strings.Contains(text, "hosts per shard") {
		t.Fatalf("create: %s", text)
	}
	body, ok := f.posted("redis.api.cloud.ru/v1/clusters")
	if !ok {
		t.Fatal("no POST /v1/clusters")
	}
	var b map[string]any
	_ = json.Unmarshal([]byte(body), &b)
	if b["replicas"] != float64(2) || b["type"] != "MASTER_REPLICA" || b["shards"] != float64(1) {
		t.Errorf("body: %s", body)
	}
}

// L-35, L-34: type, shards и replicas проверяются до облака: type=FOO и
// shards=20 доходили до превью, replicas=3 — до API.
func TestRedisCreate_ShapeCheckedBeforeCloud(t *testing.T) {
	for _, args := range []map[string]any{
		{"name": "c", "type": "FOO"},
		{"name": "c", "shards": 20},
		{"name": "c", "shards": 0},
		{"name": "c", "replicas": 3},
		{"name": "c", "replicas": 1},
		{"name": "c", "type": "STANDALONE", "replicas": 2},
	} {
		f := &apiFake{}
		r := apiFakeRegistry(t, f)
		m, text, isErr := callNamed(t, r, "redis_create", args)
		if !isErr || m["reason"] != "USAGE" {
			t.Errorf("%v: %s", args, text)
		}
		if n := f.count(); n != 0 {
			t.Errorf("%v: %d requests before the refusal", args, n)
		}
	}
	// Регистр type не важен, STANDALONE — без реплик.
	if rt, sh, rep, err := redisShape(map[string]any{"type": "standalone"}); err != nil || rt != "STANDALONE" || sh != 1 || rep != 0 {
		t.Errorf("standalone: %s %d %d %v", rt, sh, rep, err)
	}
	if _, _, _, err := redisShape(map[string]any{"shards": 8, "replicas": 2}); err != nil {
		t.Errorf("8 shards: %v", err)
	}
}

// L-32: kafka_create, redis_create и pg_create с занятым именем создавали
// второй платный кластер. Теперь USAGE с existing_id и подсказкой, POST
// не уходит. Имя «two» — на второй странице списка.
func TestDbaasCreate_RefusesTakenName(t *testing.T) {
	for _, tool := range []string{"kafka_create", "redis_create"} {
		for _, name := range []string{"one", "two"} {
			f := &apiFake{own: dbaasCreateOK}
			r := apiFakeRegistry(t, f)
			m, text, isErr := callNamed(t, r, tool, map[string]any{"name": name, "cpu": 2, "ram": 4, "confirmed": true})
			if !isErr || m["reason"] != "USAGE" || m["existing_id"] == nil || m["hint"] == nil || !strings.Contains(text, "already exists") {
				t.Errorf("%s %s: %s", tool, name, text)
			}
			if _, ok := f.posted(""); ok {
				t.Errorf("%s %s: a create was sent", tool, name)
			}
		}
	}
}

// pgFake — PostgreSQL API: кластер «db» уже есть, спецификации standard
// (max_hosts 2) и business (без max_hosts: предел из режима, 3).
func pgFake(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Host != "postgresql.api.cloud.ru" {
		return false
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/clusters":
		_, _ = io.WriteString(w, `{"clusters":[{"id":"`+auditUUID+`","name":"db","status":"READY"}]}`)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/clusters":
		_, _ = io.WriteString(w, `{"id":"op-1","done":false}`)
	case r.URL.Path == "/v1/versions":
		_, _ = io.WriteString(w, `{"versions":[{"id":"v17","name":"17"}]}`)
	case r.URL.Path == "/v1/specifications":
		_, _ = io.WriteString(w, `{"specifications":[`+
			`{"id":"std","display_name":"std","cpu":2,"memory":4,"deployment_mode":"STANDARD","min_storage_gb":20,"max_hosts":2},`+
			`{"id":"biz","display_name":"biz","cpu":4,"memory":16,"deployment_mode":"BUSINESS","min_storage_gb":20}]}`)
	default:
		http.Error(w, `{"message":"no such path"}`, 404)
	}
	return true
}

func TestPgCreate_TakenNameAndInstances(t *testing.T) {
	f := &apiFake{own: pgFake}
	r := apiFakeRegistry(t, f)
	// L-32: имя занято.
	m, text, isErr := callNamed(t, r, "pg_create", map[string]any{"name": "db", "specification_id": "std", "confirmed": true})
	if !isErr || m["reason"] != "USAGE" || m["existing_id"] != auditUUID {
		t.Errorf("taken name: %s", text)
	}
	// L-31: instances больше max_hosts спецификации — USAGE до превью;
	// без max_hosts — предел режима (business: 3).
	for _, c := range []struct {
		spec      string
		instances int
		ok        bool
	}{{"std", 2, true}, {"std", 3, false}, {"std", 5, false}, {"biz", 3, true}, {"biz", 4, false}} {
		m, text, _ := callNamed(t, r, "pg_create", map[string]any{"name": "new", "specification_id": c.spec, "instances": c.instances})
		if got := m["status"] == "confirmation_required"; got != c.ok {
			t.Errorf("%s instances=%d: %s", c.spec, c.instances, text)
		}
		if !c.ok && m["reason"] != "USAGE" {
			t.Errorf("%s instances=%d: reason %v", c.spec, c.instances, m["reason"])
		}
	}
	if m, text, _ := callNamed(t, r, "pg_create", map[string]any{"name": "new", "instances": 0}); m["reason"] != "USAGE" {
		t.Errorf("instances=0: %s", text)
	}
	if _, ok := f.posted("postgresql"); ok {
		t.Error("a create was sent")
	}
}

// L-30: без подходящей спецификации — ошибка USAGE (rc ≠ 0) со списком
// спецификаций, а не need_specification с rc 0 даже при --yes.
func TestCreate_NeedSpecificationIsUsageError(t *testing.T) {
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"pg_create", map[string]any{"name": "new", "confirmed": true}},
		{"pg_create", map[string]any{"name": "new", "specification_id": "nope", "confirmed": true}},
		{"kafka_create", map[string]any{"name": "new", "cpu": 64, "ram": 512, "confirmed": true}},
		{"redis_create", map[string]any{"name": "new", "specification_id": "nope", "confirmed": true}},
	} {
		f := &apiFake{own: pgFake}
		r := apiFakeRegistry(t, f)
		m, text, isErr := callNamed(t, r, c.tool, c.args)
		menu, _ := m["available_specifications"].([]any)
		if !isErr || m["reason"] != "USAGE" || m["status"] != "need_specification" || len(menu) == 0 || m["hint"] == nil {
			t.Errorf("%s %v: %s", c.tool, c.args, text)
		}
		if _, ok := f.posted(""); ok {
			t.Errorf("%s: a create was sent", c.tool)
		}
	}
}

// L-33: брокеры — min_brokers спецификации, иначе 1 с предупреждением;
// границы спецификации проверяются, когда они есть.
func TestKafkaBrokers(t *testing.T) {
	cases := []struct {
		arg     int
		spec    map[string]any
		want    int
		note    bool
		wantErr bool
	}{
		{0, map[string]any{}, 1, true, false},
		{3, map[string]any{}, 3, false, false},
		{0, map[string]any{"min_brokers": 3.0, "max_brokers": 11.0}, 3, false, false},
		{2, map[string]any{"min_brokers": 3.0}, 0, false, true},
		{12, map[string]any{"max_brokers": 11.0}, 0, false, true},
	}
	for _, c := range cases {
		got, note, err := kafkaBrokers(c.arg, c.spec)
		if (err != nil) != c.wantErr || got != c.want || (note != "") != c.note {
			t.Errorf("%d %v: %d %q %v", c.arg, c.spec, got, note, err)
		}
	}
	r := apiFakeRegistry(t, &apiFake{})
	m, text, _ := callNamed(t, r, "kafka_create", map[string]any{"name": "k", "cpu": 2, "ram": 4})
	if wc, _ := m["will_create"].(map[string]any); wc["brokers"] != float64(1) || !strings.Contains(text, "single broker") {
		t.Errorf("default brokers: %s", text)
	}
}

// L-18: ar_create с занятым именем создавал второй реестр.
func TestArCreate_RefusesTakenName(t *testing.T) {
	f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Host != "ar.api.cloud.ru" {
			return false
		}
		if r.Method == http.MethodGet && r.URL.Path == "/v1/registries" {
			_, _ = io.WriteString(w, `{"registries":[{"id":"`+auditUUID+`","name":"reg","status":"ACTIVE"}]}`)
			return true
		}
		_, _ = io.WriteString(w, `{"id":"op-1","done":false}`)
		return true
	}}
	r := apiFakeRegistry(t, f)
	m, text, isErr := callNamed(t, r, "ar_create", map[string]any{"name": "reg", "confirmed": true})
	if !isErr || m["reason"] != "USAGE" || m["existing_id"] != auditUUID {
		t.Errorf("taken: %s", text)
	}
	if _, ok := f.posted("ar.api.cloud.ru"); ok {
		t.Error("a registry was created")
	}
	if m, text, isErr = callNamed(t, r, "ar_create", map[string]any{"name": "fresh", "confirmed": true}); isErr || m["status"] != "creating" {
		t.Errorf("free name: %s", text)
	}
}

// L-32: стек не усыновляет чужой кластер с тем же именем. Отказ
// инструмента с existing_id на первой попытке — ошибка шага, а не
// «adopt by name» (иначе stack_destroy удалил бы чужой кластер).
func TestStack_TakenNameIsNotAdopted(t *testing.T) {
	stackHome(t)
	r := newTestServer()
	j := planJob(t, `{"name":"s","kafka":{"name":"k"}}`)
	r.handlers["kafka_create"] = func(context.Context, map[string]any) (string, bool) {
		return nameTakenResult("kafka_create", "Kafka cluster", "k", "foreign", "READY", "kafka_get cluster_id=foreign")
	}
	r.handlers["kafka_list"] = fakeOK(map[string]any{"clusters": []any{map[string]any{"id": "foreign", "name": "k", "status": "READY"}}})
	r.handlers["kafka_get"] = fakeOK(map[string]any{"status": "READY"})
	_, _ = r.handleStackApply(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
	got, err := stack.LoadJob(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "error" || !strings.Contains(got.Error, "already") || !strings.Contains(got.Error, "foreign") {
		t.Errorf("job %s: %s", got.Status, got.Error)
	}
	for _, st := range got.Steps {
		if st.ResourceID == "foreign" {
			t.Errorf("step %s adopted the foreign cluster", st.Key)
		}
	}
}

// Финальный аудит, этап 1: первая попытка шага падает до create (сбой
// списка), вторая получает existing_id — это тоже чужой кластер, не
// усыновлять (раньше отказ был только на первой попытке).
func TestStack_TakenNameOnRetryIsNotAdopted(t *testing.T) {
	stackHome(t)
	r := newTestServer()
	j := planJob(t, `{"name":"s","kafka":{"name":"k"}}`)
	calls := 0
	r.handlers["kafka_create"] = func(context.Context, map[string]any) (string, bool) {
		calls++
		if calls == 1 {
			return errResult(fmt.Errorf("check the name is free: list clusters: API 503: unavailable")), true
		}
		return nameTakenResult("kafka_create", "Kafka cluster", "k", "foreign", "READY", "kafka_get cluster_id=foreign")
	}
	r.handlers["kafka_list"] = fakeOK(map[string]any{"clusters": []any{map[string]any{"id": "foreign", "name": "k", "status": "READY"}}})
	r.handlers["kafka_get"] = fakeOK(map[string]any{"status": "READY"})
	for i := 0; i < 3; i++ {
		_, _ = r.handleStackApply(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
	}
	got, err := stack.LoadJob(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range got.Steps {
		if st.ResourceID == "foreign" || got.Outputs[st.Key] == "foreign" {
			t.Fatalf("step %s adopted the foreign cluster (job %s: %s)", st.Key, got.Status, got.Error)
		}
	}
	if got.Status != "error" || !strings.Contains(got.Error, "foreign") {
		t.Errorf("job %s: %s", got.Status, got.Error)
	}
}

// L-16: API принимает тип записи только в нижнем регистре; record_type=A
// уходит как «a», неизвестный тип — USAGE до облака.
func TestDnsRecordCreate_TypeLowerCase(t *testing.T) {
	r := newTestServer()
	for _, c := range []struct{ in, want string }{{"A", "a-record"}, {"Cname", "cname-record"}} {
		m, text, _ := callNamed(t, r, "dns_record_create", map[string]any{"zone_id": "z", "name": "www", "record_type": c.in, "value": "1.2.3.4"})
		if m["status"] != "confirmation_required" || !strings.Contains(text, c.want) {
			t.Errorf("%s: %s", c.in, text)
		}
	}
	// Мимо схемы (вызов из стека) — тоже в нижний регистр.
	if text, _ := r.handleDnsRecordCreate(context.Background(), map[string]any{"zone_id": "z", "name": "www", "record_type": "AAAA", "value": "::1"}); !strings.Contains(text, "aaaa-record") {
		t.Errorf("AAAA: %s", text)
	}
	for _, bad := range []string{"weird", "PTR"} {
		if m, text, isErr := callNamed(t, r, "dns_record_create", map[string]any{"zone_id": "z", "name": "www", "record_type": bad, "value": "x"}); !isErr || m["reason"] != "USAGE" {
			t.Errorf("%s: %s", bad, text)
		}
	}
}

// L-14: since=2d молча становился 1h, since=48h уходил в API (400 «can't
// be longer than 24 hours»). Теперь дни понимаются, больше 24h и мусор —
// USAGE до облака (фабрика без движка: в сеть не идём).
func TestMetricsHistory_Since(t *testing.T) {
	for _, c := range []struct {
		in   string
		want time.Duration
		ok   bool
	}{{"", time.Hour, true}, {"30m", 30 * time.Minute, true}, {"1d", 24 * time.Hour, true}, {"2d", 48 * time.Hour, true}, {"90", 90 * time.Minute, true}, {"soon", 0, false}, {"-1h", 0, false}, {"d", 0, false}} {
		got, err := parseSince(c.in)
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("%q: %v %v", c.in, got, err)
		}
	}
	r := newTestServer()
	for _, since := range []string{"2d", "48h", "25h", "bogus"} {
		m, text, isErr := callNamed(t, r, "metrics_history", map[string]any{"match": "cpu", "since": since})
		if !isErr || m["reason"] != "USAGE" {
			t.Errorf("since=%s: %s", since, text)
		}
	}
	// 24h и 1d проходят проверку и доходят до движка (здесь его нет).
	for _, since := range []string{"24h", "1d"} {
		if _, text, _ := callNamed(t, r, "metrics_history", map[string]any{"match": "cpu", "since": since}); strings.Contains(text, "since") {
			t.Errorf("since=%s refused: %s", since, text)
		}
	}
}

// L-19: privateKey — PEM-текст, как обещает описание; API шлёт PEM в
// Base64.
func TestCertPrivateKey_PEM(t *testing.T) {
	pem := "-----BEGIN PRIVATE KEY-----\nMIIB\n-----END PRIVATE KEY-----\n"
	for _, c := range []struct {
		in, want, format string
	}{
		{base64.StdEncoding.EncodeToString([]byte(pem)), pem, "pem"},
		{pem, pem, "pem"},
		{"bm90IGEga2V5", "bm90IGEga2V5", "base64"},
	} {
		pk := map[string]any{"privateKey": c.in, "version": map[string]any{"id": 1}}
		pemKey(pk)
		if pk["privateKey"] != c.want || pk["key_format"] != c.format {
			t.Errorf("%.20q: %v", c.in, pk)
		}
	}
}

// L-15: id, который API берёт только как UUID, проверяется до облака.
func TestGetTools_NonUUIDIsUsage(t *testing.T) {
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"vpc_get", map[string]any{"id": "nope"}},
		{"vpc_routes", map[string]any{"vpc_id": "nope"}},
		{"pg_get", map[string]any{"id": "nope"}},
		{"kafka_get", map[string]any{"cluster_id": "nope"}},
		{"redis_get", map[string]any{"cluster_id": "nope"}},
	} {
		f := &apiFake{}
		r := apiFakeRegistry(t, f)
		m, text, isErr := callNamed(t, r, c.tool, c.args)
		if !isErr || m["reason"] != "USAGE" || m["hint"] == nil {
			t.Errorf("%s: %s", c.tool, text)
		}
		if n := f.count(); n != 0 {
			t.Errorf("%s: %d requests", c.tool, n)
		}
	}
}

// L-15: несуществующая VPC — 404 → NOT_FOUND (код выхода 4), а не
// API_ERROR.
func TestVpcGet_404IsNotFound(t *testing.T) {
	f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Host != "vpc.api.cloud.ru" {
			return false
		}
		w.WriteHeader(404)
		_, _ = io.WriteString(w, `{"code":5,"message":"vpc doesn't exist"}`)
		return true
	}}
	r := apiFakeRegistry(t, f)
	for _, c := range []struct {
		tool string
		args map[string]any
	}{{"vpc_get", map[string]any{"id": auditUUID}}, {"vpc_routes", map[string]any{"vpc_id": auditUUID}}} {
		m, text, isErr := callNamed(t, r, c.tool, c.args)
		if !isErr || m["reason"] != "NOT_FOUND" || m["hint"] == nil {
			t.Errorf("%s: %s", c.tool, text)
		}
	}
}

// L-15: s3_get несуществующего объекта или бакета — NOT_FOUND.
func TestS3Get_MissingIsNotFound(t *testing.T) {
	for _, c := range []struct{ code, key string }{{"NoSuchKey", "missing.txt"}, {"NoSuchBucket", "a.txt"}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(404)
			if r.Method != http.MethodHead {
				_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>`+c.code+`</Code><Message>gone</Message><Key>`+c.key+`</Key><BucketName>bucket</BucketName><RequestId>1</RequestId></Error>`)
			}
		}))
		r := New(func(map[string]any) (*engine.Engine, error) {
			return &engine.Engine{Config: config.Config{S3: config.S3Config{Endpoint: srv.URL, AccessKey: "a", SecretKey: "s"}}}, nil
		})
		m, text, isErr := callNamed(t, r, "s3_get", map[string]any{"bucket": "bucket", "key": c.key})
		if !isErr || m["reason"] != "NOT_FOUND" || m["hint"] == nil {
			t.Errorf("%s: %s", c.code, text)
		}
		srv.Close()
	}
}

// L-06: billing_* без CLOUDRU_AGREEMENT_ID — CONFIG_INVALID с именем
// переменной, до движка и токена.
func TestBilling_NoAgreementIsConfigInvalid(t *testing.T) {
	t.Setenv("CLOUDRU_AGREEMENT_ID", "")
	r := newTestServer()
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"billing_balance", nil},
		{"billing_consumption", map[string]any{"start": "2026-10-01"}},
		{"billing_finops", map[string]any{"start": "2026-10-01", "end": "2026-10-02"}},
	} {
		m, text, isErr := callNamed(t, r, c.tool, c.args)
		hint, _ := m["hint"].(string)
		if !isErr || m["reason"] != "CONFIG_INVALID" || !strings.Contains(hint, "CLOUDRU_AGREEMENT_ID") {
			t.Errorf("%s: %s", c.tool, text)
		}
	}
}
