# Дизайн: ВМ из образов и продуктов маркетплейса в CLI

Статус: проект, версия 2 от 16.10.2026 (версия 1 — утро того же дня, до
живой пробы каталога). Код не менялся, облако не трогалось. Факты отмечены
ссылкой на файл:строку, документ или лог прогона; что не проверено вживую —
«непроверено»; мнения и гипотезы — явно.

Постановка владельца (две итерации):

> «Не понимаю, как через CLI человек развернёт, например, Astra Linux, а не
> голую ВМ. Ни один нормальный человек UUID не знает».

> «Я с помощью CLI не разворачиваю совсем голую ВМ. Я разворачиваю её как
> минимум с ОС, а могу развернуть сразу какой-то продукт. Все образы, плагины
> и прочее лежат в маркетплейсе, и id у них есть. То есть теоретически я могу
> выбрать продукт. Как это будет реализовано через CLI?»

Содержание: 1 резюме · 2 что исправлено против версии 1 · 3 что есть сейчас
и что даёт API · 4 эталоны (4.3 — CLI против консоли) · 5 модель «продукт»
(5.1 — сравнение позиций) · 6 сессии пользователя ·
7 превью, доступ и `deploy` для продукта · 8 варианты · 9 изменения по
слоям · 10 план и риски · 11 read-only проверки · 12 вопросы.

## 1. Резюме

**Рекомендация.** Продукт маркетплейса в evo — это не новая сущность, а
маршрут к одному из двух каталогов, у которых есть API: образ ВМ (compute,
`GET /api/v1/images`, `type=marketplace`) или плагин кластера (mk8s,
`GET /v3/addons`). Делаем три вещи:

