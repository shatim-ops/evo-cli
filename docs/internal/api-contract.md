# Аудит API-контракта: запросы evo против официальных спецификаций Cloud.ru

Дата сверки: 15.10.2026. Код: `main` на `ed28e57` (дерево совпадает с
`claude/friendly-newton-4ivwym` @ `fba971c`). Спецификации скачаны
`scripts/cloudru-specs.py` со страниц справочников API на cloud.ru/docs в тот же
день; в репозиторий они не кладутся (материалы Cloud.ru), хэши — ниже.

Живых запросов к API в этой сессии не было: ключей Cloud.ru в окружении нет.
Поэтому графа «проверено вживую» заполнена только из комментариев в коде,
`.github/review-context/cloudru-api.md`, BACKLOG и журналов cloud-e2e — и так и
помечена: «из комментария». Всё, что помечено «нет», проверить вживую ещё нужно.

Содержание:

1. [Источники](#1-источники)
2. [Сводка](#2-сводка)
3. [Таблица по сервисам](#3-таблица-по-сервисам)
4. [Расхождения, требующие правки](#4-расхождения-требующие-правки)
5. [Живое API расходится со спецификацией — вопросы к командам Cloud.ru](#5-живое-api-расходится-со-спецификацией--вопросы-к-командам-api-cloudru)
6. [Проверка контракта в CI](#6-проверка-контракта-в-ci)

## 1. Источники

`python3 scripts/cloudru-specs.py .cache/cloudru-specs` — 20 файлов для 16
хостов. `billing/swagger2.yaml` в прогоне не скачался (обрыв TLS на cloud.ru),
докачан вручную тем же URL: это копия `swagger.yaml` с одним путём
`/v2/consumption`.

| сервис (хост) | файл | формат | title / version | путей | sha256 (12) |
|---|---|---|---|---|---|
| compute.api.cloud.ru | compute/openapi-v3.yaml | OpenAPI 3.1.0 | SVC Public API / 0.1.0 | 58 | 45cf7cbcdde2 |
| vpc.api.cloud.ru | vpc/openapi.yaml | 3.0.3 | Evolution VPC Public API / 0.0.1 | 14 | cb9265117b39 |
| dns.api.cloud.ru | dns/openapi.yaml | 3.0.3 | Evolution DNS Public API / 0.0.1 | 31 | dbf90e160632 |
| nlb.api.cloud.ru | nlb/swagger.yaml | Swagger 2.0 | Cloud.ru Evolution Load Balancer / 1.0 (host в файле не указан) | 5 | 8593baa6ad64 |
| mk8s.api.cloud.ru | mk8s/openapi-v3.yaml | 3.0.3 | Managed Kubernetes API / 3.0.1 (код использует v3) | 18 | e8186cd966a5 |
| mk8s.api.cloud.ru | mk8s/openapi.yaml | 3.0.3 | Managed Kubernetes API / 2.1.1 (v2, кодом не используется) | 27 | d9a2a1685708 |
| postgresql.api.cloud.ru | postgresql/openapi-postgresql.yaml | 3.0.3 | Managed PostgreSQL API / 1.0.0 | 18 | bb39b2ea1fad |
| kafka.api.cloud.ru | kafka/openapi-kafka.yaml | 3.0.3 | Managed Kafka API / 1.0.0 | 11 | 60dde91da8f5 |
| redis.api.cloud.ru | redis/openapi-redis.yaml | 3.0.3 | Managed Redis API / 1.0.0 | 10 | 4631fa1f95e8 |
| ar.api.cloud.ru | ar/openapi.yaml | Swagger 2.0 | Artifact Registry OpenAPI 2.0 | 4 | 52055317b4b4 |
| containers.api.cloud.ru | containers/openapi.yaml | 3.0.3 | Container Services OpenAPI 3.0 / v2 (servers не указан) | 14 | 4693c729de62 |
| containers.api.cloud.ru | containers/openapi-jobs.yaml | 3.0.3 | Container Jobs / v2 (кодом не используется) | 10 | 9e30f5385580 |
| secretmanager.api.cloud.ru | secretmanager/api.swagger-public.yaml | Swagger 2.0 | Secret Management API / 1.0.13 | 15 | c8a2f6554e55 |
| certificatemanager.api.cloud.ru | certificatemanager/ccm.swagger.yaml | Swagger 2.0 | Certificate Manager API | 11 | f52e9663ea88 |
| monitoring.api.cloud.ru | monitoring/openapi-v3.yaml | 3.0.1 | Cloud Monitoring API / 1.0.1 | 12 | c252a078e2ad |
| audit.api.cloud.ru | audit/openapi-v3.yaml | 3.0.0 | API сервиса «Аудит-логирование» / v.1 | 4 | ff576f8e383c |
| organization.api.cloud.ru (billing) | billing/swagger.yaml | Swagger 2.0 | Public API Контроль затрат / 1.0 | 3 | d62046f4e56b |
| organization.api.cloud.ru | organization+iam/api.op.v3.yaml | 3.0.0 | Администрирование каталогов, проектов и квот (API v3) | 10 | b7e3b14732d9 |
| organization.api.cloud.ru | organization+iam/api.op_v1.v2.yaml | Swagger 2.0 | то же, API v1, v2 (в т.ч. `/v2/consumption`) | 13 | ffe78b887905 |
| iam.api.cloud.ru | organization+iam/iam-gw-public.yaml | 3.0.0 | Администрирование сервисных аккаунтов, пользователей и групп | 49 | 02814c642623 |

Чего в спецификациях нет вообще: `billing.api.cloud.ru` (хост не описан ни в
одном файле; см. §4.1) и S3 (`internal/s3` ходит по протоколу S3 через
minio-go — вне этого аудита).

Инвентаризация запросов в коде — `scripts/api-contract.json` (133 запроса,
один на метод+путь); прогон `scripts/api-contract-check.py` по этим спецификациям
даёт 70 ok, 5 error, 45 warn, 33 info, 36 accepted (принятые осознанно,
с причиной). Таблица ниже — ручной разбор того же, с серьёзностью.

## 2. Сводка

Что изменилось в понимании после сверки:

- **Пять «наблюдений вживую» объясняются спецификацией, а не капризами API**, и
  лечатся в коде: плоские `availability_zone_id` у подсетей и групп
  безопасности (§4.3), плоский `floating_ip` у ВМ (§4.4), пустой список ВМ без
  `project_id` (§5, закрыто), «статус» образа (§4.6), `events`/`pageSize` в
  аудите (§4.2).
- **Код нарушает спецификацию в 5 запросах compute**: `limit=200` при
  максимуме 100 (§4.5). Поведение сервера при этом не задокументировано —
  422 или молчаливое усечение; e2e зелёный, значит хотя бы один из вариантов
  безвреден, но какой — неизвестно.
- **Пагинация**: из 23 списковых эндпоинтов код дочитывает все страницы только
  в двух (`security-groups/{id}/rules`, `/v2/consumption`). Остальные — первая
  страница с лимитом сервера по умолчанию (compute: 50; grpc-gateway сервисы:
  не задокументирован). Самое чувствительное — флейворы (§4.7).
- **Четыре эндпоинта с «тихой» потерей данных из-за имён полей**: аудит
  (`events` vs `logs`), IAM `serviceAccounts` (`service_accounts` vs
  `serviceAccounts`), DNS `Operation.resource_id` vs `resourceId`, список зон
  DNS (`valid`/`delegated` — таких полей нет).
- **Два контракта держатся на недокументированном поведении**: токен IAM
  (`access_token` в коде, `accessToken` в спецификации — живое API отвечает
  snake_case, иначе e2e не работал бы; но `expires_in` никто не проверял) и
  баланс `billing.api.cloud.ru/v1/agreement/{id}` (эндпоинта нет ни в одной
  опубликованной спецификации).
- **Полностью совпадают со спецификацией**: NLB, Kafka, PostgreSQL (кроме
  пагинации), Secret Manager, Certificate Manager, Artifact Registry, mk8s
  node pools, `/v2/consumption`, `/v3/projects`, `/v3/quotas/usages`.

Критичность: critical — пользовательский сценарий ломается или данные
теряются молча; high — ломается в реалистичном, но не каждом сценарии;
medium — неверные/пустые данные в выводе, лишний риск при смене API; low —
косметика или лишние параметры.

## 3. Таблица по сервисам

Столбцы: эндпоинт (как шлёт код) | файл:строка | в спецификации | расхождения |
проверено вживую | серьёзность. «extra `?project_id`» — код передаёт query
`project_id`, которого у операции нет; сервер его игнорирует (e2e зелёный),
вреда нет, но при переходе на строгий валидатор это первое, что отвалится.

### 3.1 Compute — `compute.api.cloud.ru` (`compute/openapi-v3.yaml`)

| эндпоинт | файл:строка | в спецификации | расхождения | вживую | серьёзность |
|---|---|---|---|---|---|
| `POST /api/v1.1/vms` (массив; `project_id, name, flavor_name, image_name, availability_zone_id, disks[], image_metadata, interfaces[]`) | `internal/provision/adapter.go:94-82`, `types.go:48-81` | да, `/api/v1.1/vms` | **`interfaces[].interface_security_enabled` нет в спецификации**, а у элемента `additionalProperties: false` → по спецификации запрос должен отвергаться 422; `new_external_ip` описан как boolean, вживую даёт 500 (не шлём); `disks[].size` ≤ 16384; ответ — массив (код разбирает) | из комментария: поле требуется, без него 500 (review-context) | high (§5.1) |
| ↳ ответ: читает `flavor_id, image_id, availability_zone_id, public_ip, floating_ip` | `types.go:9-21` | — | **таких полей нет**: в ответе вложенные `flavor{id,name,cpu,ram}`, `image{id,name}`, `availability_zone{id,name}`, `interfaces[].floating_ip{ip_address}`; `state` enum `running, stopped, error, error_creating, error_deleting, creating, starting, stopping, rebooting, deleting, updating` — код ждёт ещё `active` (нет в enum; безвредно) | из комментария: BACKLOG [04], `flavor_id` приходит пустым | medium (§4.4) |
| `GET /api/v1/vms/{id}` | `adapter.go:126, :313` | да | нет: `interfaces[].{id,ip_address,type,primary,state,floating_ip{id,ip_address,state}}` описаны; enum `type` включает `regular, direct_ip` (+ `system, service, gateway, technical, magic_technical, fip`), `interfaces[].state=created` в enum | да (deploy ждёт `created`) | — |
| `DELETE /api/v1/vms/{id}?project_id` | `adapter.go:142`, `discovery.go:72` | да | extra `?project_id`; ответы 204/404/**405 «есть связанные сущности»**/422 «нельзя удалить в текущем статусе»; код ждёт удаляемости на 409 и 422 (`engine/destroy.go:317`) — **409 не описан, 405 не обрабатывается** (упадёт с ошибкой, ВМ и FIP останутся); тело `delete_attachments{disk_ids, external_ips}` могло бы удалять FIP вместе с ВМ | из комментария: 409/422 при `creating` (cloud-e2e 15.10) | medium (§4.8) |
| `GET /api/v1/vms?project_id&limit=200` | `discovery.go:23-30, :63` | да; `project_id` **required** | **`limit` max 100**; читается одна страница, `total` игнорируется; порядок по умолчанию `created_time desc` (поэтому `FindCreatedVM` видит свежую ВМ на первой странице) | из комментария: без `project_id` пусто (QA H5) | high (§4.5) |
| `GET /api/v1/vms` без `project_id` | `adapter.go:153` | `project_id` required → 422 | мёртвый код: вызовов нет, только в интерфейсе `api.go` | — | low |
| `GET /api/v1/flavors` | `adapter.go:183` | да | без `limit` → **первые 50 из `total`**; `disk_gb` нет (omitempty, безвредно); есть фильтры `availability_zone_id, cpu, ram, free_tier` | нет | high (§4.7) |
| `GET /api/v1/availability-zones` | `adapter.go:211` | да | ответ — массив (код разбирает оба вида) | да | — |
| `GET /api/v1/images?limit=200[&project_id]` | `adapter.go:248, :234` | да | **`limit` max 100**; одна страница; читает `os` и `status` — **таких полей нет** (состояние — `availability_zones[].state`, тип — `type: public/private/marketplace`) → фильтр `deployableImages` по `status` (`engine/init.go:176`) не работает, колонка OS в `evo images` пуста | из комментария: образы проекта видны только с `project_id` | medium (§4.5, §4.6) |
| `GET /api/v1/images/{id}` | `adapter.go:299` | да | `status`, `os` нет; `image_metadata[].is_required` нет (есть `mandatory_group`, `display_position`, `display_group`); поле `icon` — строка, про управляющие символы спецификация молчит | да (sanitize для `icon`) | low |
| `POST /api/v1/images` | `image.go:88`, `:26-48` | да | `image_metadata[].field_type` **required** с enum `info, text, linux_login, plain_password, linux_password, email, public_key, hostname` — код шлёт с `omitempty`; `availability_zones[]` с `enabled`; 409 «образ уже существует» (код — общая ошибка) | из комментария: `id`/`is_required` отвергаются, `display_name`/`display_position` обязательны (совпадает) | low |
| `DELETE /api/v1/images/{id}?project_id` | `image.go:106` | да | extra `?project_id`; 405/422 не различаются | нет | low |
| `POST /api/v1/floating-ips` | `adapter.go:358`, `types.go:232` | да | нет; `interface_id` необязателен (вариант «свободный FIP»); enum `state`: `available, in_use, error, error_creating, error_deleting, creating, deleting, updating, attaching, detaching` | да | — |
| `GET /api/v1/floating-ips/{id}` | `adapter.go:373` | да | код ждёт `active` (`engine/deploy.go:791, :856`) — **в enum нет**, есть `in_use`; безвредно | из комментария: «`in_use`/`active`» | low |
| `GET /api/v1/floating-ips?project_id&limit=200` | `discovery.go:177` | да; `project_id` required | **`limit` max 100**; одна страница; есть серверный фильтр `free=true` вместо клиентского `Available()` | нет | medium (§4.5) |
| `PUT /api/v1/floating-ips/{id}?project_id` `{interface_id, name}` | `discovery.go:184` | да | extra `?project_id`; в теле есть и `detach`, `vm_id`; ответ 200 с объектом (код терпит пустой) | да (вариант B) | low |
| `DELETE /api/v1/floating-ips/{id}?project_id` | `discovery.go:208` | да | extra `?project_id`; 422 «занят SNAT / привязан» | из комментария: привязанный не удаляется | low |
| `POST /api/v1/security-groups/{id}/rules?project_id` | `discovery.go:89`, `types.go:107` | да | extra `?project_id`; тело совпадает; `ip_protocol` enum включает `any`; ответ 201 с `id` и `state` | да (openPorts) | — |
| `GET /api/v1/security-groups/{id}/rules?offset&limit=100` | `discovery.go:122` | да | нет — **единственный compute-список с полной пагинацией**; enum `state` совпадает с `sgrules.inactive` | да | — |
| `DELETE /api/v1/security-groups/{id}/rules/{rule_id}` | `discovery.go:151` | да | нет (204/404) | да | — |
| `GET /api/v1/disk-types?project_id&limit=200` | `discovery.go:162` | да | `limit` — не параметр этой операции (игнорируется); ответ — массив, без пагинации | да | low |
| `GET /api/v1/subnets?project_id&limit=200` | `discovery.go:167`, `types.go:84` | да; `project_id` required | **`limit` max 100**; читает `availability_zone_id` — **поля нет, есть `availability_zone{id,name}`**; есть серверные фильтры `availability_zone_ids[]`, `vpc_id`; `state` (enum `created, creating, …`) не читается | из комментария: `availability_zone_id` пустой → подбор по имени | high (§4.3) |
| `GET /api/v1/security-groups?project_id&limit=200` | `discovery.go:172`, `types.go:95` | да; required | то же: `availability_zone` вложенный; фильтр `availability_zone_id` в query; `rules` в списке нет (код знает) | из комментария | high (§4.3) |
| `GET /api/v1/disks?project_id` | `disk.go:33` | да; required | без `limit` → первые 50; читаемые поля (`size, state, bootable, disk_type.name`) есть; enum `state` `available, in_use, reserved, …` | нет | medium |
| `POST /api/v1/disks?project_id` | `disk.go:57`, `:15-23` | да | extra `?project_id`; тело совпадает (`project_id` в теле required); 409 «диск существует»; про «ровно одно из `disk_type_id`/`disk_type_name`» спецификация молчит (оба optional) | из комментария: 422-валидатор | low |
| `POST /api/v1/disks/{id}/attach|detach?project_id` `{vm_id}` | `disk.go:88` | да | extra `?project_id` | нет | low |
| `PUT /api/v1/disks/{id}?project_id` `{size}` | `disk.go:109` | да | extra `?project_id`; 409/422 | из комментария: уменьшение и free-tier → 422 | low |
| `DELETE /api/v1/disks/{id}?project_id` | `disk.go:123` | да | extra `?project_id`; 405 «связанные сущности» | нет | low |
| `GET /api/v1/nat-gateways?project_id` | `nat.go:42` | да; required | без `limit` → 50; `tools_snat` читает `availability_zone_id`/`availability_zone_name` — **нет, есть `availability_zone{id,name}`** → всегда `null`; `status` нет, `state` есть (код берёт любой) | нет | medium |
| `POST /api/v1/nat-gateways?project_id` | `nat.go:69`, `:20-34` | да | extra `?project_id`; `nat_type` enum ровно `['Public sNAT']` — совпадает с константой; «ровно одно из az_id/az_name» и `additionalProperties:false` — в спецификации только второе | из комментария: живой валидатор | low |
| `DELETE /api/v1/nat-gateways/{id}?project_id` | `nat.go:85` | да | extra `?project_id` | нет | low |
| `GET https://compute.api.cloud.ru/` (doctor) | `cli/doctor.go:209-229` | нет (не эндпоинт) | проверка достижимости хоста; любой статус = ok | да | — |

### 3.2 VPC — `vpc.api.cloud.ru` (`vpc/openapi.yaml`, camelCase, grpc-gateway)

| эндпоинт | файл:строка | в спецификации | расхождения | вживую | серьёзность |
|---|---|---|---|---|---|
| `GET /v1/vpcs?project_id` | `internal/vpc/client.go:121-140` | да; **query `projectId` required** | код шлёт `project_id` (proto-имя); grpc-gateway обычно принимает оба — но контракт говорит `projectId`; `pageSize/pageToken/nextPageToken` не читаются; поля `id, name, default, type` есть (`type` enum `VPC_TYPE_CLIENT/SERVICE`) | нет (vpc_list в e2e не гоняется) | medium (§4.9) |
| `GET /v1/vpcs/{vpcId}` | `:139` | да | нет | нет | — |
| `POST /v1/vpcs` `{projectId, name, description}` | `:155`, `tools_vpc.go:78` | да | нет; Operation `id, resourceId, done, error{code,message}` совпадает | нет | — |
| `DELETE /v1/vpcs/{vpcId}` | `:160` | да | нет | нет | — |
| `GET /v1/vpcs/operations/{operationId}` | `:165` | да | нет | нет | — |
| `GET /v1/vpcs/{vpcId}/routes/static` | `:183` | да | пагинация не читается; `status` enum `ROUTE_STATUS_*` | нет | low |
| `POST /v1/vpcs/{vpcId}/routes/static` `{vpcId, projectId, subnet, nextHop{…}}` | `:201`, `tools_vpc.go:160-183, :199` | да | `vpcId` в теле не описан (есть в пути; grpc-gateway игнорирует неизвестные поля); структура `nextHop` (`virtualMachine{virtualMachine{id},interface{id}}`, `vip{vips[{id}]}`, `magicRouter{magicRouter{id},azName}`) совпадает | нет | low |
| `DELETE /v1/vpcs/{vpcId}/routes/static/{staticRouteId}` | `:206` | да | нет | нет | — |

### 3.3 DNS — `dns.api.cloud.ru` (`dns/openapi.yaml`, camelCase)

| эндпоинт | файл:строка | в спецификации | расхождения | вживую | серьёзность |
|---|---|---|---|---|---|
| `GET /v1/publicZones?project_id`, `/v1/privateZones` | `internal/dns/client.go:181-187` | да; **`projectId` required** | код шлёт `project_id`; пагинация не читается; `tools_dns.go:101-109` читает `valid`, `delegated` — **таких полей нет** (есть `state`, `confirmState`, `verificationToken`, `delegated`-подобного нет) → в `dns_zones` всегда `null`; `state` и `confirmState` отдаются с 18.10 (`state`, `confirm_state`) | из комментария: `project_id` → 200 | medium (§4.9, §4.10) |
| `GET /v1/{public,private}Zones/{id}` | `:177` | да | нет | нет | — |
| `POST /v1/{public,private}Zones` `{project_id, name, domain}` | `:190`, `tools_dns.go:152` | да; тело `projectId, name, domain` required | snake_case в теле (protojson принимает proto-имена); **Operation: код читает `resource_id`, спецификация — `resourceId`** → `op.ResourceID` пуст, id зоны приходится искать через `dns_zones` | нет | medium (§4.10) |
| `DELETE /v1/{public,private}Zones/{id}` | `:187` | да | нет | нет | — |
| `GET /v1/{public,private}RecordsSole?{public,private}_zone_id` | `:193-198`, `:69-74` | да; **`publicZoneId` required** | snake_case; пагинация не читается | из комментария (общее утверждение про snake_case) | medium |
| `GET /v1/{public,private}RecordsSole/{id}` | `:200` | да | нет | нет | — |
| `POST /v1/{public,private}RecordsSole` `{public_zone_id, name, type, values, ttl}` | `:215`, `tools_dns.go:317-323` | да; все пять required | snake_case; `resource_id` (см. выше) | нет | medium |
| `DELETE …RecordsSole/{id}` | `:213` | да | нет | нет | — |
| `GET /v1/operations/{operationId}` | `:226` | да | `resource_id` vs `resourceId` | нет | medium |

### 3.4 NLB — `nlb.api.cloud.ru` (`nlb/swagger.yaml`, snake_case)

| эндпоинт | файл:строка | в спецификации | расхождения | вживую | серьёзность |
|---|---|---|---|---|---|
| `GET /v2/networkLoadBalancers?project_id` | `internal/nlb/client.go:156`, `:101` | да; required | пагинация (`page_size/page_token`) не читается; `status` enum `NLB_STATUS_CREATING/ERROR_CREATING/RUNNING/UPDATING/…`; `external_address.ipv4` есть | нет | low |
| `GET/POST/DELETE /v2/networkLoadBalancers[/{id}]` | `:156-165`, `tools_nlb.go:288-324` | да | тело create совпадает полностью (`rules[].health_check{port,timeout,interval,unhealthy_threshold,healthy_threshold,tcp{}}`, `availability_zone_specs[].subnet_ids`, `*_address_spec`) | нет | — |
| `GET/POST/DELETE /v2/targetGroups[/{id}]` | `:170-183`, `tools_nlb.go:211` | да | `target_vm_specs[].{id, interface_id}` совпадает | нет | — |
| `GET /v2/operations/{operation_id}?id=` | `:188-205` | да; **query `id` задокументирован** | нет — наблюдение из комментария («400 id: required» без query) подтверждено спецификацией | из комментария | — |

### 3.5 Managed Kubernetes — `mk8s.api.cloud.ru` (`mk8s/openapi-v3.yaml`, camelCase)

| эндпоинт | файл:строка | в спецификации | расхождения | вживую | серьёзность |
|---|---|---|---|---|---|
| `GET /v3/clusters?project_id` | `internal/mk8s/client.go:50-56, :189` | да; **`projectId` required** | `do()` добавляет `project_id` ко **всем** запросам, включая те, где параметра нет (`/v3/clusters/{id}`, `/v3/operations/{id}`, `/v3/nodePools`); `status` enum `OBJECT_STATUS_PENDING/PROVISIONING/RUNNING/…` — stack ждёт подстроку `RUNNING` (ок); пагинация не читается | из комментария: «confirmed against the live API» | low |
| `POST /v3/clusters` | `:131-161, :226` | да | **`networkConfiguration.networkPlugin` required в спецификации** (`cilium{…}`/`calico{…}`), код не шлёт; `releaseChannel` enum `RELEASE_CHANNEL_RAPID/REGULAR/STABLE` — код передаёт строку из аргумента без проверки | из комментария: создание без него работает | medium (§5.6) |
| `DELETE /v3/clusters/{clusterId}` | `:242` | да | нет | нет | — |
| `POST /v3/clusters/{clusterId}:getKubeConfig` `{network}` | `:261` | да | тело по спецификации требует `clusterId` (дублирует путь); код шлёт только `network` | из комментария: работает | low (§5.6) |
| `GET /v3/productConfiguration?project_id` | `:279`, `:163-186` | да; **`projectId` required** | snake_case; читаемые поля есть; появились `flavors[].purpose` (control/data plane) и `availabilityZones[].flavorZoneState` (`SOLD_OUT`) — код не использует | нет | low |
| `GET /v3/operations/{operationId}` | `:295` | да | `resourceId` — совпадает (mk8s camelCase) | нет | — |
| `POST /v3/nodePools` | `nodepool.go:62`, `:13-52` | да | нет: все required (`clusterId, name, scalePolicy, machineConfiguration.flavorId, networkConfiguration.nodesSubnetId, updateConfiguration.strategy`) передаются | из комментария | — |
| `GET /v3/nodePools?clusterId` | `nodepool.go:82` | да; `clusterId` **required** | код ставит его только если не пуст (тогда 400); `scalePolicy.fixedScale.count`/`autoScale.minCount` есть | нет | low |

### 3.6 Managed PostgreSQL — `postgresql.api.cloud.ru` (`postgresql/openapi-postgresql.yaml`, snake_case)

| эндпоинт | файл:строка | в спецификации | расхождения | вживую | серьёзность |
|---|---|---|---|---|---|
| `GET /v1/clusters?project_id` | `internal/postgresql/client.go:173` | да (`project_id` optional) | пагинация не читается; `status` enum `PENDING/DEPLOYING/FAILED/UPDATING/READY/SUSPENDED/…`, stack ждёт `READY` (ок); fallback на `state` в `tools_pg.go:124` не нужен | нет | low |
| `GET/POST/DELETE /v1/clusters[/{id}]` | `:174-186`, `tools_pg.go:394-406` | да | тело совпадает (`storage.pg_data_gb`, `subnet_ids[]`, `backup{schedule, retention_policy_days}`); `subnet_id` deprecated — код не шлёт (верно) | нет | — |
| `GET /v1/operations/{id}` | `:189` | да | `resource_id` совпадает | нет | — |
| `GET /v1/versions`, `GET /v1/specifications?version_name` | `:207-218` | да | поля `display_name, cpu, memory, deployment_mode, flavor_type, min_storage_gb, max_hosts` есть | нет | — |
| databases / users / backups (`GET/POST/DELETE`) | `:222-289`, `tools_pg.go:479, :540` | да | тела совпадают (`cluster_id` в теле required — шлём); `backups[].status` enum `BACKUP_STATUS_*` | нет | — |
| пустое тело 4xx + заголовок `grpc-message` | `:71-78` | нет | формат ошибки не описан нигде | из комментария (`backup disabled`) | — (§5.7) |

### 3.7 Managed Kafka / Redis — `kafka.api.cloud.ru`, `redis.api.cloud.ru` (`internal/dbaas`)

| эндпоинт | файл:строка | в спецификации | расхождения | вживую | серьёзность |
|---|---|---|---|---|---|
| `GET /v1/clusters?product_type=kafka&project_id&page_size=1000` | `internal/dbaas/client.go:157-167`, `tools_kafka.go:86` | да (kafka: `product_type` есть) | нет; `page_token` не читается; `status` enum как у PG | из комментария: Redis отвергает `page_size<=0` | low |
| `POST /v1/clusters` (Kafka) | `tools_kafka.go:230-235` | да | **совпадает полностью**: все 10 required полей передаются | нет | — |
| `POST /v1/clusters` (Redis) `{…, storage_gb, subnet_id, type}` | `tools_redis.go:184-195` | да | `storage_gb` и `subnet_id` помечены **deprecated** (актуальные `storage_gib`, `subnet_ids[]`); `type` enum `STANDALONE, MASTER_REPLICA` | из комментария: `subnet_ids[]` роняет бэкенд (деление на ноль), `STANDALONE` отвергается | medium (§4.11, §5.8) |
| `GET /v1/versions[?product_type=kafka]`, `GET /v1/specifications?version_id&availability_zone_id` | `:199-215` | да | `availability_zone_id` **optional** в спецификации; код передаёт `Config.Region` (UUID зоны); kafka `min_storage_gigs` / redis `min_storage_gb` — код читает оба | из комментария: AZ обязателен вживую | low (§5.9) |
| `GET /v1/operations/{id}` | `:182` | да | `resource_id` совпадает | нет | — |

### 3.8 Artifact Registry — `ar.api.cloud.ru` (`ar/openapi.yaml`, camelCase)

| эндпоинт | файл:строка | в спецификации | расхождения | вживую | серьёзность |
|---|---|---|---|---|---|
| `GET /v1/registries?projectId` | `internal/artifact/client.go:127` | да; required | пагинация не читается; `status` enum `CREATING/ACTIVE/ERROR`; `registryType` enum `DOCKER/DEBIAN/RPM/GENERIC/PYPI/NPM`, `tariff` `BASIC/PREMIUM` — код приводит к верхнему регистру | нет | low |
| `GET/DELETE /v1/registries/{registryId}?projectId` | `:141, :162` | да; `projectId` required в обоих | нет | нет | — |
| `POST /v1/registries` | `:157`, `tools_ar.go:83-91` | да | нет | нет | — |
| `GET /v1/operations/{operationId}` | `:167` | да | `resourceId` совпадает | нет | — |

### 3.9 Container Apps — `containers.api.cloud.ru` (`containers/openapi.yaml`, camelCase)

| эндпоинт | файл:строка | в спецификации | расхождения | вживую | серьёзность |
|---|---|---|---|---|---|
| `GET /v2/containers?projectId` | `internal/containerapp/client.go:149` | да | пагинация (`pageSize/pageToken/total`) не читается; `status` — строка без enum (ничего, на что опираться) | нет | low |
| `GET/POST/DELETE /v2/containers[/{name}]` | `:162-206`, `:92-141` | да | тело create совпадает (все поля описаны); обязательных полей спецификация не объявляет | нет | — |
| `POST /v2/containers/{name}:start|:stop` `{}` | `:208` | да | тело по спецификации `{name, projectId}` (дублируют путь/query); код шлёт `{}` + `?projectId` | нет | low |
| `GET …/logs`, `…/systemLogs` | `:219-238` | да | `logs` постраничный, читается первая страница | нет | low |

### 3.10 Secret Manager — `secretmanager.api.cloud.ru` (`secretmanager/api.swagger-public.yaml`)

Все шесть запросов (`GET /v2/secret?projectId&depth=-1`, `GET /v2/secret/{path}`,
`POST /v2/secret`, `POST /v2/version`, `GET /v2/version/{path}?version`,
`DELETE /v2/secret/{path}?force`) — `internal/secret/client.go:116-220` —
совпадают со спецификацией по путям, параметрам, телам и читаемым полям
(`secrets[].{id,name,path,description}`, `secret`, `version`, `payload`).
`do()` добавляет `?projectId` и к POST (не описан, безвреден). Вживую не
проверялось. Серьёзность: —.

### 3.11 Certificate Manager — `certificatemanager.api.cloud.ru` (`ccm.swagger.yaml`, копия в `specs/`)

Все десять запросов (`internal/certmanager/client.go:147-275`) совпадают: клиент
построен по этому же swagger (`specs/ccm.swagger.yaml`, sha совпадает с
скачанным — файл не менялся). Единственный путь `GET /v1/certificate/{id}`
(не `{certificateId}`) — так в спецификации. Серьёзность: —.

### 3.12 Monitoring — `monitoring.api.cloud.ru` (`monitoring/openapi-v3.yaml`)

| эндпоинт | файл:строка | в спецификации | расхождения | вживую | серьёзность |
|---|---|---|---|---|---|
| `GET /v1/{project_id}/export_metrics?match&start&end` | `internal/monitoring/client.go:103-116` | да; все три required | `match` — массив (код шлёт один — ок); **единицы `start`/`end` не заданы** (код шлёт миллисекунды; «max 7-day span» в комментарии — не из спецификации); ответ описан как `string`, код терпит JSON-массив / объект / JSON lines | нет | low (§5.10) |
| `GET /v2/{project_id}/scrape_metrics?match` | `:162-175` | да; **`match` required** | код не шлёт пустой `match` → 400 вместо «всё»; 413 «превышен лимит таймсерий» не различается | нет | low |

### 3.13 Audit — `audit.api.cloud.ru` (`audit/openapi-v3.yaml`)

| эндпоинт | файл:строка | в спецификации | расхождения | вживую | серьёзность |
|---|---|---|---|---|---|
| `GET /events/list/{projectId}?start&end&pageSize&nextPageToken` | `internal/audit/client.go:108-140`, `tools_audit.go:34-52` | да | **ответ — `logs[]`, код читает `events[]`** (терпит и голый массив); **размер страницы — `limit`, код шлёт `pageSize`** (игнорируется); `nextPageToken` совпадает; есть фильтры `eventType`, `eventSource`, `sort.*` | нет | critical, если живое API соответствует спецификации (§4.2) |

### 3.14 Billing — `billing.api.cloud.ru`, `organization.api.cloud.ru`

| эндпоинт | файл:строка | в спецификации | расхождения | вживую | серьёзность |
|---|---|---|---|---|---|
| `GET https://billing.api.cloud.ru/v1/agreement/{agreement_id}` → `balance`, `active` | `internal/billing/client.go:88-105`, `tools_billing.go:226-233` | **нет** — хост `billing.api.cloud.ru` не описан ни в одной спецификации; документированные `GET /v1/agreements` (v1) и `GET /v3/agreements` (v3) на `organization.api.cloud.ru` отдают `id, credit_scheme, type, status` — без баланса | — | нет | high (§4.1) |
| `GET https://organization.api.cloud.ru/v2/consumption?agreement_id&start_date&end_date&page_filter.page_size=1000&page_filter.page_token` | `client.go:106-143` | да (`api.op_v1.v2.yaml`, `billing/swagger*.yaml`) | нет — **полная пагинация по `next_page_token`**; `page_filter.page_size` required — шлём; поля `amount, amount_nds, cost, servname, resource_name` описаны; `cost_nds`, `servicename`, `service`, `resourcename` (fallback-имена в `tools_billing.go:104-109`) — нет | нет | — |

### 3.15 Organization — `organization.api.cloud.ru` (`api.op.v3.yaml`)

| эндпоинт | файл:строка | в спецификации | расхождения | вживую | серьёзность |
|---|---|---|---|---|---|
| `GET /v3/projects` | `internal/cli/configure.go:30, :261-296`, `tools/tools_admin.go:89`, `whoami.go` | да | `page_size/page_token` есть, код читает первую страницу (размер по умолчанию не задокументирован); `status` enum `PROJECT_STATUS_ENABLED`; **400 для сервисного аккаунта не описан** (документированы 401/403/500) | из комментария: 400 «user not found» (14.10) | low (§5.11) |
| `GET /v3/quotas/usages?page_size=200&subject_type_codes&subject_ids` | `tools_admin.go:109-143` | да | нет; `individual_limit/usage` — строки (код отдаёт как есть); 403 для SA (из комментария) не описан | из комментария | — |

### 3.16 IAM — `iam.api.cloud.ru` (`iam-gw-public.yaml`)

| эндпоинт | файл:строка | в спецификации | расхождения | вживую | серьёзность |
|---|---|---|---|---|---|
| `POST /api/v1/auth/token` `{keyId, secret}` | `internal/auth/iam.go:21, :150-223` | да; тело совпадает | **ответ по спецификации `accessToken`, `expiresIn` (camelCase), код читает `access_token`, `expires_in`**. Раз e2e проходит, живой ответ содержит `access_token`; про `expires_in` данных нет — если его нет, `ExpiresAt = now` и токен обменивается заново при каждом вызове (`IsValid(5m)` всегда false) | из комментария: `access_token`, ~час (review-context) | high (§4.12) |
| `GET /api/v2/serviceAccounts?target.projectId` | `tools/tools_admin.go:68-85` | да | **ответ `serviceAccounts[]`, код читает `service_accounts`** → `iam_service_accounts` возвращает пустой список, если gateway не дублирует snake_case; `pageSize/pageToken` не читаются | нет | high (§4.12) |
| `GET https://iam.api.cloud.ru/` (doctor) | `cli/doctor.go:209` | нет (не эндпоинт) | только достижимость | да | — |

## 4. Расхождения, требующие правки

Порядок — по серьёзности. «Путь к дефекту» — реальный сценарий, в котором
пользователь это увидит.

### 4.1 Баланс читается с недокументированного эндпоинта (high)

Код: `internal/billing/client.go:92` — `GET billing.api.cloud.ru/v1/agreement/{id}`,
поля `balance`, `active` (`tools_billing.go:231-232`).

Путь к дефекту: `billing_balance` и `finops_summary` (runway, daily burn)
целиком зависят от эндпоинта, которого нет ни в одной опубликованной
спецификации. Cloud.ru вправе изменить или закрыть его без уведомления — и
MCP-инструмент начнёт отвечать ошибкой или нулевым балансом (runway = ∞) в
середине квартала.

Исправление: (1) запросить у Cloud.ru документацию эндпоинта (§5.2); (2) до
ответа — пометить инструмент в описании как «undocumented, best effort» и
при любой не-2xx отдавать `balance: unknown`, а не ошибку; (3) если
документации нет — заменить на `GET organization.api.cloud.ru/v3/agreements`
для `status` (= `active`) и считать runway только по `/v2/consumption`.

### 4.2 Аудит: не тот ключ массива и не тот параметр страницы (critical при соответствии спецификации)

Код: `internal/audit/client.go:91-92, :113, :118` — читает `events`, шлёт
`pageSize`. Спецификация: ответ `{logs: [...], nextPageToken, limit}`, параметр
`limit`.

Путь к дефекту: `audit_events` (единственный инструмент безопасности в
MCP) возвращает `count: 0, events: []` при любом окне — агент заключает «событий
не было». Пагинация `page_size` ничего не делает: сервер отдаёт свой лимит.

Исправление: читать `logs` (оставив `events` как fallback до живой проверки),
слать `limit`; добавить проброс `eventType`/`eventSource`. Сначала — один живой
`GET` с ключами проекта, чтобы подтвердить форму ответа.

Статус (16.10.2026): `logs` читается; `limit` вместо `pageSize` — живое API на
`pageSize` отвечает 415 раньше проверки доступа (реестр AUD-2), с `limit` — 401;
401 evo показывает как «аудит не включён для проекта — обратитесь в
поддержку Cloud.ru» (документация «Получить аудит-логи», реестр AUD-1), 403 —
«у роли ключа нет прав или аудит не включён».

### 4.3 Зона подсетей и групп безопасности читается из несуществующего поля (high)

Код: `internal/provision/types.go:88-104` — `Subnet.AvailabilityZoneID`,
`SecurityGroup.AvailabilityZoneID` (`json:"availability_zone_id"`). Спецификация
(`GET /api/v1/subnets`, `GET /api/v1/security-groups`): поле вложенное —
`availability_zone: {id, name}`. Поэтому «API отдаёт пустой
`availability_zone_id`» (BACKLOG [05], review-context) — не дефект API, а
чтение не того ключа; `engine/network.go:28-34` и `deployplan/resolve.go`
компенсируют это подбором по подстроке имени (`Default_ru.AZ-1`).

Путь к дефекту: пользователь переименовал подсеть (`backend-net`) или группу
(`web`) — имя больше не содержит `ru.AZ-1`, `pickInZone` берёт «единственную»
или не находит ничего: `evo deploy` падает с «no subnet for zone», либо берёт
группу чужой зоны и ВМ не поднимается (review-context: «группа из чужой зоны не
даёт ВМ загрузиться»).

Исправление: добавить `AvailabilityZone *ZoneRef` (как уже сделано для `VM`,
`types.go:20`) и метод `ZoneID()`; в `pickInZone` сначала сравнивать по id,
подбор по имени оставить запасным; либо фильтровать на сервере:
`?availability_zone_ids=<id>` (subnets) и `?availability_zone_id=<id>`
(security-groups) — тогда список короче лимита и пагинация не нужна.

### 4.4 Публичный IP ВМ читается из несуществующих полей (medium)

Код: `types.go:13-17` — `flavor_id, image_id, public_ip, floating_ip` в `VM`.
Спецификация `GET /api/v1/vms` (и ответ `POST /api/v1.1/vms`): флейвор и образ —
вложенные `flavor{id,name,cpu,ram}`, `image{id,name}`; публичный адрес —
`interfaces[].floating_ip.ip_address` (и `interfaces[].ip_address` у
`type=direct_ip`).

Путь к дефекту: `vm_list` / `evo vm list` без публичного IP и флейвора
(BACKLOG [04]); `destroy` по `vm_id` вынужден искать FIP через state и
отдельный список FIP (`tools.go:2006-2010`), и при отсутствии записи state
(ВМ создана не evo) FIP остаётся платным.

Исправление: разобрать `flavor`, `image`, `interfaces` в `VM` (поля уже есть
в `VMDetail`/`VMInterface`), `PublicIP()` — первый `floating_ip.ip_address`
или `ip_address` у `direct_ip`; `flavor_id`/`image_id`/`public_ip`/`floating_ip`
удалить. Заодно `vm_list` получит `flavor.name` без второго запроса.

### 4.5 `limit=200` при максимуме 100 (high — поведение не определено)

Код: `discovery.go:30` (`/vms`, `/subnets`, `/security-groups`, `/floating-ips`,
`/disk-types`), `adapter.go:248, :234` (`/images`). Спецификация: `limit`
integer, `default 50`, `maximum 100`; у `/disk-types` параметра `limit` нет.

Путь к дефекту: два варианта, и спецификация не говорит, какой. Если сервер
валидирует (FastAPI/pydantic обычно отвечает 422 `less_than_equal`) — ломаются
все списки compute, чего в e2e не видно, значит сейчас не так. Если сервер
усекает до 100 или до 50 — проект с >50 подсетями/FIP/образами молча получает
неполный список: `deploy` «не видит» подсеть, `fip_list` — свободный IP,
`image_get` по имени — свой образ. Третий риск: после ужесточения валидации
на стороне Cloud.ru всё падает разом.

Исправление: `limit=100` + обход по `offset` до `total` (как уже сделано для
правил SG, `discovery.go:122-146`) в `listProjectScoped` и `listImages`; у
`/disk-types` `limit` не слать. И один живой запрос с `limit=200`, чтобы
зафиксировать, что отвечает сервер сейчас (§5.3).

### 4.6 Состояние и ОС образа читаются из несуществующих полей (medium)

Код: `types.go:141-146` (`Image.OS`, `Image.Status`), `engine/init.go:176`
(фильтр `deleted/error/creating`), `engine/list_images.go:37` (колонка OS).
Спецификация: у образа нет `os` и `status`; состояние —
`availability_zones[].state`, доступность — `availability_zones[].enabled`,
тип — `type` (`public/private/marketplace`), семейство ОС — только в `name`.

Путь к дефекту: `evo init` предлагает образ, который ещё собирается или
отключён в выбранной зоне (`enabled: false`) → `POST /vms` отвечает 422/500
уже после подтверждения; `evo images` печатает пустую колонку OS.

Исправление: `Image` → `AvailabilityZones []ImageAvailZone` (структура уже есть
для `ImageDetail`) и `ReadyIn(zoneID)`; `deployableImages` фильтровать по
`state == created && enabled` для зоны деплоя; колонку OS убрать или выводить
`type`.

### 4.7 Флейворы: только первые 50 (high)

Код: `adapter.go:173-189` — `GET /api/v1/flavors` без `limit`/`offset`,
кэш на час. Спецификация: `default 50`, `max 100`, `total` в ответе; есть
фильтры `availability_zone_id`, `cpu`, `ram`, `free_tier`, `is_spot`.

Путь к дефекту: `flavorAPIName` (`engine/network.go:124-135`) ищет флейвор по
имени/ID в этом списке — пользователь указал в `evo.yaml` флейвор из консоли,
которого нет в первых 50 (GPU/спот-линейки добавляются позже и по
`created_time desc` стоят… первыми, а старые базовые — последними) → «flavor
not found», хотя консоль его показывает. Автоподбор (`sizer.Match`) выбирает
из неполного меню.

Исправление: пагинация по `offset` до `total` с `limit=100`, либо фильтр
`?name=` для точечного поиска и `?availability_zone_id=` для подбора.

### 4.8 Удаление ВМ: 405 не обрабатывается, 409 не описан (medium)

Код: `engine/destroy.go:317-320` — ждёт удаляемости на 409/422. Спецификация
`DELETE /api/v1/vms/{id}`: 422 «нельзя удалить в текущем статусе» (совпадает),
**405 «у ВМ есть связанные сущности»** (не обрабатывается: `doWithRetry`
вернёт `CodeAPIError`, `destroy` остановится, FIP и правила не удалятся),
409 нигде не описан.

Исправление: 405 → снять связанные сущности (отвязать FIP `PUT /floating-ips/{id}
{detach: true}` или сразу слать `delete_attachments: {external_ips: [fip_id]}`
в теле DELETE — это документированный способ удалить ВМ вместе с IP одним
запросом) и повторить; 409 оставить до ответа Cloud.ru (§5.4).

### 4.9 grpc-gateway сервисы: snake_case там, где контракт camelCase (medium)

Код: `vpc/client.go:124`, `dns/client.go:73-78, :180`, `mk8s/client.go:54-55`
шлют `project_id`, `public_zone_id`; тела DNS — `project_id` (`tools_dns.go:152,
:317`). Спецификации VPC, DNS, mk8s: `projectId`, `publicZoneId` (required).

Почему работает: grpc-gateway v2 принимает для query и protojson для тела
оба имени — proto (`project_id`) и JSON (`projectId`). Это свойство
реализации, не контракт; NLB и PostgreSQL на том же стеке опубликованы в
snake_case, то есть Cloud.ru сами выбирают имена на уровне сервиса и могут
выключить лениентность.

Исправление: перейти на имена из спецификации (`projectId`, `publicZoneId`,
`privateZoneId`); для DNS убрать комментарий о «доказанном» snake_case.
Дешёвая и безопасная правка — camelCase точно принимается.

### 4.10 DNS: `resource_id` вместо `resourceId`, `valid`/`delegated` нет (medium)

Код: `dns/client.go:51` (`json:"resource_id"`), `tools_dns.go:104`.
Спецификация: `Operation.resourceId`; у зоны `state`, `confirmState`,
`verificationToken` (поля `valid`, `delegated` отсутствуют).

Путь к дефекту: `dns_zone_create` не может вернуть id зоны (подсказка в ответе
«then dns_zones for the id» — симптом); `dns_zones` всегда печатает
`valid: null, delegated: null` — агент не узнаёт, делегирована ли публичная
зона, и выпуск Let's Encrypt падает с `ERROR_CODE_NOT_DELEGATED` позже.

Исправление: `ResourceID json:"resourceId"`; в `dns_zones` отдавать `state`,
`confirm_state`, `verification_token`.

### 4.11 Redis: deprecated-поля в create (medium)

Код: `tools_redis.go:184-192` — `storage_gb`, `subnet_id`. Спецификация:
оба `deprecated`, актуальные `storage_gib` и `subnet_ids[]`.

Путь к дефекту: deprecated-поле удаляют → `redis_create` получает 400 на
ровном месте. `subnet_id` оставлен сознательно (бэкенд падает на
`subnet_ids[]`, комментарий в коде), но `storage_gb` → `storage_gib` ничем не
обусловлен.

Исправление: `storage_gib` сейчас; `subnet_ids` — после ответа Cloud.ru (§5.8).

### 4.12 IAM: snake_case ответа токена и `service_accounts` (high — проверить вживую)

Код: `auth/iam.go:204-205` (`access_token`, `expires_in`),
`tools_admin.go:72` (`service_accounts`). Спецификация: `accessToken`,
`expiresIn`; `serviceAccounts`.

Путь к дефекту: (а) если живой ответ токена содержит только `accessToken` —
код бы не работал, а он работает (e2e), значит есть `access_token`; но
если нет `expires_in`, `ExpiresIn=0`, `ExpiresAt=now`, и каждый вызов API
начинается с нового обмена ключа на токен (лишний round-trip, лишние записи в
аудите IAM, выше шанс 429). Это проверяется одним запросом. (б)
`iam_service_accounts` — пустой список, если gateway отдаёт `serviceAccounts`;
тогда `k8s_create` не может найти `cluster_sa_id`.

Исправление: читать оба написания (`access_token`/`accessToken`,
`expires_in`/`expiresIn`, `service_accounts`/`serviceAccounts`) и логировать
в `--verbose`, какое пришло; при `expires_in == 0` ставить консервативный TTL
(например 50 минут из review-context «около часа»), а не «истёк».

### 4.13 Мелкое (low), одним PR

- `adapter.go:153` `ListVMs` без `project_id` — удалить (мёртвый код; по
  спецификации всегда 422).
- `image.go:59` `FieldType omitempty` → поле required с enum; при пустом —
  валидировать до запроса.
- `tools_snat.go:63` — читать `availability_zone.id`/`.name`.
- `nodepool.go:79-81` — `clusterId` обязателен: требовать аргумент.
- `monitoring/client.go:167-170` — `match` required: при пустом слать
  `{__name__=~".+"}` или возвращать usage-ошибку.
- `provision/types.go:16` комментарий и `deploy.go:625, :787, :856` — `active`
  нет в enum ни у ВМ, ни у FIP; оставить только `running` / `in_use`.
- `tools_billing.go:104-109` — fallback-имена `cost_nds`, `servicename`,
  `service`, `resourcename` в спецификации отсутствуют (есть и `amount`, и
  `cost`) — убрать, чтобы не маскировать смену схемы.
- Лишний `?project_id` у compute-операций без такого параметра
  (`discovery.go:75, :92, :183, :207`, `disk.go:59, :86, :107, :125`,
  `image.go:107`, `nat.go:71, :87`) — убрать.

## 5. Живое API расходится со спецификацией — вопросы к командам API Cloud.ru

Каждый пункт: что видит evo, что говорит спецификация, что спросить.
Помечено, откуда наблюдение (review-context / BACKLOG / комментарий в коде).

### Compute (команда Evolution Compute)

1. **`interfaces[].interface_security_enabled` при создании ВМ.** Живой API
   требует поле (review-context; без него 500), в `POST /api/v1.1/vms` его
   нет, а у элемента `interfaces` стоит `additionalProperties: false`.
   Вопрос: поле поддерживается и будет ли добавлено в спецификацию, или
   `security_groups` подразумевает `true` и поле можно не слать?
2. **`new_external_ip: true` даёт 500 `ExternalNetNotFound`** (review-context),
   хотя описано как штатный boolean. Вопрос: это дефект или требует
   предусловия (сеть с внешним доступом), и какое?
3. **`limit` > 100 в списках.** Что делает сервер при `limit=200`: 422, усечение
   до 100 или до 50? (Нужно для §4.5; evo сейчас так шлёт.)
4. **`DELETE /api/v1/vms/{id}` отвечает 409 для ВМ в `creating`** (cloud-e2e
   15.10.2026), в спецификации — 422 «нельзя удалить в текущем статусе» и 405
   «есть связанные сущности». Вопрос: когда 409, когда 422, и удаляет ли
   `delete_attachments.external_ips` привязанный FIP атомарно?
5. **`image_metadata`: `vmm_enabled`, `guest_agent_ready` — только для чтения**
   (их отправка → 500, review-context); в спецификации `image_metadata` —
   произвольный объект без перечня ключей. Вопрос: где список обязательных /
   запрещённых ключей для образов каталога (`linux_password` обязателен —
   иначе ВМ не выходит из `creating`, `engine/deploy.go:392`)?
6. **`GET /api/v1/subnets` / `security-groups`: правильно ли, что зона только
   вложенная (`availability_zone.id`)?** Если да — это наша правка (§4.3), и
   вопрос снимается; если флаг `availability_zone_id` планировался — когда.
7. **NAT: «ровно одно из `availability_zone_id`/`availability_zone_name`»**
   (живой валидатор) — в спецификации оба optional без `oneOf`. Просьба
   отразить.
8. **Состояние FIP `active`** (review-context) — в enum только `in_use`.
   Встречается ли `active` в ответах сейчас?
9. **5xx под параллельными GET** (`adapter.go:494-481`) — есть ли
   задокументированные лимиты RPS / `Retry-After` у compute?

### Organization / IAM

10. **`GET /v3/projects` для сервисного аккаунта → 400 «(user_id) is invalid
    argument: user not found»** (BACKLOG [02], 14.10.2026). В спецификации
    400 не описан. Вопрос: какой эндпоинт отдаёт проект(ы), к которым привязан
    SA (`/api/v2/serviceAccounts/{id}` содержит `target.projectId` — это
    официальный путь?).
11. **`POST /api/v1/auth/token`: ответ snake_case (`access_token`) против
    `accessToken` в спецификации.** Какое написание гарантировано, приходит ли
    `expires_in`/`expiresIn`, и будет ли gateway когда-либо переключён?
12. **`GET /api/v2/serviceAccounts`: `serviceAccounts` или `service_accounts`?**
    (Тот же вопрос про режим gateway.)

### Billing

13. **`GET billing.api.cloud.ru/v1/agreement/{id}` (баланс, `active`)** не
    описан ни в «Контроль затрат», ни в Organization v1/v2/v3. Вопрос: это
    публичный контракт? Где документация и гарантии совместимости?
14. **`/v2/consumption`: `amount` vs `cost`** — в спецификации есть оба. Что
    считать стоимостью к оплате (с НДС — `amount_nds`?), зависит ли от типа
    договора (prepaid/postpaid)?

### DNS / VPC / mk8s (grpc-gateway)

15. **snake_case в query и теле принимается** (`project_id`, `public_zone_id`)
    при camelCase в спецификации. Это гарантия или побочный эффект grpc-gateway?
    (NLB и PostgreSQL опубликованы в snake_case — хотелось бы единой политики.)
16. **mk8s `POST /v3/clusters` без `networkConfiguration.networkPlugin`
    создаёт кластер** (комментарий в `mk8s/client.go`), в спецификации поле
    required. Какой плагин ставится по умолчанию и можно ли на это опираться?
17. **mk8s `:getKubeConfig` без `clusterId` в теле** работает; спецификация
    требует. Просьба уточнить (или убрать required).

### Managed DB (PostgreSQL / Kafka / Redis)

18. **Ошибки 4xx с пустым телом и причиной в заголовке `grpc-message`**
    (`postgresql/client.go:75-82`, `dbaas/client.go:84-90`). Спецификация
    описывает `rpcStatus` в теле. Это ожидаемое поведение gateway?
19. **Redis `POST /v1/clusters` с `subnet_ids[]` роняет бэкенд (деление на
    ноль), работает только deprecated `subnet_id`** (`tools_redis.go:187-190`).
    Когда починят `subnet_ids`, и будет ли удалён `subnet_id`?
20. **Redis `type: STANDALONE` отвергается**, в enum есть. `MASTER_REPLICA`
    требует ровно 3 реплики на шард (review-context) — ограничения в
    спецификации не отражены.
21. **Kafka/Redis `GET /v1/specifications` требует `availability_zone_id`**
    (комментарий `dbaas/client.go:207-209`), в спецификации optional.
22. **Redis `GET /v1/clusters` отвергает `page_size<=0`** — задокументировать
    диапазон и значение по умолчанию (для всех dbaas-списков).

### Monitoring / Audit

23. **`export_metrics`: единицы `start`/`end`** (секунды или миллисекунды?)
    и максимальный интервал (в коде — 7 дней, из опыта). В спецификации нет.
24. **`export_metrics`: формат ответа** — описан как `string`; фактически
    JSON lines / массив? Нужна схема `{metric, values, timestamps}`.
25. **Audit `GET /events/list/{projectId}`: `logs` или `events`, `limit` или
    `pageSize`?** evo читает `events` — если по спецификации, инструмент
    никогда не показывал события (§4.2).

### Что уже сошлось (вопросов нет)

- NLB `GET /v2/operations/{id}` требует `?id=` — задокументировано.
- Certificate Manager: единственное число `/v1/certificate`, `:verb`-суффиксы —
  по swagger.
- Kafka create, NLB create, PG create, Secret Manager, Artifact Registry —
  тела и обязательные поля совпадают.
- `GET /api/v1/vms` без `project_id` пуст/422 — `project_id` required.

## 6. Проверка контракта в CI

Цель: узнать об изменении спецификации или о запросе, который ей противоречит,
до того, как это увидит пользователь — без ключей облака и без создания
ресурсов.

### Что проверять автоматически

Прототип в этом PR: `scripts/api-contract-check.py` + инвентаризация
`scripts/api-contract.json` (133 запроса; поля: метод, путь, query, поля тела,
читаемые поля ответа, enum-значения и статусы, на которые опирается код,
`accepted` — осознанные отклонения с причиной). Проверки:

| проверка | уровень | что ловит |
|---|---|---|
| путь и метод есть в спецификации | error | удалённый/переименованный эндпоинт |
| required query/body поля передаются | error | новое обязательное поле |
| передаваемые поля описаны; при `additionalProperties:false` | error / warn | переименование поля, строгий валидатор |
| числовые query ≤ `maximum`, enum-значения | error | `limit=200` (§4.5) |
| читаемые поля ответа существуют в 2xx-схеме | warn | `events`→`logs`, `resource_id`→`resourceId` (§4.2, §4.10) |
| enum-значения, на которые опирается код | warn | исчезнувший статус (`active`) |
| статусы, которые код обрабатывает, описаны | info | 409 (§4.8) |
| список постраничный, а код не пагинирует | info | §4.7 |
| sha256 спецификации изменился с прошлого прогона | info → ручной просмотр | любое изменение API |

Сейчас прогон даёт 5 error (все — `limit=200`, §4.5; `accepted` для них не
ставится намеренно, пока код не исправлен), 45 warn, 36 accepted. После
правок §4 ожидается 0 error; остальные warn либо уходят с кодом, либо
переводятся в `accepted` с причиной — так файл контракта становится и
документацией «почему мы отступаем от спецификации».

Чего прототип не делает (и не должен): не выполняет запросов, не проверяет
вложенные required глубже одного уровня, не сравнивает типы значений, не
понимает `oneOf` в телах кроме перебора вариантов.

### Как запускать

Локально скрипту нужен PyYAML (`python3 -m pip install pyyaml`; без него
`scripts/api-contract-check.py` выходит с кодом 2 и сообщением «PyYAML is
required»), CI ставит его сам:

```sh
python3 -m pip install pyyaml
python3 scripts/api-contract-check.py --code-only      # опись против кода, без сети
python3 scripts/cloudru-specs.py .cache/cloudru-specs  # скачать спецификации
python3 scripts/api-contract-check.py --specs .cache/cloudru-specs
```

Workflow `api-contract.yml`:

- `schedule` (раз в неделю) и `workflow_dispatch` — строго, со свежими
  спецификациями; PR и push в `main` по путям `scripts/api-contract*`,
  `scripts/cloudru-specs*.py`, `internal/**/*.go` — со спецификациями из кэша
  на сутки (кэш `main` читают все PR) и мягко: не скачались — сверка
  пропущена с предупреждением и строкой в summary (с 23.10.2026).
- Шаги: `pip install pyyaml` → `python3 scripts/cloudru-specs.py
  .cache/cloudru-specs` (с повтором: сайт docs рвёт TLS, см. §1) →
  `python3 scripts/api-contract-check.py --markdown >> $GITHUB_STEP_SUMMARY` →
  сравнить `manifest.json` с сохранённым в `actions/cache` (ключ по дате
  недели) и при смене sha256 добавить в summary diff путей; упасть только на
  неacceptеd `error`.
- Еженедельный прогон при падении открывает issue с меткой `api-contract`
  (по шаблону: сервис, запрос, что изменилось, ссылка на страницу
  справочника); повторное падение комментирует существующую.
- Спецификации в репозиторий не коммитятся (материалы Cloud.ru); `manifest.json`
  с хэшами можно хранить в `actions/cache` или как artifact.

Второй уровень (опционально, вместе с `cloud-e2e`): read-only «smoke» по
живому API с ключами тестового проекта — по одному GET на каждый список
(`/vms`, `/subnets`, `/flavors` с `limit=200`, `/events/list`, `serviceAccounts`,
`/v1/agreement`), сверка формы ответа с тем, что ожидает контракт (имена
ключей верхнего уровня). Закрывает именно те вопросы, что остались «нет» в
графе «вживую»: §4.2, §4.5, §4.12. Ничего не создаёт и не удаляет; нужен
секрет уже существующего e2e-проекта.

### Сколько стоит

- Разработка: прототип готов; довести до workflow — 0,5 дня (yaml, кэш
  манифеста, issue при падении) + 1 день на первичную чистку warn (большая
  часть — правки §4, которые нужны и так).
- Эксплуатация: один job ubuntu ~2–3 минуты в неделю (скачивание 20 файлов с
  cloud.ru занимает основное время; сам чек < 2 с) — в пределах бесплатных
  минут GitHub. Live-smoke — +1 минута в прогоне cloud-e2e, нулевая стоимость
  ресурсов (только GET).
- Сопровождение: каждый новый запрос в коде — строка в
  `scripts/api-contract.json` (ревью проверяет это так же, как
  `cloudru-api.md`); false positives гасятся через `accepted` с причиной.
  Риск: сайт docs меняет URL спецификаций — тогда `cloudru-specs.py` отдаёт
  `missing`, и job должен падать явно, а не «всё ок, 0 проверок».

## 7. Живая проверка 15.10.2026 (`api-probe`)

Workflow `.github/workflows/api-probe.yml` (только GET, в логе — коды HTTP,
имена полей и количества), тестовый сервисный аккаунт уровня проекта,
прогон 15.10.2026 20:23 UTC.

| Вопрос (§) | Ответ живого API | Итог |
|---|---|---|
| Токен IAM (§4.12, §5.11) | `access_token`, `expires_in` (число), а также `id_token`, `refresh_token`, `not_before`, `scopes`, `token_type` | расходится со спецификацией (`accessToken`): верен код, читаются оба написания |
| `limit=200` (§4.5, §5.3) | 200, сервер отдаёт больше 100 (`/images` — 86 из 86) | не ошибка сейчас, но вне спецификации; списки переведены на 100 со страницами |
| Флейворы (§4.7) | без `limit` — 50 из 78; с `limit=100` — 78 | **подтверждено**: флейворы за первой страницей были не видны; исправлено пагинацией |
| `/disk-types` | голый массив, не `{items}` | код и так принимает оба вида |
| ВМ (§4.4) | `flavor{…}`, `image_id`, `availability_zone{…}`, `interfaces[].floating_ip{id,ip_address,name,state}`; плоских `public_ip`/`floating_ip`/`flavor_id` нет | подтверждено; исправлено чтением вложенных полей |
| Подсети, группы безопасности (§4.3, §5.6) | только `availability_zone{id,name,enabled}` | подтверждено; исправлено |
| Образы (§4.6) | `availability_zones[]{availability_zone_id,availability_zone_name,state,enabled,size,default}`, `type`; верхних `status`/`os` нет | подтверждено; пригодность — по зонам |
| Состояния | ВМ — `running`, FIP — `in_use` (`active` не встретился) | совпадает со спецификацией |
| VPC, DNS (§4.9, §5.15) | `projectId` и `project_id` — одинаковый ответ | оба работают; оставлено как есть |
| `serviceAccounts` (§5.12) | `service_accounts`, `next_page_token` | расходится со спецификацией: верен код, читаются оба |
| Аудит (§4.2, §5.25) | `limit` → 401, `pageSize` → 415 | не проверено: у тестового ключа нет прав на аудит; код читает и `logs`, и `events` |
| `/v3/projects` (§5.10) | 400 | как и раньше для сервисного аккаунта проекта |
