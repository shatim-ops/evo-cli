Код: c53b949e8d5af3e03a1d15ed464ce4a0cfa83800 (`main` с исправлениями D1–D7; ветка прогонов `claude/live-runs-2026-10-18` = он же + сценарии и документы)

# Живые прогоны изменяющих инструментов перед догфудом (18.10.2026)

Зачем: v1.0.4 уходит догфудерам только проверенной на живом облаке. До
18.10 вживую были проверены чтения (job `readonly`) и один сценарий
изменений (`e2e`: deploy compose → destroy). Здесь — все изменяющие
инструменты реестра (`evo tools list --json`, `mutating: true`, 65 из 152).

Как: `.github/workflows/cloud-e2e.yml`, job `live` (по job на область,
входы `disk_smoke`, `net_smoke`, `dns_smoke`, `s3_smoke`, `secrets_smoke`,
`ca_smoke`, `stack_smoke`, `mcp_smoke`; сценарии — `.github/scripts/cloud-live.sh`),
дополненные `k8s-smoke` (`k8s_apply`, `k8s_helm`), `dbaas-pg`
(`pg_backup_create`, `pg_db_delete`, `pg_user_delete`) и `cleanup` (квоты,
уборка всех видов `evo-e2e-*`). Зона ru.AZ-2, flavor `lowcost10-2-4`, образ
`ubuntu-22.04`, тестовый проект и ключ сервисного аккаунта уровня проекта.
Ресурсы `evo-e2e-<область>-<run>`, удаление в том же job и проверка
остатков; логи каждого прогона проверены на ID, IP и ключи (раздел
«Логи»).

Итог: `ok` — сценарий прошёл; `дефект evo` — исправлено в evo (ID ниже);
`платформа` — поведение Cloud.ru (реестр `cloudru-discrepancies.md`);
`тест` — ошибка сценария, исправлена и повторена; `не покрыто` — с причиной.

## Сводка

- Изменяющих инструментов: **65**. Прошли вживую: **63** (из них
  `cert_issue_le` и `cert_renew` — только путь отказа: в тестовом проекте нет
  делегированной публичной зоны, CRT-3). **Не покрыто: 2** — `image_create`,
  `image_delete`: квота организации на собственные образы исчерпана (CMP-16).
- Дефекты evo: **7** (D1–D7), все исправлены и влиты, каждый с тестом на
  фейке API; D1–D6 подтверждены исправленными вживую, D7 — прогоном
  k8s-smoke без повторения сбоя.
- Штатный `e2e` (deploy compose) за ночь, 10 прогонов с 18:00 до 23:50 UTC:
  **5 ok**, **2** — apt-зеркало образа (`Failed to fetch https://repo.mng.sbercloud…`,
  OPS-2; 19:57 и 20:50 UTC), **3** — SSH к новой ВМ не поднялся за 60
  попыток (OPS-1b; 22:05, 22:30, 23:05 UTC). Сбои перемежаются с удачными;
  в те же часы ВМ областей `mcp` (deploy по MCP), `disk` и `os_smoke` SSH и
  Docker получали. evo в каждом случае назвал причину и удалил за собой всё
  (проверка сирот — зелёная). Для догфуда: каждый второй `deploy` в ru.AZ-2
  этой ночью требовал повтора по причине платформы.
- Реестр расхождений: новые AR-4, CRT-4, NET-11, NET-12, NET-13; подтверждены
  CRT-3, CMP-15, CMP-16; наблюдения OPS-1b, OPS-2c, OPS-4.
- Остатки: **нет** (финальный `cleanup_only`, run 39178722237: «остатков
  evo-e2e-* нет»). Приватная DNS-зона из прогона 1a висела в «Удаляется»
  ~6 ч и ушла после очередного `DELETE` в финальной уборке (NET-13).

## Прогоны

