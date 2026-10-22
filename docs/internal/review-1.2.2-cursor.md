# Состязательная проверка 1.2.2, 22.10.2026

База: `80511fd8f5eb5fb2fb1199c35cd2b1b0a6d91dd9` (`main`, «build(deps): minio-go 7.3.0 → 7.4.1 (#166)»). Новее этого коммита на `origin/main` нет. Облако не вызывалось: фиктивные ключи, поддельные клиенты в тестах, `HTTPS_PROXY` не понадобился.

Разбор — раздел «[Не выпущено]» в `CHANGELOG.md` и коммиты #176–#182 и #166: `ffd68ad` (текстовый вид биллинга), `b945c11` (баланс договора), `4ce123e` (`details`), `5ee66bb` (имя зоны), `835197d` (примечания k8s), `cebe4db` (только план выпуска), `6c055ee` (имя бакета), `80511fd` (`go.mod` / `go.sum`, minio-go 7.4.1).

Запускалось 22.10.2026 в 10:29 (Europe/Moscow):

- `go test -count=1 -run 'TestBilling_ProjectBoundNoAgreementBalance|TestBilling_SumsWithVAT|TestS3_BucketNameRefusedInCLILanguage|TestDeploy_ZoneByNameOnPreview|TestProvision_UnknownZoneNameRefusedBeforePreview|TestProvision_ZoneByName|TestK8s_NotesInCLIWords' ./internal/tools/` — `ok`.
- `go test -count=1 -run 'TestRub|TestRenderBillingFinops$|TestRenderBillingFinops_ProjectScope|TestRenderBilling_Sanitized|TestHandleError_JSONDetails' ./internal/cli/` — `ok`.
- `go test -count=1 -run TestResolveZoneRef ./internal/engine/` — `ok`.
- `go test -count=1 -run 'TestCUR122_' ./internal/tools/ ./internal/cli/` — шесть падений, тела тестов ниже. В дерево они не входят.
- `s3utils.CheckValidBucketName` из minio-go 7.4.1 модуля: `../x`, `a/b`, `a\b` — path separators; `a..b`, `...`, `.ab`, `ab.` — invalid characters; `192.168.0.1` — ip address; `abc`, `a-b`, `my.bucket` — `nil`.

Полный `go test ./...` в этом прогоне не запускался.

`evo <команда> --json` и MCP зовут один и тот же обработчик реестра. Расхождение полей, `reason` и превью между ними у этих изменений не нашлось. Код выхода CLI берётся из ответа: `confirmation_required` — 3, `USAGE` — 1, ответ без флага ошибки — 0 (`internal/cli/tools.go`, `handleStackApply` возвращает ошибку только при `job.Status == "error"`). MCP при старте ставит английский (`internal/cli/mcp.go`, `i18n.SetLang(i18n.EN)`); русский текст — путь CLI.

## Находки

| ID | Где | Что не так | Воспроизведение | Как исправить | Серьёзность |
|---|---|---|---|---|---|
| CUR122-1 | `internal/tools/tools_stack.go:716`, шаг — `internal/stack/store.go:595`, согласие — `internal/tools/tools_stack.go:739` | Стек с `s3[].bucket: "../x"` сохраняет план (`status: planned`, оценка postgres 4900 ₽/мес). `stack_apply` без `confirmed` отвечает `confirmation_required`. С `confirmed: true` шаг `pg_create` выполняется один раз, задание остаётся `status: running` (флаг ошибки ложный, код выхода CLI 0), `s3:0` висит в `pending`. Postgres уже создан, имя бакета до клиента S3 дойдёт на следующем опросе. Тот же JSON у CLI и MCP. | `TestCUR122_StackBadBucketPlansThenCreates`. Прогон: job `stk-2f603ae138`, затем `postgres was created (1)`. | Вызвать `s3.CheckBucketName` в `normalizeManifest` до `stack.Plan` и до согласия на apply. Отказ `USAGE`, код 1, задание не сохраняется. | блокер |
| CUR122-2 | `internal/tools/tools_s3.go:170`, правило — `internal/tools/argrules.go:255` | `s3_bucket_create` для `a..b` и `192.168.0.1` отдаёт `confirmation_required` и превью с тарифом object-storage. Регулярное выражение `^[a-z0-9.-]{3,63}$` эти имена принимает, `CheckValidBucketName` — нет. Согласие спрашивается на вызов, который клиент S3 отклонит. `../x` и `a/b` сюда не входят: их режет уже это выражение. | `TestCUR122_BucketCreatePreviewForNameS3Rejects`. | Перед превью вызвать `s3.CheckBucketName`, как у `s3_put` и `s3_rm`. | важно |
| CUR122-3 | `internal/deployplan/resolve.go:82` | `deploy_plan` с зоной `ru.az-2` при списке, где зона называется `ru.AZ-2` (id `00000000-0000-0000-0000-0000000000a2`), отвечает `USAGE`: `could not resolve availability zone "ru.az-2" to a UUID`. Сравнение имени на строке 82 регистрозависимое. `provision` и `evo deploy --region` то же имя принимают через `provision.ResolveZone`. | `TestCUR122_DeployPlanZoneNameIgnoresCase`. | Решать зону плана через `provision.ResolveZone` (или `engine.ResolveZoneRef`), одним правилом с deploy и provision. | важно |
| CUR122-4 | `internal/engine/network.go:54`, отправка — `internal/tools/tools.go:1987` и `internal/engine/deploy.go:604` | Пустой успешный список зон и имя `ru.AZ-2`: `ResolveZoneRef` возвращает зону `nil` и ошибку `nil` (`len(zones) == 0`). `provision` без `confirmed` отвечает `confirmation_required`; в превью id зоны — сырой `cfg.Region`, потому что `zone == nil` (`provisionPreview`, `internal/tools/tools.go:3284`). С `confirmed: true` вызовы идут в порядке `CreateFloatingIP`, `CreateVM`, у обоих `AvailabilityZoneID` равен `ru.AZ-2`. У `evo deploy` сеть решается до согласия (`deploy.go:542`, вопрос на строке 584), дальше то же имя уходит в `createBootFIP`. Имя при ошибке чтения списка и неизвестное имя в непустом списке отказывают до превью — это проверено отдельно. | `TestCUR122_EmptyZoneListNameReachesAPI`. Прогон: `fip zone "ru.AZ-2"`, `vm zone "ru.AZ-2"`, `mutations [CreateFloatingIP CreateVM]`. Условие узкое: список зон пуст, а единственные подсеть и группа безопасности без id зоны, иначе выбор сети откажет раньше. | Имя при пустом списке отказывать так же, как при ошибке списка: `USAGE` до согласия. Случай `{nil, nil, "ru.AZ-2"}` в `TestResolveZoneRef` сейчас закрепляет пропуск и его придётся сменить. UUID, которого нет в списке, по-прежнему пропускать. | важно |
| CUR122-5 | `internal/tools/tools_k8s.go:397`, `:508`, `:622`, `:657` | В русском CLI подсказка `k8s_nodepools` без `cluster_id`: `ID кластера — в выводе k8s_list (evo k8s list).` Неизвестная зона: сообщение `зона доступности "no-such-zone" не найдена`, hint на английском и с `k8s_options` (строки 657–659, без `i18n.T`). Те же русские hint: пустая зона кластера у `k8s_add_nodepool` называет `k8s_get` (строка 508); нет `zone` и нет зоны по умолчанию — `k8s_options` (строка 622). Три `note` у `k8s_plan`, `k8s_create` и `k8s_add_nodepool` уже называют команды evo. Английский текст MCP эти имена инструментов содержит по решению #180. | `TestCUR122_K8sRussianHintsNameCommands` (вызовы: `k8s_nodepools` без аргументов и `k8sZone` с `no-such-zone`). Строки 508 и 622 — те же литералы hint, отдельным вызовом в этом прогоне не гонялись. | Русская ветвь hint называет `evo k8s …`. Английскую оставить. Строку 657 обернуть в `i18n.T`. | мелочь |
| CUR122-6 | `internal/cli/billing_views.go:39` | `rub(1e20)`, `rub(+Inf)` и `rub(NaN)` печатают `--9 223 372 036 854 775 808 ₽`: `int64(math.Round(...))` на этом Go даёт `MinInt64`, а отрицание `MinInt64` его не меняет. `evo billing balance` на `{"balance":"NaN"}` идёт через `num` (`ParseFloat`) и тот же `rub`. `--json` и MCP вид не используют, поля ответа целы. Обычные суммы, `-0.4`, меньше рубля и управляющие символы в имени сервиса уже верны. | `TestCUR122_RubHugeAndNonFinite`. | Неконечное значение и сумму вне `int64` не проводить через `int64(math.Round)`. | мелочь |

## Падающие тесты

Тесты лежат рядом с существующими хелперами пакета (`stackHome`, `callNamed`, `newTestServer`, `fakeOK`, `planRegistry`, `planZoneAZ2`, `zoneRegistry`, `otherZoneCloud`, `cancelCloud`, `renderRU`). На базе `80511fd` они падают. Первые пять — пакет `internal/tools` (`context`, `errors`, `strings`, `sync/atomic`, `config`, `clierrors`, `i18n`, `mk8s`, `provision`). Последний — пакет `internal/cli` (`math`, `strings`, `i18n`).

Пакет `internal/tools`:

```go
func TestCUR122_StackBadBucketPlansThenCreates(t *testing.T) {
	stackHome(t)
	r := newTestServer()
	var pg atomic.Int32
	r.handlers["pg_create"] = func(context.Context, map[string]any) (string, bool) {
		pg.Add(1)
		return toJSON(map[string]any{"status": "creating", "cluster_id": "pg-1"}), false
	}
	r.handlers["pg_list"] = fakeOK(map[string]any{"clusters": []any{}})
	r.handlers["pg_get"] = fakeOK(map[string]any{"status": "CREATING"})
	manifest := map[string]any{
		"name": "s", "postgres": map[string]any{"name": "db"},
		"s3": []any{map[string]any{"bucket": "../x", "key": "k", "content": "c"}},
	}
	m, text, isErr := callNamed(t, r, "stack_plan", map[string]any{"manifest": manifest})
	if isErr && m["status"] != "planned" {
		return
	}
	id, _ := m["job_id"].(string)
	t.Errorf("stack_plan saved job %s for bucket ../x: %s", id, text)
	m, text, isErr = callNamed(t, r, "stack_apply", map[string]any{"job_id": id})
	if !isErr || m["status"] != "confirmation_required" {
		t.Fatalf("apply without consent: %s", text)
	}
	_, text, _ = callNamed(t, r, "stack_apply", map[string]any{"job_id": id, "confirmed": true})
	if pg.Load() != 0 {
		t.Fatalf("postgres was created (%d) before the bucket name failed:\n%s", pg.Load(), text)
	}
}

func TestCUR122_BucketCreatePreviewForNameS3Rejects(t *testing.T) {
	r := newTestServer()
	for _, name := range []string{"a..b", "192.168.0.1"} {
		m, text, isErr := callNamed(t, r, "s3_bucket_create", map[string]any{"bucket": name})
		if !isErr || m["reason"] != "USAGE" || m["status"] == "confirmation_required" {
			t.Errorf("%s: want USAGE before consent, got %s", name, text)
		}
	}
}

func TestCUR122_DeployPlanZoneNameIgnoresCase(t *testing.T) {
	r, _ := planRegistry(t, config.Config{ImageID: "img-u"})
	m, text, isErr := callNamed(t, r, "deploy_plan", map[string]any{"name": "web", "zone": "ru.az-2"})
	if isErr {
		t.Fatalf("ru.az-2 is ru.AZ-2: %s", text)
	}
	vm, _ := m["vm"].(map[string]any)
	zone, _ := vm["zone"].(map[string]any)
	if zone["id"] != planZoneAZ2 || zone["name"] != "ru.AZ-2" {
		t.Fatalf("zone %+v in %s", zone, text)
	}
}

func TestCUR122_EmptyZoneListNameReachesAPI(t *testing.T) {
	cloud := &fipZoneCloud{otherZoneCloud: otherZoneCloud{
		cancelCloud: cancelCloud{cancel: func() {}},
		zones:       []*provision.AvailabilityZone{},
		subnets:     []*provision.Subnet{{ID: "sn-1", Name: "default"}},
		groups:      []*provision.SecurityGroup{{ID: "sg-1", Name: "default", Default: true}},
	}}
	r := zoneRegistry(t, cloud, config.Config{ProjectID: "p", ImageID: "img-1", KeyID: "k", Secret: "s"})
	m, text, _ := callNamed(t, r, "provision", map[string]any{"name": "web", "region": "ru.AZ-2"})
	if m["status"] != "confirmation_required" {
		t.Fatalf("preview: %s", text)
	}
	_, text, _ = callNamed(t, r, "provision", map[string]any{
		"name": "web", "region": "ru.AZ-2", "confirmed": true,
	})
	if cloud.fipZone == "ru.AZ-2" || cloud.createReq.AvailabilityZoneID == "ru.AZ-2" {
		t.Fatalf("name reached the API (fip zone %q, vm zone %q, mutations %v):\n%s",
			cloud.fipZone, cloud.createReq.AvailabilityZoneID, cloud.mutations, text)
	}
}

type fipZoneCloud struct {
	otherZoneCloud
	fipZone string
}

func (c *fipZoneCloud) CreateFloatingIP(_ context.Context, req provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
	c.fipZone = req.AvailabilityZoneID
	c.mutations = append(c.mutations, "CreateFloatingIP")
	return &provision.FloatingIP{ID: "fip-boot", IPAddress: "203.0.113.9", State: "available"}, nil
}

func (c *fipZoneCloud) CreateVM(_ context.Context, req provision.CreateVMRequest) (*provision.VM, error) {
	c.creates++
	c.createReq = req
	c.mutations = append(c.mutations, "CreateVM")
	return nil, errors.New("stop after the zone was sent")
}

func TestCUR122_K8sRussianHintsNameCommands(t *testing.T) {
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	r := newTestServer()
	m, text, isErr := callNamed(t, r, "k8s_nodepools", nil)
	hint, _ := m["hint"].(string)
	if !isErr || strings.Contains(hint, "k8s_") || strings.Contains(hint, "confirmed=true") {
		t.Errorf("nodepools hint: %s", text)
	}
	cloud := &otherZoneCloud{cancelCloud: cancelCloud{cancel: func() {}}}
	r = zoneRegistry(t, cloud, config.Config{ProjectID: "p", KeyID: "k", Secret: "s"})
	pc := &mk8s.ProductConfig{}
	pc.Zones = append(pc.Zones, struct {
		ZoneID           string   `json:"zoneId"`
		Name             string   `json:"name"`
		AvailableFlavors []string `json:"availableFlavors"`
	}{ZoneID: "z", Name: "ru.AZ-3"})
	_, err := r.k8sZone(context.Background(), pc, "no-such-zone")
	var ce *clierrors.CLIError
	if !errors.As(err, &ce) || strings.Contains(ce.Hint, "k8s_") || strings.Contains(ce.Hint, "confirmed=true") {
		t.Fatalf("zone hint: %v", err)
	}
}

```

Пакет `internal/cli`:

```go
func TestCUR122_RubHugeAndNonFinite(t *testing.T) {
	prev := i18n.Lang()
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(prev) })
	for _, v := range []float64{1e20, math.Inf(1), math.NaN()} {
		got := rub(v)
		if strings.HasPrefix(got, "-") || strings.Contains(got, "9223372036854775808") {
			t.Errorf("rub(%v) = %q", v, got)
		}
	}
	got := renderRU(t, renderBillingBalance, `{"balance":"NaN","active":true}`)
	if strings.Contains(got, "9223372036854775808") || strings.HasPrefix(strings.TrimSpace(got), "-") {
		t.Errorf("balance NaN:\n%s", got)
	}
}
```

## Проверено и не подтвердилось

- Текстовый вид биллинга на обычных данных. `TestRub` и `TestRenderBillingFinops`: `0`, `0.3`, меньше рубля, `-0.4`, отрицательные целые, период текущего года, один день, другой год, битая дата, «показано N из M», пустые `anomalies` — «нет.». `balance: null` при пустом договоре заменяется русской фразой про `CLOUDRU_AGREEMENT_ID` (`TestRenderBillingFinops_ProjectScope`). Управляющие символы в имени сервиса снимает `TestRenderBilling_Sanitized`. Поля до #176 на месте: `git diff ffd68ad^ ffd68ad` добавляет суммы с НДС и оставляет `total_cost`, `daily_burn`, `by_service.cost`, `anomalies.prev` / `now` / `delta_pct`, `line_items`, `vat`. `TestBilling_SumsWithVAT` это держит. `--json` вид не вызывает (`internal/cli/trees.go`).
- `balance: null` без `balance_error` инструмент не отдаёт: оба места, где баланс обнуляется, пишут `balance_error` (`internal/tools/tools_billing.go:418`). Если договор задан и `Balance()` упал, русская строка начинается с «недоступен: » и дальше идёт стабильная английская `balance_error` — та же строка, что в `--json` и MCP.
- Баланс договора на сервере с `--allow-project`. `balanceRefused` (`internal/tools/tools_billing.go:94`) — единственные ворота; оба вызова `cli.Balance` стоят за ним (`:120` после отказа в `handleBillingBalance`, `:334` в `billing_finops`). `SetProjectBound` ставит только `newMCPServer`. `TestBilling_ProjectBoundNoAgreementBalance` обходит каждый `billing_*` с `project_id` и без и проверяет, что запроса на хост биллинга нет. Стек баланс не читает. `evo tools call` проектным сервером не является: журнал прямо говорит, что CLI не менялся.
- Секрет пользователя в `details`. `answerDetails` (`internal/cli/tools.go:898`) копирует поля ответа, кроме текста ошибки, `reason`, `hint` и `status: error`. `pg_user_create` пароль в ответ не кладёт (`internal/tools/tools_pg.go:635`, ошибка — `fmt.Errorf` без поля пароля). `secret_create` вырезает `payload` из ответа. kubeconfig в ошибочный `details` не попадает: в ответ он попадает у успешного `k8s_kubeconfig`. Пароль ВМ, который сгенерировал evo, в `details` — исключение FB121-1, его закрывает `TestHandleError_JSONDetails`. Отдельного расхождения значений с ответом MCP нет: `details` — те же лишние поля.
- Зона. Неизвестное имя в непустом списке отказывает до превью и до мутаций: `TestProvision_UnknownZoneNameRefusedBeforePreview`, `TestDeploy_ZoneByNameOnPreview`. Имя при ошибке чтения списка отказывает (`TestResolveZoneRef`, случай с `connection reset`). UUID, которого нет в списке, проходит и в `ResolveZoneRef`, и в `deployplan.Resolve`. `vm.zone` стека решается на плане через `provision.ResolveZone`: неизвестное имя не сохраняет задание. Регистр имени у `provision` и `deploy --region` уже общий (`TestProvision_ZoneByName`). Пустой список в `deploy_plan` имя не пропускает: после промаха не-UUID получает `could not resolve availability zone`.
- Примечания k8s. `TestK8s_NotesInCLIWords`: русские `note` плана, создания и добавления группы узлов называют `evo k8s …` и не содержат `confirmed=true`; английские note прежние. Поля `message` у `confirmation_required` по-прежнему говорят `k8s_create` / `k8s_add_nodepool` — это текст согласия, не `note` и не `hint`.
- Имя бакета на прямых вызовах. `TestS3_BucketNameRefusedInCLILanguage`: `s3_ls`, `s3_get`, `s3_put`, `s3_rm`, `s3_bucket_delete` отвечают `USAGE` на `../x`, `a/b`, `a\b` до превью. Удаление стека зовёт `s3_rm` (`internal/tools/tools_stack.go:1067`), а тот имя проверяет. #166 меняет только зависимость; обычные имена (`abc`, `a-b`, `my.bucket`) клиент 7.4.1 принимает. Поведение пути — #182.