1. **Образы — по имени, с поиском и полями продукта (M).** `evo image list
   <подстрока> [--type marketplace|public|private]`, `evo image get <имя>`,
   `evo vm create <name> --image <имя|UUID> [--meta k=v]`. Поиск — серверный
   (`name=`/`display_name=` ищут по подстроке без учёта регистра — проверено
   вживую, api-probe #2, 16.10). Ссылка на образ разрешается одним общим
   резолвером: UUID → точное `name` → точное `display_name` → подстрока с
   единственным совпадением. Неоднозначность — список кандидатов (без
   терминала) или меню (на терминале). Заполнение `image_metadata` — одна
   реализация по `field_type`, а не по имени поля (у РЕД ОС 7.3 обязательное
   `fqdn` типа `hostname`, которое evo сегодня не заполнит). Так Astra, Альт,
   РЕД ОС, GitLab, n8n, PostgreSQL и прочие 60 образов маркетплейса
   разворачиваются той же командой, что Ubuntu.
2. **Плагины — отдельное дерево `k8s` (M).** `evo k8s addons [query]`,
   `evo k8s addon-versions <name>`, `evo k8s add-addon <cluster> <name>
   [--version] [--config file.json]`, `evo k8s cluster-addons`,
   `evo k8s delete-addon`. API есть и описан в спецификации mk8s v3;
   `configurationSchema` — JSON-схема, по ней evo проверяет `--config` и
   спрашивает обязательное до обращения в облако. В evo этого нет совсем
   (`internal/tools/tools.go:224-327` — только кластеры, нодпулы, kubectl, helm).
3. **Витрина — только чтение (S, после 1 и 2).** `evo marketplace list
   [query]` — объединение двух каталогов с колонкой «КАК РАЗВЕРНУТЬ»
   (`vm create --image …` или `k8s add-addon …`), как `doctl 1-click list
   --type droplet|kubernetes`. Команды `marketplace deploy` нет: параметры
   развёртывания у ВМ и у кластера разные, а общий API каталога маркетплейса
   (карточки, категории, SaaS, офферы) отсутствует — подтверждено владельцем.

**Почему не единое `evo product deploy`.** Ни один эталон так не делает:
Azure — `az vm image` и `az k8s-extension`, AWS — AMI/подписка и `aws eks
create-addon`, DigitalOcean — `droplet create --image` и `kubernetes 1-click
install`, GCP — Marketplace через консоль или Terraform. Общий глагол
потребовал бы маршрутизации по типу продукта, которого в compute и mk8s нет
как поля, и упёрся бы в типы без API (SaaS). Мнение: витрина-объединение даёт
то, что просит владелец («выбрать продукт»), не плодя четвёртый путь
создания ВМ (`docs/internal/command-model.md` §5 уже насчитал 16 расхождений
между тремя).

**Чего нельзя сделать без Cloud.ru.** Лицензия и цена продукта: у всех
проверенных образов, включая Astra KES, `price-calculation` отвечает
`image = 0`, `mkp_image = 0`, а два образа Astra отвечают 422 «sku_identifier
cannot be blank» — в биллинге у них нет SKU (`BACKLOG.md` `[images]`,
api-probe #4). Документация утверждает, что образы маркетплейса бесплатны, но
в этом же каталоге Windows Server и сертифицированные Astra/РЕД ОС — вопрос
к маркетплейсу и биллингу (§12). Пароль администратора и адрес веб-интерфейса
продукта (GitLab, Keycloak) ни в одном поле API нет — только в карточке
маркетплейса, к которой API тоже нет; evo может показать `description`
образа и честно сказать «см. документацию продукта».

## 2. Что исправлено против версии 1

Проба каталога (api-probe #2–#4, 16.10, итог в `BACKLOG.md` `[images]`;
имена образов — из лога run 38868189205, шаг `images_marketplace`) опровергла
четыре вывода версии 1:

| версия 1 | факт | следствие |
|---|---|---|
| «Astra Linux в документации нет; гипотеза — её нет и в каталоге» | Astra есть: 7 образов `type=marketplace` (`AstraLinuxSmolensk1.8.3KES`, `AstraLinuxSmolensk1.8.2KES`, `astra_1.8.1`, `alse-smolensk-1-8-3-mkp`, `alse-adv-1-7-7`, `alse-base-1-7-7`, `astra-voronej`), во всех трёх зонах. Там же Альт (7), РЕД ОС (3), SberLinux, Найс ОС, Windows Server 2019/2022, Talos | Документация Cloud.ru ([concepts__images](https://cloud.ru/docs/virtual-machines/ug/topics/concepts__images)) отстаёт от каталога; на неё как на перечень полагаться нельзя |
| «Маркетплейс — Linux с предустановленным ПО, бесплатно» | В `marketplace` лежат и ОС (в том числе коммерческие), и приложения (GitLab, n8n, PostgreSQL, MongoDB, MySQL, Elasticsearch, Keycloak, Zabbix, Grafana Loki, VictoriaMetrics, Jaeger, Passbolt, Nextcloud, Docker, NGFW, ГигаАгент, OpenClaw), и тестовые образы (`alt-test`, `astra-voronej-cc11079`, образ с именем `null`) | Тип `marketplace` ничего не говорит о платности и назначении; нужен признак «ОС / приложение», которого в API нет |
| «Семейство без версии → последняя версия, как GCP/YC» | Имена разнобойные (`AstraLinuxSmolensk1.8.3KES`, `astra_1.8.1`, `alse-base-1-7-7`); версия из имени не читается | Семейства — только для `public` Ubuntu/Debian/CentOS/Fedora (`init`); для маркетплейса — точное имя или выбор из найденного |
| «`name=` — непроверено, точное или подстрока» | Подстрока без учёта регистра: `name=ubuntu` → 17, `name=Ubuntu-24.04` и `name=ubuntu-24.04` → по 3 (`NVLINK-Ubuntu-24.04-gpu`, `Ubuntu-24.04`, `ubuntu-24.04-gpu`), `display_name=Astra` → 7 | Поиск — серверный; точное совпадение добирается у себя из ответа подстроки |

Также подтвердилось, что `price-calculation` отвечает 200 ключу проекта
(BACKLOG :21 про «нет API цен» уже пересмотрен владельцем 16.10), и
обнаружилось, что список с `project_id` отдаёт 83 образа (45 marketplace,
20 private, 18 public) против 86 без него — 23 общих образа в проектной
выборке не видны. Гипотеза: фильтр по доступности в проекте или зоне;
непроверено, вопрос к compute.

## 3. Что есть сейчас и что даёт API

### 3.1 Код (ссылки — на `main` после #69)

| Факт | Где |
|---|---|
| `evo list-images` печатает весь каталог одной таблицей; колонки ОС и Состояние пустые — таких полей в API нет | `internal/engine/list_images.go:31-42`; `docs/internal/api-contract.md:115` |
| Образы проекта в CLI-списке не показываются, в MCP `list_images` — показываются | `engine/list_images.go:17` против `tools/tools.go:2175-2191` |
| `evo vm create <name>` = инструмент `provision`; образ только `--image-id <UUID>`; без него `CLOUDRU_IMAGE_ID is required` | `cli/trees.go:52`; `tools/tools.go:47, :1466-1469` |
| `deploy --image-id <UUID>`, то же требование | `cli/main.go:535`; `engine/deploy.go:348-353` |
| `deploy_plan` единственный принимает образ по имени (точное совпадение) | `deployplan/resolve.go:263-298` |
| `init` предлагает только Ubuntu/Debian по regex имени | `engine/init.go:54-60, :213-216, :220-251` |
| Заполнение `image_metadata` — три копии, **по имени поля** (`public_key`, `hostname`, `linux_password`, `name`), `mandatory_group` без `TrimSpace` | `engine/deploy.go:372-430`; `tools/tools.go:1554-1613`; `deployplan/resolve.go:307-355` |
| Логин по умолчанию расходится: `ubuntu` у `deploy`, `user1` у двух других | `engine/deploy.go:405`; `tools/tools.go:1583`; `deployplan/resolve.go:308`; `command-model.md` §5 п. 12 |
| Произвольное поле продукта передать нельзя ни одним путём | те же три места |
| `provision` не открывает порты в группе безопасности; `deploy_plan` открывает tcp из `ports` | `command-model.md` §5 п. 9; `tools/tools.go:165` |
| `Image` не разбирает `type`, `display_name`, `description`, `min_*` | `provision/types.go:159-165` |
| `ImageMetadataField.Required` читает `is_required`, которого нет; нет `display_name`, `display_position`, `display_group` | `provision/types.go:263-271` |
| Создание ВМ идёт по `image_name` (UUID давали 500) | `provision/types.go:54-67`; `engine/deploy.go:471` |
| `deploy` ставит Docker скриптом get.docker.com, если его нет на ВМ | `engine/deploy.go:224`; `bootstrap/install.sh:54`; BACKLOG `[sec-supply]` |
| Плагинов mk8s в evo нет: инструменты `k8s_*` — кластеры, нодпулы, kubeconfig, kubectl, apply, helm | `tools/tools.go:224-327`; `cli/trees.go:83-96` |
| Цена — зашитая карта без лицензии | `internal/pricing/pricing.go:1-16` |
| Коды выхода: не найдено 4, аргументы отвергнуты до облака 1 `USAGE`, API 5 | `docs/exit-codes.md`; DECISIONS D11 |

### 3.2 Compute API (спецификация `compute/openapi-v3.yaml`, скачана `scripts/cloudru-specs.py` 16.10)

- `GET /api/v1/images`: `name`, `display_name` (подстрока, регистр не важен —
  вживую), `type` (`public|private|marketplace`), `enabled`,
  `availability_zone_id`, `need_gpu`, `free_tier_enabled`, `project_id`;
  `order_by` только `created_time`; `limit` ≤ 100. Каталог — 86 образов,
  одна страница.
- `PublicImageResponse`: `id, name, display_name, description, project_id,
  icon (SVG), min_cpu, min_ram, min_disk, min_gpu, public, type,
  user_data_template, image_metadata[], created_time, modified_time,
  availability_zones[]{…, enabled, state}`. **Нет**: семейства ОС, признака
  «ОС или приложение», категории, цены, лицензии, ссылки на карточку.
- `image_metadata[]`: `id, name, display_name, display_position, field_type,
  display_group, default_value, sensitive, mandatory_group,
  mandatory_group_display_name`. Вживую у 60 образов маркетплейса (лог
  `image_fields`): у 58 — тот же набор, что у Ubuntu (`name`:`linux_login` с
  default, `public_key` и `linux_password` в группе `authentication`,
  `hostname`, флаги `vmm_enabled`/`guest_agent_ready`). Исключения:
  - `redos_7.3_image`, `REDOS7.3-sert`: обязательное `fqdn` (`field_type`
    `hostname`, группа `fqdn`);
  - `OpensearchDashboards`: поля продукта `opensearch_user` (text),
    `opensearch_password` (`plain_password`, sensitive), `opensearch_host`
    (text), `opensearch_version` (text, с default) — единственный образ, где
    `--meta` нужен по существу;
  - Windows Server (4 образа): только `name` и `linux_password` (у одного —
    `plain_password`), **без `public_key` и `hostname`** — вход по RDP;
  - грязь в данных: `mandatory_group` `" authentication"`, `" hostname"`,
    `" name"` (с пробелом), `"Метод аутентификации"`, `null`;
    `guest_agent_ready` типа `text` с `mandatory_group: null`
    (`alse-smolensk-1-8-3-mkp`); у `Jaeger`, `Zabbix_Proxy`,
    `alse-smolensk-1-8-3-mkp` по два логин-поля (`name` и `linux_login`).
- `POST /api/v1.1/vms`: `image_id` или `image_name`; `image_metadata` —
  объект «имя → строка»; 422 «Не заполнены обязательные поля с метаданными
  для образа», «Ошибка в метаданных виртуальной машины». Поле `cloud_init`
  описано (BACKLOG `[05]` считал его неподтверждённым; вживую не проверено).
- `POST /api/v1/projects/{project_id}/price-calculation`: по спецификации
  `{flavor, image, disks, floating_ips, nat_gateways, total_price_month,
  total_price_hour}`; вживую ещё `mkp_image`, `custom_image_size`.
  Эндпоинта принятия условий/оферты нет.

### 3.3 mk8s API (`mk8s/openapi-v3.yaml`; вживую не проверено)

- `GET /v3/addons?projectId=…&filter=…&orderBy=…` → `Addon{name, displayName,
  description, categories[], isBillable, createdAt, updatedAt}`; `filter` по
  `name, displayName, categories, isBillable, createdAt`.
- `GET /v3/addons/{addonName}` → `AddonRelease{addonVersion, appVersion,
  addonNamespace, configurationSchema (JSON-схема строкой), mode
  (ACTIVE/EXPERIMENTAL/END_OF_SUPPORT/END_OF_LIFE/INACTIVE), requirements
  {kubeVersions, readyWorkers}}`.
- `POST /v3/clusterAddons {clusterId, name, appVersion, addonConfiguration
  (JSON строкой, подстановки `{{clusterID}}`, `{{clusterName}}`),
  resolveMode (NONE/OVERWRITE/PRESERVE)}` → `ClusterAddon{id, status …}`;
  `GET/PATCH/DELETE /v3/clusterAddons/{id}` (`preserve` при удалении).
- В v2-спецификации есть `CalculatePriceRequest_Addon{addonName}` и
  `CalculatePriceExtendedResponse_AddonPrice` — расчёт цены плагина; в v3 не
  нашёл, нужно проверить, каким эндпоинтом (§11).

### 3.4 Чего нет ни в одном API

Каталог маркетплейса как витрина: категории, карточка продукта, документация,
способ поставки (образ / Helm / SaaS / Ansible / ML-модель — типы из
[партнёрской программы](https://cloud.ru/docs/marketplace-partner/ug/topics/quickstart)),
модель оплаты (BYOL / pay-as-you-go / pay-as-you-allocate), оферта, пароль
администратора приложения. Владелец подтвердил: такого API нет.

## 4. Эталоны

Проверено по документации 16.10: Azure (`terms`, `k8s-extension`), Yandex
Cloud (`get-latest-from-family`, Marketplace = образы в `standard-images`),
DigitalOcean (`image list-*`, `kubernetes 1-click`), Linode (StackScripts),
AWS EKS add-ons, GCP Marketplace CLI. Остальное — по памяти, помечено.

### 4.1 Образы ВМ

| Эталон | Найти | Запустить по имени | Платные образы | Берём |
|---|---|---|---|---|
| **Azure** | `az vm image list --publisher Canonical --offer ubuntu-24_04-lts --all -o table` | URN `Canonical:ubuntu-24_04-lts:server:latest` или алиас `--image Ubuntu2204` | `az vm image terms show/accept --urn …` — отдельный шаг до создания ([learn.microsoft.com](https://learn.microsoft.com/en-us/cli/azure/vm/image/terms)) | алиасы для базовых ОС; отдельная команда условий — только если Cloud.ru даст API |
| **AWS** (память) | `aws ec2 describe-images --owners amazon --filters Name=name,Values='ubuntu/images/*24.04*'` | AMI ID; алиасы SSM `resolve:ssm:/aws/service/canonical/ubuntu/server/24.04/stable/current/amd64/hvm/ebs-gp3/ami-id` | подписка AWS Marketplace в консоли; без неё `OptInRequired` | честная ошибка API, если подписки нет |
| **GCP** (память) | `gcloud compute images list --filter="family~ubuntu"` | `--image-family ubuntu-2404-lts-amd64 --image-project ubuntu-os-cloud` | лицензия — свойство образа, платится с ВМ | семейство для базовых ОС |
| **Yandex Cloud** | `yc compute image list --folder-id standard-images`; продукты Marketplace — те же образы, ID продукта в карточке ([docs](https://yandex.cloud/en-ru/docs/compute/operations/images-with-pre-installed-software/create)) | `yc compute image get-latest-from-family ubuntu-2404-lts --folder-id standard-images`; `instance create --create-boot-disk image-family=…,image-folder-id=standard-images` | лицензия привязана к образу (`--product-ids` у `image create`); тарифицируется с ВМ | ближайший рынок: маркетплейс = образы в публичной папке, без отдельного `yc marketplace` (непроверено, что его нет) |
| **Hetzner** (память) | `hcloud image list --type system` | `hcloud server create --image ubuntu-24.04 …` | нет | имя — первичный идентификатор |
| **DigitalOcean** | `doctl compute image list-distribution`, `list-application` (1-Click), `list --public` ([docs](https://docs.digitalocean.com/reference/doctl/reference/compute/image/list-application/)) | `doctl compute droplet create web --image ubuntu-24-04-x64` или слаг 1-Click | плата за ПО — с дроплетом | **разделение списков дистрибутив / приложение / свои** |
| **Linode** | `linode-cli images list`; Marketplace Apps = StackScripts с UDF-полями ([Akamai](https://techdocs.akamai.com/quick-deploy-apps/docs/get-started)) | `linodes create --image linode/ubuntu24.04 --stackscript_id … --stackscript_data '{…}'` | — | поля продукта объектом; описание полей до создания |
| **Multipass**, **Incus** (память) | `multipass find`; `incus image list images:debian` | `multipass launch 24.04`; `incus launch images:debian/12 c1` | — | алиасы в выводе списка, не в документации |

### 4.2 Плагины кластера и витрина

| Эталон | Каталог | Установка | Платные | Берём |
|---|---|---|---|---|
| **Azure AKS** | `az aks enable-addons --addons …` — фиксированный список управляемых; `az k8s-extension` — расширения, в том числе из Marketplace ([learn.microsoft.com](https://learn.microsoft.com/en-us/azure/aks/deploy-extensions-az-cli)) | `az k8s-extension create --cluster-name … --extension-type … --plan-name/--plan-product/--plan-publisher` | план Marketplace — флаги `--plan-*` | плагины — своё дерево рядом с кластером, не «продукт» |
| **AWS EKS** | `aws eks describe-addon-versions --addon-name …` (у Marketplace-аддонов есть `ProductUrl`) ([AWS blog](https://aws.amazon.com/blogs/awsmarketplace/deploy-third-party-software-add-ons-aws-marketplace-amazon-eks-clusters/)) | `aws eks create-addon --cluster-name … --addon-name …` | подписка в Marketplace до `create-addon`; нужны права License Manager | версии и совместимость с кластером — до установки |
| **DigitalOcean** | `doctl kubernetes 1-click list`; общая витрина `doctl 1-click list --type droplet|kubernetes` ([docs](https://docs.digitalocean.com/reference/doctl/reference/kubernetes/1-click/list/)) | `doctl kubernetes 1-click install <cluster-id> --1-clicks loki,netdata` | — | **единственная витрина-объединение в CLI: только чтение + тип → команда установки** |
| **GCP** | Marketplace — консоль; для части VM-продуктов «Deploy via CLI» = Terraform-модуль ([docs](https://docs.cloud.google.com/marketplace/docs/deploy-through-CLI)) | нет `gcloud marketplace deploy` | — | подтверждение: общий «deploy продукта» в CLI не делает даже GCP |
| **Helm / Artifact Hub** (память) | `helm search hub cert-manager` | `helm install … --values` | — | JSON-схема values (`values.schema.json`) — аналог `configurationSchema` |

Вывод: имя или слаг — первичная ссылка везде; каталоги разделены по типу
цели (ВМ / кластер); витрина-объединение, если есть, — только чтение (DO);
платность решается либо отдельным шагом условий (Azure, AWS), либо
автоматически с ресурсом (GCP, YC). Cloud.ru сейчас ближе ко второму, но без
признака лицензии в API.


### 4.3 Что из этого есть именно в CLI, а что только в консоли

Запрос соседней сессии и владельца: отделить CLI от консоли и пометить
уверенность. «Проверено» — по официальной документации 16.10.2026.

| Эталон | В CLI есть | Только консоль / не CLI | Уверенность |
|---|---|---|---|
| Azure `az vm image` | `list/show`, `terms show/accept/cancel` для платных образов Marketplace; `az vm create --image <URN\|алиас>` | карточка продукта, цена плана | проверено ([terms](https://learn.microsoft.com/en-us/cli/azure/vm/image/terms)) |
| Azure AKS | `az aks enable-addons --addons …` (фиксированный список управляемых); `az k8s-extension create … --plan-name/--plan-product/--plan-publisher` для приложений Marketplace | витрина Marketplace как каталог | проверено ([k8s-extension](https://learn.microsoft.com/en-us/azure/aks/deploy-extensions-az-cli)) |
| GCP | `gcloud compute images list`, `--image-family`; для части VM-продуктов Marketplace — Terraform-модуль «Deploy via CLI» | каталог Marketplace, Click-to-Deploy, принятие условий — консоль; `gcloud marketplace …` нет | проверено ([deploy-through-CLI](https://docs.cloud.google.com/marketplace/docs/deploy-through-CLI)) |
| Yandex Cloud | `yc compute image list --folder-id standard-images`, `get-latest-from-family`; `yc managed-kubernetes marketplace helm-release install/list/get/uninstall/update` (алиас `mkt`) — продукты Marketplace для кластера | витрина Marketplace для ВМ (ID продукта — в карточке); верхнеуровневого `yc marketplace` в справочнике не видно | проверено для k8s ([mkt](https://yandex.cloud/en/docs/managed-kubernetes/cli-ref/marketplace/)); отсутствие `yc marketplace` — вероятно, полный список команд не просмотрен |
| DigitalOcean | `doctl compute image list-distribution/list-application`, `doctl 1-click list --type droplet\|kubernetes`, `doctl compute droplet create --image <slug>`, `doctl kubernetes 1-click install <cluster> --1-clicks a,b` | описание и цена приложения | проверено ([1-click](https://docs.digitalocean.com/reference/doctl/reference/kubernetes/1-click/list/)) |
| AWS | `aws ec2 describe-images`, SSM-алиасы AMI; `aws eks describe-addon-versions`, `aws eks create-addon` (аддоны Marketplace — с `ProductUrl`) | подписка на продукт Marketplace (есть и API `aws marketplace-*`, но подписка через CLI — не уверен) | EKS проверено ([AWS blog](https://aws.amazon.com/blogs/awsmarketplace/deploy-third-party-software-add-ons-aws-marketplace-amazon-eks-clusters/)); EC2 — по памяти |
| Helm / Artifact Hub | `helm search hub <keyword>` (Artifact Hub или свой Monocular), затем `helm repo add` + `helm install` | карточка пакета, values в UI | проверено ([helm.sh](https://helm.sh/docs/helm/helm_search_hub)) |
| Linode | `linode-cli images list`, `stackscripts list --label …`, `linodes create --image … --stackscript_id … --stackscript_data '{…}'` | карточка Marketplace App с описанием полей | проверено ([Akamai](https://techdocs.akamai.com/quick-deploy-apps/docs/get-started)) |

Итог для evo: везде поиск и запуск по имени/слагу есть в CLI; каталог
продуктов для ВМ и для кластера — **разные команды** у всех, кто имеет оба
(Azure, AWS, DO, YC); единая витрина в CLI — только у DO, и только чтение;
платность — либо отдельный шаг (Azure `terms`, AWS подписка), либо
автоматически с ресурсом (GCP, YC); у GCP CLI-пути для Marketplace по сути нет.


## 5. Модель «продукт»

**Решение (мнение):** существительные остаются по каталогам — `image`
(образы ВМ любого типа) и `k8s` (плагины), плюс витрина `marketplace` только
для чтения. «Продукт» в evo — это строка витрины с полем `kind`
(`vm-image` | `k8s-addon`) и готовой командой развёртывания.

| | `image` | `k8s add-addon` | `marketplace` (витрина) |
|---|---|---|---|
| Источник | compute `GET /images` (`type` любой) | mk8s `GET /v3/addons` | объединение двух |
| Ссылка | `name` / `display_name` / UUID | `name` плагина (машинное) | то же, с префиксом вида при коллизии: `vm:gitlab`, `k8s:gitlab` |
| Поля продукта | `image_metadata` (типы `linux_login`, `public_key`, `linux_password`, `plain_password`, `hostname`, `text`, `flag`) | `configurationSchema` (JSON-схема) | — |
| Цель | ВМ (flavor, диск, сеть, FIP) | кластер (`clusterId`, версия, `resolveMode`) | — |
| Цена | `price-calculation` (`image`, `mkp_image`) | `isBillable` + расчёт цены mk8s (эндпоинт уточнить) | показывает признак платности, не цену |
| Что дальше | ssh/RDP, порты | статус `ClusterAddon`, namespace | — |

Почему не единое `evo product <name> deploy`:
- маршрутизация по типу есть только в двух каталогах; SaaS, Helm-чарты
  партнёров, ML-модели из партнёрской программы в API не видны — команда
  обещала бы больше, чем может;
- параметры развёртывания не пересекаются (flavor и подсеть против кластера
  и версии): общий флаговый интерфейс либо раздувается, либо отсылает к
  специфичным командам — тогда зачем он;
- четвёртый путь создания ВМ запрещён постановкой; `marketplace deploy
  --kind vm` был бы им по факту.

Что берём у витрины DO: `evo marketplace list --kind vm|k8s`, у каждой
строки — `KIND`, `NAME`, `TITLE`, `BILLABLE?`, `DEPLOY` (команда). Это
S-работа после образов и плагинов, потому что сама ничего не знает — только
зовёт два списка.

**Признак «ОС или приложение» у образа** в API отсутствует. Для витрины и
карточки evo может пометить эвристикой по `description`/`display_name`
(«Linux», «Server», «ОС» → ОС) — только как подсказку, не как фильтр.
Правильный ответ — поле в API (§12, compute).

**Мусор в каталоге** (`null`, `alt-test`, `astra-voronej-cc11079`,
`debian-11_error`, `img-nester-*`, `ubuntu-test2204`): по умолчанию evo
скрывает образы, у которых ни одна зона не `enabled && state == created`
(`enabled=true` оставляет 78 из 86, но совпадает ли это с тестовыми —
непроверено). Эвристики по имени (`test`, `error`) не вводим: слишком легко
спрятать чужой рабочий образ; `--all` показывает всё. Вопрос компании —
почему тестовые образы видны всем клиентам.


### 5.1 Сравнение позиций

Владелец просит решение втроём: соседняя сессия (её позиция — комментарий в
PR #65 от 16.10 08:07), Grok (на момент этой версии ответа в PR нет) и
эта. По каждому расхождению — аргументы и рекомендация.

| Вопрос | Соседняя сессия | Эта сессия | Grok | Рекомендация |
|---|---|---|---|---|
| Одна точка поиска поверх каталогов | да: `evo marketplace search astra` → продукт / тип / зоны / команда развёртывания | да, то же; вид (`vm-image` / `k8s-addon`), признак платности, команда | — | **согласие**; имя глагола — см. ниже |
| Развёртывание | в своих деревьях: `vm create --image "<название>"`, `k8s addon install <кластер> <плагин>` | в своих деревьях: `vm create --image <имя\|UUID>`, `k8s add-addon <кластер> <плагин>` | — | **согласие** по сути |
| Единый `marketplace install` | не делать: разные параметры, цена, удаление | не делать: плюс четвёртый путь ВМ и отсутствие признака вида в API | — | **согласие** |
| Главный риск | нет контракта «что продукт сообщает после запуска» (URL, пароль, готовность) | тот же; плюс лицензия и цена, которых API не показывает | — | **согласие**; предложение по контракту — ниже |
| Глагол поиска: `search` или `list` | `search` | `list <подстрока>`, как `image list astra` | — | **`list`**: в evo нет глагола `search`, списки везде `list` (`command-model.md` §7); витрина — это тоже список с фильтром. Если владелец хочет `search` как слово витрины — алиас не вводим, а переименовываем, но тогда и `image search`. Расхождение косметическое |
| Форма команды плагина: `k8s addon install` или `k8s add-addon` | `k8s addon install` | `k8s add-addon` | — | **`add-addon`** в v1.1: деревья `trees.go` — группа → глагол, без третьего уровня (`treeGroup.Cmds` плоский, `cli/trees.go:26-45`); `add-nodepool` уже задаёт этот стиль. Вложенная группа `k8s addon <verb>` читается лучше, но требует изменения механизма деревьев — отдельное решение, не в этой функции |
| `--image` принимает `display_name` («название») | да: `--image "<название>"` | да, после точного `name`; плюс подстрока с единственным совпадением | — | **согласие** по `display_name`; подстрочное авто-совпадение — вопрос владельцу (§12, п. 3): удобно для `--image alse-base`, но это выбор без подтверждения в платном действии (превью его называет) |
| Порядок работ | не оговорён | образы → плагины → витрина; витрина без двух каталогов пуста | — | витрина последней |
| Чего не хватает в позиции соседа (дополнения) | — | `--port` у `vm create` (иначе GitLab снаружи недоступен — `provision` портов не открывает); заполнение метаданных по `field_type` (`fqdn` у РЕД ОС 7.3); отказ `deploy` до создания ВМ на ОС без Docker; образ `Docker` как умолчание; цена из `price-calculation` с оговоркой про лицензию | — | включить в v1.1 |

**Контракт «что продукт сообщает после запуска»** — расхождения нет, есть
предложение, как его закрыть без нового API. В enum `field_type` при
создании образа есть тип `info` (`api-contract.md:117`) — поле без ввода,
только для показа. Если маркетплейс договорится с вендорами заполнять в
образе два-три `info`-поля с фиксированными именами (например,
`product_url_template` = `http://{{ip}}:8080`, `product_login_hint` =
«пароль в /etc/gitlab/initial_root_password», `product_ready_hint` =
«веб-интерфейс через ~3 мин»), evo покажет их после создания ВМ без
единой строчки специального кода: `image get` и итог `vm create` уже
печатают поля. Это вопрос к маркетплейсу (§12), а не к evo; до ответа —
`description` образа и честное «см. документацию продукта».

Когда Grok ответит в PR #65, колонка заполняется, и расхождения с ним
разбираются тем же способом; документ — источник правды, комментарии PR —
обсуждение.


## 6. Сессии пользователя

Имена образов — реальные из каталога (публичные), остальные значения
условные. Вывод на русском (D6).

### 6.1 «Хочу Astra»

```text
$ evo image list astra
ИМЯ                         НАЗВАНИЕ                                   ТИП          ЗОНЫ   ДИСК≥  ПОЛЯ
AstraLinuxSmolensk1.8.3KES  Astra Linux Смоленск 1.8.3 KES             marketplace  1,2,3  10 ГБ  стандартные
AstraLinuxSmolensk1.8.2KES  Astra Linux Смоленск 1.8.2 KES             marketplace  1,2,3  10 ГБ  стандартные
astra_1.8.1                 Astra Linux SE «Смоленск» 1.8.1 (KES)      marketplace  1,2,3  20 ГБ  стандартные
alse-smolensk-1-8-3-mkp     Astra Linux SE Смоленск 1.8.3              marketplace  1,2,3  …      стандартные
alse-adv-1-7-7              Astra Linux SE "Усиленный" 1.7.7           marketplace  1,2,3  …      стандартные
alse-base-1-7-7             Astra Linux SE «Базовый» 1.7.7             marketplace  1,2,3  …      стандартные
astra-voronej               astra-voronej-cc11079                      marketplace  …      …      стандартные
Найдено 7 из 86 (поиск по имени и названию, без учёта регистра).
«стандартные» поля evo заполнит сам: логин, ключ SSH, пароль, имя хоста.

$ evo vm create astra-1 --image astra
Ошибка: образ «astra» неоднозначен — 7 совпадений
Уточните имя: evo image list astra; например --image AstraLinuxSmolensk1.8.3KES
$ echo $?
1
```

На терминале без `--json` вместо ошибки — меню с теми же строками
(`engine/init.go:183-208`), выбор по номеру. Подстрока с **единственным**
совпадением проходит без вопросов: `--image alse-base` → `alse-base-1-7-7`,
в превью «образ alse-base-1-7-7 (по запросу «alse-base»)».

```text
$ evo vm create astra-1 --image AstraLinuxSmolensk1.8.3KES
План: создать ВМ astra-1 из образа AstraLinuxSmolensk1.8.3KES (Astra Linux Смоленск 1.8.3 KES, marketplace) в ru.AZ-2
  Проект:   my-project (…a1b2)
  Flavor:   lowcost10-2-4 (2 vCPU, 4 ГБ)
  Диск:     SSD 10 ГБ (минимум образа — 10 ГБ)
  Сеть:     подсеть default-AZ-2, floating IP — новый
  Поля:     name=user1, hostname=astra-1, public_key=~/.evo/id_ed25519.pub, linux_password=•••• (случайный, не сохраняется)
  Цена:     ≈ 2 044 ₽/мес — API расчёта цены: ВМ 1 971, диск 73, образ 0, лицензия продукта не отражена
            (у образов Astra в биллинге нет SKU; уточните условия лицензии в карточке маркетплейса)
Создать? [y/N] y
✔ ВМ astra-1 создана: 1.2.3.4 (ru.AZ-2)
  Подключение: ssh user1@1.2.3.4 -i ~/.evo/id_ed25519
  Удалить:     evo vm delete astra-1
```

Строка про лицензию — пока биллинг не ответит (§12); как только у образов
появится SKU и `mkp_image > 0`, превью покажет сумму.

### 6.2 «Хочу GitLab»

```text
$ evo image get gitlab
Образ:       GitLab (GitLab), тип marketplace
Описание:    <description из API — как есть>
Минимум:     4 vCPU, 8 ГБ RAM, диск 40 ГБ          ← min_cpu/min_ram/min_disk
Зоны:        ru.AZ-1, ru.AZ-2, ru.AZ-3 (готов)
Поля при создании:
  name            логин          evo заполнит сам (по умолчанию user1)
  public_key      ключ SSH       evo заполнит сам
  linux_password  пароль         evo создаст случайный
  hostname        имя хоста      evo заполнит сам (= имя ВМ)
Доступ к продукту: в полях образа нет адреса и пароля GitLab — смотрите
  документацию продукта в карточке маркетплейса (API ссылку не отдаёт).
Порты: evo vm create не открывает порты; для веб-интерфейса добавьте --port 80,443.
Создать: evo vm create gitlab-1 --image GitLab --flavor <≥4 vCPU, 8 ГБ> --port 80,443
```

`--port` — новый флаг `provision`/`vm create` (как `ports` у `deploy_plan`,
`tools/tools.go:165`; правила через существующий `sgrules.Open`, ID — в state,
`destroy` их убирает). Без него GitLab создаётся, но снаружи недоступен —
сегодня это тихий провал.

```text
$ evo vm create gitlab-1 --image GitLab --flavor lowcost10-4-8 --port 80,443 --yes
✔ ВМ gitlab-1 создана: 1.2.3.5 (ru.AZ-2), порты 80, 443 открыты
  Подключение: ssh user1@1.2.3.5 -i ~/.evo/id_ed25519
  Продукт:     http://1.2.3.5 — первый вход по документации GitLab
  Удалить:     evo vm delete gitlab-1
```

Что показывать как «продукт»: `http://<ip>` печатаем, если открыт 80/443;
где пароль администратора — не знаем, и не выдумываем. Гипотеза, не для
кода: у образа GitLab пароль root в `/etc/gitlab/initial_root_password`, как
у ванильного Omnibus; проверять на e2e, а не в документации evo.

Образ с полями продукта (единственный в каталоге сегодня):

```text
$ evo vm create osd-1 --image OpensearchDashboards --yes
Ошибка: не заполнены поля образа OpensearchDashboards: opensearch_host (текст), opensearch_user (текст), opensearch_password (секрет)
Передайте --meta opensearch_host=… --meta opensearch_user=… и секрет через --meta-file или EVO_META_OPENSEARCH_PASSWORD; на терминале без --yes evo спросит
$ echo $?
1
```

Обязательность здесь — гипотеза: у этих полей `mandatory_group` пустой и
`default_value` пустой; правило «пусто + нет default → спросить» надёжнее,
чем «отправить пусто» (пустые значения давали 500, `tools/tools.go:1599`).
На терминале ввод `plain_password` скрыт; в превью, логе и state — `••••`.

### 6.3 «Хочу Windows Server»

У образов `windows2022std-evo-kmsB` и подобных только два поля: `name` и
`linux_password`, ключа и `hostname` нет. Это меняет «что дальше»:

```text
✔ ВМ win-1 создана: 1.2.3.6 (ru.AZ-2)
  Подключение: RDP 1.2.3.6:3389, пользователь Administrator, пароль показан один раз ниже
  Пароль:      Xk9!…  (не сохраняется; задайте свой через --meta linux_password=… или --meta-file)
  Порт 3389 не открыт: evo vm create win-1 … --port 3389
```

Правило: строка «Подключение» зависит от полей образа — `public_key` есть →
ssh; только пароль → печатаем пароль один раз (как `deploy_apply`,
`tools/tools.go:179`) и говорим про RDP, если в `display_name`/`description`
есть «Windows» (эвристика, помечаем). Лицензия Windows — вопрос биллингу.

### 6.4 «Хочу cert-manager в кластер»

Имена плагинов — условные: каталог вживую не читали.

```text
$ evo k8s addons cert
ИМЯ            НАЗВАНИЕ       КАТЕГОРИИ     ПЛАТНЫЙ  ВЕРСИИ (app)         ПОДДЕРЖКА
cert-manager   cert-manager   security,pki  нет      1.15.3, 1.14.7       active
Найдено 1. Подробнее: evo k8s addon-versions cert-manager

$ evo k8s add-addon prod-1 cert-manager
План: установить плагин cert-manager 1.15.3 (addon 1.15.3-1) в кластер prod-1 (ru.AZ-2)
  Проект:       my-project (…a1b2)
  Namespace:    cert-manager
  Требования:   Kubernetes ≥1.28 (кластер 1.30 — ок), готовых воркеров ≥1 (3 — ок)
  Конфигурация: по умолчанию (схема не требует обязательных полей)
  Конфликты:    RESOLVE_CONFLICTS_MODE_NONE — ранее установленные вручную манифесты не трогаем
  Цена:         плагин бесплатный (isBillable=false); ресурсы кластера не меняются
Установить? [y/N] y
✔ Плагин cert-manager добавлен (id …), статус PROVISIONING → RUNNING за 48 с
  Проверить: evo k8s cluster-addons prod-1; удалить: evo k8s delete-addon prod-1 cert-manager
```

Обязательные поля конфигурации берутся из `configurationSchema`
(`required`): на терминале — вопросы, без терминала — `USAGE` со списком,
`--config file.json` — целиком. Схема проверяется локально до вызова
(библиотека JSON Schema уже понадобится; зависимость — в §9).

### 6.5 Витрина

```text
$ evo marketplace list gitlab
ВИД       ИМЯ      НАЗВАНИЕ   ПЛАТНЫЙ  РАЗВЕРНУТЬ
vm-image  GitLab   GitLab     ?        evo vm create <имя> --image GitLab --port 80,443
k8s-addon gitlab   GitLab     нет      evo k8s add-addon <кластер> gitlab
«?» — compute не отдаёт признак платности образа; см. evo image get GitLab.
```

## 7. Превью, доступ и `deploy` для продукта

**Превью** (`confirm.Preview`, `internal/confirm/confirm.go:38-47`) для ВМ
из образа маркетплейса дополняется: тип образа и `display_name`; таблица
полей с источником (`evo`, `--meta`, файл, вопрос, default) и маскированием
`sensitive`/`*_password`; цена из `price-calculation` с разбивкой и явной
строкой про лицензию: «образ 0 ₽ по API; лицензия продукта не отражена»,
пока биллинг не подтвердит, что 0 означает «бесплатно», а не «не
тарифицируется через этот эндпоинт». Образы, на которых `price-calculation`
отвечает 422 (`alse-smolensk-1-8-3-mkp`, `alse-adv-1-7-7`), получают
предупреждение «цена образа недоступна (в биллинге нет SKU)» и оценку по
карте — не отказ: отказывать за чужой дефект данных нечестно, но и молчать
нельзя.

**Доступ.** Правило из §6: по полям образа. `public_key` есть → ssh и путь к
ключу; только пароль → пароль один раз; ни того ни другого (не встречалось)
→ «вход по документации продукта». URL продукта печатаем только как
`http://<ip>` при открытых 80/443. Пароли продукта, которых нет в
`image_metadata`, не придумываем.

**`deploy` (compose) на не-Ubuntu/Debian.** Три факта: (1) Docker ставится
`get.docker.com` (`bootstrap/install.sh:54`), который знает Ubuntu, Debian,
Raspbian, CentOS, Fedora, RHEL, SLES (по памяти; Astra, Альт, РЕД ОС — нет);
(2) если Docker уже есть, установка пропускается (`engine/deploy.go:224`);
(3) в маркетплейсе есть образ `Docker` (Docker предустановлен — гипотеза по
имени, описание не читали). Предложение:

| образ | `deploy` сегодня | предложение v1.1 |
|---|---|---|
| Ubuntu, Debian (`public`) | работает, проверено e2e | без изменений; `init` по умолчанию предлагает их |
| образ `Docker` (`marketplace`) | должен работать (Docker есть) | **кандидат в образ по умолчанию для `init`/`deploy`**: убирает установку скриптом с get.docker.com от root (BACKLOG `[sec-supply]`, QA r3 W1); проверить e2e |
| CentOS 9, Fedora 41 | скрипт их знает, не проверялось | разрешить с предупреждением «не проверялось» |
| Astra, Альт, РЕД ОС, SberLinux, Найс ОС | ВМ создаётся и оплачивается, затем `docker_failed` | **отказ до создания ВМ**: «образ X не подходит для deploy: Docker ставится только на Ubuntu/Debian/CentOS/Fedora или уже должен быть в образе. Варианты: `--image Docker`, или `vm create --image X` и установка из репозитория ОС». Рецепты по ОС (`apt install docker.io` для Astra, `apt-get install docker-engine` для Альт, `dnf install docker-ce` для РЕД ОС — по памяти, непроверено) — v1.2 после живой проверки, не раньше |
| Windows | не применимо | отказ до создания ВМ |

Семейство для этого решения читается из `name`/`display_name` по словарю
(`ubuntu|debian|centos|fedora|astra|alse|alt|redos|ред ос|sberlinux|nice|windows`);
неизвестное семейство → отказ с подсказкой `--image Docker`. Мнение: лучше
отказать и ошибиться в сторону осторожности, чем создать платную ВМ и упасть.

## 8. Варианты

| | A. Минимум | **B. Каталоги по видам (рекомендуется)** | C. Единый `product` |
|---|---|---|---|
| Суть | `list-images --type/--name`, `vm create --image <имя|UUID>` точным совпадением, без `--meta`, без плагинов | §1: `image list/get`, резолвер, `--meta`, `--port`, заполнение по `field_type`, цена из API, `k8s` addons, витрина-чтение | B + `evo product list/get/deploy` с маршрутизацией по виду, локальный реестр алиасов, кэш, условия |
| Закрывает | «Astra без UUID» — да; GitLab с портами — нет; Opensearch, РЕД ОС 7.3 — нет; плагины — нет | все четыре сессии §6 | то же |
| Плюсы | 1–2 дня | убирает три копии метаданных; совпадает с эталонами; каждая часть независимо полезна | один глагол для владельца |
| Минусы | UUID остаётся в `deploy`/`init`/конфиге; `fqdn` у РЕД ОС даст 422 после оплаты | новый пакет, миграция трёх путей (риск регрессии `deploy`, есть e2e); зависимость для JSON Schema | строит маршрутизатор без признака вида в API; `deploy` продукта = четвёртый путь ВМ; алиасы в файле расходятся с каталогом |
| Стоимость | S | M: образы 4–6 дней, плагины 3–4 дня (без живого доступа к mk8s — больше), витрина 1 день | L, 3+ недели, часть блокирована API |

Оценки — мнение исполнителя.

## 9. Изменения по слоям

**`internal/provision`.**
- `Image`: `DisplayName`, `Description`, `Type`, `MinCPU/MinRAM/MinDisk/MinGPU`,
  `CreatedTime`, `Public`; убрать мёртвые `OS`/`Status`; `ReadyIn(zoneID)`.
- `ImageMetadataField`: `DisplayName`, `DisplayPosition`, `DisplayGroup`,
  `MandatoryGroupDisplayName`; `Required`/`is_required` убрать;
  `MandatoryGroup` читать с `strings.TrimSpace` и `null` → `""`.
- `ListImages(ctx, ImageFilter{Query, Type, ZoneID, Enabled, GPU, FreeTier,
  ProjectID})` — серверные `name`/`display_name` (два запроса, объединение по
  `id`) поверх `listAll`.
- `PriceCalculation(ctx, projectID, req) (PriceResp, error)` с полями
  `Image`, `MkpImage`, `CustomImageSize`; 422 → ошибка с текстом, вызывающий
  решает (превью — предупреждение).

**Новый `internal/images`** (без вывода и cobra):
- `Catalog.Load`, `Search(query, filter)`, `Resolve(ref) (Image, []Image,
  error)`: UUID → точный `name` (без регистра) → точный `display_name` →
  единственная подстрока → `NOT_FOUND`/`USAGE`-неоднозначность.
- `Family(img) (family string, version string, ok bool)` по словарю §7 —
  для `init`, `deploy` и пометки «ОС» в витрине; не для выбора «последней
  версии» у маркетплейса.
- `FillMetadata(detail, inputs, auto) (meta, missing, filled, err)` —
  **по `field_type`**: `linux_login` → логин (default или `user1` — один
  default на все пути, расхождение `ubuntu`/`user1` закрывается), `public_key`
  → ключ, `linux_password`/`plain_password` в группе аутентификации →
  случайный пароль, `hostname` (включая `fqdn`) → имя ВМ (для `fqdn` —
  `<имя>.local`? — вопрос compute, пока имя), `flag` с default → default,
  `text` без default и без группы → `missing`. Имя поля — вторичный ключ для
  старых образов. `missing` — то, что спрашиваем или требуем в `--meta`.
  `filled[i].Source`, `.Sensitive` — для превью.
- `Access(detail) (kind ssh|password|unknown, login)` — для строки
  «Подключение».
- `DockerSupport(img) (ok, tested bool, hint string)` — таблица §7.

**`internal/engine`.** `Deploy`/`init` → `images.Resolve`/`FillMetadata`;
`Config.Image` (`image:` в YAML, `CLOUDRU_IMAGE`), `ImageID` — синоним на
релиз; `init` предлагает семейства `public` и образ `Docker`, помечая
пригодность для `deploy`; отказ `deploy` по `DockerSupport` до создания ВМ.

**`internal/tools`.**
- `image_list` (новый; `list_images` — алиас): `query`, `type`, `zone`,
  `gpu`, `all`, `limit` (50); строки `{id, name, display_name, type,
  zones_ready, min_disk_gb, family, access, fields_required}` — без
  `availability_zones[]`, `icon`, `user_data_template` (сейчас `list_images`
  отдаёт объекты с зонами, `image_get` — SVG и Jinja целиком: сотни строк
  контекста агента).
- `image_get`: аргумент `image` (имя|UUID), `full=true` для шаблона и иконки;
  по умолчанию карточка §6.2 в JSON: `fields[{name, display_name, field_type,
  required, sensitive, filled_by: "evo"|"user"|"default"}]`, `access`,
  `docker_support`, `price_note`.
- `provision`, `deploy`, `deploy_plan`: `image` (имя|UUID; `image_id` —
  синоним), `image_metadata: {k: v}`, `ports: [..]` у `provision` (как у
  `deploy_plan`). Превью `confirmation_required`: `image{name, display_name,
  type}`, `metadata_fields[{name, source, sensitive}]` без значений
  секретов, `price{source: api|estimate, image, mkp_image, note}`, `ports`.
  Неоднозначность → `isError: true`, `reason: USAGE`, `candidates[]`; не
  найдено → `NOT_FOUND`; пропущенные поля → `USAGE`, `missing_fields[]` —
  агент добирает у человека, в v1.1+ через elicitation (D10).
- Плагины: `k8s_addons` (`query`, `billable`, `category`), `k8s_addon_versions`
  (`name`), `k8s_cluster_addons` (`cluster_id`), `k8s_add_addon`
  (`cluster_id`, `name`, `app_version`, `config` объект, `resolve_mode`,
  `confirmed`; mutating), `k8s_delete_addon` (`preserve`, `confirmed`). Клиент
  mk8s уже есть (`internal/mk8s`), добавляются методы `/v3/addons`,
  `/v3/clusterAddons`.
- `marketplace_search` (`query`, `kind`) — объединение `image_list` и
  `k8s_addons` с полем `deploy_with` (имя инструмента и аргументы). Один
  инструмент вместо двух вызовов — экономия контекста агента.

**`internal/cli`.** Деревья в `trees.go`: группа `image` (`list`, `get`);
в `k8s` — `addons`, `addon-versions`, `cluster-addons`, `add-addon`,
`delete-addon` (в стиле `add-nodepool`); группа `marketplace` (`list`).
`vm create`: `--image`, `--meta k=v` (повторяемый), `--meta-file`, `--port`.
`list-images`, `--image-id` — скрытые синонимы до v1.2 (как `cloudru-vm`,
D2). Скрытый ввод секретов — `golang.org/x/term` уже в зависимостях
(`confirm.go:28`). JSON Schema для `configurationSchema` — новая зависимость
(например, `santhosh-tekuri/jsonschema`); без неё — проверка только
`required` верхнего уровня своим кодом. Мнение: начать без зависимости.

**Общее с ядром создания ВМ.** `images.Resolve` → `images.FillMetadata` →
`provision.CreateVMRequest{ImageName, ImageMetadata}` → `confirm.Preview` →
`sgrules.Open(ports)` — для всех трёх путей. Это вынос общего, не четвёртый
путь; полное слияние трёх путей — отдельная работа (`command-model.md` §5,
BACKLOG `[cli-practice]`).

## 10. План и риски

**v1.1 — образы (M):**
1. `provision`: поля образа и метаданных по спецификации, `ImageFilter`,
   `PriceCalculation`.
2. `internal/images` с тестами на фикстурах из лога api-probe (имена и
   поля 60 образов публичны): неоднозначность, `fqdn`, пробелы в
   `mandatory_group`, два логин-поля, Windows без ключа, Opensearch с
   полями продукта.
3. `evo image list/get`, `vm create --image/--meta/--port`, `deploy --image`,
   `init --image`; синонимы старых флагов; отказ `deploy` до создания ВМ.
4. Превью с полями и ценой; `docs/commands.md`, `docs/mcp.md`, BACKLOG.
5. e2e: `vm create --image AstraLinuxSmolensk1.8.3KES` (создаёт платную ВМ —
   только вручную, с `destroy`), `deploy` на образе `Docker`.

**v1.1 — плагины (M, после живой read-only пробы mk8s `/v3/addons`):**
клиент, `k8s addons`/`addon-versions`/`cluster-addons` (чтение), затем
`add-addon`/`delete-addon` с гейтом и e2e на тестовом кластере.

**v1.1 — витрина (S):** после образов и плагинов.

**v1.2:** `--meta-file`, `EVO_META_*`, вопросы на терминале для полей
продукта и `configurationSchema`; elicitation в MCP (D10); рецепты Docker
для отечественных ОС после проверки; удаление `list-images`/`--image-id`;
кэш каталога — только по измерениям (сейчас одна страница, кэш не нужен).

**Упирается в API / ответы Cloud.ru (не планируем):** цена и лицензия
продукта в превью как факт, а не как предупреждение; карточка, категории,
документация и пароль администратора продукта; SaaS и прочие виды без API;
принятие условий.

**Риски.**
- Регрессия `deploy` при выносе `FillMetadata` (три копии различаются
  деталями); снимается e2e и тестом «запрос для Ubuntu-24.04 байт в байт как
  раньше».
- Правило «пусто + нет default → обязательное» может потребовать значения,
  которое платформа считает необязательным (Opensearch); тогда образ не
  создаётся без `--meta` — хуже, чем 500 после оплаты, но заметно. Снимается
  ответом compute (§12) или пробой с пустым значением на e2e.
- Проектная выборка (`project_id`) скрывает 23 общих образа — если резолвер
  будет искать только с `project_id`, часть каталога пропадёт. Искать без
  `project_id`, образы проекта добирать отдельно (как `list_images` сейчас).
- `price-calculation` 422 на части образов — превью должно деградировать до
  оценки с предупреждением, не падать.
- Плагины mk8s вживую не проверены вовсе: формы ответов, `filter`,
  `configurationSchema`, цена. Сначала read-only проба.

## 11. Read-only проверки

Есть два места: `api-probe.yml` (сырые GET, уже содержит пробы каталога
образов — `.github/workflows/api-probe.yml:106-162`) и job `readonly` в
`cloud-e2e.yml` (`.github/scripts/cloud-readonly.sh`, через CLI). В лог —
коды, имена полей, счётчики и публичные имена каталога; ID и данные проекта
не печатать.

Добавить в `api-probe.yml`:

```sh
echo "== образы: почему проектная выборка меньше общей"
probe images_shared_vs_project "$C/images?$P&limit=100" '{total, missing_vs_shared: null}'   # сравнить множества name с images?limit=100 в jq; печатать только count и имена отсутствующих
probe images_enabled_names     "$C/images?enabled=false&limit=100" '{total, names: [.items[].name]}'  # совпадает ли с тестовыми

echo "== образы: описание и признаки вида (для витрины)"
probe images_desc_len          "$C/images?type=marketplace&limit=100" '[.items[] | {name, desc_len: (.description|length), has_url: ((.description // "") | test("https?://"))}]'

echo "== цена: Windows и приложения (image / mkp_image > 0?)"
# price для windows2022std-evo-kmsB, GitLab, Docker — как в шаге price, печатать только флаги

echo "== mk8s: каталог плагинов (вживую не проверялся)"
M=https://mk8s.api.cloud.ru
probe addons_list      "$M/v3/addons?projectId=$PROJECT&pageSize=100" '{n: (.addons|length), names: [.addons[].name], billable: ([.addons[] | select(.isBillable)] | length), categories: ([.addons[].categories[]] | unique)}'
probe addons_filter    "$M/v3/addons?projectId=$PROJECT&filter=isBillable%3Dtrue" '{n: (.addons|length)}'
for a in $(curl -sS -H "Authorization: Bearer $TOKEN" "$M/v3/addons?projectId=$PROJECT&pageSize=100" | jq -r '.addons[].name' | head -5); do
  probe "addon_versions_$a" "$M/v3/addons/$a?projectId=$PROJECT" '{n: (.releases|length), modes: ([.releases[].mode]|unique), schema_len: ((.releases[0].configurationSchema // "")|length), required: ((.releases[0].configurationSchema // "{}") | fromjson? | .required // null)}'
done
# цена плагина: найти эндпоинт расчёта в v3 (в v2 — CalculatePriceRequest_Addon); печатать http и keys
```

В `cloud-readonly.sh` (через CLI, когда команды появятся): `evo image list
astra --json | jq length` = 7; `evo image get AstraLinuxSmolensk1.8.3KES
--json | jq '.fields | map(.name)'`; `evo vm create x --image astra --json
--non-interactive` → код 1, `reason USAGE`, 7 кандидатов; `--image nosuch`
→ код 4; `evo k8s addons --json | jq length`.

## 12. Вопросы

**Владельцу:**
1. Витрина `evo marketplace list` только для чтения, развёртывание — через
   `vm create --image` и `k8s add-addon`: согласны, или нужен один глагол
   несмотря на §5?
2. `deploy` на Astra/Альт/РЕД ОС: отказ до создания ВМ (предложено) или
   попытка с предупреждением? Образ `Docker` как умолчание для `init` — да?
3. Подстрока с единственным совпадением проходит без вопроса
   (`--image alse-base`) — приемлемо для платного действия, или только точное
   имя и меню?
4. Нужен ли `--port` у `vm create` в v1.1 (без него GitLab/Windows снаружи
   недоступны)?
5. Какие плагины mk8s важны первыми (для порядка проверки и e2e)?

**Маркетплейс:**
1. Лицензии Astra, Альт, РЕД ОС, Windows Server, Platform V, NGFW: как
   тарифицируются при создании ВМ через API — автоматически в счёте
   (pay-as-you-go), BYOL, или ВМ не создастся без шага в консоли? Почему у
   `alse-smolensk-1-8-3-mkp` и `alse-adv-1-7-7` нет SKU — они вообще
   создаются?
2. Будет ли API каталога: карточка (документация, ссылка, вендор,
   категория, вид «ОС / приложение / плагин / SaaS»), условия, цена?
3. Где покупателю брать пароль администратора приложения (GitLab, Keycloak,
   Passbolt) — есть ли единое правило (поле образа, файл на ВМ, первый вход)?
4. Тестовые образы (`null`, `alt-test`, `astra-voronej-cc11079`) видны всем
   клиентам в `type=marketplace` — это ожидаемо?

**Compute:**
1. Признак вида образа (ОС / приложение) и семейство/версия ОС в
   `PublicImageResponse` — планируются?
2. Почему `GET /images?project_id=…` отдаёт 83 (в том числе 63 общих) против
   86 без `project_id`: фильтр по зоне, по доступности, по правам?
3. Правило обязательности `image_metadata`: `mandatory_group` плюс пустой
   `default_value`? Поле `text` без группы и без default (Opensearch) —
   обязательно или нет? Что ожидается в `fqdn` (`field_type` hostname)?
   Пробелы и `null` в `mandatory_group` — дефект данных или норма?
4. Что означает `enabled=false` у 8 образов — тестовые или временно скрытые?
5. `cloud_init` в `POST /api/v1.1/vms` — поверх `user_data_template` или
   вместо (BACKLOG `[05]`)?
6. Тестовые и сломанные образы в `public` (`debian-11_error`, `img-nester-*`,
   `ubuntu-test2204`) — можно ли их не публиковать?

**mk8s:**
1. Каким эндпоинтом в v3 считается цена платного плагина (`isBillable`)?
   В v2 есть `CalculatePriceRequest_Addon`.
2. Есть ли у плагинов связь с карточками маркетплейса (один и тот же продукт
   как образ ВМ и как плагин, например GitLab)?
3. `addonConfiguration` — JSON строкой с подстановками `{{clusterID}}`:
   экранирование и лимит размера?

**Биллинг:**
1. `price-calculation`: `image` и `mkp_image` = 0 для всех проверенных
   образов, включая Astra KES и Windows (Windows — непроверено) — это
   «бесплатно» или «лицензия считается другим механизмом»? Что такое
   `custom_image_size`?
2. Можно ли считать `price-calculation` публичным источником цены для
   превью CLI (стабильность, права сервисного аккаунта)?
3. Появится ли плата за лицензию отдельной строкой в счёте и в этом ответе?

## Источники

- Спецификации compute и mk8s (`scripts/cloudru-specs.py`, реестр
  https://cloud.ru/docs/console_api/ug/topics/overview__reestr_api), 16.10.2026.
- Лог api-probe run 38868189205 (шаги `images_*`, `name_*`, `display_*`,
  `filter_*`, `image_fields`); итог проб #2–#4 — `docs/internal/standalone/BACKLOG.md` `[images]`.
- `docs/internal/command-model.md` (§5 три пути создания ВМ, §7 грамматика).
- https://cloud.ru/docs/images/ug/topics/concepts__marketplace-images
- https://cloud.ru/docs/virtual-machines/ug/topics/concepts__images
- https://cloud.ru/docs/marketplace-partner/ug/topics/quickstart
- https://learn.microsoft.com/en-us/cli/azure/vm/image/terms
- https://learn.microsoft.com/en-us/azure/aks/deploy-extensions-az-cli
- https://aws.amazon.com/blogs/awsmarketplace/deploy-third-party-software-add-ons-aws-marketplace-amazon-eks-clusters/
- https://yandex.cloud/ru/docs/compute/cli-ref/image/get-latest-from-family
- https://yandex.cloud/en-ru/docs/compute/operations/images-with-pre-installed-software/create
- https://docs.digitalocean.com/reference/doctl/reference/compute/image/list-application/
- https://docs.digitalocean.com/reference/doctl/reference/kubernetes/1-click/list/
- https://docs.cloud.google.com/marketplace/docs/deploy-through-CLI
- https://techdocs.akamai.com/quick-deploy-apps/docs/get-started