| Прогон | run id | Что | Итог | Остатки |
|---|---|---|---|---|
| 0 | 39150480619 | `cleanup_only`: остатки до старта, квоты | ok; `org_quotas` — `unavailable` (у ключа нет области организации) | нет |
| 1a | 39152048559 | disk, net, dns, s3, secrets, ca + e2e | D1 (AR); s3 — ключи окружения; e2e — ok | DNS-зона `evo-e2e-dns-…` (удаление без `type=private` → 403; дальше — NET-13) |
| cleanup | 39158678958 | `cleanup_only` | зона в «Удаляется», `DELETE` принят 25 раз | та же зона |
| 1b | 39160570296 | stack, mcp, os_smoke, ca, s3, dns, secrets + e2e | s3, os, mcp stdio — ok; D4; e2e — OPS-2c | нет |
| cleanup | 39163067817 | `cleanup_only` | та же зона (NET-13) | та же зона |
| 1c | 39164767739 | disk, net, ca, dns + e2e | net, dns — ok; D5; e2e — OPS-2 | нет |
| 1d | 39167011818 | disk, stack, mcp, ca + e2e | ca, disk — ok; image — квота; D6; логи удалены (ID ВМ в выводе сценария) | нет |
| 1e | 39169320594 | stack, mcp + e2e | stack, mcp stdio, e2e — ok | нет |
| 2 | 39170448486 | k8s_smoke, mcp + e2e | k8s до apply — ok, helm — OIDC 502 (OPS-4 → D7); e2e — OPS-1b | нет |
| 2b | 39172091812 | k8s_smoke, mcp, stack + e2e | stack, mcp (stdio и HTTP) — ok; k8s — снова OIDC 502; e2e — OPS-1b | нет |
| 2c | 39173624385 | k8s_smoke + e2e (код с D7) | ok целиком | нет |
| 3 | 39175190124 | dbaas_smoke + e2e | PG, Kafka, Redis — ok; e2e — OPS-1b | нет |
| 4 | 39178022110 | secrets_smoke + e2e (код с D4) | ok | нет |
| cleanup | 39178722237 | финальный `cleanup_only` | ok: зона 1a удалена, остатков нет | нет |

## Результаты по областям (итог по последнему прогону области)

