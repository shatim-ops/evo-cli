Код: dfb8e78 (уточняется по итогам; полный SHA — в конце работ)

# Живые прогоны изменяющих инструментов перед догфудом (18.10.2026)

Зачем: v1.0.4 уходит догфудерам только проверенной на живом облаке. До
18.10 вживую были проверены чтения (job `readonly`) и один сценарий
изменений (`e2e`: deploy compose → destroy). Здесь — все изменяющие
инструменты реестра (`evo tools list --json`, `mutating: true`, 65 из 152).

Как: `.github/workflows/cloud-e2e.yml`, job `live` (по job на область,
входы `disk_smoke`, `net_smoke`, `dns_smoke`, `s3_smoke`, `secrets_smoke`,
`ca_smoke`, `stack_smoke`, `mcp_smoke`; сценарии — `.github/scripts/cloud-live.sh`),
дополненные `k8s-smoke`, `dbaas-pg` и `cleanup`. Зона ru.AZ-2, flavor
`lowcost10-2-4`, образ `ubuntu-22.04`, тестовый проект и ключ сервисного
аккаунта уровня проекта. Ресурсы `evo-e2e-<область>-<run>`, удаление в том
же job и проверка остатков; после каждого прогона — `cleanup_only`.

Итог: `ok` — сценарий прошёл; `дефект evo` — исправляется в evo (ID ниже);
`платформа` — поведение Cloud.ru (реестр `cloudru-discrepancies.md`);
`тест` — ошибка сценария, исправлена и повторена; `не покрыто` — с причиной.

## Прогоны

| Прогон | run id | Что | Итог | Остатки |
|---|---|---|---|---|
| 0 | 39150480619 | `cleanup_only`: остатки до старта, квоты | ok; квоты `org_quotas` — `unavailable` (у ключа нет области организации) | нет |
| 1a | 39152048559 | disk, net, dns, s3, secrets, ca + штатные e2e | см. таблицу; e2e — ok | DNS-зона `evo-e2e-dns-…` (403 при удалении без `type=private` — тест) |
| cleanup | 39158678958 | `cleanup_only` | остаток удалить не удалось | DNS-зона `evo-e2e-dns-…`: `DELETE` принят 25 раз, зона в списке; в консоли — «Удаляется» |
| 1b | 39160570296 | stack, mcp, os_smoke, ca, s3, dns, secrets + e2e | см. таблицу; e2e — платформа (OPS-2c) | нет (по именам) |
| cleanup | 39163067817 | `cleanup_only` | та же зона DNS 1a (NET-13) | зона 1a в «Удаляется» |
| 1c | 39164767739 | disk, net, ca, dns + e2e | net, dns — ok; disk — D5; ca — тест (адрес); e2e — платформа (OPS-2) | нет |
| 1d | 39167011818 | disk, stack, mcp, ca + e2e | ca, disk (кроме образа) — ok; stack — D6; логи удалены (ID ВМ в выводе скрипта) | нет |
| 1e | 39169320594 | stack, mcp + e2e | stack, mcp stdio, e2e — ok; mcp HTTP — тест (разбор isError) | нет |
| 2 | 39170448486 | k8s_smoke, mcp + e2e | k8s: create/nodepool/kubectl/apply — ok, helm — платформа (OIDC 502, OPS-4); e2e — платформа (OPS-1b) | нет |

## Результаты по областям

