# Бриф: Evolution Data Platform в evo (для сессии «CLI+MCP · 1.3 — разработка»)

Решение владельца 22.10.2026: Data Platform идёт в план; цена — через
калькулятор (команда калькулятора использование подтвердила, D17 без
условий). Бриф составлен по публичной документации и спецификациям, без
живых вызовов. Сессия разработки закладывает его в `release-plan.md`
(объём и версия — решение владельца) и начинает с api-probe.

## 1. Что это и зачем evo

Data Platform — кластер, внутри которого живут инстансы продуктов:
Managed Spark, Trino, Airflow, Metastore, Flink, BI (у BI публичного API нет). Инстансы одного
кластера размещаются в одной VPC и зоне. Официальных MCP-серверов у
Cloud.ru для этого нет (владелец, 22.10). Для агента ценность — не
жизненный цикл инстансов, а **задачи Spark**: «запусти расчёт, скажи, когда
закончился, покажи операции» — это первый сценарий, который стоит показать.

## 2. Факты из документации и спецификаций

Эндпоинт один — `https://dataplatform.api.cloud.ru`. Спецификации (все
Swagger 2.0, кроме Spark — OpenAPI 3). Расхождения документации и
спецификаций — реестр `docs/internal/cloudru-discrepancies.md`, раздел
«Evolution Data Platform» (DP-1…13), он же отдаётся команде Data Factory:

| Продукт | Страница API | Спецификация | Вызовы |
|---|---|---|---|
| Data Platform (кластеры) 1.0 | `/docs/dataplatform/ug/topics/api` | `/docs/api/specs/dataplatform/ug/_specs/data-platform-public-api.yaml` | clusters: list, create, get, delete, stop; operations |
| Managed Spark 3.0.3 | `/docs/spark/ug/topics/api` | `/docs/api/specs/spark/ug/_specs/spark-public-api.yaml` | sparks: list/create/get/update/delete, stop, resume; jobs: list/create/get/cancel/batch-cancel, operations; spark-connects; справочники sparkFlavors, sparkVersions, sparkBaseImages |
| Managed Trino 2.0 | `/docs/trino/ug/topics/api` | `/docs/api/specs/trino/ug/_specs/trino-public-api.yaml` | trinos: list/create/get/update/delete; trinoCatalogs: list/create/get/update/delete; trinoFlavors, trinoVersions |
| Managed Airflow 2.0 | `/docs/managed-airflow/ug/topics/api` | `/docs/api/specs/managed-airflow/ug/_specs/airflow-public-api.yaml` | airflows: list/create/get/update/delete, operations; airflowFlavors |
| Managed Metastore 2.0 | `/docs/metastore/ug/topics/api` | `/docs/api/specs/metastore/ug/_specs/metastore-public-api.yaml` | metastores: list/create/get/update/delete, operations; metastoreFlavors, metastoreVersions |
| Managed Flink 2.0 | `/docs/managed-flink/ug/topics/api` | `/docs/api/specs/managed-flink/ug/_specs/flink-public-api-v1.yaml` | flinks: list/create/get/update/delete, stop, resume, operations; jobs: list/create/get/delete, operations; flinkFlavors, `/v1/versions` |
| Managed BI | страницы API нет (404) | — | публичного API нет (DP-12) |

Общее:
- **Асинхронность.** Create/delete/stop/update возвращают `Operation`
  (`id`, `done`, `error`, `resourceId`); у каждого ресурса есть
  `GET …/{id}/operations` с пагинацией.
- **Идемпотентность.** Заголовок `Idempotency-Key: <UUIDv4>`: повтор с тем
  же ключом возвращает ту же операцию, второй кластер не создаётся. Без
  ключа сервер генерирует свой и отдаёт в ответе. В спецификациях заголовок
  **не описан** — только в документации.
- **Списки** постраничные (`pageSize`, `pageToken`), фильтр по `name`
  (`filter=name=…`).
- **Создание кластера:** `projectId`, `name`, `serviceAccountId`,
  `networkConfiguration{zoneIds[], vpcId, subnetId}` — все обязательны.
