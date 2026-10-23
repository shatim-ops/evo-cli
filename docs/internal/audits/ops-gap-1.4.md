# Операционные разрывы evo против облачных CLI и MCP: анализ к плану 1.4–1.6

Сессия «CLI+MCP · Продукт и стратегия · Fable», 23.10.2026, по поручению
владельца через «1.3 — релиз». Код — `main` на `228a54b` (после
`1.3.0-rc.1`). Это вход для совещания, не решение: план ведёт «1.3 —
релиз», продуктовый код, CHANGELOG и `release-plan.md` здесь не трогаются.

Пометки: **факт** — проверено по коду evo или по странице первичного
источника (дата обращения 23.10.2026); **гипотеза** — по спецификации или
сниппету поиска, вживую не проверено; **мнение** — оценка автора. Сведения о
Cloud.ru — только из публичной документации и публичных спецификаций API
(`scripts/cloudru-specs.py`); в облако сессия не ходила.

## 1. Резюме

**Где evo сегодня.** 154 инструмента, каждый — команда CLI; превью с ценой,
согласие, отказ до согласия на вызов, который не пройдёт, идемпотентность
create/delete, `--read-only`, `--toolsets`, `--allow-project`, маскирование
секретов — по этим осям evo впереди всех рассмотренных CLI и большинства
официальных MCP-серверов (факт, §2 и §3, «Где evo впереди»). Но по
**операциям дежурного над уже созданной ВМ** evo отстаёт даже от `hcloud` и
`doctl`: нет stop/start/reboot, resize, rebuild, снимков, консоли, ssh-обёртки
(факт: в реестре таких инструментов нет; API Cloud.ru их даёт — §3).

**Подозрения проверены по коду** (§3, карточки G1–G7):

| Подозрение | Вердикт |
|---|---|
| Группа безопасности общая на зону, `vm create --port` открывает порт для 0.0.0.0/0 всем ВМ группы | **правда** (`provision/sg.go`, `sgrules.Open`): источник не задаётся, своя группа не создаётся; превью не говорит, скольких ВМ коснётся правило |
| Нельзя передать свой SSH-ключ | **отчасти**: `--meta <поле public_key>=…` заменяет ключ evo (не добавляет), не документировано как возможность, в превью — только имя поля |
| `status` и `vm delete` видят ВМ только из каталога создания | `status` — **правда**; `vm delete --vm-id/--name` — **нет**, работает по живому API; правила портов из чужого каталога `destroy` не найдёт |
| Нет user-data | **правда**; поле `cloud_init` в `POST /api/v1.1/vms` есть в спецификации 23.10.2026, вживую не подтверждено (гипотеза) |
| Нет stop/start/reboot/resize | **правда**; API: `set-power`, `PUT /vms/{id}` (`flavor_name`), `rebuild`, `remote-console` |
| Нельзя выбрать тип загрузочного диска | **правда** для `vm create` (`"SSD"` строкой), `deploy` — `PickDiskType` без флага |
| Нет создания подсети и группы, правил с источником CIDR | **правда**; есть только `open_ports` (ingress tcp/udp, `cidr`) |

**Топ разрывов по ценности** (мнение; обоснование — карточки):

| # | Разрыв | Кого задевает | Риск ИБ | Размер | Версия (черновик) |
|---|---|---|---|---|---|
| 1 | Нет `vm stop/start/reboot` (G5) | дежурный, агент | низкий | M | 1.4 |
| 2 | `--port` открывает порт всей зоне для 0.0.0.0/0 без источника и без счёта ВМ в превью (G1) | enterprise, агент | **высокий** | S (источник) / L (своя группа) | 1.4 / 2.0 |
| 3 | Свой SSH-ключ только заменой ключа evo (G2) | дежурный, enterprise | средний | S/M | 1.4 |
| 4 | Нет подсети, группы безопасности и общих правил (G7) | enterprise, агент | средний | M | 1.5 |
| 5 | Нет user-data (G4) | enterprise, агент | средний | M + проба | 1.5 |
| 6 | Нет `vm resize` (G5) | дежурный | низкий | M | 1.5 |
| 7 | Нет консоли и сброса пароля (G9) | дежурный в инциденте | средний (ссылка — доступ) | S | 1.5 |
| 8 | Нет снимков и копий ВМ/дисков (G8) | дежурный, enterprise | нет | M | 1.6 |
| 9 | Нет тегов (G10) | enterprise, FinOps | нет | M | 1.6 |
| 10 | Нет типа загрузочного диска и дисков данных при создании (G6, G11) | enterprise | нет | S | 1.4 / 1.5 |

Остальное: `status` только из каталога (G3, S, 1.4), `fip_detach` (G15, S,
1.4), ssh-обёртка (G12, развилка Р3), выборка полей (G13 — `--fields` уже в
плане 1.4), массовые операции (G14 — не раньше защит для автономных агентов).

**Главный тезис-оппонент.** Модель «одна команда + умолчания» — правильный
выбор против `az vm create` с десятками флагов и `openstack server create`, где
новичок не поднимет ВМ без чтения трёх страниц. Но умолчание, которое меняет
**общий** ресурс (группу зоны), переносит риск с того, кто вызвал команду,
на всех соседей — здесь мы повторяем ошибку gcloud (`--source-ranges` по
умолчанию `0.0.0.0/0`) и усугубляем её тем, что не показываем область
действия. Дешёвое исправление (источник и число ВМ в превью) — в 1.4;
правильное (своя группа на ВМ, как у `hcloud`/`doctl`/AWS по смыслу) — цель
2.0 (развилка Р1).

**Развилки для владельца** — §5: Р1 своя группа на ВМ или общая; Р2 согласие
на `stop`/`reboot`; Р3 ssh-обёртка локальной командой или инструментом; Р4
путь user-data; Р5 состав rc-волны 1.4; Р6 пароль ВМ по запросу; Р7 теги как
основа учёта.

## 2. Матрица «возможность × конкуренты × evo»

Обозначения: ✓ — есть; ± — частично или с оговоркой; — — нет; «?» — не
найдено в первичных источниках. Колонка «OpenStack» — `openstack` CLI, им
пользуются VK Cloud и Selectel (у обоих своего CLI управления ВМ нет —
факт). Terraform/OpenTofu — база: ресурсы зависят от провайдера, поэтому в
строках ресурсов стоит «пров.»; провайдер для Cloud.ru здесь не исследован.
Все ячейки конкурентов — факты по страницам из §6, если не отмечено «?».

### 2.1 Создание ВМ

| Возможность | AWS | Azure | GCP | yc | OpenStack | hcloud | doctl | TF | **evo** |
|---|---|---|---|---|---|---|---|---|---|
| Образ по имени/семейству | ✓ `--image-id` (только id) | ✓ `--image` алиас/URN | ✓ `--image`/`--image-family` | ✓ `image-family`/`image-name` | ✓ `--image` | ✓ `--image` | ✓ `--image` | пров. | ✓ `vm create --image` имя/отобр. имя/UUID (`provision`, `internal/tools/tools.go`) |
| Flavor / размер | ✓ `--instance-type` | ✓ `--size` | ✓ `--machine-type`, custom cpu/mem | ✓ `--cores --memory --platform` | ✓ `--flavor` | ✓ `--type` | ✓ `--size` | пров. | ✓ `--flavor`, авто — наименьший lowcost (`engine.PickVMFlavor`) |
| Тип загрузочного диска | ✓ `VolumeType` в `--block-device-mappings` | ✓ `--storage-sku` | ✓ `--boot-disk-type` | ✓ `type=` в `--create-boot-disk` | ✓ `volume_type=` | — (диск в типе сервера) | — (в slug) | пров. | **—** `"SSD"` строкой (`tools.go:1974`); `deploy` — `PickDiskType` без флага |
| Размер загрузочного диска | ✓ `VolumeSize` | ✓ `--os-disk-size-gb` | ✓ `--boot-disk-size` | ✓ `size=` | ✓ `--boot-from-volume N` | — | — | пров. | ✓ `--disk-size` (поднимается до `min_disk` образа) |
| Диски данных при создании | ✓ доп. mappings | ✓ `--data-disk-sizes-gb` | ✓ `--create-disk` (повтор.) | ✓ `--create-disk`, `--attach-disk` | ✓ `--block-device-mapping` | ✓ `--volume` | ✓ `--volumes` | пров. | **—** только `disk create --attach-to` после создания (`disk_create`) |
| Сеть и подсеть | ✓ `--subnet-id` | ✓ `--vnet-name --subnet` (автосоздание) | ✓ `--network --subnet` | ✓ `subnet-name=` | ✓ `--nic net-id=` | ✓ `--network` | ✓ `--vpc-uuid` | пров. | ± `--subnet-id`; VPC не выбирается, по умолчанию подсеть зоны (`provision.PickSubnet`) |
| Публичный IP | ✓ `--associate-public-ip-address` | ✓ `--public-ip-address` (`""` — нет) | ✓ `--address`/`--no-address` | ✓ `nat-ip-version=ipv4` | ± отдельно `floating ip` | ✓ по умолчанию, `--without-ipv4` | ✓ всегда | пров. | ✓ по умолчанию FIP, `--no-public-ip` |
| Группа безопасности своя на ВМ | ✓ `--security-group-ids` (общие объекты VPC, на ENI) | ✓ `--nsg` (общий, на NIC/подсеть) | — правила VPC по тегам ВМ (`--tags`) | ✓ `security-group-ids=` на NIC | ✓ `--security-group` (повтор.) | ✓ `--firewall` (общий объект, применяется к серверу/метке) | ✓ firewall по id/тегу дроплета | пров. | **—** всегда группа зоны по умолчанию (`provision/sg.go`) |
| Источник CIDR при открытии порта | ✓ `--cidr` | ✓ `--source-address-prefixes` | ✓ `--source-ranges` (по умолчанию 0.0.0.0/0) | ✓ `v4-cidrs=` | ✓ `--remote-ip` (по умолчанию 0.0.0.0/0) | ✓ `--source-ips` | ✓ `address:` в правиле | пров. | ± `open_ports --cidr`; у `vm create --port` — только 0.0.0.0/0 (`sgrules.Open`) |
| Свой SSH-ключ | ✓ `--key-name` | ✓ `--ssh-key-values`/`--generate-ssh-keys` | ± метаданные `ssh-keys`/OS Login | ✓ `--ssh-key` | ✓ `--key-name` | ✓ `--ssh-key` (неск.) | ✓ `--ssh-keys` | пров. | ± `--meta <поле public_key>=…` заменяет ключ evo (`images.FillMetadata`); `deploy_apply` — файл `user_authorized_keys` |
| user-data / cloud-init | ✓ `--user-data` | ✓ `--custom-data`, `--user-data` | ✓ `--metadata-from-file startup-script=` | ✓ `--metadata-from-file user-data=` | ✓ `--user-data` | ✓ `--user-data-from-file` | ✓ `--user-data-file` | пров. | **—** (API: `cloud_init` — гипотеза) |
| Метки / теги | ✓ `--tag-specifications` | ✓ `--tags` | ✓ `--labels` | ✓ `--labels` | ✓ `--tag`, `--property` | ✓ `--label` | ✓ `--tag-names` | пров. | **—** |
| Несколько интерфейсов | ✓ `--network-interfaces` | ✓ `--nics` | ✓ `--network-interface` (повтор.) | ✓ `--network-interface` (повтор.) | ✓ `--nic` (повтор.) | ✓ `--network` (повтор.) | — | пров. | **—** |

