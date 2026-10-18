Cursor: break-report, main=6ef5e255f6abd11e7bdeb9ca00397f374faef3d8

Спецификации скачаны `scripts/cloudru-specs.py` в `.cache/cloudru-specs` (monitoring в манифесте `missing` — страница на cloud.ru/docs не отдала файл; на находки это не влияет). Формы ответов фейков — из этих файлов. Для сервисных аккаунтов ключи тела — из реестра IAM-2 (`service_accounts`, `next_page_token`), он важнее camelCase в `iam-gw-public.yaml`.

Тесты только падают. Код не менялся.

| ID | Серьёзность | Область | Вход → неверный результат | Ожидалось (со ссылкой на документ) | Тест | Предложение |
|---|---|---|---|---|---|---|
| B-1 | blocker | `pg_create` | Фейк `GET /v1/clusters`: страница 1 — кластер `one`, `next_page_token=p2`; страница 2 — кластер `page-two` (`id` `00000000-0000-4000-8000-000000000002`, `status=READY`). Вызов `pg_create` с `name=page-two`, `specification_id=std`, `confirmed=true`. Ответ `status=creating`, `operation_id=op-1`: ушёл `POST /v1/clusters`. | Описание инструмента: «Refuses a name an existing cluster has» (`internal/tools/tools.go`, `pg_create`). Занятое имя — `USAGE`, код выхода 1, без создания (`internal/tools/namecheck.go`, `docs/exit-codes.md`). Форма списка — `postgresql/openapi-postgresql.yaml`: `clusters` + `next_page_token`, query `page_token`, размер страницы по умолчанию 100. | `TestBreak_PgCreate_TakenNameOnNextPage` | Читать страницы так же, как Kafka/Redis (`internal/dbaas/client.go`: `page_token` / `next_page_token`, стоп на повторе токена). Имя с любой страницы — отказ до POST. |
| B-2 | blocker | `ar_create` | Фейк `GET /v1/registries`: страница 1 — `one` и `nextPageToken=p2`, страница 2 — реестр `page-two`. `ar_create` `name=page-two` `confirmed=true` → `status=creating`, POST реестра. | Описание: «refuses a name an existing registry has» (`ar_create` в `internal/tools/tools.go`). Тот же контракт `USAGE` без POST. Форма — `ar/openapi.yaml`: `registries` + `nextPageToken`, query `pageToken`. | `TestBreak_ArCreate_TakenNameOnNextPage` | Листать `pageToken`, пока токен новый. Занятое имя на поздней странице не создавать. |
| B-3 | medium | списки | Тот же двухстраничный фейк. В ответе только id первой страницы `…0001`, id `…0002` нет: `lb_list`, `tg_list`, `vpc_list`, `vpc_routes`, `dns_zones`, `dns_records`, `k8s_list`, `ca_list`, `pg_list`, `ar_list`, `org_projects`, `iam_service_accounts`. Ошибки нет. | Списки в `docs/tools.md` обещают ресурсы проекта, не «первую страницу». Токен следующей страницы есть в спецификации соответствующего GET (NLB `next_page_token`/`page_token`; VPC, DNS, mk8s, Container Apps, Artifact Registry — `nextPageToken`/`pageToken`; PostgreSQL и Organization `GET /v3/projects` и `GET /v3/quotas/usages` — `next_page_token`/`page_token`). IAM: тело как в IAM-2, query следующей страницы в спецификации — `pageToken`. | `TestBreak_List_DropsNextPage` | Следовать токену, который называет спецификация (для IAM — `pageToken`, тело читать и как `service_accounts`/`next_page_token`). Повтор токена — стоп. Не слать оба написания query сразу: чужой параметр у аудита давал 415 (AUD-2). |
| L-1 | low | `ca_logs` | Код `Logs` читает один `GET /v2/containers/{name}/logs` и, если строк больше 100, оставляет последние 100 этой страницы. Спека `ListContainerLogsResponse`: `data`, `total`, `nextPageToken`; query `pageToken` и `pageSize`. | Описание инструмента: «last 100 lines». | нет теста | **нужна живая проверка.** Порядок страницы ни в спецификации, ни в реестре не записан. Гипотеза: первая страница — не последние строки, и «последние 100» теряются. Шаги: контейнер с заведомо длинным логом; `ca_logs` (или `evo` вызов того же инструмента) сравнить с логом в консоли Cloud.ru; если строк больше одной страницы и хвост не совпал — дефект подтверждён. Фейк под неизвестный порядок не ставился. |