- **Создание инстанса** (Spark/Trino/Airflow/Metastore): `clusterId`,
  `projectId`, `name`, `networkConfiguration{subnetId, zoneId}`, `logGroupId`
  (группа логирования — обязательна у всех четырёх), флейвор и число узлов
  (`computeConfiguration`/`nodeConfiguration`/`resourcesConfiguration`),
  у Spark — ещё `user` (доступ к Spark History) и `enabledExternalHost`;
  у Airflow — `s3Conf{tenantId, bucketName, bucketId}`; у Trino —
  `catalogIds[]`. Пароли и ключи в запросах — только **ID секретов** Secret
  Manager (`passwordSecretId`, `accessKeySecretId`), значений в API нет.
- **Задача Spark:** `name`, `jobType` (JAVA/PYTHON/SCALA/R), параметры по
  типу (`python.mainAppFile`, `args`, `env`, `configSpark`, `pyFiles`…),
  `driver{cores, memory, gpus}`, `executor{count, cores, memory, gpus}`,
  образ — базовый (`sparkBaseImages`) или свой.
- **Статусы** кластера и инстансов: PENDING → CREATING → RUNNING;
  STOPPING/STOPPED (кластер), SUSPENDING/SUSPENDED/RESUMING, UPDATING,
  DELETING/DELETED, FAILED, ERROR. У Spark enum статусов в спецификации
  нет — значения узнать живым запросом.

## 3. Расхождения и неизвестное — снять api-probe до кода

api-probe (только чтение, из `main`, одобрения не требует):
1. **Путь.** Документация: `POST /api/v1/{project_id}/clusters`;
   спецификация: `POST /v1/clusters` с `projectId` в теле/query. Какой
   живой — проверить `GET /v1/clusters?projectId=…` и вариант с префиксом.
2. **Аутентификация.** В спецификациях схемы нет; документация отсылает к
   общему руководству (токен IAM). Проверить, что токен IAM принимается
   (у Foundation Models он принимался не везде — FM-1).
3. **Статусы Spark** (нет enum) и форма `Operation.error`.
4. **Что отдают справочники** `*Flavors`/`*Versions`/`sparkBaseImages` —
   это вход для превью (имя флейвора, CPU/RAM, зоны) и для калькулятора.
5. **Кластер: есть `RESUMING`, но нет вызова resume** (только `stop`);
   у Spark `:resume` есть. Уточнить, как возобновляют кластер.
6. **`Idempotency-Key` вживую.** Повтор `POST` с тем же ключом возвращает
   ту же операцию, а не второй ресурс? Если заголовок отвергается или
   игнорируется — записать в `cloudru-api.md` и защищать от дублей по
   (project, name) перед созданием, как `k8s_create`.
7. **Цена.** Ни в одной спецификации нет стоимости. Калькулятор:
   `api.cloud.ru/calculator/v2/prices/<продукт>` (D17) — выяснить имена
   продуктов для Spark/Trino/Airflow/Metastore/кластера и состав ответа.
   Найденное — в реестр (DP-N: гипотезы DP-1, DP-4, DP-10, DP-12, DP-13
   подтвердить или снять) и в `.github/review-context/cloudru-api.md`.

## 4. Как это ложится на evo (механизмы, не новые пути)

- **Клиент:** новый пакет `internal/dataplatform` по образцу
  `internal/dbaas` (один HTTP-клиент на эндпоинт, типы из спецификаций,
  пагинация до конца списка — правило «неполный список — ошибка», 1.1.0).
  `Idempotency-Key` — на каждом create: ключ из аргумента вызова или
  детерминированный от (project, name) — одна точка, как у повторного
  `k8s_create` (USAGE с ID существующего, дубля нет).