### 2.2 Жизненный цикл

| Возможность | AWS | Azure | GCP | yc | OpenStack | hcloud | doctl | TF | **evo** |
|---|---|---|---|---|---|---|---|---|---|
| stop / start / reboot | ✓ `stop/start/reboot-instances` | ✓ `stop/deallocate/start/restart` | ✓ `stop/start/reset/suspend` | ✓ `stop/start/restart` | ✓ `server stop/start/reboot` | ✓ `poweroff/poweron/reboot/shutdown` | ✓ `droplet-action power-off/…` | пров. (state) | **—** (API `set-power`: `power_on`/`power_off`/`reboot`) |
| Resize (смена flavor) | ✓ `modify-instance-attribute` (ВМ остановлена) | ✓ `az vm resize` | ✓ `set-machine-type` (TERMINATED) | ✓ `instance update --cores --memory` (stop → update → start) | ✓ `server resize --flavor`, `confirm/revert` | ✓ `change-type [--keep-disk]` | ✓ `resize --size [--resize-disk]` (сам выключает) | пров. | **—** (API `PUT /vms/{id}` `flavor_name`; нужна ли остановка — гипотеза) |
| Rebuild / reimage | — (terminate + run) | ✓ `az vm reimage`, `redeploy` | — (MIG `recreate-instances`) | — | ✓ `server rebuild` | ✓ `rebuild --image` | ✓ `rebuild --image` | пров. | **—** (API `rebuild`, `disks/{id}/reimage`) |
| Снимки / бэкапы | ✓ `create-snapshot`, AMI | ✓ `az snapshot create` | ✓ `disks snapshot` | ✓ `snapshot create`, `yc backup` | ✓ `server image create`, `volume snapshot` | ✓ `create-image --type snapshot\|backup`, `enable-backup` | ✓ `snapshot`, `enable-backups` | пров. | **—** для ВМ/дисков (есть `pg_backup_create`; API Backup Evolution) |
| Консоль (serial / VNC) | ✓ `get-console-output`, Serial Console | ✓ `az serial-console connect`, `boot-diagnostics get-boot-log` | ✓ `get-serial-port-output`, `connect-to-serial-port` | ✓ `get-serial-port-output`, `connect-to-serial-port` | ✓ `console log show`, `console url show` | ✓ `request-console` (VNC) | — | — | **—** (API `remote-console` vnc/serial) |
| Увеличение диска | ✓ `modify-volume` (онлайн) | ✓ `disk update --size-gb` | ✓ `disks resize` (только рост) | ✓ `disk resize` | ✓ `volume set --size` | ✓ `volume resize` | ✓ `volume-action resize` (только рост) | пров. | ✓ `disk resize` (только рост, `disk_resize`) |

### 2.3 Сеть

| Возможность | AWS | Azure | GCP | yc | OpenStack | hcloud | doctl | TF | **evo** |
|---|---|---|---|---|---|---|---|---|---|
| VPC | ✓ `create-vpc` | ✓ `vnet create` | ✓ `networks create` | ✓ `network create` | ✓ `network create` | ✓ `network create` | ✓ `vpcs create` | пров. | ✓ `vpc create` (`vpc_create`, D23) |
| Подсеть | ✓ `create-subnet` | ✓ `vnet subnet create` | ✓ `subnets create` | ✓ `subnet create --range` | ✓ `subnet create --subnet-range` | ✓ `network add-subnet` | — (в VPC) | пров. | **—** только `vpc subnets` (список) |
| Группа безопасности и правила (источник, протокол, направление) | ✓ `create-security-group`, `authorize-…-ingress/egress` | ✓ `nsg create`, `nsg rule create --direction --protocol --source-…` | ✓ `firewall-rules create --direction --action --source-ranges` | ✓ `security-group create --rule direction=,protocol=,v4-cidrs=` | ✓ `security group rule create --ingress/--egress --protocol --remote-ip/--remote-group` | ✓ `firewall create/add-rule --direction --protocol --source-ips` | ✓ `firewall create --inbound-rules/--outbound-rules` | пров. | ± `open_ports` (ingress tcp/udp, `cidr`); нет create/delete группы, egress, icmp, источника-группы |
| Floating / static IP | ✓ `allocate/associate/release-address` | ✓ `public-ip create` | ✓ `addresses create`, `add/delete-access-config` | ✓ `address create`, `add-one-to-one-nat` | ✓ `floating ip create/set/unset` | ✓ `floating-ip create/assign/unassign` | ✓ `reserved-ip create/assign/unassign` | пров. | ± `fip-attach`, `fip-release`; детача нет (`fip_attach`, `release_floating_ip`) |
| Балансировщик | ✓ `elbv2 create-load-balancer` (идемпотентно по имени) | ✓ `lb create` + probe/rule | ✓ `forwarding-rules` + backend-services | ✓ `network-load-balancer create`, ALB | ✓ Octavia `loadbalancer/listener/pool/member` | ✓ `load-balancer create` | ✓ `load-balancer create --forwarding-rules` | пров. | ✓ `tg create` + `lb create` (L4) |
| NAT | ✓ `create-nat-gateway` (`--client-token`) | ✓ `nat gateway create` | ✓ `routers nats create` | ✓ `gateway create` | ✓ `router create --enable-snat` | — | — | пров. | ✓ `snat create` (D20) |

### 2.4 Наблюдаемость и доступ

| Возможность | AWS | Azure | GCP | yc | OpenStack | hcloud | doctl | TF | **evo** |
|---|---|---|---|---|---|---|---|---|---|
| Логи | ✓ `logs tail --follow` | ✓ `vm monitor log show`, `monitor log-analytics query` | ✓ `logging read` | ✓ `logging read -f` | — | — | — | — | ± `logs` (compose по SSH, только ВМ развёртывания), `ca_logs` |
| Метрики | ✓ `cloudwatch get-metric-statistics` | ✓ `monitor metrics list` | ? (в gcloud не найдено) | ? (группы `monitoring` нет) | — | ✓ `server metrics` (alpha) | — (только алерты) | — | ✓ `metrics history`, `metrics scrape` |
| ssh-обёртка | ✓ `ec2-instance-connect ssh` | ✓ `az ssh vm` (сертификат Entra ID) | ✓ `compute ssh` (ключ в метаданные, IAP) | ✓ `compute ssh` (OS Login) | ✓ `server ssh` | ✓ `server ssh [-- cmd]` | ✓ `compute ssh --ssh-command` | — | **—** строка `ssh -i …` в ответе `vm create` |
| Аудит | ✓ `cloudtrail lookup-events` | ✓ `monitor activity-log list` | ± `logging read` по `cloudaudit` | ✓ `audit-trails` (управление трейлами) | — | — | — | — | ✓ `audit events` (`audit_events`; контракт §4.2 — не проверен вживую) |

### 2.5 Удобство и безопасность

