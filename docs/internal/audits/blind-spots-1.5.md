# Аудит слепых зон 1.5 (Fable, 28.10.2026): параметры платформы, которых evo не знает

Аудит назначил владелец 28.10.2026; ставит «CLI+MCP · 1.4.x и 1.5 — релиз».
Код — `main` на `82be4ab` (после #491). Только чтение: продуктовый код не
менялся, в облако сессия не ходила (пробы §10 — предложения для api-probe и
cloud-e2e по «да» владельца). Спецификации Cloud.ru скачаны
`scripts/cloudru-specs.py` 28.10.2026 (20 файлов; в репозиторий не кладутся).
Сведения о платформе, которых нет в публичной документации, — только в
закрытом реестре `evo-internal/discrepancies/cloudru-discrepancies.md` (пункты
CMP-18…22, IAM-6, NET-18, NET-19, K8S-15, DB-24, CAP-5, NLB-4, NLB-5, AR-5,
SEC-2, DOC-3, дополнение NET-17; все — гипотезы до пробы; пробы P34–P46).

**Вопрос аудита.** Какие параметры ресурсов Cloud.ru Evolution влияют на
работоспособность, цену или безопасность, но evo их не учитывает. Мерило —
сценарий владельца: пользователь Claude Desktop пишет «мне надо сделать
ребёнку простой портал, подними мне инфру», агент сам выбирает ресурсы и
создаёт их через `evo mcp`. Можно ли отдать это агенту без присмотра, чтобы
ресурс получился таким, каким его задумали (CLAUDE.md §6)?

**Метод.** Пройдены все 66 изменяющих инструментов карты контракта
(`docs/internal/contract-map.json`, `Mutating`) и инструменты чтения, которыми
агент узнаёт допустимые значения; спецификации — по полям. Для каждого
параметра платформы — пять проверок: (а) может ли агент его задать; (б) может
ли узнать допустимые значения инструментом чтения; (в) видит ли его в превью
до согласия вместе с ценой; (г) что evo выбирает по умолчанию и говорит ли об
этом; (д) отказывает ли невыполнимое до вопроса (ось 1 §6) или молча
подменяет. В таблицах: **да** / **нет** / **частично** (с пояснением); «—» —
неприменимо. Часть кода (DBaaS, k8s, Container Apps, Artifact Registry, сеть,
S3, секреты) прочитана тремя субагентами сессии; каждое их утверждение,
вошедшее в топ-риски, перепроверено по файлу и строке.

**Факты, гипотезы и мнения разделены.** Факт — проверено по коду `main`
(файл:строка) или по полю спецификации (имя поля). Гипотеза — поведение
живого API, которое спецификация не описывает; у каждой гипотезы — проба §10.
Мнение — оценка риска, версии и формы правки; решения — владельца.

Версии в таблицах: **1.5к** — до кандидата 1.5 (дёшево, тот же класс, что
уже принятые D49 и D50); **1.6**; **позже** (1.7+); **план** — уже есть
строкой плана. «D» — нужно решение с номером; «RELEASING» — ломающее по
`docs/RELEASING.md` (меняет код выхода, `reason` или ответ MCP без `--yes`),
выпускается в minor только по условиям 1–5.

Содержание:

1. [Резюме и топ-рисков](#1-резюме-и-топ-рисков)
2. [ВМ: flavor, образ, диск, сеть, цена](#2-вм-flavor-образ-диск-сеть-цена)
3. [Группы безопасности, floating IP, SNAT](#3-группы-безопасности-floating-ip-snat)
4. [Балансировщик, DNS, сертификаты, S3, секреты, VPC](#4-балансировщик-dns-сертификаты-s3-секреты-vpc)
5. [PostgreSQL, Kafka, Redis](#5-postgresql-kafka-redis)
6. [Kubernetes, Container Apps, Artifact Registry](#6-kubernetes-container-apps-artifact-registry)
7. [Сквозное: стек, списки, квоты, скиллы](#7-сквозное-стек-списки-квоты-скиллы)
8. [Хватает ли агенту инструментов: три задачи](#8-хватает-ли-агенту-инструментов-три-задачи)
9. [Класс D50, ломающее, решения D, версии](#9-класс-d50-ломающее-решения-d-версии)
10. [Пробы для api-probe и cloud-e2e](#10-пробы-для-api-probe-и-cloud-e2e)
11. [Источники](#11-источники)

## 1. Резюме и топ-рисков

**Вердикт (мнение).** Сценарий «подними мне инфру для портала» агенту без
присмотра отдать нельзя — не из-за согласия и уборки (там evo впереди всех,
`ops-gap-1.4.md`), а из-за того, что evo выбирает за агента и не показывает:
долю vCPU, доступность flavor и типа диска в зоне, минимум образа, публичный
API кластера, HTTPS-путь, которого в продукте нет. Ресурс создаётся, цена
названа, согласие получено — а портал работает на 10 % vCPU, балансировщик без
TLS и с нездоровой целью, приватная зона DNS ни к чему не привязана.

**Что подтвердилось из примера владельца 28.10.** Доля vCPU отдаётся API
полем `FlavorSchema.oversubscription` (`1:1`, `1:3`, `1:10`) и фильтром
списка, а не только именем (факт спецификации; что отвечает живой API — проба
P34). evo это поле не читает; `deploy_plan` без подходящего flavor молча берёт
`lowcost10-2-4`; у `provision` нет `cpu`/`ram`. Это FB15-1.

**Топ-рисков для сценария «агент без присмотра»** (мнение; порядок — по
произведению вероятности на цену ошибки):

| # | Риск | Находки | Класс |
|---|---|---|---|
| 1 | Flavor выбирается по vCPU и RAM без доли vCPU, GPU/spot, минимума образа и доступности в зоне; три разных правила подбора на три пути создания | FB15-1, 2, 3, 5, 15 | работоспособность, цена; D50 (FB15-2, 5) |
| 2 | HTTPS-пути для «портала» нет: NLB v2 без TLS, один listener, `lb_update` нет, а описания `cert_*` обещают «привязать к listener» | FB15-20 | работоспособность; документация |
| 3 | Превью `k8s_create` молчит о `kube_api_internet=true` и `cluster_sa_id`; превью `k8s_add_nodepool` — о `count=2` и `disk_gb=20`; Kafka `ui_enabled` уходит в запрос, но не в превью | FB15-35, 41 | дыра в согласии (условие 1 RELEASING) |
| 4 | `open_ports` считает порт «уже открытым» без учёта источника: правило из `10.0.0.0/8` гасит запрос на `0.0.0.0/0`, ответ — успех, сервис недоступен | FB15-16 | работоспособность; неверный результат, которого скрипт не видит |
| 5 | Flavor узлов и мастеров k8s без `purpose`, `flavorZoneState` (`SOLD_OUT`), `oversubscription`; прецедент — `gen-16-128 sold out` в 1.2.3 | FB15-40 | работоспособность; D50 |
| 6 | Секрет в `env` Container App хранится открытым текстом в конфигурации приложения и возвращается `ca_get`; тип `secret` у переменной API есть, evo его не шлёт | FB15-44 | безопасность |
| 7 | Приватная ВМ в зоне без SNAT: нет исходящего интернета, образы Cloud.ru при первой загрузке ждут зеркало apt; превью молчит | FB15-9 | работоспособность |
| 8 | `tg_create` берёт публичную (`direct_ip`) ВМ и ВМ в любом состоянии; группа безопасности цели не проверяется — балансировщик нездоров после согласия | FB15-21, 23, 47 | работоспособность |
| 9 | Диск: тип, пределы размера и скорость по размеру (`speed_limits`) не читаются; `disk_type` уходит в API как есть | FB15-6 | цена, работоспособность; D50 |
| 10 | Kafka и Redis без пользователей, ролей и топиков — кластер создан, подключиться нечем; PostgreSQL без восстановления из копии | FB15-34, 36 | работоспособность, безопасность (учётные данные вне согласия evo) |

Остальное — в таблицах; всего находок 49 (FB15-1…49), из них 11 — класс
«отказ до вопроса» (D50), 6 — класс «превью не говорит, что evo выбрало»
(условие 1 RELEASING, дыра в согласии), 17 — поля, которые списки уже
читают из API, но не отдают агенту (§7, FB15-49: одна правка класса).

## 2. ВМ: flavor, образ, диск, сеть, цена

Инструменты: `provision` (`evo vm create`), `deploy`, `deploy_plan` →
`deploy_apply`, шаг `vm` стека; чтение — `list_flavors`, `image_list`,
`image_get`, `list_zones`, `list_disk_types`, `list_subnets`,
`list_security_groups`, `vm_get`, `vm_list`.

### 2.1 Что отдаёт платформа о flavor (факты спецификации)

`GET /api/v1/flavors` → `FlavorSchema` (compute `openapi-v3.yaml`): кроме
`id`, `name`, `cpu`, `ram`, `free_tier`, которые evo читает
(`internal/provision/types.go:260–267`), в каждом flavor есть:

| Поле | Что значит (описание спецификации) |
|---|---|
| `oversubscription` | гарантированная доля vCPU: `1:1` — 100 %, `1:3` — 30 %, `1:10` — 10 % |
| `gpu` | число GPU (0–16) |
| `flavor_type.{display_group_type, is_spot, cpu_type, gpu_type, display_name}` | группа консоли (`general1:1`, `general1:3`, `general1:10`, `gpu`, `spot`, `custom`, `high_frequency`), прерываемость, модель CPU |
| `assured_bandwidth`, `maximum_bandwidth`, `pps` | гарантированная и максимальная полоса сетевых интерфейсов, пакетов в секунду (0 — не используется) |
| `availability_zones[].{availability_zone_id, enabled, state, available}` | в каких зонах flavor доступен; `state` ∈ `created, error_creating, error_deleting, creating, deleting` |
| `pci_device_limit`, `pci_device_available` | слоты PCI: предел числа дисков и интерфейсов |
| `display_name`, `description` | имя и описание для человека |

Фильтры того же запроса: `availability_zone_id`, `cpu`, `ram`, `gpu`,
`flavor_type`, `oversubscription`, `free_tier`, `is_spot`, `display_groups`,
`name`. Гипотеза владельца от 28.10 — «доля vCPU, похоже, есть только в
имени» — спецификацией опровергнута: доля отдаётся полем `oversubscription`
и у flavor, и у его типа, и доступна как фильтр списка. Что именно отвечает
живой API (все ли flavor несут поле, совпадает ли `lowcost10-*` с `1:10`) —
гипотеза, проба P34.

### 2.2 Таблица: ВМ

| № | Параметр | Платформа (поле) | (а) задать | (б) узнать | (в) в превью | (г) умолчание и сказано ли | (д) невыполнимое | Риск | Предложение (одна точка) | Версия |
|---|---|---|---|---|---|---|---|---|---|---|
| FB15-1 | Доля vCPU (oversubscription) | `FlavorSchema.oversubscription`, `flavor_type.oversubscription`, фильтр `?oversubscription=` | **нет**: у `provision`, `deploy`, `deploy_plan`, `vm` стека — только имя flavor или `cpu`/`ram_gb` (`deploy_plan`) | **нет**: `list_flavors` отдаёт `name, vcpus, ram_gb, free_tier, est_monthly_rub` (`internal/tools/tools_deploy.go:455–467`); `provision.Flavor` поля не читает (`types.go:260–267`) | **нет**: превью называет имя, vCPU, RAM и цену (`engine/create_preview.go:528–533`) — долю агент может только угадать по имени | `provision`/`deploy`: самый маленький flavor со словом `low` в имени (`engine.PickVMFlavor`, `create_preview.go:443–447`); `deploy_plan` без `cpu`/`ram_gb` — `lowcost10-2-4` (`deployplan/resolve.go:262`); с `cpu`/`ram_gb` — наименьший по vCPU и RAM без учёта доли (`:245–258`); `deploy` с compose — `sizer.Match` по vCPU и RAM (`sizer/sizer.go:29–50`). Описание `provision` говорит «smallest lowcost, not free-tier»; что это 10 % vCPU — нигде | — | **работоспособность, цена**: «портал» на автоподборе получает 2 vCPU с гарантией 10 % (гипотеза о `lowcost10` = `1:10`, P34); агент, попросивший 2 vCPU для 1С, получает 0,2 vCPU гарантии | читать `oversubscription`, `gpu`, `flavor_type.{display_group_type,is_spot}` в `provision.Flavor`; `list_flavors` отдаёт `cpu_share` (`"1:10"`), `gpu`, `spot`, `zones`; превью ВМ — строка «flavor lowcost10-2-4: 2 vCPU (гарантия 10 %), 4 ГБ»; аргумент `cpu_share` (enum из каталога, не из кода) у `provision`, `deploy`, `deploy_plan`, `vm` стека — одним правилом автоподбора (FB15-15) | показ — 1.5к (не ломающее: новые поля); аргумент и правило подбора — 1.6, D |
| FB15-2 | Доступность flavor в зоне | `FlavorSchema.availability_zones[].{enabled,state,available}`, `FlavorExtendedWithFlavorTypeSchema.available`, фильтр `?availability_zone_id=` | — | **нет**: `list_flavors` не называет зоны; список читается без фильтра зоны (`provision/adapter.go:155–178`) | **нет** | автоподбор выбирает из всех flavor каталога, зона не учитывается (`PickVMFlavor`, `selectFlavor`, `sizer.Match`) | **молча**: flavor, недоступный в зоне ВМ, проходит превью с ценой и спрашивает согласие; отказ платформы — после (гипотеза о коде ответа, P35) | **работоспособность**: вызов, который заведомо упадёт, после согласия (класс D15, D39, D50) | `ListFlavors` с `?availability_zone_id=` зоны ВМ (или фильтр по `availability_zones[].available`) — в одной точке адаптера; явный flavor не из зоны — отказ до вопроса той же функцией `engine.flavorNotFound` с текстом «недоступен в зоне X» | 1.5к (расширение D50; код без `--yes` 3 → 4 — RELEASING, те же условия, что D50) |
| FB15-3 | GPU и прерываемые (spot) flavor | `FlavorSchema.gpu`, `flavor_type.is_spot`, `display_group_type` ∈ {`gpu`, `spot`, …}, фильтры `gpu`, `is_spot` | **нет** (только по имени flavor) | **частично**: `list_flavors` ставит GPU в конец по подстроке `gpu` в имени (`provision/flavor_order.go:14–37`), поля `gpu`/`spot` не отдаёт | **нет** | автоподбор ВМ не исключает ни GPU, ни spot явно: `PickVMFlavor` берёт «самый маленький с `low`», `selectFlavor` и `sizer.Match` — самый маленький вообще (GPU-flavor большие, но spot могут быть малыми — гипотеза, P34); `pickK8sFlavor` читает `gpu` числом и GPU отвергает (`tools_k8s.go:160–173`) — у ВМ такого правила нет | — | **работоспособность, цена**: прерываемая ВМ под «порталом» или GPU-flavor по опечатке имени — после согласия | те же поля в `provision.Flavor`; автоподбор ВМ исключает `gpu > 0` и `is_spot` явно, как `pickK8sFlavor`; явный spot/GPU flavor — строка превью «прерываемая ВМ» / «GPU» | 1.5к |
| FB15-4 | Полоса сети и pps flavor | `assured_bandwidth`, `maximum_bandwidth`, `pps` | — | **нет** | **нет** | — | — | низкий для портала; **работоспособность** для сетевых нагрузок | `list_flavors` и превью показывают, когда поля не 0 | позже |
| FB15-5 | Минимум образа: `min_cpu`, `min_ram` против flavor | `PublicImageResponse.min_cpu`, `min_ram` (по умолчанию 1), как уже учитываемый `min_disk` | — | **да**: `image_get` отдаёт карточку с `min_cpu`, `min_ram`, `min_disk` (`tools_image_catalog.go:222–241`, `ImageDetail`); `evo image get` печатает «flavor — не меньше минимума выше» (`cli/images.go:271–274`) | **нет**: превью не сравнивает flavor с минимумом образа | ни один автоподбор и ни одно превью не сравнивает (`MinCPU`/`MinRAM` читаются только в `cli/images.go` и `image_create`); `min_disk` поднимает диск одним правилом `provision.BootDiskGB` (`provision/bootdisk.go:30–43`) — для vCPU и RAM такого правила нет | **молча**: образ маркетплейса с `min_cpu: 4` на автоподборе получает `lowcost10-2-4`; что ответит платформа (422 до создания или ВМ, которая не поднимется) — гипотеза, P36 (cloud-e2e) | **работоспособность**: продукты маркетплейса (1С, GitLab и т. п.) — главные кандидаты на «портал» и «базу для 1С» | одно правило `provision.FlavorFloor(image, flavors)` рядом с `BootDiskGB`: автоподбор берёт наименьший flavor ≥ минимума образа и пишет в превью «поднят до минимума образа»; явный flavor ниже минимума — отказ до вопроса (класс D50) | 1.5к (отказ — RELEASING, условия 1–5: согласие на заведомо падающий вызов) |
| FB15-6 | Тип диска: скорость, пределы, зоны | `PublicDiskTypeResponse.{speed_limits[]{size,limit_bw,limit_rate}, min_size, max_size, free_tier, availability_zones[].enabled}`; `GET /disk-types?availability_zone_id=` | **частично**: `deploy_plan disk_type` и `disk_create disk_type`; у `provision` и `deploy` — нет, в запрос идёт строка `"SSD"` (`tools.go:1977`; G6, строка плана 1.5 `--disk-type`) | **нет**: `list_disk_types` отдаёт только `id`, `name` (`tools_deploy.go:481–491`); `provision.DiskType{ID, Name}` (`types.go:214`) | **частично**: превью ВМ называет размер диска, тип — нет (`create_preview.go:540–541`); превью `disk_create` — размер и тип | тип `SSD`, иначе любой со «ssd» в имени, иначе первый по имени (`provision.PickDiskType`, `order.go:33–58`, D27); пределы типа и зоны не читаются; `disk_create` проверяет только константу 16384 (`tools_disk.go:109`) | **молча**: тип, выключенный в зоне, или размер вне `[min_size, max_size]` типа — отказ платформы после согласия (`WrongDiskTypeByIdExceptionSchema` 422 — спецификация); явный `disk_type`, которого нет, уходит в API как есть (`tools_disk.go:140–145`; строка плана G130 «`disk_create` с неизвестным типом») | **цена, работоспособность**: IOPS/полоса диска — главный параметр «базы для 1С»; HDD против SSD — 3 против 7 ₽/ГБ в тарифной карте evo | читать полный `DiskType`; `list_disk_types` отдаёт `min_size`, `max_size`, `speed_limits`, `zones`; планировщики `provision`/`deploy`/`disk_create` проверяют тип в зоне и размер в пределах типа до вопроса (минимум зависит от облака — чтение, как W6 `specStorage`, не правило схемы D36); превью — «SSD: до N МБ/с, M Мбит/с при таком размере» | чтение и превью — 1.5к; отказ до вопроса — 1.5к (RELEASING) |
| FB15-7 | Шифрование, общий и read-only диск | `PublicDiskCreateRequest.{encrypted, shared, readonly}` (по умолчанию `false`); у диска в теле создания ВМ (`VmCreateNewDiskItem`) таких полей нет | **нет** (`provision.CreateDiskReq` — `name, size, disk_type_*, availability_zone_id, project_id`, `provision/disk.go:18–25`) | — | — | незашифрованный, не общий | — | **безопасность** (шифрование at rest для комплаенса); низкий для портала | `encrypted` (и `shared` по запросу) у `disk_create`; загрузочный диск ВМ зашифровать при создании API не даёт — пункт реестра CMP-20 (гипотеза: есть ли иной путь) | 1.6 |
| FB15-8 | Квоты проекта | `GET /v3/quotas/usages?subject_type_codes=QUOTA_SUBJECT_TYPE_PROJECT&subject_ids=P` → `v3QuotaUsage{resource_type_code, individual_limit, individual_usage}`; коды и единицы — `GET /v3/quotas/resourceTypes` (`v3QuotaResourceType{code, name, product_name, unit}`) | — | **частично**: `org_quotas` читает использование (`tools_admin.go:236–288`), но отдаёт сырые `resource_type_code` без имени и единицы (`/resourceTypes` не читается) и на 403 отвечает `unavailable` | **нет**: ни одно превью не говорит «vCPU: занято 6 из 8, этой ВМ нужно 2» | превью квоту не читает; `provision.IsQuotaError` — разбор текста ошибки после отказа (`provision/quota.go`), используется для floating IP в `deploy` (`engine/deploy.go:1038–1055`) и `deploy_apply` | **после согласия**: исчерпанная квота — ошибка API после «да»; в стеке — после уже созданных шагов | **работоспособность, деньги** (полустек) | `engine.QuotaCheck` в `completePreview` рядом с `NetworkCheck`: чтение best effort по правам ключа; известная исчерпанная квота — отказ до вопроса (класс D50); `org_quotas` отдаёт `name`, `unit`, `product_name` | чтение — 1.6; отказ — 1.6, D; проба P37 (видит ли ключ проекта свои квоты) |
| FB15-9 | Исходящий интернет приватной ВМ (SNAT в зоне) | `GET /api/v1/nat-gateways` (есть ли шлюз в зоне); описание `snat_create`: SNAT обслуживает маршрутизируемые подсети зоны | `no_public_ip` у `provision`, `external_ip=false` у `deploy_plan`, `private` у `vm` стека | **да**: `snat_list` | **нет**: превью приватной ВМ не говорит, есть ли в зоне SNAT | ВМ без floating IP и без проверки SNAT; комментарий кода: образы Cloud.ru при первой загрузке ходят в интернет за зеркалом apt, иначе 10+ минут тайм-аутов до старта sshd (`tools.go:1990–1993`); `provision`, `deploy_plan`, `resolveNetwork` `ListNATGateways` не вызывают (grep по `internal/tools/tools.go`, `engine/`, `deployplan/`) | **молча**: приватная ВМ в зоне без SNAT создаётся, оплачивается, долго грузится и не ставит пакеты | **работоспособность**: типичная цель балансировщика (`tg_create` требует приватную ВМ) | `NetworkCheck` получает `no_egress: true` при `no_public_ip` и отсутствии шлюза в зоне (одна точка `engine.checkNetwork`, одно чтение `ListNATGateways`); строка превью «исходящего интернета у ВМ не будет: в зоне нет SNAT (snat_create)»; та же проверка у группы узлов k8s (§6) | 1.5к (предупреждение, не ломающее) |
| FB15-10 | Цена flavor в `list_flavors` и тарифная карта | `POST /projects/{id}/price-calculation` (ВМ) — используется в превью ВМ (`deployplan.QuoteVM`) | — | **частично**: `list_flavors` даёт `est_monthly_rub` только из тарифной карты `vcpu×0,60 + ram×0,40 ₽/ч + 10 ГБ SSD` (`pricing.EstimateFor`, `pricing/pricing.go:137–177`; `tools_deploy.go:462`) — одна цена для `lowcost10-2-4` и flavor `1:1` 2-4; `note` честно называет это оценкой | **да** для ВМ (API, при сбое — карта); для диска отдельно — только карта (`create_preview.go:218–221`) | — | — | **цена**: агент, выбирающий flavor по `est_monthly_rub`, не видит разницы между долями CPU (гипотеза о реальных тарифах — P34: сравнить `price-calculation` для `1:10` и `1:1` одного размера) | в 1.7 (D17) — цена калькулятора по flavor; до того: `cpu_share` в `list_flavors` (FB15-1) и в `note` фраза «карта не различает долю vCPU»; у стека — своя третья карта (`stack/store.go:799–809`: ВМ 800/1700 ₽, PG 4900 ₽/vCPU) — свести к `pricing` одной точкой | 1.7 (D17); фраза и сведение карт — 1.5к |
| FB15-11 | Прочие поля создания ВМ | `placement_group_id/name`, `tag_ids/names`, `description`, `cloud_init`, `interfaces[].{security_groups, ip_address}` (свой SG, фиксированный внутренний IP), несколько `interfaces[]` и `disks[]` (диски данных), `attach_external_ip_name` | **нет** у всех (`provision.CreateVMRequest`, `types.go:98–131`) | — | — | — | — | известные разрывы G1, G2, G4, G7, G10, G11 (`ops-gap-1.4.md`), в плане 1.5–1.9; новое — группа размещения (анти-аффинити для HA) и фиксированный внутренний IP (нужен агенту, чтобы прописать адрес в конфиг до создания) | по плану; `ip_address` и `placement_group` — строкой плана 1.9 | план |
| FB15-12 | Зона: закрыта, по умолчанию, free tier | `PublicAvailabilityZoneResponse.{enabled, default}`, `?free_tier_default=` | да | **да**: `list_zones` отдаёт объект зоны целиком (`tools.go:2952–2968`) | да | закрытая зона — отказ до вопроса (`provision.ClosedZoneError`, `tools.go:1893`, `resolve.go:62`); без зоны — отказ (D15, D48) | да | — | закрыто; замечаний нет | — |
| FB15-13 | Состояние и тип подсети | `Subnet.{state, type}`: `technical`, `zone_technical`; `state` ≠ `created` | `subnet_id` | **частично**: `list_subnets` отдаёт `id, name, availability_zone_id` — без `type`, `state`, `default`, `routed_network`, `vpc_id`, `subnet_address`, `dns_servers` (`tools_deploy.go:498–507`), хотя `provision.Subnet` их читает (`types.go:142–162`) | **частично**: предупреждение «нет DNS» есть (`NetworkCheck`) | техническая подсеть и подсеть не в `created` получают ранг −1, но остаются кандидатами: единственная подсеть зоны с рангом −1 выбирается (`provision/subnet.go:14–45`) | **молча** (гипотеза: примет ли платформа ВМ в технической подсети, P38) | **работоспособность** | `PickSubnet` не берёт ранг < 0; `list_subnets` отдаёт `type`, `state`, `default`, `routed_network`, `vpc_id`, `subnet_address`, `dns` — иначе агент не выберет подсеть для балансировщика (`routed_network`) и не поймёт, в какой VPC встанет кластер (FB15-49) | 1.5к (поля), 1.6 (ранг) |
| FB15-14 | Что создано: flavor и диски ВМ после создания | `PublicVmResponse.{flavor{…}, disks[], availability_zone, tags, placement_group, locked}` | — | **частично**: `vm_get` отдаёт `id, name, state, private_ip, public_ip, interfaces` (`tools_vm.go:76–83`), `vm_list` — `id, name, state, zone, public_ip, created_time` (`:121–138`); flavor, диски, зона у `vm_get` — нет | — | — | — | низкий; агент не может сверить созданное с задуманным (ось 2 §6) | `vm_get` отдаёт `flavor{name, cpu, ram, cpu_share}`, `disks[]`, `zone`, `locked` | 1.6 |
| FB15-15 | Три правила автоподбора flavor | — | — | — | — | `engine.PickVMFlavor` (provision, deploy с заданным flavor), `deployplan.selectFlavor` (deploy_plan: пропускает free tier, запасной `lowcost10-2-4`), `sizer.Match` (deploy с compose: free tier **не** пропускает — `sizer/sizer.go:29–50`, тест `sizer_test.go:15–62` ждёт выбора `free`); разные ответы на один каталог | free tier ограничен одной ВМ (комментарий `create_preview.go:419`): второй `deploy` с compose на free-flavor — отказ платформы после согласия (гипотеза, P39) | **работоспособность**; нарушение §2 (одна точка решения) | одна функция `provision.PickFlavor(flavors, want PickWant)` для всех путей, с правилами FB15-1…3, 5 внутри; поведение по умолчанию прежнее, кроме явных исключений (spot, GPU, free tier, минимум образа) | 1.6, D (меняет выбор между равными — «не ломающее» по букве D27, но меняет создаваемый ресурс: «Миграция») |

### 2.3 Паритет

Все предложения — поле реестра и `CLISpec` одной точкой: новые поля
`provision.Flavor` и `provision.DiskType` читаются адаптером; `list_flavors`,
`list_disk_types`, `list_subnets`, `vm_get` отдают их на обеих поверхностях
(`--json` = ответ инструмента); правило подбора — одна функция `provision`;
отказы — в планировщике (D35), без частных случаев по имени инструмента.
Различие терминала (вопрос о зоне в TTY, строка плана 1.5) не затрагивается.

## 3. Группы безопасности, floating IP, SNAT

Инструменты: `open_ports`, `close_ports`, `fip_attach`, `release_floating_ip`,
`snat_create`, `snat_delete`; чтение — `list_security_groups`,
`list_floating_ips`, `snat_list`.

| № | Параметр | Платформа (поле) | (а) | (б) | (в) | (г) | (д) | Риск | Предложение | Версия |
|---|---|---|---|---|---|---|---|---|---|---|
| FB15-16 | «Уже открыто» у `open_ports` — без источника и диапазонов | правило: `direction, ip_protocol, port_range, remote_ip_prefix, remote_security_group_id` | `cidr` есть | `list_security_groups rules=true` отдаёт правила с `remote_ip_prefix` | превью называет `will_open`, `already_open` | — | **молча**: ключ «уже открыто» — `протокол + порт` без `remote_ip_prefix` (`tools_deploy.go:563–572`): правило `tcp 80:80` из `10.0.0.0/8` гасит запрос `cidr=0.0.0.0/0`, ответ — успех, ничего не создано, порт снаружи закрыт; правило `any` или диапазон `1:65535` не совпадает с ключом — создаётся дубль | **работоспособность**; неверный результат с кодом 0 (условие 1 RELEASING) | ключ `протокол + порт + источник`; диапазон и `any`, покрывающие порт, — «уже открыто шире» строкой превью; та же функция у `close_ports` (она уже учитывает `cidr`, D37) | 1.5к (RELEASING: ответ «открыто» → «создано правило»; условие 1) |
| FB15-17 | SNAT: VPC, обслуживаемые подсети, второй шлюз в зоне, цена | `PublicNATGatewayCreateRequest.{vpc_id, description}` (спецификация 28.10 — поле есть), ответ `nat_rules[]{subnet_id}`, `floating_ip`, `vpc_id`; 422 `ExternalNetHasNoAvailableIps` | **нет**: `vpc_id` не передаётся (`provision/nat.go:29–35`; комментарий «дополнительных полей нет» — спецификация теперь их описывает) | **частично**: `snat_list` отдаёт `id, name, nat_type, status, zone` — без `vpc_id`, `floating_ip`, `nat_rules` (`tools_snat.go:67–78`) | превью — имя, тип, зона; цены нет (`tariff_url`), у стека — 1000 ₽ своей картой | VPC — выбор платформы (реестр NET-17: по умолчанию проекта); не сказано | **после согласия**: второй шлюз той же зоны (строка плана G130) и исчерпанный пул публичных адресов | **работоспособность** (не та VPC), **цена** | `vpc_id` у `snat_create` (умолчание — VPC подсети зоны, названа в превью); планировщик читает `ListNATGateways` зоны до вопроса: шлюз есть — `USAGE` с его id (класс D20); `snat_list` отдаёт `vpc_id`, `floating_ip`, `nat_rules` | 1.5к (список, превью), 1.6 (аргумент; отказ — RELEASING) |
| FB15-18 | Floating IP: зона, детач | `PublicFloatingIpResponse.availability_zone`; `FloatingIpUpdateRequest.detach`; 422 `InterfaceAlreadyConnectedAnotherFloatingIp` | `fip_attach`; детача нет (G15, строка плана 1.5) | **частично**: `list_floating_ips` не отдаёт зону (`provision.FloatingIP` её не читает, `types.go:450–463`) | да | — | занятый адрес, адрес шлюза, ВМ с адресом — до вопроса (W6, D31); **зона адреса против зоны ВМ не проверяется** (гипотеза о коде ответа, P40) | низкий/средний | читать `availability_zone` адреса; `fip_attach` отказывает до вопроса на чужую зону; `fip_detach` — по плану | 1.5к (поле), план |
| FB15-19 | Правила группы: направление, `icmp`/`any`, диапазон, источник-группа, `default` | `direction` egress, `ip_protocol` ∈ {tcp, udp, icmp, any}, `port_range` диапазон, `remote_security_group_id/name`; `SecurityGroup.default` | **нет**: только ingress tcp/udp одиночные порты (`tools.go:271–291`) | **частично**: `SGRule` не читает `remote_security_group_id` (`types.go:224–233`) — правило «от группы балансировщика» в списке выглядит как правило без источника; `list_security_groups` не отдаёт `default` (`tools_deploy.go:516–535`) | — | — | — | известный разрыв G7 (`sg_rule_add`, 1.6); новое — поле источника-группы в чтении | читать `remote_security_group_id` в `SGRule`; `list_security_groups` отдаёт `default` | 1.5к (чтение), план |

## 4. Балансировщик, DNS, сертификаты, S3, секреты, VPC

| № | Параметр | Платформа (поле) | (а) | (б) | (в) | (г) | (д) | Риск | Предложение | Версия |
|---|---|---|---|---|---|---|---|---|---|---|
| FB15-20 | HTTPS и несколько listener у балансировщика | NLB `swagger.yaml` v2: `v2Listener{name, port, target_port}` — поля протокола и сертификата **нет**; `v2Rule.listeners[]`, `rules[]` — массивы; `PATCH /v2/networkLoadBalancers/{id}` есть | **нет**: один `rule` с одним listener (`tools_nlb.go:360–368`); `lb_update` нет | `lb_get` сырой | `listener: "80→80"` | — | — | **работоспособность**: «портал с HTTPS» через NLB невозможен; описания `cert_issue_le` («issue → bind to the LB listener») и `cert_list` («for an HTTPS load balancer») (`tools.go:1179, 1195`) обещают привязку, которой нет ни в продукте, ни в evo — агент выпустит сертификат и пойдёт гадать | описания `cert_*` и `lb_create` говорят прямо: «L4 TCP, TLS завершает приложение на ВМ»; `lb_create listeners[]` (80 и 443 одним вызовом) и `lb_update` — 1.6; вопрос платформе (DOC-3 реестра): где терминируется TLS в Evolution | описания — 1.5к; инструменты — 1.6 |
| FB15-21 | Цель балансировщика: тип интерфейса и состояние ВМ | `v2TargetVMSpec.interface_id`; `VMInterface.type` ∈ {`regular`, `direct_ip`}; описание `provision`: «private VM … the valid load-balancer target» | `interface_id` | `vm_get` отдаёт `interfaces[].type` (`tools_vm.go:52–58`); `tg_get` сырой | превью `tg_create` — только число целей | берётся primary-интерфейс, затем первый с id, тип не проверяется (`tools_nlb.go:23–38`) | **молча**: публичная (`direct_ip`) ВМ и ВМ не в `running` принимаются в превью; комментарий кода говорит, что NLB отвергает `direct_ip` — отказ после согласия или нездоровая цель (гипотеза, P41) | **работоспособность** | планировщик `tg_create` читает ВМ до вопроса: `direct_ip` — `USAGE`, не `running` — предупреждение; превью называет цели с интерфейсом и зоной | 1.5к (RELEASING, класс D50) |
| FB15-22 | Проверка доступности: тип, пороги | `v2HealthCheck.{tcp, http{path}, timeout, interval, unhealthy_threshold, healthy_threshold, disabled}` | **нет**: `tcp{}`, 5 с / 10 с / 3 / 3 зашиты (`tools_nlb.go:364–367`); только `hc_port` | `lb_get` сырой | превью — только `health_check_port` | не сказано | — | средний: HTTP-приложение, падающее на readiness, для L4 «здорово» | `hc_http_path` (переключает на `http`), пороги — аргументами; превью называет тип и пороги | 1.6 |
| FB15-23 | Группа безопасности цели пропускает `target_port` от балансировщика | compute SG цели; описание документации Cloud.ru о подсети балансировщика — не проверялось | — | `list_security_groups rules=true` | **нет**: `lb_create` группу цели не читает (`tools_nlb.go:296–415`) | — | **молча**: приватная ВМ стека создаётся без портов (`stack/store.go:655–686` — `provision` без `ports`), `target_port` никто не открывает — балансировщик нездоров после согласия (гипотеза о поведении платформы, P42) | **работоспособность** (стек `vm` + `nlb`) | превью `lb_create` читает группу цели и предупреждает, если `target_port` закрыт; шаг `vm` стека получает `ports: [target_port]` с источником — подсетью балансировщика (когда появится `source`, G1) | 1.6 |
| FB15-24 | Приватная зона DNS без VPC; токен подтверждения | `v1CreatePrivateZoneRequest.linkedVpcIds[]`; `v1PublicZone.{verificationToken, confirmState}`; `PATCH` зон есть | **нет**: тело — `project_id, name, domain, description` (`tools_dns.go:224–227`) | `dns_zones` отдаёт `confirm_state`, `delegated`, `valid`, но не `verificationToken` (он в сыром `dns_zone_get`); ответ `dns_zone_create` говорит «dns_zones … return a verification_token» (`tools_dns.go:235`) — не тот инструмент | — | приватная зона создаётся **ни к одной VPC не привязанной** (факт тела запроса; что делает платформа — гипотеза, P43); `dns_zone_update` нет | — | **работоспособность** (внутренние имена не разрешаются) | `linked_vpc_ids` у `dns_zone_create type=private` (умолчание — VPC подсети зоны профиля, названа в превью); `dns_zones` отдаёт `verification_token`; текст ответа — на `dns_zone_get` | 1.5к (поле и текст), 1.6 (аргумент) |
| FB15-25 | Выпуск Let's Encrypt: делегирована ли зона | `v1ErrorCode NOT_DELEGATED`; `dns_zones.delegated` | — | `dns_zones` | превью офлайн: только текст-предупреждение (`tools_certmanager.go:70–84`) | — | **после согласия**: `dns_names` не сверяются с делегированными зонами; занятое имя — тоже после согласия (REL-11, строка плана K1 — перевод `cert_issue_le` на `gated` в 1.5) | **работоспособность**: для свежего домена заведомо падающий вызов — обычный случай | планировщик `cert_issue_le` (K1): зона каждого `dns_names` найдена в `dns_zones` и `delegated` — иначе отказ до вопроса | план (K1, 1.5) |
| FB15-26 | Значение записи DNS, TTL 0 | `values[]`, `ttl`, `wildcard`, типы `gslb_*` у приватных зон | `value`/`values`, `ttl ≥ 0` | — | да | 300 | **после согласия**: формат значения (IP для A, FQDN для CNAME) не проверяется; `ttl: 0` схема пропускает (гипотеза об ответе API) | низкий/средний | правило значения по типу в планировщике (A — IPv4, AAAA — IPv6, CNAME/MX/NS — имя); `ttl` минимум по пробе | 1.6 |
| FB15-27 | S3: класс хранения, публичный доступ, версии, жизненный цикл, регион, бакет до `s3_put` | minio-go: `PutObjectOptions.StorageClass`, политики бакета, versioning, lifecycle; `MakeBucketOptions.Region` | **нет** для всех, кроме `region` и `content_type` | `s3_buckets` — имя и дата; `s3_ls` — ключ, размер, время (класса нет) | нет; цены нет (`tariff_url`) | класс — умолчание платформы (гипотеза: STANDARD); регион — профиль, иначе `ru-central-1`, в превью не назван | **после согласия**: бакета нет (превью `s3_put` офлайн, строка плана дельта-аудита 1.3.0), чужой регион | **цена** (класс невидим), **работоспособность** («статический сайт» в бакете без публичного доступа не сделать) | `storage_class` у `s3_put` и в `s3_ls`; `s3_bucket_policy` (public-read) — отдельным решением ИБ; бакет и регион — в планировщике до вопроса | 1.6; бакет до вопроса — план |
| FB15-28 | Секреты: ключ KMS, папка, метки, версии | `corev2CreateSecretRequest.{kmsKeyId, labels}`, `/v2/folder`, `v2VersionState` | `kms_key_id`; меток нет | **нет** списка ключей KMS; `secret_list` не отдаёт `kmsKeyId`, `labels`, `primary` (сырой `secret_get` — да) | `kms_key_id` в превью нет (`willPreview(…, nil, nil)`, `tools_secret.go:127`) | ключ платформы по умолчанию; не сказано | **после согласия**: неверный `kms_key_id`; секрета нет у `secret_add_version` (превью офлайн); папка — гипотеза (создаётся ли сама, P44) | средний (**безопасность**: не тот ключ) | `kms_key_id` в превью; `secret_add_version` читает секрет до вопроса (K1); `secret_list` отдаёт `kms_key_id`, `labels`; значение секрета стека лежит открытым в файле задания до шага (`stack/store.go:351–404`) — строка плана G130-30 | 1.5к (превью, поля), план |
| FB15-29 | VPC без подсети; адресные пулы | `CreateVPCRequest{projectId, name, description}` (CIDR нет), `/v1/vpcs/{id}/addressPools`, compute `POST /api/v1/subnets` (`PublicSubnetCreateRequest{vpc_id, subnet_address, prefix_length, routed_network, dns_servers, default}`) | **нет** `subnet_create` (G7, 1.6); пулов нет | `list_subnets` без `vpc_id` (FB15-13) | — | — | `vpc_create` даёт VPC, в которую evo ничего не положит: `lb_create` отвечает `CONFIG_INVALID` «создайте подсеть в консоли» (`tools_nlb.go:533–538`) | **работоспособность** | по плану 1.6 (`subnet_create`); `vpc_create` в превью говорит, что подсеть нужна отдельно; `vpc_get` отдаёт пулы | план; текст — 1.5к |
| FB15-30 | Статический маршрут: существование next hop | `RouteNextHop{virtualMachine, vip, magicRouter, ip{address, announce}}` | три вида; `ip` не поддержан | `vpc_routes` | аргументы как есть | — | **после согласия**: интерфейс/VIP не проверяются на существование и принадлежность VPC (G130: «`vpc_route_create` с чужим next hop» — строка плана) | средний | план | план |

## 5. PostgreSQL, Kafka, Redis

Общее (факты): ни у одного превью DBaaS нет цены — `CreatePreview.Price()`
считает только ВМ и диск, остальным даёт `tariff_url`
(`create_preview.go:212–233`); у стека — своя карта констант
(`store.go:799–809`). `list_subnets` не отдаёт `vpc_id`, `type`, `state`
(FB15-13), поэтому агент не узнаёт, в какой VPC встанет кластер. Явный
`subnet_id`, которого нет в проекте, не отвергается: `pickDBaaSSubnet`
передаёт его с зоной профиля (`tools_kafka.go:195–204`), отказ — платформы,
после согласия. В спецификациях трёх сервисов **нет** групп безопасности,
публичного доступа, шифрования, меток, защиты от удаления, автороста
хранилища, типа диска и IOPS, срока поддержки версии — это не пробел evo,
но агенту стоит сказать, что этих ручек нет (скилл).

| № | Параметр | Платформа (поле) | (а) | (б) | (в) | (г) | (д) | Риск | Предложение | Версия |
|---|---|---|---|---|---|---|---|---|---|---|
| FB15-31 | Спецификация: цена, `flavor_type`, `allow_primary_standby`, предел меню | PG `Specification{cpu, memory, deployment_mode, flavor_type (MEMORY/CPU/BALANCE), allow_primary_standby, max_hosts, min_storage_gb}`; Kafka/Redis аналогично | `specification_id` или `cpu`+`ram` | `pg_specs` отдаёт `flavor_type`, `max_hosts`, но не `allow_primary_standby` (grep: поля нет в `tools_pg.go`); компактное меню `need_specification` без `flavor_type`; меню и `pg_specs` обрезаны до 12 самых дешёвых (`specMenuCap`, `tools_pg.go:26`, `specifications_truncated`); цены на спецификацию нет нигде | да (cpu, memory_gb, deployment_mode); цены нет | нет умолчания — отказ с меню (хорошо) | да (`USAGE`) | **цена, работоспособность**: «база для 1С» (память, HA, business) может не попасть в 12 строк; агент не отличит `MEMORY` от `BALANCE` | `allow_primary_standby` и `flavor_type` в меню; `pg_specs` с аргументом `min_cpu`/`min_ram` вместо обрезки; цена спецификации — D17 (1.7) | 1.5к (поля), 1.7 (цена) |
| FB15-32 | PG: расписание и срок хранения копий в превью, CRON | `backup{schedule, retention_policy_days}` (спецификация: без `@`, не чаще раза в 6 ч) | `backup_schedule`, `backup_retention_days`, `no_backup` | — | **нет**: `will_create` несёт только `backup_enabled` (`tools_pg.go:417, 723`); `0 3 * * 0` и 14 дней в превью не названы | умолчания не сказаны (описание схемы — да) | **после согласия**: CRON не проверяется (гипотеза о коде ответа, P45) | **цена** (хранение копий), **безопасность** (копии нельзя включить потом — сказано) | `backup_schedule` и `backup_retention_days` в `will_create` и превью; формат CRON — правило схемы (D36) после пробы | 1.5к |
| FB15-33 | PG: пулер, WAL-диск, локали, окно обслуживания, логи | `pooler_config{enabled, pool_mode, parameters}` (не переключается после создания — спецификация), `storage.pg_wal_gb`, `initial_database_lc_collate/_lc_ctype` (`/v1/versions/{v}:locales`), `maintenance_window`, `logging` | **нет** (кроме локалей у `pg_db_create`) | `pg_get` сырой — после создания | нет | умолчания платформы молча: пулер выключен, локаль `C`, WAL по умолчанию; «standard нельзя масштабировать после создания» (текст спецификации у `specification_id`) в превью нет | — | **работоспособность** (локаль и пулер — навсегда) | `pooler`, `wal_gb`, `lc_collate`/`lc_ctype`, `maintenance_window` у `pg_create`; превью называет умолчания и «standard не масштабируется» | 1.6 |
| FB15-34 | PG: восстановление, масштаб, пауза | `recovery_spec{cluster_id, pitr, backup_id}` при создании; `PATCH /v1/clusters/{id}`, `:suspend`, `:resume`, `:switchover`, `:storageInfo` (`read_only_mode`) | **нет инструментов** (`postgresql/client.go` их не вызывает) | `pg_backups` — да | — | — | — | **работоспособность, деньги**: копии делаются, но не восстанавливаются через evo; диск, заполненный до read-only, не расширить; кластер не остановить на ночь | `pg_restore` (новый кластер из копии/PITR), `pg_resize`, `pg_suspend`/`pg_resume` — по классу `vm_stop` (превью, согласие, `already_*`) | 1.6–1.7 |
| FB15-35 | Kafka: `ui_enabled` в запросе, но не в превью | `CreateClusterRequest.ui_enabled` (веб-UI кластера) | `ui_enabled` | `kafka_get` сырой (`kafka_ui_host`) | **нет**: `will_create` и превью без `ui_enabled` (`tools_kafka.go:387–394`), в тело уходит (`:402`) | `false` | — | **дыра в согласии**: включается веб-интерфейс, о котором превью не сказало (условие 1 RELEASING) | `ui_enabled` в `will_create` и превью (`willPreview` keys) | 1.5к |
| FB15-36 | Kafka/Redis: пользователи, роли, топики | Kafka `/topics` (`num_partitions`, `replication_factor`), `/users` (`granted_roles`), `/roles`; Redis `/users`, `/roles` | **нет инструментов** (`dbaas/client.go`) | нет | — | — | — | **работоспособность, безопасность**: кластер создан, подключиться нечем — учётные данные выдаются в консоли, вне согласия и маскирования evo; пустые `granted_roles` = «ничего нельзя» (спецификация) | `kafka_topic_create`, `kafka_user_create`, `redis_user_create` (пароль — по правилу секретов) — новые create с инвариантами трёх осей | 1.6 |
| FB15-37 | Redis: устаревшее поле хранилища, параметры, умолчание 3 хоста | `storage_gib` (актуальное), `storage_gb` (deprecated); `parameters` (политика вытеснения, персистентность — гипотеза о списке); `type` MASTER_REPLICA = мастер + 2 реплики | `storage_gb` уходит deprecated-полем (`tools_redis.go:190`; api-contract §4.11); `parameters` нет | `redis_specs` | да: `hosts_per_shard`, предупреждение «Single-AZ» | MASTER_REPLICA — 3 оплачиваемых хоста по умолчанию; превью называет хосты, описание — нет | — | **цена** (умолчание — самое дорогое из возможных), риск отключения deprecated-поля | `storage_gib` сейчас; описание `redis_create`: «MASTER_REPLICA — 3 хоста, STANDALONE — 1»; `parameters` — 1.6 | 1.5к |
| FB15-38 | DBaaS: версия и чужая подсеть до вопроса | `version`/`version_id`, `subnet_ids` | да | `pg_versions`, `kafka_versions`, `redis_versions` — сырой список | да | «последняя» = последний элемент списка (порядок — гипотеза) | **после согласия**: версия не сверяется со списком; `subnet_id` не из проекта проходит (см. выше); Redis `subnet_ids[]` роняет бэкенд (DB реестра) | **работоспособность** | планировщик сверяет версию и подсеть со списками (класс D50) | 1.5к (RELEASING) |
| FB15-39 | Списки кластеров без ключевых полей | PG `Cluster.{instances, storage, subnet_ids, backup, connection_string}`; Kafka `{storage_gib, kafka_hosts, kafka_ui_host, topic_partitions}`; Redis `{storage_gib, redis_hosts, zone_ids}` | — | **частично**: `pg_list`, `kafka_list`, `redis_list` отдают `id, name, status, version, …` без хостов, хранилища, подсетей (`tools_pg.go:117–137`, `tools_kafka.go:259–264`, `tools_redis.go:43–47`); `*_get` — сырой объект | — | — | — | низкий; `evo-cleanup` и агент не видят, что оплачивается | FB15-49 | 1.5к |

## 6. Kubernetes, Container Apps, Artifact Registry

| № | Параметр | Платформа (поле) | (а) | (б) | (в) | (г) | (д) | Риск | Предложение | Версия |
|---|---|---|---|---|---|---|---|---|---|---|
| FB15-40 | Flavor k8s: назначение, доступность в зоне, доля vCPU | mk8s `FlavorConfiguration.{purpose (CONTROLPLANE/DATAPLANE), oversubscription, infraType, availabilityZones[].flavorZoneState (AVAILABLE/SOLD_OUT)}`, `ProductConfiguration.{zones[].availableFlavors, diskTypes[]{name, zones}}` | `flavor` или `cpu`/`ram_gb` | **нет**: `mk8s.ProductConfig` читает только `flavorId, name, cpu, ram, gpu` (`mk8s/client.go:170–193`); `zones[].availableFlavors` читается, но нигде не используется; `k8s_options` отдаёт `name, cpu, ram` без GPU-flavor (`tools_k8s.go:105–124`) | flavor, cpu, ram — да; цены нет | самый маленький без GPU с cpu ≥ 2, ram ≥ 4 — одна функция для мастеров и узлов (`pickK8sFlavor`, `tools_k8s.go:153–185`, D29); `purpose` и зона не учитываются | **молча**: flavor для data plane может уйти мастерам и наоборот; flavor `SOLD_OUT` в зоне или не из `availableFlavors` зоны уходит в запрос — отказ после «да» (прецедент: `gen-16-128 sold out`, cloud-e2e 1.2.3, реестр K8S-12) | **работоспособность, цена** | читать поля; `pickK8sFlavor` фильтрует по `purpose` и `flavorZoneState` зоны; `k8s_options` отдаёт `purpose`, `cpu_share`, `zones_available`, `disk_types` | 1.5к (RELEASING у отказа, класс D50) |
| FB15-41 | Превью `k8s_create` и `k8s_add_nodepool` молчат об умолчаниях | `networkConfiguration.kubeApiInternet` (по умолчанию `true` у evo), `identityConfiguration.clusterSaId`; `scalePolicy.fixedScale.count`, `machineConfiguration.disk.size` | да | — | **нет**: `resolved()` превью `k8s_create` — зона, подсеть, flavor, версия, мастера (`tools_k8s.go:138–145`); `kube_api_internet` и `cluster_sa_id` — только если переданы; у `k8s_add_nodepool` `count=2` и `disk_gb=20` читаются после гейта (`:587–588`) | публичный kube-API и сервисный аккаунт агента — молча; 2 узла по 20 ГБ — молча | — | **дыра в согласии** (условие 1 RELEASING): человек соглашается на кластер, не видя, что его API открыт в интернет, а узлов два | все умолчания — в `resolved()` и `will_create`; цена узлов — D17 | 1.5к |
| FB15-42 | Платформа выбирает молча: CNI, канал, CIDR, логи/мониторинг/аудит, шифрование | `networkConfiguration.networkPlugin{cilium, calico}` (required в спецификации, не шлётся — api-contract §5.16), `releaseChannel` (не задаётся), `podsSubnetCidr`, `servicesSubnetCidr`, `loggingService.enabled` (по умолчанию `true`), `monitoringService`, `auditService`, `keyManagementService`, `bootstrapManagedAddons`, `magicRouterAnnouncement` | **нет** | `k8s_get` сырой — после создания; CNI в ответе нет вовсе | нет | версия берётся из канала `STABLE`, а кластер подписан на канал по умолчанию платформы (гипотеза о расхождении); явная `version` со списком не сверяется — опечатка после согласия | **после согласия** (версия) | **работоспособность, безопасность** (сетевые политики зависят от CNI), **цена** (Logging, если платный — гипотеза) | `k8s_plan` называет, что выбрала платформа (CNI, канал, CIDR, логи); `network_plugin`, `release_channel` аргументами; версия — сверка со списком до вопроса | 1.6; сверка версии — 1.5к |
| FB15-43 | Группа узлов: автомасштаб, тип диска, taints/labels, autoRepair, kube-reserved | `scalePolicy.autoScale{minCount, maxCount}`, `disk.typeName` (каталог `diskTypes`), `taints`, `labels`, `autoRepair.enabled`, `remoteAccess`, `GET /v3/nodePools:calcKubeReserved?cpu&ramGb&diskSizeGb` | **нет** (`nodepool.go:20–62`) | `k8s_nodepools` не отдаёт taints/labels/disk/autoRepair (`tools_k8s.go:459–481`) | нет | fixed scale, тип диска — умолчание кластера, autoRepair — платформы | — | **цена** (автомасштаб), **работоспособность** (выделенные пулы; агент не знает, сколько ресурсов узла доступно подам — `calcKubeReserved` не используется) | `autoscale_min/max`, `disk_type`, `taints`, `labels` у `k8s_add_nodepool`; `k8s_options` показывает kube-reserved для flavor | 1.6 |
| FB15-44 | Container Apps: секрет в `env` открытым текстом | `containers[].env[]{name, value, type}` — `type: "secret"` (значение — имя секрета Secret Management) | **нет**: `containerapp.Env.Type` есть (`containerapp/client.go:107`), но не задаётся (`tools_ca.go:118–121`) | `ca_get` возвращает сырой объект с `env[].value` | имена переменных — да, значения — нет | — | — | **безопасность**: пароль БД, переданный агентом в `env`, лежит в конфигурации приложения открытым и возвращается `ca_get`; маскирование H7 — по ключам JSON (`password`, `secret`, …), не по имени переменной (`mcp/http_redact.go:46–66`) — `{"name":"DB_PASSWORD","value":"…"}` не маскируется (гипотеза о сумме эффектов, факт о списке ключей) | `secret_env` (имя → путь секрета, `type: secret`) у `ca_deploy`; `ca_get` маскирует `env[].value` по имени переменной (H7 расширить на пары name/value) | 1.5к |
| FB15-45 | Container Apps: формат `cpu`/`memory`, протокол, тайм-ауты, пробы, имя и образ до вопроса | `resources.{cpu, memory}` (строки без enum), `template.protocol` (зашит `http_1`, `tools_ca.go:130`), `timeout`, `idleTimeout`, `scaling.rule`, `livenessProbe`/`readinessProbe`, `ingress.accessSettings.enableAuth`, `customDomains`; `POST /v2/containers:check_name` | `cpu`, `memory` — свободные строки; остальное нет | нет | да (cpu, memory, инстансы, порт, root) | `0.1` / `256Mi` / 0–1; «`min_instances > 0` — оплата постоянно» не сказано | **после согласия**: формат `cpu`/`memory` не проверяется (строка плана 471); имя не проверяется на занятость (строка 482), образ — на существование (OPS-13) | **работоспособность, цена** | по строкам плана; `protocol`, `timeout`, `idle_timeout`, `secret_env` — 1.6; описание: «`min_instances > 0` тарифицируется постоянно» | план; 1.6 |
| FB15-46 | Artifact Registry: тариф в превью, `public`, обновление | `tariff` (BASIC/PREMIUM, по умолчанию BASIC), `isPublic`, `retentionPolicy`, `quarantineMode`, `registryMode`/`upstream` (прокси-кеш), `PATCH /v1/registries/{id}` | `tariff`, `public` | enum схемы; что даёт PREMIUM — нигде | **частично**: `tariff` в превью только если передан (`strings.ToUpper("")` → пустое, `Add` его отбрасывает: `tools_ar.go:94`, `create_preview.go:117–127`) | BASIC — молча | — | **цена, безопасность** (`public=true` — анонимный pull без предупреждения; `ar_update` нет — не вернуть) | превью всегда называет `tariff`; предупреждение у `public`; `ar_update` | 1.5к (превью), 1.6 |

## 7. Сквозное: стек, списки, квоты, скиллы

| № | Что | Факты | Риск | Предложение | Версия |
|---|---|---|---|---|---|
| FB15-47 | Стек собирает ресурсы, которые не стыкуются | `nlb.external` в манифесте — `bool` с нулевым значением `false` (`stack/store.go:148–155`, `:702–705`), у `lb_create` умолчание `true`; шаг `dns` ждёт `$lb_external_ip`, которого у внутреннего балансировщика нет; шаг `vm` без `ports` (`:655–686`); `vpc{name}` создаёт VPC, на которую не ссылается ни один шаг; цена стека — своя карта констант (`:799–809`); `secrets[].value` — открытым в файле задания до шага (`:351–404`, строка плана G130-30) | **работоспособность**: манифест «vm + nlb + dns» из описания `stack_plan` не доходит до DNS | `nlb.external` по умолчанию `true`, как у инструмента (одна точка умолчания — схема `lb_create`); `stack_plan` отказывает до задания, если `dns.target=lb` при `external=false`; `vm.ports` в манифесте; цена стека — из превью шагов (`CreatePreview`), не из констант | 1.5к (умолчание — RELEASING? нет: меняется создаваемый ресурс, нужна «Миграция» — D) |
| FB15-48 | Скиллы и описания не ведут к размеру | `evo-scenarios` маршрутизирует только deploy/vm/postgres/cleanup (`skills/evo-scenarios/SKILL.md:28–32`): для Kafka, Redis, k8s, Container Apps, LB, DNS, сертификатов, S3 скилла нет; `evo-postgres` шаг 1 — «самая маленькая standard» без таблицы «нагрузка → режим/тип/узлы/хранилище»; `evo-vm` говорит «detach first» без инструмента; единственная подсказка размера в описаниях — `k8s_helm` «monitoring stacks want ≥8GB nodes» | агент угадывает | таблица размеров в `evo-postgres` и `evo-vm` (портал / 1С / бот → flavor с долей vCPU, диск, режим); строки Kafka/Redis/k8s в `evo-scenarios` с оговоркой «пользователи и топики — в консоли» до FB15-36; `evo-k8s` — по плану (строка «кандидаты в набор», 1.5) | 1.5к (тексты) |
| FB15-49 | Списки не отдают поля, которые уже прочитаны из API | `list_subnets` (`type, state, default, routed_network, vpc_id, subnet_address, dns_servers`), `list_security_groups` (`default`), `list_floating_ips` (зона), `list_disk_types` (пределы, скорость, зоны), `list_flavors` (доля, GPU, spot, зоны), `snat_list` (`vpc_id, floating_ip, nat_rules`), `dns_zones` (`verification_token`), `secret_list` (`kms_key_id, labels`), `pg_list`/`kafka_list`/`redis_list` (хосты, хранилище, подсети), `k8s_nodepools` (taints, labels, disk), `vm_get` (flavor, диски) — 11 списков | агент выбирает вслепую; `evo-cleanup` не видит, что оплачивается | одно правило: список отдаёт поля, по которым выбирают и платят; инвариант «Процесса»: поле структуры клиента, которое читает планировщик, есть и в ответе списка (AST, как `TestCompletion_EveryReferenceIsCompletedOrSaysWhy`); налог контекста — по храповику #486 | 1.5к |
| FB15-8 | Квоты | см. §2 | | | 1.6 |
| FB15-15 | Три автоподбора | см. §2 | | | 1.6, D |

Объявление дополнений `internal/tools/completion.go` (`valueRefs`: аргумент →
инструмент-список и поле) — готовая единая точка для (б): тот же реестр может
питать описание аргумента («допустимые значения — `list_flavors.name`») и
проверку планировщика «значение есть в списке» (FB15-2, 38, 42). Мнение:
расширять его, а не заводить второй перечень (§2).

## 8. Хватает ли агенту инструментов: три задачи

Мнение по трём задачам из брифа, при нынешнем реестре.

**«Простой портал ребёнку»** (ВМ с Docker, домен, HTTPS). Путь: `deploy` —
есть, с превью и ценой; домен — `dns_record_create` в делегированной зоне —
есть; HTTPS — **нет**: NLB без TLS (FB15-20), а описания `cert_*` ведут в
тупик; остаётся TLS на самой ВМ (certbot), о чём ни скилл, ни описание не
говорят. Размер: `sizer.Match` даёт 2 vCPU/4 ГБ без доли vCPU (FB15-1) —
для портала на `1:10` хватит, но агент этого не знает и не может обещать.
Порты: `deploy` открывает порты compose для `0.0.0.0/0` всей зоне (G1, план).
Итог: выполнимо с присмотром; без присмотра — HTTPS и доля vCPU.

**«База для 1С»** (PostgreSQL, память, HA, быстрый диск). `pg_create` с
меню спецификаций — есть, но меню обрезано до 12 дешёвых и без `flavor_type`
и `allow_primary_standby` (FB15-31); пулер, WAL, локаль — умолчания навсегда
(FB15-33); цены нет до 1.7; тип диска у DBaaS API не даёт. ВМ-вариант
(1С на ВМ): образ маркетплейса с минимумом vCPU/RAM, который автоподбор не
соблюдает (FB15-5), диск без IOPS (FB15-6). Итог: без присмотра — нет; с
присмотром — человек должен сам знать спецификацию.

**«Бот»** (Container App с секретом, постоянно слушающий). `ca_deploy` —
есть; `min_instances=1` — есть, что это постоянная оплата — не сказано;
секрет — только открытым в `env` (FB15-44); `protocol` зашит (FB15-45);
образ — в Artifact Registry, тариф которого в превью не виден (FB15-46).
Итог: выполнимо, небезопасно для секрета.

Что нужно агенту в первую очередь (мнение): (1) поля в списках (FB15-49) и
доля vCPU в превью (FB15-1) — иначе он не может рассуждать о размере; (2)
честные описания там, где продукта нет (HTTPS, пользователи Kafka/Redis);
(3) таблица «задача → размер» в скиллах (FB15-48) — пока нет цены у DBaaS и
k8s, это единственный источник масштаба.

## 9. Класс D50, ломающее, решения D, версии

**Класс D50 — отказ до вопроса на вызов, который заведомо упадёт** (ось 1
§6; смена кода без `--yes` 3 → 1/2/4 — RELEASING, условия 1–5 по образцу
D15, D39, D48, D50):

| Находка | Что отказывает до вопроса | Чем (одна точка) |
|---|---|---|
| FB15-2 | flavor, недоступный в зоне ВМ | `engine.flavorNotFound` + фильтр зоны в `ListFlavors` |
| FB15-5 | явный flavor ниже `min_cpu`/`min_ram` образа | `provision.FlavorFloor` рядом с `BootDiskGB` |
| FB15-6 | тип диска не в зоне, размер вне пределов типа | планировщики `provision`/`deploy`/`disk_create`, чтение `DiskType` |
| FB15-17 | второй SNAT в зоне | `takenName`-класс по зоне (D20) |
| FB15-18 | floating IP чужой зоны | планировщик `fip_attach` |
| FB15-21 | цель `direct_ip` у `tg_create` | планировщик `tg_create` читает ВМ |
| FB15-25 | `cert_issue_le` без делегированной зоны | планировщик K1 (план) |
| FB15-38 | версия не из списка, подсеть не из проекта (PG/Kafka/Redis) | планировщики DBaaS |
| FB15-40 | flavor k8s `SOLD_OUT` в зоне или не того `purpose` | `pickK8sFlavor` |
| FB15-42 | версия k8s не из каналов | `resolveK8sPlan` |
| FB15-8 | исчерпанная квота (когда ключ её видит) | `engine.QuotaCheck` (1.6, D) |

**Дыра в согласии — превью не говорит, что evo выбрало** (условие 1
RELEASING, но правка — добавление полей в превью, не ломающая): FB15-35
(`ui_enabled`), FB15-41 (`kube_api_internet`, `cluster_sa_id`, `count`,
`disk_gb`), FB15-46 (`tariff`), FB15-32 (расписание копий), FB15-28
(`kms_key_id`), FB15-1 (доля vCPU).

**Ломающее по `docs/RELEASING.md`** (нужна «Миграция» и решение D):
FB15-16 (`open_ports` перестаёт считать порт открытым без учёта источника:
ответ `already_open` → создание правила; условие 1 — неверный результат с
кодом 0), все отказы класса D50 выше (код 3 → 1/2/4 без `--yes`), FB15-47
(умолчание `nlb.external` стека: меняется создаваемый ресурс), FB15-15
(единое правило подбора: меняет выбор между равными — D27 называет такое
не ломающим, но ресурс другой — «Миграция»).

**Требует решения D (вопросы владельцу, с рекомендацией):**

1. **Доля vCPU по умолчанию (FB15-1, 15).** (а) оставить `lowcost` по
   умолчанию, но показывать долю в превью и списке; (б) автоподбор по
   `cpu_share` ≥ `1:3`, `lowcost` — только явно; (в) умолчание — `1:1`.
   Рекомендация: (а) в 1.5к, (б) — 1.6 после P34 с ценой: evo обещает
   «самое дешёвое», и превью должно говорить, что это 10 %.
2. **Пакет D50-отказов (FB15-2, 5, 6, 21, 38, 40, 42) одним решением** или
   по одному на инструмент? Рекомендация: одно решение «ресурс, которого
   каталог не даёт, — отказ до вопроса» с перечнем в таблице, как D28 — и
   инвариант W8 с мирами на каждый пункт.
3. **`open_ports` «уже открыто» с учётом источника (FB15-16).**
   Рекомендация: (а) ключ с источником, диапазон/`any` — «открыто шире»
   без создания; альтернатива (б) — всегда создавать правило с заданным
   источником, даже при более широком: дубли. (а).
4. **Единый автоподбор flavor (FB15-15).** Рекомендация: 1.6, вместе с
   `cpu_share`; до того — `sizer.Match` пропускает free tier (1.5к, не
   ломающее по D27: free tier и так ограничен одной ВМ).
5. **Секреты Container Apps (FB15-44).** Рекомендация: `secret_env` в 1.5к
   и маскирование `env[].value` по имени — без решения D (расширение H7);
   запрет открытого `env` с именем вида `*PASSWORD*` — решение D (ломающее).
6. **Описания там, где продукта нет (FB15-20, 36).** Текстовая правка без D;
   вопрос платформе DOC-3 — через «релиз».

## 10. Пробы для api-probe и cloud-e2e

Только чтение, кроме P36, P39, P42 (cloud-e2e, создание ВМ, с уборкой — по
«да» владельца). В лог — коды HTTP, имена полей, числа; id и адреса
маскируются. Подробные запросы — в реестре `evo-internal`, «Что проверить
пробой» P34–P46.

| № | Проба | Закрывает |
|---|---|---|
| P34 | `GET /flavors?limit=100`: у скольких flavor есть `oversubscription`, `gpu`, `flavor_type.is_spot`, `availability_zones[]`; значения `oversubscription` у `lowcost10-*`, `lowcost30-*` и общих; `price-calculation` для двух flavor одного размера с разной долей — отношение цен | FB15-1, 3, 10 |
| P35 | `GET /flavors?availability_zone_id=<AZ-1>` против `<AZ-2>`: разница множеств; `availability_zones[].available=false` — есть ли | FB15-2 |
| P36 | cloud-e2e: `POST /api/v1.1/vms` с flavor ниже `min_cpu` образа маркетплейса — 422 до создания или ВМ в `error_creating`/`running`; уборка | FB15-5 |
| P37 | `GET /v3/quotas/usages?subject_type_codes=QUOTA_SUBJECT_TYPE_PROJECT&subject_ids=P` и `/v3/quotas/resourceTypes` ключом проекта: код, число строк, коды типов (vCPU, RAM, диск, FIP, SG) | FB15-8 |
| P38 | `GET /subnets`: есть ли подсети `type ∈ {technical, zone_technical}` и в `state ≠ created`; принимает ли `price-calculation`/валидатор ВМ в технической подсети — только документация | FB15-13 |
| P39 | cloud-e2e: вторая ВМ на free-tier flavor при живой первой — код ответа | FB15-15 |
| P40 | `GET /floating-ips`: есть ли `availability_zone`; документация — можно ли привязать адрес к ВМ другой зоны | FB15-18 |
| P41 | `POST /v2/targetGroups` с интерфейсом `direct_ip` (живой прогон, цель — существующая публичная ВМ теста): код и текст | FB15-21 |
| P42 | cloud-e2e: `vm(private) + nlb` без открытого `target_port` — состояние цели в `lb_get` через 2 мин | FB15-23, 47 |
| P43 | `GET /v1/privateZones/{id}` зоны, созданной без `linkedVpcIds`: что в поле; документация — разрешает ли имена | FB15-24 |
| P46 | `productConfiguration` mk8s: число flavor `SOLD_OUT` по зонам и `purpose`; согласованность с `zones[].availableFlavors` | FB15-40 |
| P44 | `POST /v2/secret` с путём `newfolder/name` без `POST /v2/folder`: 200 или ошибка (с уборкой) | FB15-28 |
| P45 | PG: `POST /v1/clusters` с `backup.schedule` `@daily` и `*/5 * * * *` — только валидация (ожидается 400 до создания; при 200 — уборка) | FB15-32 |

## 11. Источники

- Код `main` на `82be4ab`: `internal/tools/tools.go` (схемы и `provision`),
  `tools_deploy.go`, `tools_disk.go`, `tools_snat.go`, `tools_vm.go`,
  `tools_image_catalog.go`, `tools_nlb.go`, `tools_dns.go`,
  `tools_certmanager.go`, `tools_s3.go`, `tools_secret.go`, `tools_vpc.go`,
  `tools_pg.go`, `tools_kafka.go`, `tools_redis.go`, `tools_k8s.go`,
  `tools_ca.go`, `tools_ar.go`, `tools_admin.go`, `completion.go`;
  `internal/provision` (`types.go`, `order.go`, `flavor_order.go`,
  `subnet.go`, `sg.go`, `bootdisk.go`, `quota.go`, `disk.go`, `nat.go`,
  `adapter.go`, `price.go`); `internal/engine` (`create_preview.go`,
  `network.go`, `deploy.go`); `internal/deployplan` (`resolve.go`,
  `price.go`); `internal/sizer`; `internal/pricing`; `internal/stack/store.go`;
  `internal/mk8s`, `internal/containerapp`, `internal/artifact`,
  `internal/postgresql`, `internal/dbaas`; `skills/*/SKILL.md`.
- Спецификации Cloud.ru (скачаны 28.10.2026 `scripts/cloudru-specs.py`):
  compute `openapi-v3.yaml` (`FlavorSchema`, `FlavorTypeSchema`,
  `AvailabilityZoneFlavorSchema`, `PublicDiskTypeResponse`, `SpeedLimit`,
  `PublicDiskCreateRequest`, `PublicVmCreateRequest` v1.1,
  `VmCreateInterfaceSection`, `PublicNATGatewayCreateRequest`,
  `PublicSecurityGroupRuleCreateRequest`, `PublicSubnetCreateRequest`),
  organization `api.op.v3.yaml` (`v3QuotaUsage`, `v3QuotaResourceType`),
  nlb `swagger.yaml` (`v2Listener`, `v2HealthCheck`, `v2TargetVMSpec`),
  dns `openapi.yaml` (`v1CreatePrivateZoneRequest`, `v1PublicZone`),
  certificatemanager `ccm.swagger.yaml`, secretmanager
  `api.swagger-public.yaml`, vpc `openapi.yaml`, postgresql
  `openapi-postgresql.yaml` (`CreateClusterRequest`, `Specification`),
  kafka `openapi-kafka.yaml`, redis `openapi-redis.yaml`, mk8s
  `openapi-v3.yaml` (`FlavorConfiguration`, `ProductConfiguration`,
  `NetworkConfigurationRequest`, `CreateNodePoolRequest`), containers
  `openapi.yaml` (`ContainerResources`, `Env`, `Template`), ar `openapi.yaml`.
- Документы: `docs/internal/api-contract.md` (§3.5, §4.6, §4.7, §4.11,
  §5.16), `docs/internal/audits/ops-gap-1.4.md` (G1–G15),
  `docs/internal/release-plan.md` (1.5–1.7), `DECISIONS.md` (D15, D17, D27,
  D29, D35, D36, D37, D39, D48–D50), `docs/RELEASING.md`,
  `docs/internal/contract-map.json`; реестр расхождений `evo-internal`
  (NET-17, K8S-12, DB-10, CMP-17).