Kafka и Redis в B-1 не входят: `ListClusters` уже листает `next_page_token`, это закрыто `TestDbaasCreate_RefusesTakenName` (имя `two` на второй странице).

## Проверено, не сломалось

1. **Деньги и сироты.** Дыра — B-1 и B-2: проверка имени смотрит урезанный список и после `confirmed=true` создаёт второй ресурс. Kafka/Redis эту проверку проходят на двух страницах. `k8s_create` имя по списку не проверяет; уникальность имён кластера в реестре расхождений не записана — отдельной находкой не ставил. Обрыв `deploy` после потерянного ответа ВМ ищет её по имени и времени (`FindCreatedVM`); это уже покрыто тестами репозитория, нового отказа не собирал.
2. **Удаление не того.** `destroy` при 404 и чужом `project_id` в записи state не помечает ВМ удалённой и возвращает `CONFIG_INVALID` (`internal/engine/destroy.go`). Нового обхода гейта не нашёл. `confirmed` строкой `"true"` не проходит `.(bool)` — вызов остаётся `confirmation_required`, удаление не идёт.
3. **State.** Файл новее `CurrentVersion` читается с ошибкой (`TestRead_NewerVersionRefused`). Параллельные `deploy`/`destroy` в бэклоге закрыты `state.Update`. Новой гонки не воспроизводил.
4. **Ввод.** YAML с цепочкой якорей (`*a` десять раз на уровень, пять уровней) `yaml.v3` отвергает за микросекунду: `yaml: document contains excessive aliasing`. В compose это `COMPOSE_INVALID`, не раздувание памяти. Отдельный тест не добавлял: дефекта нет.
5. **Ответы API.** Находки B-1…B-3. Бесконечный цикл по одному и тому же токену у клиентов без цикла не возникает; у Kafka/Redis повтор токена останавливает чтение. `null` вместо массива в эти списки не подставлял.
6. **MCP.** Флаги `--kubeconfig`, `--server`, `--token`, `--kuberc` режет `kubetools.CheckConnectionFlags` до запуска kubectl, в любом транспорте (есть тесты). Обхода не нашёл.
7. **Коды выхода.** У B-1/B-2 успешный `status=creating` вместо `USAGE` / код 1 — это та же находка, не отдельная.
8. **Окружение.** `HOME`, read-only `CLOUDRU_CONFIG_DIR`, прокси и `NO_COLOR` в этом прогоне не гонял.

## Итог

Добавлено 3 теста, все падают на `main` `6ef5e255f6abd11e7bdeb9ca00397f374faef3d8`: `TestBreak_PgCreate_TakenNameOnNextPage`, `TestBreak_ArCreate_TakenNameOnNextPage`, `TestBreak_List_DropsNextPage`. Ветка `cursor/break-2026-10-18`. Полный SHA последнего коммита — в заголовке `git log -1` этой ветки (файл отчёта не может содержать SHA коммита, который его создаёт).

## Таблица для issue

Заголовок: `[break] break-report 2026-10-18`. Метка: `отзыв`.

| ID | Серьёзность | Область | Суть |
|---|---|---|---|
| B-1 | blocker | pg_create | Имя кластера PostgreSQL со второй страницы списка не считается занятым, уходит POST второго платного кластера |
| B-2 | blocker | ar_create | То же для реестра Artifact Registry (`nextPageToken`) |
| B-3 | medium | списки | 12 инструментов списка отдают только первую страницу, хотя в ответе есть токен следующей |
| L-1 | low | ca_logs | Одна страница логов; «последние 100 строк» при нескольких страницах — гипотеза, нужна живая проверка |