| Возможность | AWS | Azure | GCP | yc | OpenStack | hcloud | doctl | TF | **evo** |
|---|---|---|---|---|---|---|---|---|---|
| Форматы и выборка | ✓ `--output` 6 видов, `--query` JMESPath, `--filters` | ✓ `--output` 7 видов, `--query` | ✓ `--format` с проекциями, `--filter` | ✓ `--format`, `--jq` | ✓ `-f json\|yaml\|csv\|value`, `-c` | ✓ `-o json\|yaml\|columns`, `-l` | ✓ `-o json`, `--format` колонки | `-json` у plan/show | ± `--json`; `--fields` — план 1.4 |
| Ожидание асинхронных операций | ✓ `ec2 wait` (44 ожидания), `--dry-run` | ✓ `--no-wait`, `az vm wait --custom` | ✓ `--async`, `operations wait` | ✓ `--async` | ✓ `--wait` | ✓ действия ждут, `--poll-interval` | ✓ `--wait` | plan/apply синхронны | ✓ `*_operation --wait`/`wait_seconds`, ВМ ждёт 25 мин (D16), прогресс MCP (1.3); «ждать готовности» k8s/PG — план 1.4 |
| Профили | ✓ `--profile` | ✓ `az account set`, `--subscription` | ✓ `configurations` | ✓ `config profile` | ✓ `--os-cloud` (clouds.yaml) | ✓ `context` | ✓ `--context` | workspaces | ✓ `--profile`, `configure list` |
| Идемпотентность | ± `--client-token` у `run-instances`, NAT; у `create-vpc` нет | ± PUT-семантика ARM | — (нет токена; bulk — откат) | — | — | — | — | ✓ по state | ✓ занятое имя — отказ до согласия, потерянный ответ — поиск по имени, повторный delete — `already_deleted` (D20–D28), тест-инварианты |
| Dry-run | ✓ `--dry-run` (проверка прав, не ресурсов) | ✓ `--validate` (шаблон ARM) | — | — | — | — | — | ✓ `plan`, `-detailed-exitcode` | ✓ `--dry-run` у любой команды, превью без ключа (D31) |
| Превью стоимости | — (отдельно `aws pricing`) | — (отдельно) | — | — | — | — | — | ± HCP cost estimation (платно, 3 облака), Infracost | ✓ цена в превью (`price-calculation`, D17, `tariff_url`) |
| Подтверждения | — (сразу) | ✓ `delete --yes` (по умолчанию спрашивает) | ✓ спрашивает, `--quiet` | — (`delete` без вопроса) | — | — (`delete` без вопроса и без `--force`) | ✓ `delete -f` (по умолчанию спрашивает) | ✓ `apply` спрашивает, `-auto-approve` | ✓ превью и вопрос у платного и разрушительного, `--yes`; одинаково в MCP |
| Массовые операции | ✓ списки id, `--count` | ✓ `--ids` | ✓ позиционные имена, `bulk create` | ✓ `delete A B` | ✓ `--min/--max` | ✓ `delete A B…` | ✓ по тегу, `-f` | ✓ | **—** |
| Автодополнение | ✓ `aws_completer` | ± `az interactive`; скрипт не найден в docs | ✓ установщик, `beta interactive` | ✓ bash/zsh | ✓ `openstack complete` | ✓ 4 оболочки | ± (страница 404) | ✓ | ✓ `evo completion` bash/zsh/fish/powershell |

### 2.6 Для агентов (MCP)

| Возможность | AWS | Azure | GCP | yc | OpenStack (VK, Selectel) | hcloud | doctl | TF | **evo** |
|---|---|---|---|---|---|---|---|---|---|
| Официальный сервер | ✓ awslabs/mcp (API MCP: `call_aws`), управляемый AWS MCP Server (`run_script`) | ✓ Azure MCP Server 2.0 (`compute`: create, power state, update/resize, delete) | ✓ Compute Engine remote MCP (31 инструмент: create/delete/start/stop/reset/set_machine_type), gcloud-mcp (`run_gcloud_command`) | ✓ yandex-cloud/mcp (toolkit, 42 инструмента) | — (только неофициальные) | — | ✓ mcp-digitalocean (droplets: power, resize, rebuild, snapshot…) | ✓ terraform-mcp-server (реестр, HCP runs; локальный apply не выполняет) | ✓ сам evo (`evo mcp`) |
| Только чтение | ✓ `READ_OPERATIONS_ONLY`; прокси `--read-only` по `readOnlyHint` | ✓ `--read-only` | ✓ OAuth-scope `compute.read-only`; IAM deny по `tool.isReadOnly` | ? не документировано | — | — | ? не документировано | ✓ `ENABLE_TF_OPERATIONS=false` по умолчанию | ✓ `--read-only` (H11), инструменты с согласием скрыты из `tools/list` |
| Наборы инструментов | ± по серверам | ✓ `--mode namespace`, `--namespace`, `--tool` | ± по серверам; denylist у gcloud-mcp | ± по серверам (`-s toolkit`) | — | — | ✓ `--services` | ✓ toolsets | ✓ `--toolsets`, `agent setup` по группам (D22) |
| Согласие на платное/разрушительное | ± `REQUIRE_MUTATION_CONSENT` через elicitation (выкл. по умолчанию); управляемый сервер — нет | ± elicitation только для инструментов, раскрывающих секреты; `--dangerously-disable-elicitation` | ± elicitation поддерживается; превью нет | ? | — | — | ? нет | ✓ `action_run` за флагом; `auto_approve` | ✓ превью + `confirmed=true`, `confirmation_required` (D9, D10); отказ до согласия на провальный вызов |
| Цена в превью для агента | — (отдельный Pricing MCP) | — (`pricing` namespace отдельно) | — | ? | — | — | — | ± HCP | ✓ та же, что в CLI |
| Защита от инъекций через данные облака | ± рекомендация «не подключать к недоверенным данным» | ± предупреждение в README | ✓ Model Armor (инспекция и блок) | ? | — | — | ? | ± «не используйте с недоверенными клиентами» | ± `_untrusted` у журналов, инструкция сервера (D33); имена ресурсов — план 1.4 |
| Маскирование секретов в ответах | ? не найдено | ± аннотация Secret + elicitation | ? | ? | — | — | ? | — | ✓ H7 по HTTP, `--allow-secret-export` |

### 2.7 Наблюдения по конкурентам (факты и мнения)

- **Операции над ВМ есть у всех.** Даже минималистичные `hcloud` и `doctl`
  дают stop/start/reboot, resize, rebuild, snapshot, консоль/пароль и ssh.
  Официальные MCP Azure, GCP и DigitalOcean тоже включают power state и
  resize. Это базовый слой, по которому evo оценивается первым делом (факт по
  спискам команд; мнение — об оценке).
- **Группа безопасности — общий объект везде, но привязка к ВМ явная.** AWS,
  Azure, yc, OpenStack, hcloud, doctl: группа/файрвол создаётся отдельно и
  назначается на ВМ или интерфейс; `az vm create` и `hcloud server create`
  делают группу по запросу с правилом SSH. У gcloud правила на уровне VPC
  по тегам, умолчание источника `0.0.0.0/0` — тот же класс риска, что у evo.
  Ни один CLI не считает «сколько ВМ затронет правило» (факт по страницам;
  мнение — класс риска).
- **Источник CIDR параметризован у всех**, кроме evo `--port` (факт).
- **Свой SSH-ключ — флаг первого уровня у всех** (`--key-name`,
  `--ssh-key(s)`, `--ssh-key-values`), у `hcloud` — несколько сразу (факт).
- **Согласие и dry-run слабые у всех CLI.** AWS `--dry-run` проверяет права,
  а не ресурсы; `yc` и `hcloud` удаляют без вопроса; цены не показывает никто
  — только Terraform через HCP (платно, три облака) или Infracost. Превью с
  ценой — реальное преимущество evo (факт).
- **Идемпотентность — редкость.** `--client-token` только у части вызовов
  AWS; `elbv2` идемпотентен по имени; у остальных повтор — ошибка 409 или
  дубль. Правила D20–D28 — тоже преимущество, его стоит удерживать при
  добавлении операций (факт; мнение).
- **MCP: read-only есть у AWS, Azure, GCP, Terraform; у yc и DigitalOcean
  не документирован.** Согласие на мутацию — через elicitation у AWS API MCP
  (выключено по умолчанию) и Azure (только секреты); GCP — IAM deny по
  атрибуту `isReadOnly` и Model Armor. Модель evo «инструмент сам возвращает
  превью и ждёт `confirmed`» независима от клиента — это сильнее, чем
  elicitation, которую клиент может не поддерживать (факт; мнение).
- **Паритет CLI ↔ MCP нигде не заявлен**: MCP-серверы — отдельные проекты
  (awslabs/mcp, microsoft/mcp, google/mcp, yandex-cloud/mcp), их инструменты
  — подмножество API, а не команд CLI. gcloud-mcp и AWS API MCP идут другим
  путём — «один инструмент, который запускает CLI» с denylist; это дёшево,
  но отдаёт агенту весь CLI без превью (факт; мнение).
- **Где подход evo слабее не по функциям, а по форме.** `az`, `gcloud`, `aws`,
  `yc` дают выборку полей и форматы (`--query`, `--format`, `--jq`): дежурный
  в evo всегда пишет `| jq`. `--fields` в плане 1.4 закрывает основное;
  язык запросов — не брать (BACKLOG cli-practice) — согласен (мнение).