- **Реестр:** группы `dp` (кластеры), `spark`, `trino`, `airflow`,
  `metastore` — каждый инструмент с `CLISpec` (`evo dp cluster create`,
  `evo spark job run`…), описания EN/RU, `readOnlyHint` только у чтений;
  `--toolsets` пополняются; `docs/tools.md`, `docs/commands.md`
  перегенерировать. Тест-инварианты (`TestParity_*`, `TestContract_*`,
  `TestReadOnly_*`, `TestContract_EveryPreviewPricedOrListed`) должны
  пройти без исключений в списках.
- **Согласие и превью:** каждое создание — `engine.CreatePreview` с ценой
  калькулятора (D17) и `tariff_url`; обязательные поля (`serviceAccountId`,
  `logGroupId`, подсеть, флейвор) проверяются **до** вопроса о согласии
  (D15: вызов, который заведомо упадёт, отказывает до превью). Удаление
  кластера — разрушительное, превью называет инстансы внутри.
- **Операции:** ожидание по `GET …/operations` тем же циклом, что ждёт ВМ
  (`config.DefaultTimeout`, D16); Ctrl-C и таймаут не теряют ID ресурса
  (`details` в конверте ошибки, FB121-1). `stop` и `resume` — изменяющие,
  оба с превью и согласием (решение владельца 22.10, как `ca_stop`: stop
  прерывает работу кластера, это не только про деньги); resume — платное,
  превью с ценой калькулятора или `tariff_url`.
- **`--allow-project` (H9):** все виды — в `project_bound.go`
  (`kindDPCluster`, `kindSpark`…) с проверкой `projectId` ресурса по GET.
- **`--read-only`:** чтения и справочники доступны; `spark_job_*` —
  изменяющие (запуск задачи платный).
- **Секреты:** в API только ID секретов — значений в ответах и превью нет;
  H7 не затрагивается, но `user` у Spark (логин/пароль Spark History) —
  проверить, не возвращает ли API пароль в GET.
- **Контракт:** пять спецификаций — в `scripts/cloudru-specs.py` (ключи
  `dataplatform`, `spark`, `trino`, `managed-airflow`, `metastore`), чтобы
  `api-contract-check` и job `contract` их проверяли.
- **cloud-e2e:** новый вход `dp_smoke` (кластер → Spark → задача Python
  на базовом образе → cancel → удаление) — платный и долгий, как DBaaS;
  уборка `evo-e2e-dp-*` в `cleanup_only`; квота тестового проекта на
  кластер DP — запросить у владельца заранее.

## 5. Объём и порядок (решение владельца 22.10: api-probe и квота — 1.3, инструменты — 1.4)

| Волна | Что | Инструментов (оценка) |
|---|---|---|
| 1 | кластеры DP (list/get/create/delete/stop/operations) + Spark: инстансы (list/get/create/delete/stop/resume), задачи (list/get/run/cancel), справочники (flavors/versions/base images) | ~17 |
| 2 | Trino (инстансы, каталоги, справочники), Metastore | ~16 |
| 3 | Airflow, Flink (инстансы, задачи); Spark Connect; update у всех | ~24 |

Волна 1 даёт сценарий «агент запускает Spark-задачу и следит за ней»;
без него Data Platform в evo — только ещё один жизненный цикл инстансов.
Trino-каталоги содержат адреса и ID секретов внешних БД — в превью
показывать без значений.

## 6. Критерии готовности волны

- Каждый инструмент: тест на фейке API (падал без правки), паритет
  CLI ↔ MCP, превью с ценой калькулятора или `tariff_url`, `--allow-project`
  и `--read-only` закреплены инвариантами; повторный create — без дубля.
- api-contract-check зелёный на новых спецификациях; расхождения — в
  реестре с ID `DP-N`.
- cloud-e2e `dp_smoke` зелёный, остатков нет; живые статусы и форма
  ошибок задокументированы.
- H7: ответы `spark_get`, `trino_catalog_get` и остальных GET не несут
  секретов открытым текстом (пароль `user`, ключи S3) — закреплено тестом
  маскирования (`TestRedact_*`) на живой форме ответа.
- CHANGELOG, `docs/tools.md`, `docs/commands.md`, README «Что сейчас
  можно» — новые команды названы.
