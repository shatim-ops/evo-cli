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
| cleanup | 39158678958 | `cleanup_only` | идёт | — |

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

## Дефекты evo

| ID | Серьёзность | Область | Суть | PR |
|---|---|---|---|---|
| D1 | высокая | Artifact Registry | `ar_list` не завершается (1000 страниц за ~10 мин), `ar_create` из-за этого отказывает до создания; на main видно и в readonly (код 124, warn) | #110 (влит) |

## Покрытие

Изменяющих инструментов: 65. Прошли вживую (по итогам прогонов, растёт):
см. строку «покрыто» в сводке каждого job `live` и таблицу выше.