## 3. Разрывы: карточки

Каждая карточка: вердикт по коду с доказательством, кого задевает, частота,
риск ИБ, поддержка API Cloud.ru (по публичной спецификации compute
`openapi-v3.yaml`, обращение 23.10.2026, `scripts/cloudru-specs.py`;
вживую не проверено — везде, где не сказано иное), трудоёмкость, ломающее ли
по `docs/RELEASING.md`, есть ли в `release-plan.md`.

Шкала трудоёмкости: **S** — один инструмент или один аргумент, одна точка в
`internal/tools` плюс `CLISpec`, русское описание, объявления инвариантов
(`createNames`/`deleteTargets`/`project_bound`), фикстуры мира паритета и
матрицы сбоев; **M** — несколько инструментов или новый запрос в
`internal/provision` с превью и ценой; **L** — меняется общий путь создания
ВМ (`provision`, `deploy`, `deploy_apply`, `stack` — BACKLOG A-029: три пути
с отдельной логикой SG, подсети, FIP и state) или нужна живая проба до
проектирования.

### G1. Группа безопасности общая на зону; `--port` открывает порт для 0.0.0.0/0 всем ВМ группы

**Вердикт: правда.** Доказательство:

- `internal/tools/tools.go`, схема `provision`, аргумент `ports`: «Inbound
  ports to open in the VM's security group from 0.0.0.0/0 … The group is
  shared by the zone's VMs».
- `internal/provision/sg.go`, `PickSecurityGroup`: для новой ВМ берётся
  группа зоны с флагом `default` (имя у Cloud.ru — `SSH-access_<зона>`),
  иначе первая по имени; своей группы на ВМ evo не создаёт
  (`POST /api/v1/security-groups` в `provision.CloudAPI` нет).
- `internal/sgrules/sgrules.go`, `Open`: правило `ingress` с
  `RemoteIPPrefix: "0.0.0.0/0"`, источник не параметризован; у `provision`
  аргумента источника нет. У инструмента `open_ports` аргумент `cidr` есть,
  но он не связан с `vm create --port`.
- Удаление: `destroy` снимает только правила, которые создал сам (по id в
  state), и оставляет правило, пока другая запись того же state им пользуется
  (`neededBy`). Правило, открытое с другой машины или другим пользователем в
  той же группе, не учитывается.

**Последствия.** Порт, открытый для одной ВМ, открыт для всех ВМ зоны в
группе по умолчанию — включая чужие ВМ проекта. Это не дефект реализации, а
следствие модели «одна команда + умолчания», перенесённой на общий ресурс.
В enterprise-проекте с несколькими командами `evo vm create --port 5432`
открывает PostgreSQL на всех ВМ зоны для всего интернета. Превью называет
порты, но не говорит, на сколько ВМ это распространится.

- У конкурентов (факт, §2.1): источник правила — параметр у всех (`aws
  --cidr`, `az --source-address-prefixes`, `yc v4-cidrs=`, `hcloud
  --source-ips`, `doctl address:`); группа привязывается к ВМ или интерфейсу
  явно; `az vm create --nsg-rule SSH` и `hcloud server create` создают группу
  под ВМ по запросу. Умолчание `0.0.0.0/0` без источника есть у gcloud
  `firewall-rules create` — но там правило адресуется тегами ВМ, а не всей
  зоне.
- Кого задевает: платформенная команда enterprise (главное), дежурный, ИИ-агент
  (агент читает «open port 80» в задаче и открывает его всей зоне).
- Частота: каждое `vm create --port`, каждый `deploy` (открывает порты
  compose).
- Риск ИБ: **высокий**.
- API: `POST /api/v1/security-groups` (имя, зона, правила при создании),
  `interfaces[].security_groups` в `POST /api/v1.1/vms` (группа на интерфейс),
  правило с `remote_ip_prefix` и `remote_security_group_id`. Всё в
  спецификации; вживую evo пользуется только `POST/GET/DELETE …/rules`.
- Трудоёмкость: `--source <CIDR>` у `--port` и у `ports` MCP — **S**
  (параметр `sgrules.Open`, запись в state, превью); своя группа на ВМ по
  умолчанию — **L** (три пути создания, удаление группы в `destroy`, превью
  «сколько ВМ в группе» для общей группы, cloud-e2e).
- Ломающее: `--source` — нет (новый аргумент, умолчание прежнее). Своя группа
  по умолчанию — не ломающее по букве RELEASING (команды, флаги, коды и формат
  те же), но меняет создаваемые ресурсы — продуктовый PR, «Миграция» как
  описание изменения поведения, решение владельца.
- В плане: частично — «Предупреждения о небезопасной конфигурации прямо в
  превью и режим "запретить небезопасное"» (1.4, требования ИБ). Своей группы
  и источника правил в плане нет.

### G2. Нельзя передать свой SSH-ключ

**Вердикт: отчасти правда.** `provision` кладёт в поле образа типа
`public_key` ключ evo (`eng.EnsureSSHKey`, `images.FillMetadata`, `Inputs.
PublicKey`). Явное значение `--meta <поле>=…` (`image_metadata`) побеждает
(`internal/images/metadata.go:241`), поэтому свой ключ передать можно, но:

- это заменяет ключ evo, а не добавляет второй: поле у образа одно; строка
  `ssh` в ответе и `evo logs` для такой ВМ не работают;
- возможность нигде не описана как «свой ключ»; превью показывает только
  имена полей (`image_metadata_fields`);
- у `deploy_apply` есть другой механизм — файл
  `<state>/.evo/user_authorized_keys`, ключи дописываются на ВМ по SSH
  ключом evo (`internal/tools/userkeys.go`); у `provision` его нет
  (`release-plan.md`, 1.2.3: «ключи пользователя в `provision` — только
  вместе с их показом в превью», решение владельца открыто).

- У конкурентов (факт, §2.1): флаг первого уровня у всех — `aws --key-name`,
  `az --ssh-key-values`, `yc --ssh-key`, `openstack --key-name`, `hcloud
  --ssh-key` (несколько), `doctl --ssh-keys`; `gcloud compute ssh` сам
  кладёт ключ пользователя в метаданные проекта.
- Кого задевает: дежурный (хочет свой ключ или ключ команды), платформенная
  команда (ключ evo на машине CI — не тот субъект доступа), агент (не задевает).
- Частота: каждая ВМ, которую будет администрировать человек.
- Риск ИБ: средний — ключ evo лежит в каталоге профиля; машина с evo
  становится точкой доступа ко всем её ВМ.
- API: поле образа `public_key` (одно) — факт; `cloud_init` в
  `POST /api/v1.1/vms` для `ssh_authorized_keys` — в спецификации есть,
  вживую не подтверждено (BACKLOG [05], 15.10: поле «ничем не подтверждено»;
  в спецификации 23.10 оно описано) — гипотеза, проверить.
- Трудоёмкость: `--ssh-key <файл|строка>` с дописыванием через
  `user_authorized_keys`-механизм `deploy_apply` — **S/M**; через
  `cloud_init` — **M** и живая проба.
- Ломающее: нет.
- В плане: строка про показ ключей в превью `provision` (1.2.3, открыто);
  аргумента нет.

### G3. `evo status` и `evo vm delete` видят ВМ только из каталога создания

**Вердикт: `status` — правда, `vm delete` — нет (с 1.0 по `--vm-id`/`--name`).**

- `handleStatus` (`internal/tools/tools.go:2292`) читает только локальный
  state каталога (`engine.Status`); `refresh` дочитывает облако по ВМ из
  записи, но не ищет ВМ по имени.
- `handleDestroy` (`tools.go:2342`): с `vm_id` или `name` — `destroyTargeted`
  по живому API из любого каталога; запись в state, если есть, используется
  для правил и FIP, иначе FIP находится по имени `<ВМ>-fip` в облаке
  (`IsEvoFIPOf`). Без аргументов — текущее развёртывание каталога.
- Правила группы безопасности, открытые из другого каталога, `destroy` не
  найдёт (они только в том state) — остаются в общей группе (см. G1).

- Кого задевает: дежурный (приходит с другой машины), агент (другой рабочий
  каталог сервера MCP).
- Частота: средняя; в документации описано, но подсказка в `status` не
  направляет к `vm get`.
- Риск ИБ: низкий; операционный — правило-сирота в общей группе (G1).
- API: `GET /api/v1/vms`, `/vms/{id}` — используются.
- Трудоёмкость: подсказка «ВМ не в этом каталоге — `evo vm get --name`» и
  `status --vm <имя>` — **S**; поиск правил evo по описанию `evo: port …` в
  облаке при `destroy` без state — **M**.
- Ломающее: нет (расширение).
- В плане: нет.

### G4. Нет user-data / cloud-init

**Вердикт: правда.** У `provision`, `deploy`, `deploy_apply`, `stack` нет
аргумента пользовательских данных; `provision.CreateVMRequest` поля
`cloud_init` не содержит (удалено 15.10, BACKLOG [05]). Единственный путь
настройки при первом старте — `user_data_template` своего образа
(`image_create`) и его поля.

- Кого задевает: платформенная команда (bootstrap агентов мониторинга, CA
  компании, join в домен), дежурный (разовая ВМ с нужным пакетом), агент
  (без user-data агент ставит софт по SSH — медленнее и с большим доступом).