| Область | Сценарий | Результат | run id | Заметка |
|---|---|---|---|---|
| deploy (e2e) | init → deploy compose → HTTP → logs -f → destroy → сироты | ok | 39173624385 | падения 1b–2b, 3 — apt-зеркало образа (OPS-2) и SSH (OPS-1b), evo дал верный диагноз |
| ВМ и диски | provision → disk_create → disk_attach (с #117) → disk_resize подключённого (`in_use`, 10 → 20 ГБ) → disk_detach → disk_delete; выключение ВМ изнутри, отключение загрузочного диска → destroy | ok | 39167011818 | в 1a resize упал в переходном состоянии сразу после attach; в 1c attach — D5 |
| образы | image_create из загрузочного диска → image_delete | не покрыто: квота | 39167011818 | 422 `organization_quota_exceeded` (CMP-16) |
| сеть | vpc_create/get/delete; provision публичная и `no_public_ip`; destroy `release_ip=false` (FIP свободен); tg_create → lb_create (RUNNING, публичный IP) → lb_delete → tg_delete; fip_attach свободного FIP к ВМ без адреса; open_ports (+ повтор — `already_open`); vpc_route_create/delete; snat_create/delete; destroy освобождает перепривязанный FIP | ok, 14 изменяющих | 39164767739 | маршрут в 198.18.255.0/24 — 400 (NET-12), в 10.255.255.0/24 — ok; SNAT сразу после создания не удаляется (NET-11) |
| FIP | release_floating_ip занятого (отказ) и свободного | ok | 39172091812 | |
| DNS | приватная зона → zone_get (и без `type`, #111) → запись A → records/get → delete записи → delete зоны | ok | 39164767739 | операции приватной зоны не завершаются, удаление — от секунд до часов (NET-13) |
| S3 | bucket_create → put → ls → get (sha256) → bucket_delete непустого (отказ) → rm → bucket_delete | ok | 39160570296 | в 1a — неверные ключи S3 окружения, владелец заменил |
| секреты | secret_create → value (sha256) → add_version → value v2 → get → list → delete → value удалённого (NOT_FOUND) | ok | 39178022110 | значения в лог не попадают, сравнение по sha256 |
| сертификаты | cert_import → get → versions → usage → cert_private_key (ключ — пара сертификату) → cert_delete; cert_renew и cert_issue_le — отказ без делегированной зоны | ok | 39178022110 | D4 (`private_key` в Base64, 1b) подтверждён исправленным |
| AR / CA | ar_create → docker push ключом SA → ca_deploy → HTTP 200 по `publicUri` → ca_logs → ca_stop → ca_start (снова 200) → ca_delete → ar_delete | ok | 39167011818 | D1 исправлен вживую; `ca_logs` — 0 строк после обслуженных запросов (под вопросом, не дефект без доказательства) |
| стек | stack_plan → stack_apply (secret, private VM, TG, NLB) → stack_status → повтор apply без дублей → stack_destroy → повторный stack_destroy → stack_status destroyed | ok | 39172091812 | D6 подтверждён исправленным |
| MCP stdio | deploy_plan → deploy_apply без confirmed (отказ, ВМ нет) → deploy_apply → deploy (та же ВМ) → status → logs → destroy без confirmed (превью называет ВМ) → destroy vm_id | ok | 39172091812 | |
| MCP HTTP | provision без confirmed (отказ, ВМ нет) → provision → destroy name | ok | 39172091812 | в 1b–2 «isError ?» — ошибка разбора в сценарии (`false // "?"`) |
| ОС | os_smoke: Astra, РЕД ОС — отказ до создания (код 4); nginx-ubuntu — ВМ, SSH, порт | ok | 39160570296 | |
| k8s | k8s_create → add-nodepool → kubectl → k8s_apply → k8s_kubectl delete → k8s_helm install/list/uninstall → k8s_add_addon cert-manager (поды Running) → k8s_delete | ok, 7 изменяющих | 39173624385 | в 2 и 2b — OIDC 502 (OPS-4), исправлено повтором D7 |
| PostgreSQL | pg_create → второй pg_create с тем же именем (отказ) → READY → user_create, db_create → backups → operation → превью pg_delete → backup_create → db_delete → user_delete → pg_delete | ok, 7 изменяющих | 39175190124 | |
| Kafka | kafka_create (1 брокер) → повтор имени (отказ) → READY → kafka_get → превью → kafka_delete | ok | 39175190124 | |
| Redis | redis_create (MASTER_REPLICA, 1 шард) → повтор имени (отказ) → READY → redis_get → превью → redis_delete | ok | 39175190124 | |

## Дефекты evo

| ID | Серьёзность | Область | Суть | PR |
|---|---|---|---|---|
| D1 | высокая | Artifact Registry | `ar_list` не завершался (1000 страниц за ~10 мин), `ar_create` отказывал до создания: API без `pageSize` отдаёт пустые страницы с новым токеном (AR-4); на `main` было видно и в readonly (код 124, warn) | #110 |
| D2 | средняя | DNS | без `type` инструменты по зоне и записи шли в API публичных зон: приватная зона — 403, `dns_zone_delete` её не удалял, шаг `dns` стека с приватной зоной не работал | #111 |
| D3 | низкая | DNS | `dns_zones` не показывал состояние зоны (`valid`/`delegated` в API нет): удаляемая зона выглядела живой | #114 |
| D4 | средняя | сертификаты | `cert_private_key` отдавал Base64 вместо обещанного PEM и без `key_format`: живой API называет поле `private_key` (CRT-4) | #115 |
| D5 | средняя | диски | `disk_attach` сразу после `disk_create` — 422 «no openstack_id» без ожидания (ждал только `disk_create attach_to`), хотя протокол догфуда обещает ожидание | #117 |
| D6 | средняя | стек | повторный `stack_destroy`, как советует его ответ, делал удалённый стек `destroy_failed`: `VM_NOT_FOUND` не считался «уже удалено» | #118 |
| D7 | низкая | Kubernetes | обмен токена OIDC (`POST /auth/system/openid/token`) не повторялся на 5xx: шлюз отвечал 502, и `k8s_helm`/`k8s_kubectl` падали | #119 |

Исправления сценария и CI (не продукт): маскирование в `cloud-live.sh`
(маски не внутри `$(…)`, весь вывод итогов через `redact.sed`), вывод
`vm create`/`vm delete` job `os-smoke` через `redact.sed` (#116), job
`readonly` только с `main` (окружение `cloud-readonly`).

## Не покрыто и почему

- `image_create`, `image_delete` — 422 `organization_quota_exceeded`: квота
  организации на собственные образы исчерпана (CMP-16). Сценарий готов
  (`disk_smoke`: образ из отключённого загрузочного диска), пройдёт при
  свободной квоте.
- `cert_issue_le`, `cert_renew` — проверен только отказ: выпуск Let's
  Encrypt требует делегированной публичной зоны DNS, которой у тестового
  проекта нет (CRT-3).
- `snat_create` — покрыт, потому что в ru.AZ-2 шлюза не было; шлюз — один на
  зону, чужие не трогаются.

## Логи

Репозиторий публичный: логи всех прогонов проверены поиском UUID, IPv4,
32-значных hex-ID, хостов `*.cr.cloud.ru` и `*.mk8s.*`. Найдены и удалены
(логи прогонов стёрты): ID floating IP в шаге `vm delete` job `os-smoke`
(run 39160570296 и шесть прошлых прогонов `os-smoke` на `main`) и ID ВМ в
промежуточном выводе `stack_destroy` сценария (run 39167011818). В
остальных логах — только служебные ID раннера и публичные ID каталога
(зона ru.AZ-2, публичные образы, flavor), как и в прогонах `e2e` на `main`.
