# План живых проверок evo

Версия 18.10.2026, `main` после #100–#103. Что сюда входит: пункты
`docs/internal/standalone/BACKLOG.md` с пометкой «нужна живая проверка»,
обещания в `docs/` без живого подтверждения и поведение MCP-клиентов.
Фейки своё выбрали (break r1, r2, r3-A); новые тесты на фейках здесь не
предлагаются.

Что уже проверяется вживую и в план не входит: сквозной
`init → deploy → verify → logs -f → destroy` с удалением правила порта
(cloud-e2e `e2e`), ВМ из образов по имени (`os-smoke`), кластер
Kubernetes с пулом, `kubectl` и плагином (`k8s-smoke`), минимальные
PostgreSQL/Kafka/Redis (`dbaas_smoke`), чтения всех read-only инструментов,
MCP stdio/HTTP и гейт подтверждений (`readonly`), форма ответов и
пагинация IAM, порядок `GET /vms` (`api-probe`, с #103).

Куда: **api-probe** — только чтение, ключ наблюдателя, без одобрения,
бесплатно; **cloud-e2e** — создаёт ресурсы, окружение `cloud-e2e`,
одобрение владельца; **TTY** — вручную на терминале или в MCP-клиенте;
**догфуд** — естественно ложится на участников волны, ссылка на раздел
`dogfood-protocol.md`. Стоимость — по тем же источникам, что в протоколе
(cloud.ru/docs «Тарификация», прочитаны 18.10.2026; реестр расхождений).

Сортировка: сначала риск для денег клиента, внутри — по стоимости
проверки.

## Таблица

| № | Пункт | Что проверяем | Чтение / создание | Шаги в командах evo | Длительность | Стоимость (источник) | Уборка | Провал — это | Куда |
|---|---|---|---|---|---|---|---|---|---|
| LC-1 | [break-r3a] вопрос y/N при Ctrl-C | `handleInterrupt` на терминале: `y` удаляет ВМ и ждёт удаляемости, освобождает FIP; Enter оставляет ВМ с id в подсказке; второй Ctrl-C не оставляет полуудалённую ВМ без записи | создание | `evo deploy` (или `evo vm create t --image ubuntu-22.04`), на «ожидание ВМ…» Ctrl-C → `y`; повторить с Enter → `evo destroy`; повторить с двумя Ctrl-C подряд | 3 × 5 мин | ВМ ≈1,8 ₽/ч (превью в guide.md), итого < 2 ₽ | `evo vm list`, `evo vpc fips` | ВМ или адрес остались без записи в `.evo/state.json` и без id на экране; код не 130 | TTY; догфуд §4 |
| LC-2 | SIGINT посреди MCP `provision` после `CreateVM` | класс R3A: обрыв вызова после создания ВМ — ВМ либо удалена, либо записана и названа; повтор не плодит вторую; `destroy` дочищает | создание | в cloud-e2e: `timeout -s INT 40 evo --yes tools call provision --arg name=evo-e2e-int --json` (40 с — после `POST /vms`, до активности); затем `evo --yes tools call destroy --arg name=evo-e2e-int`; проверка `vm_list`, `list_floating_ips` | 10 мин | < 1 ₽ | шаг `destroy` + существующая проверка «сирот нет» | вторая ВМ или FIP `fip-evo-e2e-int` после повтора; ВМ без записи | cloud-e2e: новый шаг в job `e2e` (набросок ниже) |
| LC-3 | MCP-клиенты при долгом вызове и отмене | что делает Cursor и Claude Code, когда `provision`/`deploy_apply` идёт 3–7 мин: ждёт, обрывает по своему таймауту, повторяет вызов сам; что видит агент после «Stop» пользователя; остаётся ли ВМ и знает ли о ней агент | создание | агенту: «создай ВМ df-cancel»; через 30 с после подтверждения — «Stop» в клиенте; затем «что ты создал?»; `evo vm list`; то же в HTTP-режиме (`evo mcp --http`) | 4 × 10 мин (2 клиента × stdio/HTTP) | < 2 ₽ | `evo --yes tools call destroy --arg name=df-cancel` | клиент повторил вызов и в проекте две ВМ; агент отвечает «ничего не создано», а ВМ есть; слот сервера не вернулся (следующий вызов `SERVER_BUSY`) | TTY (владелец, до волны); догфуд §2.15 и §4 |
| LC-4 | [break-r2] R2-L1, часть 1: имя занято ВМ в `ERROR` | возможен ли вообще вход R2-L1: отвечает ли `POST /vms` 409 на имя ВМ в `error_creating`/`ERROR`. Если 409 — две ВМ с одним именем в проекте невозможны, сценарий снимается | создание | в cloud-e2e после `os-smoke`-подобного отказа (ВМ в `error_creating`, например образ Astra, IMG-1) — `evo vm create <то же имя> --image ubuntu-22.04 --yes --json`; код и `reason` | 10 мин | < 1 ₽ | `vm delete` обеих | 200 вместо 409: тогда часть 2 (ниже) обязательна | cloud-e2e: шаг в `os-smoke` |
| LC-5 | [break-r2] R2-L1, часть 2: обрыв создания при двух ВМ с одним именем | `FindCreatedVM` при потерянном ответе выбирает новую ВМ, не старую в `ERROR` | создание | только если LC-4 дал не-409: ВМ `evo-e2e-dup` в `ERROR` + `timeout -s KILL 8 evo --yes tools call provision --arg name=evo-e2e-dup` (обрыв до ответа) → повторный `provision` → сравнить `vm_id` в state с `created_time` в `vm_list` | 15 мин | < 1 ₽ | удалить обе по id | в `.evo/state.json` id старой ВМ | cloud-e2e, условно |
| LC-6 | [docs/exit-codes.md] повторный `destroy` ВМ в `deleting` | текст и код: evo говорит «still being created» на 409/422, хотя ВМ удаляется (BACKLOG, ревью Grok #89) | создание | в job `e2e` после `destroy --yes` сразу второй `evo destroy --yes --json` без паузы 30 с; затем через 60 с третий | +2 мин к `e2e` | 0 (ВМ уже есть) | есть | второй `destroy` — код не 0/5 или подсказка про «создаётся» | cloud-e2e: правка шага `destroy --yes (всегда)` |
| LC-7 | Токен не обновляется в долгой команде | `deploy` с токеном, у которого осталось < 5 мин, доходит до конца или падает `AUTH_EXPIRED` с id ВМ | создание | `evo whoami` (свежий токен, `token_ttl_seconds` в `--json`); подождать `ttl − 240` с; `evo deploy --yes` с `CLOUDRU_TIMEOUT=900` | ~1 ч ожидания + 10 мин | < 2 ₽ | `evo destroy` | падение посреди ожидания ВМ с кодом 1 или 2, ВМ остаётся; повтор создаёт вторую | TTY (владелец); догфуд §4 строка «Долгая операция и токен» |
| LC-8 | [docs/guide.md §11] лицензия образа маркетплейса не в превью | сколько реально списано за ВМ из образа с лицензией (Windows Server 1 006 ₽/мес по карточке, BIL-1): почасово или помесячно | создание | `evo vm create df-win --image windows2022std-evo-kmsB --yes` → через 1 ч `vm delete` → на следующий день `billing_consumption` за эти сутки, строка образа | 1 ч + сутки | допущение: 1,4 ₽ за час лицензии, но возможна помесячная 1 006 ₽ | `vm delete`, `evo vpc fips` | списано больше часа лицензии; строки лицензии в `billing_consumption` нет | TTY (владелец, тестовый проект); не догфуд |
| LC-9 | [break-98] L-1 `ca_logs` на длинном логе | «последние 100 строк» при логе длиннее одной страницы — действительно последние | создание | `ca_deploy` образа `nginx` с `public=true` → URL из `ca_get` → 300 запросов `curl "$URL/?n=$i"` → `ca_logs`: последняя строка содержит `n=300`, строк ровно 100; сравнить с логом в консоли; `ca_delete` | 15 мин | допущение: < 1 ₽ (масштаб в ноль, тариф CA не прочитан) | `ca_delete` | хвост `ca_logs` не совпадает с консолью | cloud-e2e: job `ca-smoke` (набросок ниже); либо догфуд §2.12 |
| LC-10 | [break-98] пагинация IAM `pageToken` vs `page_token` | вторая страница сервисных аккаунтов приходит, записи не дублируются | чтение | api-probe с #103 уже шлёт запрос второй страницы с маленьким `pageSize`; прочитать результат последнего прогона; если ещё не запускался после #103 — запустить | 5 мин | 0 | — | вторая страница равна первой или 400 | api-probe (уже есть) |
| LC-11 | [fable-review] `org_quotas` в скоупе проекта | какой параметр и скоуп принимает `GET quotas/usages` у ключа проекта (без него — 403) | чтение | api-probe: `probe quotas_project "$ORG/v3/quotas/usages?project_id=…"`, `…?projectId=…`, без параметров; печатать код и ключи | 5 мин | 0 | — | все варианты 403/400 — инструмент с ключом проекта бесполезен, менять описание | api-probe |
| LC-12 | [docs/mcp.md] `k8s_helm` и `k8s_apply` вживую | `helm install` стандартного чарта и `apply` манифеста через evo на живом кластере; `helm` с `repo_url`; удаление релиза | создание | в `k8s-smoke` после `add-addon`: `evo --yes k8s apply <id> --manifest "$(cat nginx.yaml)"`; `evo --yes k8s helm <id> install --release evo-e2e-nginx --chart bitnami/nginx --repo-url https://charts.bitnami.com/bitnami`; `evo k8s kubectl <id> get pods`; `evo --yes k8s helm <id> uninstall --release evo-e2e-nginx` | +10 мин к `k8s-smoke` | 0 сверх кластера (≈13 ₽/ч, k8s pricing) | удаление кластера уже есть | `apply`/`helm` падают политикой или таймаутом, релиз без egress в `ImagePullBackOff` (тогда это K8S-3, не evo) | cloud-e2e: расширение `k8s-smoke` |
| LC-13 | [docs/tools.md] `stack_plan → stack_apply → stack_destroy` вживую | стек из ВМ и секрета создаётся за несколько cranks, `stack_status` доходит до `done`, `stack_destroy` убирает всё, включая FIP | создание | `evo --yes tools call stack_plan --args-file stack.json` → цикл `stack_apply`/`stack_status` до `done` (≤ 10 мин) → `stack_destroy` → `vm_list`, `secret_list`, `list_floating_ips` | 15 мин | < 1 ₽ | `stack_destroy` + «сирот нет» | job в `error` с живыми ресурсами; после `destroy` остался FIP или секрет | cloud-e2e: job `stack-smoke` (набросок ниже) |
| LC-14 | [docs/install.md] Windows: установка, ACL ключа, `deploy` по SSH, `k8s kubectl` | полный путь на Windows: sha256 в PowerShell, `Path`, `configure` (ACL файла — `icacls`), `evo deploy` (SSH, known_hosts в `.evo\`), `k8s kubectl` с `kubectl.exe` в `PATH` | создание | по `docs/install.md` §Windows и §kubectl; `icacls %APPDATA%\evo\credentials.yaml` — только владелец, SYSTEM, Administrators; `evo deploy` из PowerShell; Ctrl-C на ожидании | 1 ч | < 2 ₽ | `evo destroy` | ACL шире; `deploy` падает на SSH/CRLF; `kubectl` не найден при наличии в `PATH` | догфуд: P3, P4, P8 (§1, §2.1, §2.7) |
| LC-15 | [docs/troubleshooting.md] корпоративный прокси | `evo doctor`, `vm list`, `deploy` (SSH-порт 22 наружу), `k8s kubectl` за `HTTPS_PROXY` с паролем; пароль не в выводе | чтение + одна ВМ | `HTTPS_PROXY=http://user:pass@proxy:3128 evo doctor --json` и далее по ядру; `evo deploy`; `grep pass` по всему выводу | 30 мин | < 1 ₽ | `evo destroy` | пароль в тексте ошибки или в `doctor`; `kubectl` не получил прокси; зависание > 60 с без ошибки | догфуд: P1, P3, P5, P8 (§1) |
| LC-16 | [docs/guide.md §11] доступность отечественных ОС проекту | изменилось ли с 16.10: `image get astra…` — `available_in_project`; если да — `os-smoke` зелёный | чтение; создание в `os-smoke` | `evo image list astra --json`, `evo image get <имя> --json` (поле `available_in_project`); при `true` — запуск `os-smoke` | 5 мин; 45 мин | 0; ВМ < 2 ₽ | есть в `os-smoke` | — (информация для §2.3 протокола) | api-probe (чтение) → cloud-e2e `os-smoke` |
| LC-17 | [реестр BIL-5/BIL-6] `billing_balance`, `billing_consumption` с ключом проекта | работают ли с ключом уровня проекта; нужен ли `CLOUDRU_AGREEMENT_ID`; форма ответа | чтение | `evo tools call billing_balance --json`, `billing_consumption --arg start=<дата>`; с `CLOUDRU_AGREEMENT_ID` и без | 5 мин | 0 | — | оба — ошибка с любым ключом: убрать обещание «баланс и расход» из §5 протокола | api-probe (пробы P13, P20); догфуд §2.13 как подтверждение на ключах участников |
| LC-18 | [реестр DB-17/DB-20/DB-21] тарификация `DEPLOYING` и `FAILED` | списываются ли деньги за кластер, который создаётся 40 мин или завис в `FAILED` | чтение после `dbaas_smoke` | на следующий день после прогона `dbaas_smoke` — `billing_consumption` за сутки прогона, строки Kafka/Redis/PostgreSQL против времени `DEPLOYING` из лога | 10 мин | 0 (прогон уже был) | — | оплачено больше времени `READY`: пометка ⚠ в §2.8–2.9 становится фактом с цифрой | TTY (владелец, консоль + `billing_consumption`) |
| LC-19 | [docs/mcp.md] HTTP-режим и Docker-образ у реального клиента | Cursor и Claude Code по `http://localhost:8080/mcp` с bearer: `initialize`, сессия, `tools/call` с подтверждением, отключённые по HTTP инструменты отвечают понятной ошибкой | чтение | `MCP_BEARER_TOKEN=… evo mcp --http 127.0.0.1:8080`; конфиг клиента из `docs/mcp.md`; `secret_value` → ошибка политики | 20 мин | 0 | — | клиент не держит `Mcp-Session-Id`; SSE рвётся через keep-alive; `confirmation_required` клиент показывает как сбой | догфуд: P7 (§2.15) |

Бесплатных (только чтение или уже оплаченный прогон): LC-10, LC-11, LC-16
(часть 1), LC-17, LC-18, LC-19 — 6 из 19. Требуют одобрения владельца
(cloud-e2e): LC-2, LC-4, LC-5, LC-6, LC-9, LC-12, LC-13, LC-16 (часть 2).
Вручную на TTY: LC-1, LC-3, LC-7, LC-8. На догфудеров: LC-1, LC-3, LC-7,
LC-14, LC-15, LC-17, LC-19.

## Описания

**LC-1, LC-7.** Проверяются человеком за терминалом, потому что
`handleInterrupt` спрашивает только при TTY, а срок токена — реальное время.
Для LC-7 `token_ttl_seconds` даёт `evo whoami --json`; порог обновления —
5 минут до истечения (`internal/auth/iam.go`). Исход LC-7 влияет на §4
протокола: если команда падает, строка «Долгая операция и токен» становится
известным ограничением до фикса.

**LC-2, LC-3.** Один класс: вызов оборван после `CreateVM`. LC-2 — детерминированный вариант
для CI (сигнал процессу `evo tools call`), LC-3 — поведение клиентов, которое
никакой CI не воспроизведёт. LC-3 стоит пройти владельцу до волны: если
клиент повторяет вызов сам, в протокол §2.15 добавляется предупреждение
«не нажимайте Stop во время создания».

**LC-4, LC-5.** Реестр CMP-12 говорит: 409 на второе имя в проекте
(2 из 2), но обе ВМ были живые. Для ВМ в `error_creating` ответ не
проверялся. Сначала дешёвая часть; часть 2 — только если имя не занято.

**LC-8, LC-18.** Два вопроса о счёте, которые закрывают пометки
«допущение» в §5 протокола. Делаются на тестовом проекте владельца:
участникам волны не стоит проверять лицензии на свои 4 000 ₽.

**LC-9, LC-13.** Две области, которых нет в cloud-e2e вовсе (Container
Apps, стеки). Наброски job ниже.

## Наброски для cloud-e2e

Общие правила из существующих job: `environment: cloud-e2e`, `concurrency:
cloud-e2e`, `permissions: contents: read`, имена ресурсов `evo-e2e-*`,
вывод через `redact.sed`, уборка с `if: always()`, затем шаг «сирот нет».

### Шаг в `e2e`: обрыв `provision` (LC-2) и повторный `destroy` (LC-6)

```yaml
      - name: SIGINT посреди provision (после CreateVM)
        run: |
          set +e
          timeout --preserve-status -s INT 40 \
            evo --yes tools call provision --arg name=evo-e2e-int --arg image=ubuntu-22.04 --json \
            2>&1 | sed -E -f .github/scripts/redact.sed; echo "код: ${PIPESTATUS[0]}"
          set -e
          # Инвариант: ВМ либо нет, либо она одна и destroy её видит.
          n=$(evo tools call vm_list --json | jq '[.vms[]? | select(.name=="evo-e2e-int")] | length')
          [ "$n" -le 1 ] || { echo "::error::после обрыва ВМ evo-e2e-int: $n"; exit 1; }
          evo --yes tools call provision --arg name=evo-e2e-int --arg image=ubuntu-22.04 --json >/dev/null 2>&1 || true
          n=$(evo tools call vm_list --json | jq '[.vms[]? | select(.name=="evo-e2e-int")] | length')
          [ "$n" -le 1 ] || { echo "::error::повтор создал вторую ВМ"; exit 1; }
      - name: destroy evo-e2e-int (всегда)
        if: always()
        run: evo --yes tools call destroy --arg name=evo-e2e-int --json | jq -c '{status}' || true
      # LC-6: в шаге «destroy --yes (всегда)» убрать sleep 30 между первой и
      # второй попыткой и печатать код и reason второй.
```

### Job `ca-smoke` (LC-9)

```yaml
  ca-smoke:
    environment: cloud-e2e
    if: ${{ inputs.ca_smoke }}          # новый boolean-вход, default false
    runs-on: ubuntu-latest
    timeout-minutes: 30
    steps:
      # checkout, setup-go, сборка, configure — как в e2e
      - name: ca_deploy nginx с публичным URL
        run: |
          evo --yes tools call ca_deploy --arg name=evo-e2e-ca --arg image=nginx \
            --arg port=80 --arg public=true --json | jq -c '{status, operation_id}'
          for _ in $(seq 1 30); do
            url=$(evo tools call ca_get --arg name=evo-e2e-ca --json | jq -r '.url // .public_url // empty')
            [ -n "$url" ] && break; sleep 10
          done
          [ -n "$url" ] || { echo "::error::нет URL приложения"; exit 1; }
          echo "URL=$url" >> "$GITHUB_ENV"
      - name: 300 запросов, затем ca_logs — хвост последний
        run: |
          for i in $(seq 1 300); do curl -s -o /dev/null "$URL/?n=$i"; done
          sleep 60
          evo tools call ca_logs --arg name=evo-e2e-ca --json > logs.json
          # Ключ массива строк — по `evo tools describe ca_logs`.
          lines=$(jq -r '.lines // .logs // .data | length' logs.json)
          last=$(jq -r '(.lines // .logs // .data)[-1] // empty' logs.json)
          echo "строк: $lines; последняя: ${last//[0-9.]*\.[0-9.]*/<ip>}" | head -c 300
          [ "$lines" -le 100 ] || { echo "::error::больше 100 строк"; exit 1; }
          grep -q 'n=300' <<<"$last" || { echo "::error::последняя строка не n=300 — хвост не последний (break-98 L-1)"; exit 1; }
      - name: ca_delete (всегда)
        if: always()
        run: evo --yes tools call ca_delete --arg name=evo-e2e-ca --json | jq -c '{status}' || true
```

Точный ключ массива строк в ответе `ca_logs` — по `evo tools describe
ca_logs`; критерий «строка > 100» достаточен, точное сравнение с консолью —
вручную по ссылке из `ca_get`.

### Job `stack-smoke` (LC-13)

```yaml
  stack-smoke:
    environment: cloud-e2e
    if: ${{ inputs.stack_smoke }}       # новый boolean-вход
    runs-on: ubuntu-latest
    timeout-minutes: 40
    steps:
      # checkout, setup-go, сборка, configure — как в e2e
      - name: stack_plan
        run: |
          # Манифест: поля — по `evo tools describe stack_plan` (internal/stack/store.go:
          # vm — один объект, secrets — массив; здесь только ВМ).
          cat > stack.json <<'EOF'
          {"manifest":{"name":"evo-e2e-stack","vm":{"name":"evo-e2e-stack-vm","image":"ubuntu-22.04"}}}
          EOF
          evo --yes tools call stack_plan --args-file stack.json --json > plan.json
          jq -r '.job_id' plan.json > job_id
      - name: stack_apply до done (≤ 20 мин)
        run: |
          id=$(cat job_id)
          for i in $(seq 1 40); do
            st=$(evo --yes tools call stack_apply --arg job_id="$id" --json | jq -r '.status')
            echo "crank $i: $st"; [ "$st" = done ] && exit 0
            [ "$st" = error ] && { echo "::error::job error"; exit 1; }
            sleep 30
          done
          echo "::error::job не дошёл до done"; exit 1
      - name: stack_destroy (всегда)
        if: always()
        run: |
          id=$(cat job_id 2>/dev/null) || exit 0
          evo --yes tools call stack_destroy --arg job_id="$id" --json | jq -c '{status}' || true
      - name: Сирот стека нет (всегда)
        if: always()
        run: |
          evo tools call vm_list --json | jq -e '[.vms[]? | select(.name|startswith("evo-e2e-stack"))] | length == 0'
          evo tools call list_floating_ips --json | jq -e '[.floating_ips[]? | select((.name // "")|startswith("fip-evo-e2e-stack"))] | length == 0'
```

Набросок показывает порядок шагов и уборку; ключи ответов (`job_id`,
`status`, массив адресов) — сверить по `evo tools describe` перед
добавлением job. Вторым шагом волны — тот же стек с `secrets` и `nlb`.

### Шаги в `os-smoke` (LC-4) и `k8s-smoke` (LC-12)

`os-smoke`: после отказа создания из недоступного образа (ВМ в
`error_creating`, если она вообще появляется) — `evo --yes vm create <то же
имя> --image ubuntu-22.04 --json`, печать `reason`; затем удаление обеих по
id. `k8s-smoke`: между «add-addon cert-manager» и «Удаление кластера» —
`evo --yes k8s apply <id> --manifest "$(cat nginx.yaml)"` с минимальным
`Deployment nginx` и `evo --yes k8s helm <id> install --release evo-e2e-nginx
--chart bitnami/nginx --repo-url https://charts.bitnami.com/bitnami`,
проверка `evo k8s kubectl <id> get pods`, `helm <id> uninstall --release
evo-e2e-nginx`. Таймаут job поднять со 180 до 200 мин.