- Частота: высокая в enterprise, низкая в догфуде.
- Риск ИБ: средний — user-data часто несёт секреты; нужны маскирование в
  превью/state и явное предупреждение.
- API: `cloud_init: string` в `POST /api/v1.1/vms` (спецификация 23.10.2026:
  «шаблон скрипта cloud-init … имя хоста, IP, логин, пароль, SSH-ключи»).
  Как он сочетается с `user_data_template` образа — не описано. **Гипотеза,
  проверить живой пробой** (cloud-e2e, одобрение владельца).
- Трудоёмкость: **M** (аргумент `user_data` из файла по правилу секретов —
  файл или stdin, не командная строка; маскирование H7; превью с
  отпечатком, не текстом; живая проба).
- Ломающее: нет.
- В плане: нет.

### G5. Нет stop/start/reboot/resize/rebuild

**Вердикт: правда.** В реестре нет `vm_stop`, `vm_start`, `vm_reboot`,
`vm_resize`, `vm_rebuild` (есть только `ca_start`/`ca_stop` для Container
Apps). BACKLOG [04] фиксирует отсутствие `vm start/stop/reboot` с 10.2026.

- API: `POST /api/v1/vms/{id}/set-power` (`power_on`, `power_off`, `reboot`),
  `PUT /api/v1/vms` (массовая смена состояния `running`/`stopped`),
  `PUT /api/v1/vms/{id}` (`flavor_name` — смена конфигурации; нужно ли
  выключать ВМ — спецификация не говорит, гипотеза), `POST /vms/{id}/rebuild`
  (образ и метаданные), `POST /disks/{id}/reimage`.
- У конкурентов (факт, §2.2): есть у всех CLI, включая `hcloud`
  (`poweroff/poweron/reboot/change-type/rebuild`) и `doctl`
  (`droplet-action power-off/resize/rebuild`); у `yc` resize — через
  stop → `instance update` → start, у AWS и gcloud — только на остановленной
  ВМ. Официальные MCP Azure, GCP и DigitalOcean дают power state и resize
  агенту.
- Кого задевает: дежурный — каждый инцидент («перезагрузи», «погаси на
  ночь», «добавь памяти»); платформенная команда — экономия (stop ночью);
  агент — без stop агент удаляет и создаёт заново.
- Частота: самая высокая среди разрывов.
- Риск ИБ: низкий; операционный — reboot/stop прерывают сервис, `rebuild`
  уничтожает данные.
- Трудоёмкость: `vm_stop`/`vm_start`/`vm_reboot` — **S** каждый, **M**
  вместе (одна функция `set-power` с превью, согласием и ожиданием состояния
  через `opwait`; идемпотентность: `stop` уже остановленной — `status:
  already_stopped`, код 0, по аналогии с D26); `vm_resize` — **M** (превью с
  ценой до/после через `price-calculation`, согласие, проверка состояния);
  `vm_rebuild` — **M** («удаляет», превью с образом и полями метаданных,
  пароль один раз, как в `provision`).
- Ломающее: нет (новые инструменты).
- В плане: нет (только BACKLOG [04]).
- Развилка: `stop` — платное действие? Нет (диск и IP оплачиваются, ВМ —
  нет), но разрушительное для сервиса: согласие нужно; `start` — платное.

### G6. Нельзя выбрать тип загрузочного диска

**Вердикт: правда для `vm create`.** `handleProvision` шлёт
`DiskTypeName: "SSD"` строкой (`internal/tools/tools.go:1974`); `deploy`
берёт `provision.PickDiskType` (SSD по умолчанию, D27) без флага;
`disk_create` тип принимает. Список типов есть (`evo disk types`).

- API: `disks[].disk_type_name` в `POST /api/v1.1/vms`.
- Кого задевает: платформенная команда (HDD под дешёвые узлы, быстрые диски
  под БД), дежурный — редко.
- Частота: низкая/средняя.
- Риск ИБ: нет.
- Трудоёмкость: **S** (`--disk-type` у `vm create`, `disk_type` у
  `provision`; одно правило `PickDiskType` на три пути; превью и цена уже
  считают диск).
- Ломающее: нет.
- В плане: нет.

### G7. Нет создания подсети и группы безопасности, правил с источником CIDR

**Вердикт: правда.** В реестре: `vpc_create`, `list_subnets`,
`list_security_groups` (с правилами), `open_ports` (только ingress tcp/udp,
`cidr` есть). Нет: `subnet_create/delete`, `sg_create/delete`,
`sg_rule_delete`, правил egress, `icmp`/`any`, источника «другая группа».

- API: `POST/PUT/DELETE /api/v1/subnets`, `POST/PUT/DELETE
  /api/v1/security-groups`, `POST/PUT/DELETE …/rules` (`direction`,
  `ether_type`, `ip_protocol` tcp/udp/icmp/any, `port_range`,
  `remote_ip_prefix`, `remote_security_group_id`).
- Кого задевает: платформенная команда (ландшафт с нуля без консоли и
  Terraform), агент (задача «подними изолированную сеть»), дежурный — редко.
- Частота: средняя (один раз на проект, но без этого «с нуля» evo не
  самодостаточен).
- Риск ИБ: средний — правила ИБ создаются из CLI, нужен тот же режим
  «запретить небезопасное» (1.4).
- Трудоёмкость: **M** — 5–6 инструментов; `open_ports` расширить до общего
  `sg_rule_add` (направление, протокол, источник) по правилу «одна точка
  решения» (CLAUDE.md §2), не дублировать.
- Ломающее: нет, если `open_ports` остаётся с прежними умолчаниями.
- В плане: нет.

### G8. Снимки, резервные копии ВМ и дисков

**Вердикт: нет.** Есть только `pg_backup_create`. Для ВМ и дисков — ничего;
`image_create` требует отсоединённого диска (не снимок).

- API: Backup Evolution — `POST /api/v1/backups` (ресурс, тип), `…/action`
  (восстановление), `protection-plans` (расписание); BACKLOG docs-review:
  квота 1024 ГБ, до 5 ресурсов зоны в плане.
- Кого задевает: дежурный (копия перед опасным изменением — базовая
  гигиена), платформенная команда (политика резервного копирования),
  агент (перед `rebuild`/`resize`).
- Частота: средняя; ценность для enterprise высокая.
- Риск ИБ: нет; платно — превью с тарифом хранения.
- Трудоёмкость: **M** (`backup_create/list/restore/delete`,
  `protection_plan_*` — вторая очередь; тариф хранения — та же проблема, что
  у `image_create`: страница тарифа).
- Ломающее: нет. В плане: нет (BACKLOG docs-review).

### G9. Консоль ВМ (serial/VNC) и сброс пароля

**Вердикт: нет.** Нет инструмента консоли; пароль выдаётся один раз при
создании.

- API: `POST /vms/{id}/remote-console` (`vnc`|`serial`, возвращает ссылку),
  `get-vnc`, `set-password`, `delete-password`, `check-guest-agent`.
- Кого задевает: дежурный — когда SSH не поднялся (самый частый тупик в
  cloud-e2e и догфуде: ВМ `active`, SSH нет); агент — не нужно.
- Частота: низкая, но в инциденте — критична.
- Риск ИБ: ссылка консоли — это доступ к ВМ: как секрет (`confirmed`,
  маскирование по HTTP, H5/H7); `set-password` — пароль не в командной
  строке (правило секретов из файла/stdin).
- Трудоёмкость: **S** каждый (`vm_console`, `vm_set_password`).
- Ломающее: нет. В плане: нет.

### G10. Метки и теги

**Вердикт: нет.** Ни один инструмент не принимает и не показывает теги.

- API: `tag_ids`/`tag_names` у ВМ, дисков, подсетей, групп, копий; отдельный
  сервис тегов (`GET /api/tags/v1/tags`, в реестре спецификаций не скачан —
  проверить).
- Кого задевает: платформенная команда (владелец, окружение, центр затрат —
  FinOps по тегам), агент (пометить созданное: `evo`, задача).
- Частота: в enterprise — на каждом ресурсе.
- Риск ИБ: нет.
- Трудоёмкость: **M** (`--tag` у создающих команд, `tags` в списках, поиск
  спецификации сервиса тегов; потенциально — замена поиска FIP по имени
  `<ВМ>-fip` на тег `evo:vm=<id>`, что надёжнее D25/D26-поиска по имени).
- Ломающее: нет. В плане: нет.

### G11. Диски данных при создании и несколько интерфейсов

**Вердикт: нет.** `disk create --attach-to` подключает после создания;
второго интерфейса нет.

- API: `disks[]` с `bootable: false`, `interfaces[]` (число — по PCI-слотам
  flavor), `POST /api/v1/interfaces` к существующей ВМ.
- Кого задевает: платформенная команда (БД-узел с отдельным диском данных;
  ВМ в двух подсетях). Агент — редко.
- Частота: низкая.
- Трудоёмкость: диски при создании — **S/M** (`--data-disk size[:type]`,
  повторяемый; превью с ценой); интерфейсы — **M** (`vm_interface_add`).
- Ломающее: нет. В плане: нет.

### G12. ssh-обёртка

**Вердикт: нет.** `evo vm create` печатает строку `ssh -i …`; команды
`evo vm ssh <имя>` нет; `logs` ходит по SSH только к ВМ развёртывания.