| Область | Сценарий | Результат | run id | Заметка |
|---|---|---|---|---|
| deploy (e2e) | init → deploy → HTTP → logs -f → destroy → сироты | ok | 39152048559 | |
| ВМ | `provision` (публичная, `no_public_ip`) | ok | 39152048559 | |
| ВМ | `destroy` по имени, `release_ip=false` | ok | 39152048559 | FIP остался свободным |
| диски | `disk_create`, `disk_attach` | ok | 39152048559 | |
| диски | `disk_resize` сразу после attach | вопрос | 39152048559 | 422 `disk_can_not_be_changed_from_current_state`; повтор с ожиданием состояния |
| VPC | `vpc_create` → `vpc_get` → `vpc_delete` | ok | 39152048559 | операции done за секунды |
| NLB | `tg_create` (ВМ без публичного IP) → `lb_create` → `lb_delete` → `tg_delete` | ok | 39152048559 | `NLB_STATUS_RUNNING`, публичный IP есть |
| FIP | `fip_attach` свободного адреса к ВМ без адреса | ok | 39152048559 | destroy этой ВМ освободил адрес |
| SG | `open_ports` | тест | 39152048559 | порты в ответе строками (`["18085"]`); правило удалено уборкой |
| VPC | `vpc_route_create` | платформа? | 39152048559 | 400 `InvalidArgument` без поля; тело совпадает со спецификацией; повтор |
| SNAT | `snat_create` | ok | 39152048559 | в ru.AZ-2 шлюза не было |
| SNAT | `snat_delete` сразу после создания | платформа | 39152048559 | 422 `nat_can_not_be_deleted_from_current_state`; уборка удалила позже |
| DNS | приватная зона | тест | 39152048559 | вызовы без `type=private`; операция создания не done за 10 мин, зона в списке |
| S3 | бакет | не покрыто | 39152048559 | ключи `CLOUDRU_S3_*` окружения cloud-e2e не приняты («Invalid or unknown access key id»); владелец заменил |
| секреты | create → add_version → value (sha256) → get → delete | ok | 39152048559 | значение в ответах не возвращается |
| сертификаты | `cert_import` → get → versions → usage → `cert_delete` | ok | 39152048559 | |
| сертификаты | `cert_private_key` | вопрос | 39152048559 | ключ не совпал по sha256 PEM; повтор со сверкой по паре |
| сертификаты | `cert_renew` (импортированный), `cert_issue_le` | платформа (ожидаемо) | 39152048559 | 400 «no delegated zones» (CRT-3) |
| AR / CA | `ar_create` | дефект evo D1 | 39152048559 | `ar_list` — «more than 1000 pages» через ~10 мин; исправлено #110 |
| AR / CA | `ar_create` → docker push → `ca_deploy` → `ca_logs` → `ca_stop` → `ca_start` → `ca_delete` → `ar_delete` | ok, кроме адреса | 39160570296 | D1 исправлен вживую; push ключом SA работает; `ca_get` за 15 мин не дал адреса `https://` — повтор с поиском имени хоста |
| S3 | bucket_create → put → ls → get (sha256) → bucket_delete непустого (отказ) → rm → bucket_delete | ok | 39160570296 | новые ключи и транспорт #113 |
| сертификаты | `cert_private_key` | дефект evo D4 | 39160570296 | живой ответ `private_key`, а не `privateKey` (CRT-4): ключ в Base64; исправлено #115 |
| MCP stdio | deploy_plan → deploy_apply (без и с confirmed) → deploy → status → logs → destroy vm_id (превью и удаление) | ok | 39160570296 | та же ВМ, второй нет; правило порта закрыто |
| MCP HTTP | provision без confirmed (отказ, ВМ нет) → provision → destroy name | тест | 39160570296 | ВМ создана и удалена; разбор потока SSE исправлен |
| FIP | `release_floating_ip` занятого (отказ) и свободного | ok | 39160570296 | |
| стек | stack_plan → stack_apply → stack_status (done, 4 шага) → повтор apply без дублей | ok | 39160570296 | |
| стек | stack_destroy | вопрос | 39160570296 | `destroy_failed` (1 шаг), остатков по именам нет; повтор с выводом шага |
| сеть | net целиком: VPC, provision (публичная, `no_public_ip`), `destroy release_ip=false`, TG/NLB, `vpc_route_create`/`delete`, `fip_attach`, `open_ports` (+ повтор `already_open`), `snat_create`/`delete`, destroy освобождает перепривязанный FIP | ok, 14 изменяющих | 39164767739 | маршрут в 198.18.255.0/24 — 400 (NET-12), в 10.255.255.0/24 — ok; SNAT удаляется через 10 мин (NET-11) |
| DNS | приватная зона (без `type` в `dns_zone_get` — #111), запись A, get, delete записи и зоны | ok | 39164767739 | операция создания зоны не done (NET-13) |
| диски | `disk_attach` через 2 с после `disk_create` | дефект evo D5 | 39164767739 | 422 «no openstack_id» (CMP-15); исправлено #117 |
| AR | проба `GET /v1/registries` | платформа | 39164767739 | без `pageSize` — пустые страницы с новым токеном бесконечно (AR-4) |
| диски | attach (с #117) → resize подключённого (`in_use`, 10 → 20 ГБ) → detach → delete; выключение ВМ изнутри, отключение загрузочного | ok | 39167011818 | в 1a resize упал в переходном состоянии после attach |
| образы | `image_create` из загрузочного диска | не покрыто: квота | 39167011818 | 422 `organization_quota_exceeded` (CMP-16); `image_delete` — тоже не покрыто |
| CA / AR | ar_create → push → ca_deploy → HTTP 200 по `publicUri` → ca_logs → stop → start (снова 200) → delete → ar_delete | ok | 39167011818 | `ca_logs` — 0 строк после запросов |
| стек | stack_destroy повторно, пока ресурсы удаляются | дефект evo D6 → ok | 39167011818, 39169320594 | `destroy_failed` при VM_NOT_FOUND; исправлено #118, вживую — destroyed |
| MCP HTTP | provision (confirmed) → destroy name | ok | 39170448486 | HTTP 200, `result` без ошибки; «?» в 1b–1e — разбор скрипта (`false // "?"`) |
| k8s | k8s_create → nodepool → kubectl → k8s_apply | ok | 39170448486 | |
| k8s | k8s_helm install | платформа | 39170448486 | обмен токена OIDC — 502 (OPS-4); повтор в 2b |
| ОС | os_smoke: Astra, РЕД ОС (отказ до создания, код 4), nginx-ubuntu (ВМ, SSH, порт) | ok | 39160570296 | |
| deploy (e2e) | deploy compose | платформа | 39160570296 | apt-зеркало образа недоступно (OPS-2c), evo дал верный диагноз; уборка чистая |

## Дефекты evo

| ID | Серьёзность | Область | Суть | PR |
|---|---|---|---|---|
| D1 | высокая | Artifact Registry | `ar_list` не завершается (1000 страниц за ~10 мин), `ar_create` из-за этого отказывает до создания; на main видно и в readonly (код 124, warn) | #110 (влит) |
| D2 | средняя | DNS | без `type` инструменты по зоне и записи шли в API публичных зон: приватная зона — 403, `dns_zone_delete` её не удалял, шаг `dns` стека с приватной зоной не работал | #111 (влит) |
| D3 | низкая | DNS | `dns_zones` не показывал состояние зоны (`valid`/`delegated` в API нет): удаляемая зона выглядела живой | #114 (влит) |
| D6 | средняя | стек | повторный `stack_destroy` (как советует его ответ) делал удалённый стек `destroy_failed`: `VM_NOT_FOUND` не считался «уже удалено» | #118 (влит) |
| D5 | средняя | диски | `disk_attach` сразу после `disk_create` — 422 «no openstack_id» без ожидания (ждал только `disk_create attach_to`), хотя протокол обещает ожидание | #117 (влит) |
| D4 | средняя | сертификаты | `cert_private_key` отдавал Base64 вместо PEM и без `key_format` (живой API — `private_key`) | #115 (влит) |

## Покрытие

Изменяющих инструментов: 65. Прошли вживую (по итогам прогонов, растёт):
см. строку «покрыто» в сводке каждого job `live` и таблицу выше.