- Кого задевает: дежурный.
- Частота: ежедневно.
- Риск ИБ: низкий (ключ evo и known_hosts проекта уже есть, #169).
- Трудоёмкость: **S** как локальная команда — но это исключение из паритета
  (CLAUDE.md §1: команды без инструмента — только закрытый список
  `localCommands` с причиной, как `evo verify`). Причина есть: сервер MCP
  интерактивную сессию не держит, а `k8s_kubectl` уже даёт агенту
  неинтерактивный путь. Вариант с инструментом `vm_exec` (одна команда по
  SSH, как `logs`) — **M** и отдельное решение по ИБ (исполнение
  произвольных команд агентом).
- Ломающее: нет. В плане: нет.

### G13. Выборка полей вывода

**Вердикт: нет.** Только `--json` и текст; `--fields`, `--format
table|yaml`, `--jq` отсутствуют. BACKLOG cli-practice (15.10): языки запросов
не берём, `--jq` — по запросу.

- Кого задевает: дежурный и скрипты (сейчас `| jq`), агент — не задевает
  (читает JSON).
- Трудоёмкость: `--fields` — **S/M**; в плане 1.4 уже есть.
- Ломающее: нет.

### G14. Ожидание долгих операций и массовые операции

**Вердикт: частично.** `*_operation --wait`/`wait_seconds`, прогресс по MCP
(1.3). «Подождать до готовности» у k8s/PG/Kafka/Redis — в плане 1.4.
Массовых операций (несколько ВМ одной командой) нет; API даёт
`PUT /api/v1/vms` для состояния пакетом. Низкий приоритет: массовое
удаление агентом — именно то, от чего защищает план «режим без удалений».

### G15. Детач floating IP

**Вердикт: нет.** `fip_attach` есть, обратного нет; `fip-release` занятый
адрес не освобождает (D31). API: `PUT /floating-ips/{id}` с `detach`.
Трудоёмкость **S**. В плане нет.

### Где evo впереди (факты по коду и документации)

| Возможность | evo | У конкурентов |
|---|---|---|
| Превью с ценой до платного действия, на обеих поверхностях | `CreatePreview`, `price-calculation`, `tariff_url` (D17), `--dry-run` у любой команды | CLI облаков цены не показывают; Terraform — только план без цены (оценка — HCP/Infracost, см. §6) |
| Согласие одинаково для человека и агента | `confirmed=true` ↔ `--yes`, `confirmation_required` (D9, D10) | MCP-серверы полагаются на клиента; AWS/Azure дают read-only флаг, не превью |
| Отказ до согласия на вызов, который не пройдёт | D15, D19–D21, D23, D29–D31 | нет аналога: CLI отдают ошибку API после отправки |
| Идемпотентность create/delete | `takenName`, `lostCreate`, `already_deleted` (D20–D28), тест-инварианты | `aws ec2 run-instances --client-token`; у `gcloud`/`az` повтор создания — ошибка 409 |
| Паритет CLI ↔ MCP | один реестр, `TestParity_*` (D13, D14) | MCP-серверы — отдельные проекты от CLI (awslabs/mcp, Azure MCP) |
| Граница прав на сервере | `--read-only`, `--toolsets`, `--allow-project`, H1–H13, маскирование H7 | `--read-only`/`--mode` есть у AWS и Azure MCP (см. §6); маскирования секретов в ответах — не найдено |
| Защита от инъекций через данные | `_untrusted` у журналов, инструкция сервера (D33); полная пометка полей — 1.4 | в документации AWS/Azure MCP — предупреждения, механизма пометки не найдено |
| Безопасность учётных данных | ключ из файла/stdin, 0600, состояние с блокировкой, `redact.sed` | сопоставимо (профили `aws configure`, `az login`) |

## 4. Черновик раскладки по версиям

Критерий ценности, в порядке важности: (1) снимает риск ИБ, который evo
создаёт сам; (2) открывает enterprise-использование (без консоли и
Terraform); (3) даёт ежедневную ценность дежурному. Правила: каждая
возможность — инструмент реестра и команда CLI (`CLISpec`), сначала тест,
превью и согласие для платного и разрушительного, cloud-e2e для изменений в
облаке с одобрения владельца, ломающее — только по условиям 1–5 RELEASING
или в 2.0. Это черновик к совещанию: план ведёт «1.3 — релиз».

Что уже стоит в 1.4 (`release-plan.md`) и сюда не дублируется: режим без
удалений, потолок стоимости, предупреждения ИБ в превью и «запретить
небезопасное», набор инструментов по правам ключа, `--fields`, номер
договора в профиле, цена калькулятора, ожидание готовности k8s/PG/Kafka/
Redis, Data Platform волна 1, Workflow Studio и Repo, `_untrusted` для имён,
уведомление о новой версии.

### 1.4 — «дежурный и ИБ своих умолчаний» (minor, с rc)

| # | Что | Почему сейчас | Размер | Ломающее | Проверка |
|---|---|---|---|---|---|
| O1 | `vm_stop`, `vm_start`, `vm_reboot` (`evo vm stop/start/reboot`): превью, согласие, ожидание состояния через `opwait`, идемпотентность (`already_stopped`/`already_running` — код 0, как D26) | самый частый сценарий дежурного; без них агент удаляет и создаёт заново | M | нет | мир паритета, матрица сбоев, cloud-e2e `e2e` (stop → start на ВМ теста) |
| O2 | `--source <CIDR>` у `vm create --port` и `ports` MCP (`source` аргумент), превью называет источник и **число ВМ в группе**; умолчание прежнее `0.0.0.0/0`, но строкой «внимание» | G1: закрывает ИБ-риск собственного умолчания без ломки; ложится в строку 1.4 «предупреждения ИБ в превью» | S | нет | тест превью, `sgrules` с источником, cloud-e2e |
| O3 | `--ssh-key` у `vm create` (`ssh_key` MCP): дописывается к ключу evo, показывается в превью отпечатком (решение из 1.2.3 «только вместе с показом в превью») | G2; дежурный получает доступ своим ключом | S/M | нет | тест превью; cloud-e2e: вход обоими ключами |
| O4 | `--disk-type` у `vm create` и `deploy` (`disk_type`), одно правило `PickDiskType` на три пути | G6, S, безрисково | S | нет | детерминизм D27, превью с ценой |
| O5 | `status`: подсказка «ВМ не в этом каталоге — `evo vm get --name …`»; `destroy` без state ищет правила `evo: port …` в группе по описанию | G3 | S/M | нет | мир паритета |
| O6 | `fip_detach` (`evo vpc fip-detach`) | G15, S, парно к `fip_attach` | S | нет | cloud-e2e |
| O7 | Живая проба `cloud_init` в `POST /api/v1.1/vms` (api-probe, только чтение не подойдёт — шаг cloud-e2e с одобрением): работает ли вместе с `user_data_template` образа | снимает гипотезу под G4 к 1.5 | — | — | cloud-e2e |

Итого 1.4 добавляет 5 инструментов и 3 аргумента; все без ломающих
изменений. Темы «дежурный» и «ИБ своих умолчаний» одна rc-волна.

### 1.5 — «ландшафт без консоли» (minor, с rc)

| # | Что | Размер | Примечание |
|---|---|---|---|
| O8 | `subnet_create/delete`, `sg_create/delete`, `sg_rule_add` (расширение `open_ports`: направление, протокол `icmp`/`any`, источник CIDR или группа), `sg_rule_delete` | M | G7; превью правила словами; предупреждение/запрет небезопасного из 1.4 применяется к каждому правилу |
| O9 | `vm create --security-group <имя|uuid>` и `--new-security-group`: своя группа на ВМ (создаётся с правилом 22 и правилами `--port`), `destroy` удаляет её | M/L | G1 целиком; умолчание **не меняется** в 1.x (развилка Р1) |
| O10 | `--user-data <файл|->` у `vm create` и `deploy` (если O7 подтвердил): по правилу секретов (файл/stdin), превью — отпечаток и размер, маскирование H7 | M | G4 |
| O11 | `vm_resize` (`evo vm resize --flavor`): превью с ценой до и после (`price-calculation`), согласие, состояние ВМ до/после по результату O1 | M | G5; требование «ВМ остановлена» — гипотеза, проверить живой пробой |
| O12 | `vm_console` (`evo vm console --serial|--vnc`, ссылка как секрет: `confirmed`, H5/H7), `vm_set_password` (пароль из файла/stdin) | S | G9 |
| O13 | `--data-disk size[:type]` у `vm create` (повторяемый) | S/M | G11 |

### 1.6 — «эксплуатация и учёт» (minor)

| # | Что | Размер | Примечание |
|---|---|---|---|
| O14 | Резервные копии ВМ и дисков: `backup_create/list/get/restore/delete`, затем `protection_plan_*` | M | G8; тариф хранения — та же задача, что у `image_create` |
| O15 | Теги: `--tag` у создающих команд, `tags` в списках и `get`, `--tag` как фильтр списков; evo помечает своё (`evo:vm=<id>`), поиск FIP и правил по тегу вместо имени | M | G10; спецификацию сервиса тегов добавить в `scripts/cloudru-specs.py` |
| O16 | `vm_rebuild` (`evo vm rebuild --image`, «удаляет», пароль один раз) | M | G5 |
| O17 | `vm_interface_add/delete` | M | G11, по запросу |
| O18 | `evo vm ssh <имя>` как локальная команда (причина в `localCommands`) или `vm_exec` как инструмент — развилка Р3 | S / M | G12 |

### Дальше / 2.0

- Своя группа безопасности на каждую ВМ **по умолчанию** и `--port` без
  источника — отказ (ломающее поведение и коды: 2.0 или по условиям 1–5,
  если владелец признает открытие порта всей зоне дефектом согласия — развилка Р1).
- Пароль ВМ только по запросу (`vm_set_password`), а не в ответе создания
  (меняет формат ответа — 2.0).
- Массовые операции (`vm stop --all --tag …`) — только вместе с потолком
  стоимости и режимом без удалений.
- Выборка `--jq`/`--format yaml` — по спросу; `--fields` в 1.4 закрывает
  основное.

## 5. Развилки для владельца

**Р1. Группа безопасности новой ВМ: общая по умолчанию или своя?**
- (а) Оставить общую, добавить `--source` и предупреждение «правило
  коснётся N ВМ» (1.4) + `--new-security-group` по запросу (1.5). Плюс:
  без ломки, совместимо с `deploy`. Минус: небезопасное умолчание остаётся;
  агент по-прежнему откроет порт всей зоне, если никто не читает «внимание».
- (б) Своя группа на ВМ по умолчанию с 1.5 (с правилом 22 и `--port`),
  общая — по флагу. Плюс: как у `hcloud`/`doctl`/AWS по смыслу (правила
  привязаны к ресурсу). Минус: три пути создания (A-029), `destroy` должен
  удалять группу, квоты на группы, `deploy` в существующую ВМ; де-факто
  ломающее поведение — хотя RELEASING его не называет ломающим, нужна
  «Миграция» и решение D.
- (в) (б) только для агента: сервер MCP с флагом (как `--read-only`)
  создаёт ВМ в своей группе. Минус: расхождение CLI и MCP — против D14.
- Мнение: (а) в 1.4 обязательно; (б) — цель 2.0, решить после живой пробы
  квот на группы и стоимости в коде.

**Р2. `stop`/`reboot` — нужно ли согласие?** Они не платные, но прерывают
сервис; `start` — платный. Варианты: согласие у всех трёх (просто, одно
правило «меняет — спрашивает»); согласие только у `stop`/`reboot` при ВМ с
публичным IP (сложнее, частный случай — против CLAUDE.md §2). Мнение: согласие у всех,
`--yes` в скриптах, как у остальных изменяющих инструментов.

**Р3. ssh-обёртка: локальная команда или инструмент?**
- `evo vm ssh` в `localCommands` — быстро, но первое исключение из паритета
  ради удобства (сейчас исключения — по природе: `configure`, `verify`).
- `vm_exec` — инструмент «одна команда по SSH ключом evo», доступен агенту:
  мощно и опасно (исполнение произвольных команд на ВМ), требует класса
  «удаляет» и, вероятно, отдельного флага сервера.
- Мнение: `evo vm ssh` локально в 1.6; `vm_exec` — не раньше режима без
  удалений и потолка стоимости.

**Р4. user-data: ждать подтверждения поля `cloud_init` или идти через
`user_authorized_keys`-механизм (по SSH после старта)?** По SSH работает уже
сейчас, но требует доступа оператора к порту 22 и ключа evo на ВМ;
`cloud_init` — правильный путь, но не подтверждён. Мнение: O7 в 1.4,
решение по O10 после пробы.

**Р5. Что считать «операцией дежурного» для rc-волны 1.4?** Если O1–O6 идут
одной волной, rc задерживается на самом большом (O1). Альтернатива: 1.4.0 с
O2–O6 (S-размеры) и 1.4.x/1.5 с O1. Мнение: O1 — главная ценность версии,
без неё 1.4 не «операционная»; держать вместе.

**Р6. Пароль ВМ в ответе создания** (сейчас один раз в ответе) против
`vm_set_password` по запросу — только в 2.0; решать не сейчас, но учесть
при проектировании O12, чтобы не делать два механизма.

**Р7. Теги как основа учёта (O15) или имя `<ВМ>-fip` как сейчас?** Теги
надёжнее для D25/D26-поиска и дают FinOps по владельцу, но это второй
механизм рядом с именованием; переход — 1.6 с сохранением поиска по имени
до 2.0.

## 6. Источники

Дата обращения ко всем внешним страницам — 23.10.2026. Пометка «(стр.)» —
страница прочитана целиком; «(сниппет)» — только выдача поиска, факт
ненадёжен. Страницы читались исследовательскими субагентами сессии; что не
нашлось, перечислено в конце.

### evo (код `main` на `228a54b`)

- `internal/tools/tools.go` — схема и обработчик `provision`
  (`handleProvision`: группа зоны, `DiskTypeName: "SSD"`, `EnsureSSHKey`,
  `FillMetadata`), `open_ports`, `handleStatus`, `handleDestroy`,
  `destroyTargeted`.
- `internal/provision/sg.go` (`PickSecurityGroup`, `IsDefaultGroup`),
  `sgcheck.go`; `internal/sgrules/sgrules.go` (`Open`, `RemoteIPPrefix:
  "0.0.0.0/0"`, `neededBy`, `Close`).
- `internal/images/metadata.go` (`FillMetadata`: явные значения побеждают,
  поля типа `public_key`); `internal/tools/userkeys.go`
  (`user_authorized_keys` у `deploy_apply`).
- `internal/engine/deploy.go` (`PickDiskType`), `internal/provision/order.go`.
- `internal/tools/cli_spec.go`, `registry.go`, `create_names.go`,
  `delete_gone.go`, `project_bound.go` — точки касания нового инструмента.
- `internal/cli/local_commands.go` — закрытый список локальных команд.
- `docs/tools.md`, `docs/commands.md`, `docs/mcp.md` (H1–H13),
  `docs/RELEASING.md`, `docs/internal/release-plan.md`,
  `docs/internal/standalone/DECISIONS.md` (D9–D33),
  `docs/internal/standalone/BACKLOG.md` ([04] `vm start/stop/reboot`, [05]
  `cloud_init`, cli-practice, docs-review Backup Evolution),
  `docs/internal/api-contract.md`, `docs/internal/handoff/1.3.md`.

### Cloud.ru (публичные спецификации, `scripts/cloudru-specs.py`)

- Compute `openapi-v3.yaml` (SVC Public API, 97 операций): `POST
  /api/v1.1/vms` (`cloud_init`, `image_metadata`, `disks[]` с
  `disk_type_name` и `bootable: false`, `interfaces[].security_groups`,
  `tag_ids`/`tag_names`, `placement_group_*`), `POST /api/v1/vms/{id}/
  set-power` (`power_on`/`power_off`/`reboot`), `PUT /api/v1/vms`
  (массово `running`/`stopped`), `PUT /api/v1/vms/{id}` (`flavor_name`,
  `name`, `tag_*`), `POST …/rebuild`, `POST …/remote-console`
  (`vnc`/`serial`), `get-vnc`, `set-password`, `delete-password`, `POST
  /api/v1/security-groups`, `POST …/rules` (`direction`, `ether_type`,
  `ip_protocol` tcp/udp/icmp/any, `port_range`, `remote_ip_prefix`,
  `remote_security_group_id`), `POST /api/v1/subnets`, `POST
  /api/v1/interfaces`, `POST /api/v1/backups`, `…/backups/{id}/action`,
  `protection-plans`, `disks/{id}/reimage`, `placement-groups`, `tasks`.
  Источник: https://cloud.ru/docs/virtual-machines/ug/topics/api-ref (стр.,
  через скрипт). Вживую ни один из новых путей не проверен.
- VPC `openapi.yaml` (26 операций): VPC, пулы адресов, статические
  маршруты, VIP — подсетей и групп безопасности в этом API нет, они в
  Compute. https://cloud.ru/docs/evolution-vpc/ug/topics/api-ref (стр.).

### AWS

- `aws ec2 run-instances`: https://docs.aws.amazon.com/cli/latest/reference/ec2/run-instances.html (стр.)
- `stop-instances`, `modify-instance-attribute`, `create-snapshot`, `get-console-output`, `modify-volume`, `create-vpc`, `authorize-security-group-ingress`, `allocate-address`, `create-nat-gateway`, `ec2/wait/index.html` — тот же префикс `…/reference/ec2/` (стр.)
- `elbv2 create-load-balancer`: https://docs.aws.amazon.com/cli/latest/reference/elbv2/create-load-balancer.html (стр.)
- `logs tail`, `cloudwatch get-metric-statistics`, `cloudtrail lookup-events`, `ec2-instance-connect ssh` — `…/reference/<сервис>/…` (стр.)
- Форматы, фильтры, профили, автодополнение, автоподсказки: https://docs.aws.amazon.com/cli/latest/userguide/cli-usage-output-format.html, `cli-usage-filter.html`, `cli-configure-files.html`, `cli-configure-completion.html`, `cli-usage-parameters-prompting.html` (стр.)
- awslabs/mcp: https://github.com/awslabs/mcp, https://awslabs.github.io/mcp/ (стр.); AWS API MCP Server README: https://github.com/awslabs/mcp/blob/main/src/aws-api-mcp-server/README.md (стр.: `READ_OPERATIONS_ONLY`, `REQUIRE_MUTATION_CONSENT`, denyList/elicitList)
- Управляемый AWS MCP Server: https://docs.aws.amazon.com/aws-mcp/latest/userguide/getting-started-aws-mcp-server.html, `understanding-mcp-server-tools.html`, `security_iam_service-with-iam.html` (стр.); прокси `--read-only`: https://github.com/aws/mcp-proxy-for-aws (стр.); анонс: https://aws.amazon.com/about-aws/whats-new/2025/11/aws-mcp-server/ (сниппет)

### Azure

- `az vm` (create, start/stop/deallocate/restart/redeploy/reimage, resize, open-port, wait): https://learn.microsoft.com/en-us/cli/azure/vm (стр.)
- `az snapshot`, `az disk`, `az serial-console`, `az ssh`, `az network vnet`, `az network nsg rule`, `az network public-ip`, `az network lb`, `az network nat gateway`, `az monitor metrics`, `az monitor activity-log`, `az interactive`, форматы вывода: https://learn.microsoft.com/en-us/cli/azure/… (стр.)
- Azure MCP Server: https://github.com/microsoft/mcp/blob/main/servers/Azure.Mcp.Server/README.md (стр.); флаги `--mode`, `--namespace`, `--read-only`, `--dangerously-disable-elicitation`: https://raw.githubusercontent.com/microsoft/mcp/main/servers/Azure.Mcp.Server/docs/azmcp-commands.md (стр.); каталог инструментов и аннотации: https://learn.microsoft.com/en-us/azure/developer/azure-mcp-server/tools/ (стр.); `compute`: https://learn.microsoft.com/en-us/azure/developer/azure-mcp-server/tools/azure-compute (стр.)

### Google Cloud

- `gcloud compute instances create`, `bulk create`, `stop`, `set-machine-type`, `get-serial-port-output`, `disks snapshot`, `disks resize`, `networks create`, `networks subnets create`, `firewall-rules create`, `addresses create`, `forwarding-rules create`, `routers nats create`, `compute ssh`, `logging read`, `config configurations`, `topic/formats`, `topic/filters`: https://docs.cloud.google.com/sdk/gcloud/reference/… (стр.)
- Установка и интерактивная оболочка: https://docs.cloud.google.com/sdk/docs/install-sdk, https://docs.cloud.google.com/sdk/docs/interactive-gcloud (стр.)
- Реестр MCP Google: https://github.com/google/mcp (стр.); Compute Engine MCP: https://docs.cloud.google.com/compute/docs/reference/mcp, https://docs.cloud.google.com/compute/docs/use-compute-engine-mcp (стр.); режим только чтения через IAM deny: https://docs.cloud.google.com/mcp/prevent-read-write-tool-use (стр.); обзор и Model Armor: https://docs.cloud.google.com/mcp/overview (стр.); gcloud-mcp: https://github.com/googleapis/gcloud-mcp/blob/main/packages/gcloud-mcp/README.md (стр.); Cloud Run MCP: https://github.com/GoogleCloudPlatform/cloud-run-mcp (стр.)

### Yandex Cloud

- `yc compute instance create`, `update`, `get-serial-port-output`, `connect-to-serial-port`, `ssh`, `snapshot create`, `disk resize`: https://yandex.cloud/en/docs/cli/cli-ref/compute/cli-ref/… (стр.); смена ресурсов ВМ (stop → update → start): https://yandex.cloud/en/docs/compute/operations/vm-control/vm-update-resources (стр.)
- `yc vpc subnet create`, `security-group create`, `address create`, `gateway create`, `yc load-balancer network-load-balancer create`, `yc logging read`, `yc audit-trails`, `yc config profile`: https://yandex.cloud/en/docs/cli/cli-ref/… (стр.); установка и автодополнение: https://yandex.cloud/en/docs/cli/operations/install-cli (сниппет)
- MCP: https://github.com/yandex-cloud/mcp (стр.); образ Marketplace «MCP server for Yandex Cloud» (42 инструмента): https://yandex.cloud/en/marketplace/products/yc/mcp-yandex-cloud-app (стр.); MCP Hub: https://yandex.cloud/en/docs/ai-studio/concepts/mcp-hub (сниппет)

### VK Cloud и Selectel (OpenStack CLI)

- VK Cloud, инструменты: https://cloud.vk.ru/docs/en/tools-for-using-services/cli, OpenStack CLI: https://cloud.vk.ru/docs/en/tools-for-using-services/cli/openstack-cli, создание ВМ: https://cloud.vk.ru/docs/en/computing/iaas/instructions/vm/vm-create (стр.; блок флагов `server create` на странице обрезан)
- Selectel, OpenStack CLI: https://docs.selectel.ru/en/cloud-servers/tools/openstack-cli/about-openstack-cli/, настройка: https://docs.selectel.ru/en/cloud-servers/tools/openstack-cli/configure-openstack-cli/, создание сервера (`--security-group --key-name --user-data`): https://docs.selectel.ru/en/cloud-servers/create/create-server/, `selvpc`: https://docs.selectel.ru/en/cloud-servers/tools/selvpc/ (стр.)
- OpenStack CLI: `server`, `volume`, `network`, `subnet`, `router`, `security group rule`, `floating ip`: https://docs.openstack.org/python-openstackclient/latest/cli/command-objects/… (стр.); Octavia: https://docs.openstack.org/python-octaviaclient/latest/cli/index.html (стр.); форматы cliff: https://docs.openstack.org/cliff/latest/user/demoapp.html (стр.)
- Официальных MCP у VK Cloud и Selectel не найдено; неофициальные листинги (mcp.so, glama.ai) — сниппеты, в анализе не используются.

### Hetzner `hcloud`

- Справочник команд из репозитория: https://github.com/hetznercloud/cli (каталог `docs/reference/manual`: `hcloud_server_create.md`, `server change-type`, `rebuild`, `create-image`, `request-console`, `ssh`, `metrics`, `firewall create/add-rule/apply-to-resource`, `network create/add-subnet`, `floating-ip create`, `load-balancer create`, `context`) (стр.)
- MCP: поиск по организации hetznercloud пуст — https://github.com/orgs/hetznercloud/repositories?q=mcp (стр.)

### DigitalOcean `doctl`

- `doctl compute droplet create`, `droplet-action` (power, resize, rebuild, snapshot, password-reset), `volume-action resize`, `compute firewall create`, `reserved-ip create`, `load-balancer create`, `vpcs create`, `compute ssh`, `monitoring alert`, глобальные флаги, `auth`: https://docs.digitalocean.com/reference/doctl/reference/… (стр.)
- MCP: https://github.com/digitalocean-labs/mcp-digitalocean (стр.; droplet-инструменты в `pkg/registry/droplet/README.md`), настройка и удалённые серверы: https://docs.digitalocean.com/reference/mcp/configure-mcp/ (стр.)

### Terraform / OpenTofu

- `terraform plan`: https://developer.hashicorp.com/terraform/cli/commands/plan, `apply`: https://developer.hashicorp.com/terraform/cli/commands/apply, state: https://developer.hashicorp.com/terraform/language/state, `lifecycle`: https://developer.hashicorp.com/terraform/language/meta-arguments/lifecycle (стр.); `tofu apply`: https://opentofu.org/docs/cli/commands/apply/ (стр.)
- Оценка стоимости HCP Terraform: https://developer.hashicorp.com/terraform/cloud-docs/cost-estimation, редакции: https://developer.hashicorp.com/terraform/cloud-docs/overview (стр.); Infracost: https://github.com/infracost/infracost, https://www.infracost.io/docs/ (стр.)
- terraform-mcp-server: https://github.com/hashicorp/terraform-mcp-server, https://developer.hashicorp.com/terraform/docs/tools/mcp-server/reference (стр.; `ENABLE_TF_OPERATIONS`, toolsets, предупреждение о недоверенных клиентах)

### Не найдено или не подтверждено

- `cloud_init` в `POST /api/v1.1/vms` Cloud.ru — описан в спецификации, живого подтверждения нет (BACKLOG [05], 15.10: «ничем не подтверждено»); как сочетается с `user_data_template` образа — не описано.
- Нужна ли остановка ВМ для `PUT /api/v1/vms/{id}` со сменой `flavor_name` — спецификация не говорит.
- Спецификация сервиса тегов Cloud.ru (`GET /api/tags/v1/tags`) в реестр `scripts/cloudru-specs.py` не входит.
- Yandex Toolkit MCP: список инструментов и режим только чтения — README сервера отдаёт 404; согласие и защита от инъекций не документированы.
- DigitalOcean MCP: режим только чтения, согласие, защита от инъекций — не документированы.
- `yc`: нет `rebuild`, нет группы `monitoring`, нет подтверждения у `delete`; `hcloud`: нет NAT, логов, аудита; `doctl`: нет консоли и чтения метрик; `gcloud`: команды чтения метрик не найдено.
- Dry-run с ресурсами и превью цены — ни у одного CLI облака; только Terraform `plan` и HCP/Infracost.
- Статус GA у управляемого AWS MCP Server (руководство называет preview) и у Compute Engine MCP (пометки Preview нет, GA явно не заявлен).
