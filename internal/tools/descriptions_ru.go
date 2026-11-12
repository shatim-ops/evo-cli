// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

// A tool is described twice, from one place: in English for the agent
// (Tool.Description and the schema's descriptions — what MCP and
// 'evo tools describe' with EVO_LANG=en show) and in Russian for the person
// at the CLI (toolsRU: the help of the tool's command, 'evo tools describe'
// and 'evo tools list' in Russian). MCP stays English (D6); the Russian
// texts are not in the schema, which MCP clients get as it is
// (docs/internal/cli-mcp-parity/spec.md, REQ-5).
//
// The Russian texts are written for the CLI: consent is the prompt or --yes
// (the CLI adds that line itself), not confirmed=true; another tool is named
// by its command ('evo vm list'), or as 'evo tools call <tool>' while it has
// none; other arguments are named as flags (--subnet-id).
// TestRegistry_DescriptionsBothLanguages keeps every tool and every argument
// described in both languages.

// toolRU is the Russian description of a tool.
type toolRU struct {
	// Short is the summary: one phrase without a final period (the
	// command's Short).
	Short string
	// Long is the rest of the description; the command's help is Short,
	// a period, then Long.
	Long string
	// Args describe the arguments, by name. confirmed with the common
	// English text (confirmedDesc) gets confirmedDescRU without an entry.
	Args map[string]string
}

// confirmedDescRU is confirmedDesc in Russian.
const confirmedDescRU = "Должно быть true; ставится только после согласия пользователя"

// argDescriptionsRU is Tool.ArgDescriptionsRU: the Russian texts of ru, and
// confirmedDescRU for a confirmed argument with the common English text.
func argDescriptionsRU(t Tool, ru map[string]string) map[string]string {
	out := make(map[string]string, len(ru)+1)
	for k, v := range ru {
		out[k] = v
	}
	props, _ := t.InputSchema["properties"].(map[string]any)
	if p, ok := props["confirmed"].(map[string]any); ok && out["confirmed"] == "" && p["description"] == confirmedDesc {
		out["confirmed"] = confirmedDescRU
	}
	return out
}

// toolsRU are the Russian descriptions of the tools, by tool name, grouped
// as the commands are (cliSpecs). New sets Tool.ShortRU,
// Tool.DescriptionRU and Tool.ArgDescriptionsRU from it.
var toolsRU = map[string]toolRU{
	// vm
	"vm_list": {
		Short: "Список ВМ проекта: id, имя, состояние, зона, время создания",
		Long: "Все ВМ проекта по данным облака, а не по локальным записям развёртываний: id, имя, состояние, " +
			"зона доступности и время создания. Только чтение. IP-адреса и интерфейсы одной ВМ — 'evo vm get <ВМ>'.",
	},
	"vm_get": {
		Short: "ВМ по ID или имени: внутренний и внешний IP, состояние, интерфейсы",
		Long: "Сведения о ВМ из облака по UUID или имени: внутренний IP (private_ip), внешний или floating IP (public_ip), " +
			"состояние и все сетевые интерфейсы. В отличие от 'evo status', который читает локальную запись развёртывания, " +
			"показывает и внутренний IP — его, например, прописывают в настройки другого сервиса. Только чтение.",
		Args: map[string]string{
			"name":  "Имя ВМ (вместо vm_id)",
			"vm_id": "UUID ВМ (из ответа 'evo vm create', выходных данных стека или 'evo vm list'). Нужен vm_id или name",
		},
	},
	"vm_start": {
		Short: "Запустить остановленную ВМ и дождаться состояния running",
		Long: "Запустить остановленную ВМ (power_on) и дождаться, пока она заработает. До вопроса evo читает ВМ и показывает " +
			"её имя, id и состояние. ВМ уже работает — ответ already_running без вопроса; ВМ уже запускается — evo ждёт " +
			"и второй команды не шлёт, поэтому повтор безопасен; повтор, которому нужно отправить команду, спрашивает заново. Запущенная ВМ тарифицируется по своей конфигурации: " +
			"цена неизвестна, ресурс платный.",
		Args: map[string]string{
			"name":  "Имя ВМ (вместо vm_id)",
			"vm_id": "UUID ВМ (список: evo vm list). Нужен vm_id или name",
		},
	},
	"provision": {
		Short: "Создать ВМ (по умолчанию с floating IP; без внешнего адреса — --no-public-ip)",
		Long: "Создать ВМ без docker-compose (для проекта docker-compose — 'evo deploy'). ВМ платная. По умолчанию ВМ " +
			"получает floating IP; с --no-public-ip — приватная ВМ, узел за балансировщиком или внутренний сервер.\n\n" +
			"Поля метаданных образа (SSH-ключ, имя ВМ, логин, случайный пароль) evo заполняет сам; обязательное поле, " +
			"которое evo заполнить не может ('evo image get <образ>'), задаётся через --meta.\n\n" +
			"Отказ до создания чего-либо: нет образа или зоны (--region, иначе зона профиля или CLOUDRU_REGION), образ " +
			"недоступен проекту, не задано обязательное поле.\n\n" +
			"В ответе — ssh, password (сгенерированный пароль — только в этом ответе: evo его не хранит) и state_dir " +
			"(каталог с ключом и state: evo status и evo vm delete видят ВМ, только если запускать их из того же каталога).\n\n" +
			"С --wait=false отвечает сразу, как облако приняло создание: статус creating и пароль; повтор той же " +
			"команды из того же каталога доделывает эту ВМ, второй не создаёт.",
		Args: map[string]string{
			"disk_size": "Размер загрузочного диска, ГБ (по умолчанию 10, но не меньше минимума образа)",
			"flavor":    "Flavor ВМ: имя или UUID. По умолчанию — самый маленький lowcost не бесплатного уровня",
			"image":     "Образ: имя или UUID, например Ubuntu-24.04. Задаётся --image или --image-id, не оба",
			"image_id":  "UUID образа",
			"image_metadata": "Поле метаданных образа, например admin_password=...; перекрывает заполненное evo, поле, " +
				"которого в образе нет, отклоняется",
			"name": "Имя ВМ",
			"wait": "Ждать ли ВМ: по умолчанию команда ждёт, пока ВМ запустится; --wait=false — ответ сразу, как облако " +
				"приняло создание (статус creating), закончит повтор той же командой",
			"no_public_ip": "Приватная ВМ без floating IP: узел за балансировщиком или внутренний сервер",
			"ports": "Входящий порт для --cidr: 80, 53/udp (по умолчанию tcp), в общей группе безопасности зоны; " +
				"evo vm delete удаляет правила, созданные evo",
			"cidr":       "CIDR источника правил портов (по умолчанию 0.0.0.0/0 — откуда угодно). Уже открытые правила не меняются",
			"project_id": "UUID проекта",
			"region":     "Зона доступности: имя или UUID. По умолчанию — из профиля или CLOUDRU_REGION",
			"subnet_id":  "UUID или имя подсети (по умолчанию — подсеть в зоне ВМ)",
		},
	},
	"destroy": {
		Short: "Удалить ВМ и все данные на ней (безвозвратно)",
		Long: "Удалить ВМ и все данные на ней безвозвратно. ВМ задаётся UUID или именем (имя, которое носят несколько ВМ, — " +
			"отказ: укажите UUID); превью называет ВМ, её floating IP и правила портов, которые evo открыл для неё (из любого " +
			"каталога). Они удаляются тоже, если порт не нужен развёртыванию из локального состояния; ВМ другого каталога, " +
			"которой нужен такой порт, его теряет. Правило, которое удалить не удалось, попадает в отчёт, и следующее " +
			"удаление попробует снова. Если Cloud.ru принял удаление, но ВМ осталась в списке, — код 5: floating IP, " +
			"правила и запись остаются до повторного удаления.",
		Args: map[string]string{
			"name":       "Имя ВМ (вместо vm_id)",
			"release_ip": "Освободить и floating IP ВМ.",
			"vm_id":      "UUID ВМ",
		},
	},
	"list_zones": {
		Short: "Зоны доступности (UUID и имя)",
		Long: "Зоны доступности: UUID и имя. UUID зоны передаётся в --region, например 'evo vm create --region <UUID>'. " +
			"Только чтение.",
	},
	"list_flavors": {
		Short: "Flavor ВМ: vCPU, доля vCPU, RAM, GPU, spot, зоны, оценка цены за месяц",
		Long: "Flavor ВМ: имя, vCPU, RAM, признак бесплатного уровня и оценка цены за месяц, а также то, что каталог " +
			"говорит сверх размера: cpu_share — гарантированная доля vCPU, как её пишет каталог (1:1 — 100 %, 1:3 — 30 %, " +
			"1:10 — 10 %, спецификация Cloud.ru), gpu, spot (прерываемая ВМ), zones — зоны, где ВМ можно создать " +
			"(пустой список — каталог называет зоны, но ни одна не открыта). Поля, которых каталог не прислал, не выводятся. Фильтры оставляют только " +
			"flavor, о которых каталог говорит это. Имя flavor передаётся в --flavor команд 'evo vm create' и 'evo deploy'. " +
			"Только чтение.",
		Args: map[string]string{
			"cpu":   "Только flavor с таким числом vCPU",
			"ram":   "Только flavor с таким объёмом RAM, ГБ",
			"share": "Только flavor с такой гарантированной долей vCPU, как её пишет каталог: 1:1, 1:3, 1:10",
			"zone":  "Только flavor, с которыми ВМ можно создать в этой зоне (имя из evo vm zones)",
			"gpu":   "true — только flavor с GPU, false — только без GPU",
			"spot":  "true — только прерываемые (spot) flavor, false — только остальные",
		},
	},

	// image
	"image_list": {
		Short: "Поиск образов: ОС (public), маркетплейс (marketplace), образы проекта (private)",
		Long: "Поиск по каталогу образов ВМ: образы ОС (тип public), продукты Маркетплейса (marketplace) " +
			"и собственные образы проекта (private). Образы, не готовые ни в одной зоне или недоступные проекту, скрыты; " +
			"--all показывает и их (недоступные — с пометкой). В строке — id, имя, отображаемое имя, тип, зоны, где образ " +
			"готов, и минимальный размер диска. Только чтение. Какие поля нужны ВМ из образа — 'evo image get <образ>'.",
		Args: map[string]string{
			"all":   "Показать и скрытые образы (в общем каталоге есть тестовые и неисправные)",
			"query": "Подстрока имени или отображаемого имени без учёта регистра, например ubuntu. По умолчанию — весь каталог",
			"type":  "Только этот тип. По умолчанию — все три",
			"zone":  "Только образы, готовые в этой зоне доступности: имя или UUID",
		},
	},
	"image_get": {
		Short: "Образ по имени или UUID: минимум ресурсов, зоны, поля и кто их заполнит, команда создания ВМ",
		Long: "Образ ВМ по имени, отображаемому имени или UUID. Поиск — как в 'evo image list': точное имя, точное " +
			"отображаемое имя, затем единственное совпадение по подстроке; при нескольких совпадениях — ошибка со списком " +
			"кандидатов. Показывает, готов ли образ и в каких зонах, и поля метаданных образа с тем, кто заполнит каждое: evo " +
			"(SSH-ключ, имя ВМ, логин, случайный пароль), значение по умолчанию, пользователь (обязательное поле, которое evo " +
			"заполнить не может: задаётся через --meta команды 'evo vm create') или никто. В конце — команда создания ВМ из " +
			"образа. Образ, недоступный проекту, показывается с пояснением и без команды создания. Значок и шаблон cloud-init " +
			"(user_data_template) — только с --full. Только чтение. Так же следят за сборкой образа: повторяйте команду, " +
			"пока образ не станет готов.",
		Args: map[string]string{
			"full":     "Вывести и значок SVG, и шаблон cloud-init user_data_template (большой объём)",
			"image":    "Имя, отображаемое имя или UUID образа (поиск: evo image list). Нужен image или image_id",
			"image_id": "UUID образа (поиск: evo image list); image принимает и имя",
		},
	},

	// marketplace
	"marketplace_list": {
		Short: "Витрина: ОС, продукты маркетплейса и плагины Kubernetes одним списком, с командой развёртывания",
		Long: "Витрина того, что Cloud.ru предлагает развернуть, одним списком: образы ВМ общего каталога и плагины " +
			"Managed Kubernetes. Только чтение. vm-os — эвристика: образ типа public или образ маркетплейса, имя " +
			"которого начинается с названия семейства ОС. Образы, недоступные проекту или не готовые ни в одной зоне, " +
			"скрыты; --all показывает и их.\n\n" +
			"У каждой строки — команда развёртывания: для образа — 'evo vm create <имя-ВМ> --image <образ>', для плагина — " +
			"'evo k8s add-addon <cluster-id> <плагин>'; у образа, недоступного проекту, команды нет. Платность известна " +
			"только у плагинов: цена образа неизвестна. Если один каталог прочитать не удалось, список строится по " +
			"другому с предупреждением.",
		Args: map[string]string{
			"all": "Показать и скрытые образы: недоступные проекту и не готовые ни в одной зоне",
			"kind": "Только этот вид: vm-os (образы ОС), vm-app (продукты Маркетплейса на ВМ) или k8s-addon. " +
				"По умолчанию — все три",
			"query": "Подстрока имени или отображаемого имени (у плагинов — и описания) без учёта регистра. По " +
				"умолчанию — всё",
		},
	},

	// disk
	"disk_list": {
		Short: "Диски: id, имя, размер, состояние, тип, зона, к каким ВМ подключены",
		Long:  "Диски проекта: id, имя, размер (ГБ), состояние, тип, зона и ВМ, к которым диск подключён. Только чтение.",
	},
	"list_disk_types": {
		Short: "Типы дисков (UUID и имя, например SSD или HDD)",
		Long: "Типы дисков: UUID и имя (например, SSD или HDD), а если каталог их называет — min_size_gb и max_size_gb, " +
			"free_tier, зоны, где тип включён, и speed_limits (скорость по размеру диска, в единицах спецификации Cloud.ru). " +
			"Имя или UUID типа передаётся в --disk-type команды 'evo disk create'. Только чтение.",
	},
	"disk_create": {
		Short: "Создать диск (с --attach-to — в зоне ВМ и сразу подключить)",
		Long: "Создать диск. С --attach-to <ВМ> диск создаётся в зоне этой ВМ и сразу к ней подключается: диск и ВМ " +
			"должны быть в одной зоне. Подключённый диск — новое блочное устройство, файловая система ВМ от него не растёт: " +
			"разметьте, отформатируйте и смонтируйте его на ВМ. Увеличить существующий диск — 'evo disk resize'. Диск платный.",
		Args: map[string]string{
			"attach_to": "ВМ (имя или UUID): диск создаётся в её зоне и сразу к ней подключается",
			"disk_type": "Тип диска: имя или UUID (список: evo disk types). По умолчанию — SSD",
			"name":      "Имя диска",
			"size_gb":   "Размер диска, ГБ (обязательный; не больше 16384)",
			"zone":      "Зона доступности: имя или UUID, вместо --attach-to (по умолчанию — зона из --region или профиля)",
		},
	},
	"disk_attach": {
		Short: "Подключить диск к ВМ (диск и ВМ должны быть в одной зоне)",
		Long: "Подключить существующий диск к ВМ (--vm — имя или UUID ВМ). Диск и ВМ должны быть в одной зоне; " +
			"зону и id диска показывает 'evo disk list'. Подключённый диск — новое блочное устройство: разметьте, " +
			"отформатируйте и смонтируйте его на ВМ.",
		Args: map[string]string{
			"disk_id": "UUID диска (список: evo disk list)",
			"vm":      "ВМ: имя или UUID",
		},
	},
	"disk_detach": {
		Short: "Отключить диск от ВМ",
		Long: "Отключить диск от ВМ (--vm — ВМ, к которой он подключён). Загрузочный или используемый диск не " +
			"отключайте: сначала отмонтируйте файловую систему на ВМ.",
		Args: map[string]string{
			"disk_id": "UUID диска (список: evo disk list)",
			"vm":      "ВМ, к которой подключён диск: имя или UUID",
		},
	},
	"disk_resize": {
		Short: "Увеличить диск (только рост; затем расширьте раздел и файловую систему на ВМ)",
		Long: "Увеличить диск на месте, до 16384 ГБ (16 ТБ). Только рост: уменьшение и диски бесплатного уровня облако " +
			"отклоняет (бесплатный загрузочный диск не увеличить — пересоздайте ВМ с платным диском). После увеличения " +
			"расширьте раздел и файловую систему на ВМ (growpart и resize2fs).",
		Args: map[string]string{
			"disk_id": "UUID диска (список: evo disk list)",
			"size_gb": "Новый размер, ГБ: больше текущего, не больше 16384",
		},
	},
	"disk_delete": {
		Short: "Удалить диск и все данные на нём (безвозвратно)",
		Long:  "Удалить диск и все данные на нём безвозвратно. Подключённый диск сначала отключите: 'evo disk detach'.",
		Args: map[string]string{
			"disk_id": "UUID диска (список: evo disk list)",
		},
	},

	// vpc
	"vpc_list": {
		Short: "Список VPC проекта",
		Long:  "VPC (виртуальные частные облака) проекта: id, имя, признак VPC по умолчанию, тип. Только чтение.",
	},
	"vpc_get": {
		Short: "VPC по ID: все сведения",
		Long:  "Все сведения об одном VPC по id (список: 'evo vpc list'). Только чтение.",
		Args: map[string]string{
			"id": "UUID VPC (список: evo vpc list), обязательный",
		},
	},
	"vpc_create": {
		Short: "Создать VPC (асинхронно)",
		Long: "Создать VPC (виртуальное частное облако) — только VPC: подсеть создаётся отдельно (в консоли; " +
			"создания подсетей в evo пока нет), а evo lb create для VPC без подсети отказывает. Операция " +
			"асинхронная: ход — " +
			"'evo vpc operation <operation-id>'.",
		Args: map[string]string{
			"description": "Описание VPC (необязательно)",
			"name":        "Имя VPC (обязательный)",
		},
	},
	"vpc_delete": {
		Short: "Удалить VPC (асинхронно; сначала удалите его подсети и ресурсы)",
		Long: "Удалить VPC по id (список: 'evo vpc list'). Сначала удалите его подсети и другие ресурсы. " +
			"Операция асинхронная: ход — 'evo vpc operation <operation-id>'.",
		Args: map[string]string{
			"wait": deleteWaitRU,
			"id":   "UUID VPC (список: evo vpc list), обязательный",
		},
	},
	"vpc_operation": {
		Short: "Состояние асинхронной операции VPC",
		Long: "Состояние асинхронной операции VPC по id — его возвращают 'evo vpc create', 'evo vpc delete', " +
			"'evo vpc route-create' и 'evo vpc route-delete': done — завершена ли операция, error — ошибка, elapsed_sec — сколько она идёт, resource_status — состояние её ресурса. С --wait-seconds " +
			"команда сама ждёт завершения до N секунд и выводит итоговое состояние. Только чтение.",
		Args: map[string]string{
			"operation_id": "UUID операции (обязательный)",
			"wait_seconds": opWaitDescRU,
		},
	},
	"vpc_routes": {
		Short: "Статические маршруты VPC",
		Long: "Статические маршруты VPC: id, подсеть назначения, тип следующего узла (next_hop_type), состояние. " +
			"Только чтение.",
		Args: map[string]string{
			"vpc_id": "UUID VPC (список: evo vpc list), обязательный",
		},
	},
	"vpc_route_create": {
		Short: "Добавить статический маршрут в VPC (асинхронно)",
		Long: "Добавить статический маршрут в VPC. Назначение — CIDR (--subnet); следующий узел — интерфейс ВМ " +
			"(--next-hop-vm-id и --next-hop-interface-id), виртуальный IP (--next-hop-vip-id) или Magic Router " +
			"(--next-hop-magic-router-id и --next-hop-az). Операция асинхронная: ход — 'evo vpc operation <operation-id>'.",
		Args: map[string]string{
			"description":              "Описание маршрута (необязательно)",
			"next_hop_az":              "Зона доступности (например, ru.AZ-1) для следующего узла Magic Router",
			"next_hop_interface_id":    "UUID сетевого интерфейса ВМ (вместе с --next-hop-vm-id)",
			"next_hop_magic_router_id": "UUID Magic Router — следующего узла (вместе с --next-hop-az)",
			"next_hop_vip_id":          "UUID виртуального IP — следующего узла",
			"next_hop_vm_id":           "UUID ВМ, интерфейс которой — следующий узел (вместе с --next-hop-interface-id)",
			"subnet":                   "CIDR назначения, например 192.168.0.0/24 (обязательный)",
			"vpc_id":                   "UUID VPC (обязательный)",
		},
	},
	"vpc_route_delete": {
		Short: "Удалить статический маршрут из VPC (асинхронно)",
		Long: "Удалить статический маршрут из VPC по id маршрута (список: 'evo vpc routes <vpc-id>'). " +
			"Операция асинхронная: ход — 'evo vpc operation <operation-id>'.",
		Args: map[string]string{
			"route_id": "UUID статического маршрута (список: evo vpc routes), обязательный",
			"vpc_id":   "UUID VPC (обязательный)",
		},
	},
	"list_subnets": {
		Short: "Подсети: UUID, имя, зона доступности",
		Long: "Подсети проекта: UUID, имя, зона доступности. UUID или имя подсети передаётся в --subnet-id, например " +
			"'evo vm create --subnet-id <UUID или имя>'. Только чтение.",
	},
	"list_security_groups": {
		Short: "Группы безопасности (с --rules — вместе с правилами)",
		Long: "Группы безопасности проекта: UUID, имя, зона доступности. С --rules — вместе с правилами " +
			"каждой группы: id, направление, протокол, диапазон портов, источник, описание, состояние. Только чтение.",
		Args: map[string]string{
			"rules": "Вывести и правила каждой группы (по одному запросу к API на группу)",
		},
	},
	"list_floating_ips": {
		Short: "Floating IP: адрес, состояние, свободен ли",
		Long: "Публичные IP-адреса (floating IP) проекта: адрес, состояние, свободен ли адрес для привязки, " +
			"а если API её называет — зона адреса: адрес привязывается к ВМ своей зоны. Только чтение.",
	},
	"fip_attach": {
		Short: "Привязать свободный floating IP к основному интерфейсу ВМ",
		Long: "Привязать существующий свободный публичный IP (floating IP) к основному интерфейсу ВМ: ВМ получает " +
			"внешний адрес. Нужно, когда у ВМ нет публичного IP — например, создание ВМ из большого собственного образа " +
			"завершилось раньше, чем облако назначило адрес. Свободный адрес — в 'evo vpc fips'.",
		Args: map[string]string{
			"fip": "Floating IP: адрес (например, 203.0.113.10) или UUID (список: evo vpc fips)",
			"vm":  "ВМ, к которой привязать адрес: имя или UUID",
		},
	},
	"release_floating_ip": {
		Short: "Освободить floating IP (адрес, занятый ВМ или SNAT-шлюзом, не освобождается)",
		Long: "Освободить (удалить) публичный IP: адрес уходит из проекта. Адрес задаётся аргументом <id> " +
			"или флагом --ip-address. Адрес, занятый ВМ, не освобождается: сначала отвяжите его или удалите ВМ " +
			"('evo vm delete'). Адрес SNAT-шлюза тоже не освобождается: он удаляется вместе со шлюзом " +
			"('evo snat delete').",
		Args: map[string]string{
			"id":         "UUID floating IP (список: evo vpc fips). Нужен id или ip_address, не оба",
			"ip_address": "Публичный IP-адрес, например 203.0.113.11 (вместо аргумента <id>)",
		},
	},

	// k8s
	"k8s_list": {
		Short: "Кластеры Managed Kubernetes проекта",
		Long:  "Кластеры Managed Kubernetes проекта: id, имя, состояние. Только чтение.",
	},
	"k8s_get": {
		Short: "Кластер Managed Kubernetes по ID",
		Long:  "Все сведения о кластере Managed Kubernetes по id, в том числе состояние. Только чтение.",
		Args: map[string]string{
			"cluster_id": "UUID кластера (список: evo k8s list)",
		},
	},
	"k8s_options": {
		Short: "Версии Kubernetes, flavor узлов и зоны для кластеров",
		Long: "Доступные версии Kubernetes (по каналам выпуска), flavor узлов и зоны для кластеров Managed Kubernetes; " +
			"назначение flavor (CONTROLPLANE — мастера, DATAPLANE — узлы), если каталог его называет. " +
			"Только чтение. Нужны, чтобы задать флаги 'evo k8s plan' вручную.",
	},
	"k8s_plan": {
		Short: "Шаг 1: рассчитать конфигурацию кластера, ничего не создавая",
		Long: "Шаг 1 создания кластера Managed Kubernetes: рассчитать конфигурацию — версию Kubernetes, flavor и число " +
			"мастер-узлов, подсеть, сервисный аккаунт, — ничего не создавая. Что не задано флагами, evo подбирает сам. " +
			"Проверьте расчёт и создайте кластер командой 'evo k8s create' с теми же флагами.",
		Args: map[string]string{
			"cluster_sa_id":     "UUID сервисного аккаунта кластера (по умолчанию — аккаунт ключа evo)",
			"cpu":               "vCPU мастер-узла (по умолчанию 2)",
			"flavor":            "Flavor мастер-узлов (evo k8s options); заменяет --cpu и --ram-gb",
			"kube_api_internet": "Опубликовать kube-apiserver в интернет.",
			"master_count":      "Число мастер-узлов: 1, 3 или 5 (по умолчанию 1)",
			"name":              "Имя кластера, 3–60 символов (по умолчанию k8s-<значение --product>)",
			"product":           "Назначение кластера (входит в имя)",
			"ram_gb":            "RAM мастер-узла, ГБ (по умолчанию 4)",
			"subnet_id":         "UUID или имя подсети зоны кластера для VIP мастер-узлов (по умолчанию — основная подсеть зоны)",
			"version":           "Версия Kubernetes, например v1.34.6 (по умолчанию — новейшая STABLE)",
			"zone":              "Зона мастер-узлов: имя или UUID (по умолчанию — зона --subnet-id, иначе зона по умолчанию сервиса)",
		},
	},
	"k8s_create": {
		Short: "Шаг 2: создать кластер по расчёту k8s plan (платно, асинхронно, около 10 минут)",
		Long: "Шаг 2: создать кластер Managed Kubernetes по расчёту 'evo k8s plan' — с теми же флагами. Кластер " +
			"платный. Операция асинхронная: ответ содержит cluster_id и operation_id, кластер создаётся около 10 минут " +
			"(по документации — 5–10); состояние — 'evo k8s get <cluster-id>'.",
		Args: map[string]string{
			"cluster_sa_id":     "UUID сервисного аккаунта кластера (как в evo k8s plan)",
			"cpu":               "vCPU мастер-узла (как в evo k8s plan)",
			"flavor":            "Flavor мастер-узлов (как в evo k8s plan)",
			"kube_api_internet": "Опубликовать kube-apiserver в интернет.",
			"master_count":      "Число мастер-узлов: 1, 3 или 5 (по умолчанию 1)",
			"name":              "Имя кластера (как в evo k8s plan)",
			"product":           "Назначение кластера (как в evo k8s plan)",
			"ram_gb":            "RAM мастер-узла, ГБ (как в evo k8s plan)",
			"subnet_id":         "UUID или имя подсети для VIP мастер-узлов, в зоне кластера (как в evo k8s plan)",
			"version":           "Версия Kubernetes (как в evo k8s plan)",
			"zone":              "Зона: имя или UUID (как в evo k8s plan)",
		},
	},
	"k8s_delete": {
		Short: "Удалить кластер Managed Kubernetes (асинхронно)",
		Long:  "Удалить кластер Managed Kubernetes безвозвратно. Операция асинхронная.",
		Args: map[string]string{
			"wait":       deleteWaitRU,
			"cluster_id": "UUID кластера",
		},
	},
	"k8s_nodepool_get": {
		Short: "Группа узлов кластера Managed Kubernetes по id",
		Long: "Группа узлов Managed Kubernetes по id: кластер, состояние, версия, зона, масштаб (фиксированный — desired, " +
			"автомасштаб — min_count и max_count), flavor, диск, подсеть и группа безопасности узлов, метки и ограничения (taints), " +
			"версии, до которых группу можно обновить. Только чтение. id группы — в выводе evo k8s nodepools <ID кластера>.",
		Args: map[string]string{
			"nodepool_id": "UUID группы узлов (список: evo k8s nodepools <ID кластера>), обязательный",
		},
	},
	"k8s_operation": {
		Short: "Состояние асинхронной операции Managed Kubernetes",
		Long: "Состояние асинхронной операции Managed Kubernetes по id — его возвращают создание и удаление кластера и создание группы узлов: " +
			"done — завершена ли операция, error — ошибка, elapsed_sec — сколько она идёт, resource_status — состояние её ресурса. С --wait-seconds команда сама ждёт завершения до N " +
			"секунд и выводит итоговое состояние. Только чтение.",
		Args: map[string]string{
			"operation_id": "id операции из ответа вызова, который её начал (обязательный)",
			"wait_seconds": opWaitDescRU,
		},
	},
	"k8s_kubeconfig": {
		Short: "kubeconfig кластера (по умолчанию со встроенным токеном примерно на час)",
		Long: "kubeconfig кластера Managed Kubernetes. По умолчанию в него встроен статический токен, и kubectl " +
			"и helm работают сразу: исходный kubeconfig Cloud.ru требует exec-плагин cloudlogin, без него kubectl завершается " +
			"ошибкой 'executable cloudlogin not found'. Токен действует около часа. Исходный kubeconfig с exec-плагином — " +
			"--embed-token=false.",
		Args: map[string]string{
			"cluster_id":  "UUID кластера",
			"embed_token": "Встроить статический токен, чтобы kubectl работал без плагина cloudlogin.",
			"network": "Адрес API кластера: NETWORK_TYPE_PUBLIC или NETWORK_TYPE_PRIVATE (по умолчанию — " +
				"как настроено в кластере)",
		},
	},
	"k8s_nodepools": {
		Short: "Группы узлов кластера",
		Long: "Группы узлов кластера Managed Kubernetes: имя, состояние, сколько узлов задано и сколько готово. " +
			"Только чтение. Списка по всем кластерам в API нет: id кластера — в 'evo k8s list'.",
		Args: map[string]string{
			"cluster_id": "UUID кластера (список: evo k8s list), обязательный",
		},
	},
	"k8s_add_nodepool": {
		Short: "Добавить группу рабочих узлов (платно, асинхронно)",
		Long: "Добавить в кластер Managed Kubernetes группу рабочих узлов (поды работают на них; в новом кластере " +
			"их нет). Платно, асинхронно. Ответ 503 'no healthy upstream' сервиса групп узлов — временный сбой: " +
			"повторите команду.",
		Args: map[string]string{
			"cluster_id":        "UUID кластера",
			"count":             "Число рабочих узлов, постоянное (по умолчанию 2)",
			"cpu":               "vCPU узла (по умолчанию 2)",
			"disk_gb":           "Диск узла, ГБ (по умолчанию 20)",
			"flavor":            "Flavor узлов (evo k8s options); заменяет --cpu и --ram-gb",
			"name":              "Имя группы, 4–60 символов (по умолчанию nodepool-1)",
			"nodes_subnet_id":   "UUID или имя подсети (по умолчанию — подсеть зоны)",
			"ram_gb":            "RAM узла, ГБ (по умолчанию 4)",
			"security_group_id": "UUID или имя группы безопасности (по умолчанию — группа зоны с доступом по SSH)",
			"zone":              "Зона (по умолчанию — зона мастер-узлов; если кластер её не сообщает — отказ)",
		},
	},
	"k8s_kubectl": {
		Short: "Команда kubectl в кластере (изменяющие — с подтверждением)",
		Long: "Любая команда kubectl в кластере; токен встраивается сам, плагин cloudlogin не нужен. Читающие " +
			"команды (get, describe, logs, top, events, rollout status, explain и другие) выполняются сразу, изменяющие " +
			"(create, delete, patch, scale, label, rollout restart, exec, cordon и другие) — с подтверждением; с " +
			"подтверждением и любая команда с -f/--filename, -k/--kustomize, --output-directory, --cache-dir или " +
			"форматом вывода из файла (-o go-template-file=…, jsonpath-file=…, custom-columns-file=…): она читает файлы " +
			"или любой URL либо пишет на хост (-f у logs — это --follow). Подходит " +
			"для всего, чего нет в 'evo k8s helm' и 'evo k8s apply'. Примеры аргументов: 'get pods -A', " +
			"'-n db logs deploy/postgres', 'create secret generic s --from-literal=k=v'.",
		Args: map[string]string{
			"args":       "Аргументы kubectl одной строкой, например \"get pods -A -o wide\"",
			"cluster_id": "UUID кластера",
			"confirmed": "Только для изменяющих команд, читающим не нужен. Должно быть true; ставится только после согласия " +
				"пользователя",
		},
	},
	"k8s_apply": {
		Short: "Применить манифест Kubernetes (как kubectl apply -f -)",
		Long: "Применить манифест Kubernetes (YAML или JSON, можно несколько документов) к кластеру — как " +
			"'kubectl apply -f -'. Для развёртывания и обновления рабочих нагрузок: Deployment, StatefulSet, Service, " +
			"ServiceMonitor и других. Новому кластеру нужен выход в интернет, чтобы скачивать публичные образы " +
			"(SNAT-шлюз: 'evo snat create'), а CRD (например, ServiceMonitor) должны существовать до объектов, " +
			"которые их используют.",
		Args: map[string]string{
			"cluster_id": "UUID кластера",
			"manifest":   "Манифест YAML или JSON (можно несколько документов)",
			"namespace":  "Namespace, в который применить манифест (необязательно; манифест может задать свой)",
		},
	},
	"k8s_helm": {
		Short: "Helm в кластере: install, upgrade, uninstall, list, status",
		Long: "Helm в кластере: действие <action> — install, upgrade, uninstall, list или status. install и upgrade " +
			"идемпотентны (helm upgrade --install --wait). Изменяющие действия — с подтверждением. Узлам нужен выход в " +
			"интернет, чтобы скачать образы чарта ('evo snat create'); размер группы узлов подбирайте под чарт.",
		Args: map[string]string{
			"action":     "install, upgrade, uninstall, list или status",
			"chart":      "Чарт, например bitnami/nginx (для install и upgrade)",
			"cluster_id": "UUID кластера",
			"confirmed": "Только для install, upgrade и uninstall. Должно быть true; ставится только после согласия " +
				"пользователя",
			"namespace": "Namespace (создаётся при install и upgrade)",
			"release":   "Имя релиза (для всех действий, кроме list)",
			"repo_url":  "URL репозитория чартов, который сначала добавить; имя — префикс --chart",
			"set":       "Значение, как в helm --set: ключ=значение",
			"values":    "Содержимое values.yaml",
			"version":   "Версия чарта (по умолчанию — последняя)",
		},
	},
	"k8s_addons": {
		Short: "Каталог плагинов Kubernetes: поиск по имени и описанию, категория, платность",
		Long: "Поиск по каталогу плагинов Managed Kubernetes — плагинов, собранных Cloud.ru: ingress, cert-manager, " +
			"мониторинг, драйверы CSI и другие. В строке — имя, отображаемое имя, категории, платность и команда установки; " +
			"категории — слова консоли (например, «Сети», «Безопасность», «Мониторинг»), в ответе перечислены все. " +
			"Только чтение. Версии и требования плагина — 'evo k8s addon-versions <плагин>', установка — 'evo k8s add-addon'.",
		Args: map[string]string{
			"billable": "true — только платные плагины, false — только бесплатные. По умолчанию — все",
			"category": "Только плагины этой категории: точное совпадение без учёта регистра (категории " +
				"перечислены в ответе)",
			"query": "Подстрока имени, отображаемого имени или описания без учёта регистра, например cert или ingress. По " +
				"умолчанию — весь каталог",
		},
	},
	"k8s_addon_versions": {
		Short: "Версии плагина: режим поддержки, требования к кластеру, схема настроек",
		Long: "Версии (релизы) плагина Managed Kubernetes, новые первыми: версия приложения (её принимает " +
			"'evo k8s add-addon --version') и addon_version, режим поддержки (ACTIVE, EXPERIMENTAL, END_OF_SUPPORT, " +
			"END_OF_LIFE, INACTIVE), диапазон версий Kubernetes, которому должен соответствовать кластер, сколько нужно " +
			"готовых рабочих узлов, namespace, есть ли JSON Schema настроек и её размер. Отмечена версия, которую " +
			"'evo k8s add-addon' берёт по умолчанию, — новейшая ACTIVE. Сама схема (до ~94 КБ) — только с --full. Только чтение.",
		Args: map[string]string{
			"full": "Вывести и схему настроек (configuration_schema) каждой версии (большой объём)",
			"name": "Имя плагина из evo k8s addons, например cert-manager",
		},
	},
	"k8s_add_addon": {
		Short: "Установить плагин в кластер (асинхронно; версия по умолчанию — последняя активная)",
		Long: "Установить версию плагина Managed Kubernetes в кластер. Превью только читает: выбранная версия, " +
			"namespace, требования версии и проверки кластера (версия Kubernetes, готовые рабочие узлы); что проверить " +
			"не удалось — в not_checked, явное несоответствие — отказ.\n\n" +
			"Операция асинхронная: состояние installing с operation_id. Плагин готов, когда его поды работают: " +
			"'evo k8s kubectl <cluster-id> get pods -n <namespace>'. Поды в ImagePullBackOff могут означать, что у " +
			"кластера нет выхода в интернет ('evo snat create').",
		Args: map[string]string{
			"cluster_id": "UUID кластера",
			"config": "Настройки по схеме версии (evo k8s addon-versions <плагин> --full): объект JSON или строка с ним. " +
				"По умолчанию — последние настройки плагина в кластере, иначе его настройки по умолчанию",
			"name": "Имя плагина (evo k8s addons)",
			"resolve_mode": "Объекты плагина, уже созданные в кластере: NONE (по умолчанию) — оставить, установка " +
				"тогда может не пройти; OVERWRITE — заменить",
			"version": "Версия приложения из evo k8s addon-versions; addon_version — только новейшая для своей " +
				"версии приложения. По умолчанию — новейшая ACTIVE",
		},
	},

	// pg
	"pg_list": {
		Short: "Кластеры Managed PostgreSQL проекта",
		Long:  "Кластеры Managed PostgreSQL проекта: id, имя, состояние, версия, работоспособность. Только чтение.",
	},
	"pg_get": {
		Short: "Кластер PostgreSQL по ID: версия, хранилище, строка подключения, состояние",
		Long: "Все сведения о кластере Managed PostgreSQL по id (список: 'evo pg list'): версия, узлы, хранилище, " +
			"строка подключения, подсети, состояние, доступ (размещён в своих подсетях; параметра публичного доступа нет). Только чтение.",
		Args: map[string]string{
			"cluster_id": "То же, что id",
			"id":         "UUID кластера PostgreSQL (список: evo pg list), обязательный (синоним: cluster_id)",
		},
	},
	"pg_versions": {
		Short: "Доступные версии PostgreSQL",
		Long: "Доступные версии Managed PostgreSQL (например, 15, 16, 17). Версия передаётся в --version команды " +
			"'evo pg create'. Только чтение.",
	},
	"pg_specs": {
		Short: "Конфигурации кластера PostgreSQL для версии: CPU, память, минимальный объём",
		Long: "Конфигурации кластера Managed PostgreSQL для версии: id, CPU, память (ГБ), режим развёртывания " +
			"(standard или business), минимальный объём хранилища. id конфигурации передаётся в --specification-id " +
			"команды 'evo pg create'. Только чтение.",
		Args: map[string]string{
			"version": "Версия продукта, например 17: только конфигурации этой версии (необязательно)",
		},
	},
	"pg_create": {
		Short: "Создать кластер PostgreSQL (платно, асинхронно)",
		Long: "Создать кластер Managed PostgreSQL, платно, асинхронно. Имя, которое уже носит кластер проекта, " +
			"отклоняется. Подсеть по умолчанию — основная подсеть зоны профиля. Публичного доступа нет: такого " +
			"параметра API не принимает.",
		Args: map[string]string{
			"backup_retention_days": "Срок хранения резервных копий, дней (по умолчанию 14)",
			"backup_schedule":       "Расписание резервных копий, CRON (по умолчанию '0 3 * * 0'; не чаще раза в 6 часов)",
			"cpu":                   "vCPU, вместе с --ram: подбирает конфигурацию",
			"description":           "Описание",
			"initial_database":      "База данных, создаваемая с кластером (по умолчанию defaultdb), без владельца: базу с владельцем создаёт 'evo pg db-create --owner'",
			"instances":             "Число узлов: 1–2 для standard, 1–3 для business (по умолчанию 1)",
			"name":                  "Имя кластера",
			"no_backup":             "Без автоматических резервных копий (по умолчанию включены): после создания это НЕ изменить",
			"ram":                   "RAM, ГБ, вместе с --cpu",
			"specification_id":      "UUID конфигурации, или --cpu и --ram",
			"storage_gb":            "Диск данных, ГБ (по умолчанию — минимум конфигурации)",
			"subnet_id":             "UUID или имя подсети; задаёт зону",
			"version":               "Версия PostgreSQL (по умолчанию — последняя)",
		},
	},
	"pg_delete": {
		Short: "Удалить кластер PostgreSQL вместе с данными и всеми резервными копиями (асинхронно)",
		Long: "Удалить кластер Managed PostgreSQL по id (список: 'evo pg list') вместе с данными и всеми резервными " +
			"копиями — восстановить их нельзя. Операция асинхронная.",
		Args: map[string]string{
			"wait":       deleteWaitRU,
			"cluster_id": "То же, что id",
			"id":         "UUID кластера (список: evo pg list), обязательный (синоним: cluster_id)",
		},
	},
	"pg_operation": {
		Short: "Состояние асинхронной операции PostgreSQL",
		Long: "Состояние асинхронной операции Managed PostgreSQL по id — его возвращают 'evo pg create', " +
			"'evo pg delete' и 'evo pg backup-create': done — завершена ли операция, error — ошибка, elapsed_sec — сколько она идёт, resource_status — состояние её ресурса. С --wait-seconds " +
			"команда сама ждёт завершения до N секунд и выводит итоговое состояние. Только чтение.",
		Args: map[string]string{
			"operation_id": "UUID операции (обязательный)",
			"wait_seconds": opWaitDescRU,
		},
	},
	"pg_databases": {
		Short: "Базы данных кластера",
		Long:  "Базы данных кластера PostgreSQL: имя, владелец, размер. Только чтение.",
		Args: map[string]string{
			"cluster_id": "UUID кластера (список: evo pg list), обязательный",
		},
	},
	"pg_db_create": {
		Short: "Создать базу данных",
		Long: "Создать базу данных в кластере PostgreSQL. Владелец — --owner (пользователи кластера: " +
			"'evo pg users <cluster-id>'). Превью не проверяет, что имя свободно ('evo pg databases').",
		Args: map[string]string{
			"cluster_id": "UUID кластера (обязательный)",
			"lc_collate": "Локаль сортировки (необязательно, по умолчанию C)",
			"lc_ctype":   "Локаль набора символов (необязательно, по умолчанию C)",
			"name":       "Имя базы данных (обязательный)",
			"owner":      "Пользователь-владелец (список: evo pg users), обязательный",
		},
	},
	"pg_db_delete": {
		Short: "Удалить базу данных",
		Long:  "Удалить базу данных кластера PostgreSQL вместе с её данными.",
		Args: map[string]string{
			"cluster_id": "UUID кластера (обязательный)",
			"name":       "Имя базы данных (обязательный)",
		},
	},
	"pg_users": {
		Short: "Пользователи кластера и их роли",
		Long:  "Пользователи кластера PostgreSQL и выданные им роли. Только чтение.",
		Args: map[string]string{
			"cluster_id": "UUID кластера (список: evo pg list), обязательный",
		},
	},
	"pg_user_create": {
		Short: "Создать пользователя (пароль не выводится)",
		Long:  "Создать пользователя в кластере PostgreSQL. Превью не проверяет, что имя свободно ('evo pg users'). Пароль в ответе не выводится.",
		Args: map[string]string{
			"cluster_id": "UUID кластера (обязательный)",
			"name":       "Имя пользователя (обязательный)",
			"password":   "Пароль пользователя (обязательный)",
		},
	},
	"pg_user_delete": {
		Short: "Удалить пользователя",
		Long:  "Удалить пользователя кластера PostgreSQL.",
		Args: map[string]string{
			"cluster_id": "UUID кластера (обязательный)",
			"name":       "Имя пользователя (обязательный)",
		},
	},
	"pg_backups": {
		Short: "Резервные копии кластера",
		Long:  "Резервные копии кластера PostgreSQL: имя, состояние, размер, время создания. Только чтение.",
		Args: map[string]string{
			"cluster_id": "UUID кластера (список: evo pg list), обязательный",
		},
	},
	"pg_backup_create": {
		Short: "Создать резервную копию вручную (асинхронно)",
		Long:  "Запустить резервное копирование кластера PostgreSQL вручную. Операция асинхронная: ход — 'evo pg operation <operation-id>'.",
		Args: map[string]string{
			"cluster_id": "UUID кластера (обязательный)",
			"name": "Имя резервной копии: буквы, цифры, дефисы и точки; не начинается и не заканчивается на - или . " +
				"(обязательный)",
		},
	},

	// s3
	"s3_buckets": {
		Short: "Бакеты Object Storage",
		Long:  "Бакеты Object Storage (S3-совместимое хранилище s3.cloud.ru). Только чтение.",
	},
	"s3_ls": {
		Short: "Объекты бакета: ключ, размер, время изменения",
		Long:  "Объекты бакета: ключ, размер, время изменения. Только чтение.",
		Args: map[string]string{
			"bucket":   "Имя бакета (обязательный)",
			"max_keys": "Наибольшее число объектов в ответе (по умолчанию 1000)",
			"prefix":   "Только ключи с этим префиксом (необязательно)",
		},
	},
	"s3_get": {
		Short: "Скачать объект до 10 МиБ (небольшой текст — как есть, остальное — в base64; больший объект — через S3-клиент или консоль)",
		Long: "Скачать объект до 10 МиБ; больший объект отклоняется — используйте S3-клиент или консоль. Небольшой " +
			"текст в UTF-8 выводится как есть, остальное — в base64. Только чтение.",
		Args: map[string]string{
			"bucket": "Имя бакета (обязательный)",
			"key":    "Ключ объекта (обязательный)",
		},
	},
	"s3_put": {
		Short: "Загрузить объект (существующий ключ перезаписывается)",
		Long: "Загрузить объект: текст — через --content, двоичные данные — через --content-base64. Объект с тем же " +
			"ключом перезаписывается. Бакет должен существовать: 'evo s3 bucket-create'.",
		Args: map[string]string{
			"bucket":         "Имя бакета (обязательный)",
			"content":        "Текст для загрузки",
			"content_base64": "Двоичные данные в base64 (вместо --content)",
			"content_type":   "MIME-тип (по умолчанию application/octet-stream)",
			"key":            "Ключ объекта (обязательный)",
		},
	},
	"s3_rm": {
		Short: "Удалить объект",
		Long:  "Удалить объект из бакета безвозвратно.",
		Args: map[string]string{
			"bucket": "Имя бакета (обязательный)",
			"key":    "Ключ объекта (обязательный)",
		},
	},
	"s3_bucket_create": {
		Short: "Создать бакет",
		Long: "Создать бакет Object Storage — например, перед 'evo s3 put', которому нужен существующий бакет. " +
			"Свой бакет с таким именем уже есть — ответ already_exists без вопроса: этот вызов его не создавал.",
		Args: map[string]string{
			"bucket": "Имя нового бакета: 3–63 строчные буквы, цифры, точки и дефисы, уникальное в тенанте (обязательный)",
			"region": "Регион: принимается только ru-central-1 (иначе облако отвечает 'unexpected region'). " +
				"По умолчанию — регион S3 из настроек",
		},
	},
	"s3_bucket_delete": {
		Short: "Удалить пустой бакет",
		Long:  "Удалить пустой бакет. Объекты из бакета сначала удалите: 'evo s3 rm'.",
		Args: map[string]string{
			"bucket": "Имя бакета (обязательный)",
		},
	},

	// secret
	"secret_list": {
		Short: "Секреты проекта (без значений)",
		Long: "Секреты проекта в Secret Manager Cloud.ru: id, имя, путь, описание. Только чтение; значения не " +
			"выводятся — значение секрета показывает 'evo secret value <путь>'.",
	},
	"secret_get": {
		Short: "Метаданные секрета: папка, метки, версии, ключ KMS (без значения)",
		Long: "Метаданные секрета по пути (список: 'evo secret list'): папка, метки, версии, ключ KMS. Только чтение; " +
			"значение не выводится.",
		Args: map[string]string{
			"path": "Путь секрета (папка/имя, список: evo secret list), обязательный",
		},
	},
	"secret_value": {
		Short: "Расшифрованное значение секрета",
		Long: "Расшифрованное значение секрета по пути — открытым текстом. Не сохраняйте его в журналы и файлы. " +
			"Ничего не меняет.",
		Args: map[string]string{
			"confirmed": "Ответ содержит расшифрованное значение секрета. Должно быть true; ставится только после согласия " +
				"пользователя",
			"path":    "Путь секрета (папка/имя), обязательный",
			"version": "Номер версии (по умолчанию — последняя)",
		},
	},
	"secret_create": {
		Short: "Создать секрет",
		Long: "Создать секрет по пути; путь вида папка/имя кладёт секрет в папку. Значение хранится " +
			"зашифрованным и в ответе не выводится.",
		Args: map[string]string{
			"description": "Описание (необязательно)",
			"kms_key_id":  "id ключа KMS для шифрования (необязательно)",
			"path":        "Путь секрета: имя — в корне, папка/имя — в папке (обязательный)",
			"value":       "Значение секрета открытым текстом; хранится зашифрованным (обязательный)",
		},
	},
	"secret_add_version": {
		Short: "Добавить новую версию значения секрета (ротация)",
		Long:  "Добавить новую версию значения существующего секрета (ротация).",
		Args: map[string]string{
			"path":  "Путь секрета (список: evo secret list), обязательный",
			"value": "Новое значение секрета открытым текстом (обязательный)",
		},
	},
	"secret_delete": {
		Short: "Удалить секрет со всеми версиями",
		Long: "Удалить секрет по пути вместе со всеми версиями. По умолчанию удаляется и секрет с активными " +
			"версиями; с --force=false облако отказывает в удалении такого секрета.",
		Args: map[string]string{
			"force": "Удалить секрет, даже если у него есть активные версии.",
			"path":  "Путь секрета (список: evo secret list), обязательный",
		},
	},

	// dns
	"dns_zones": {
		Short: "Зоны DNS проекта: публичные и приватные",
		Long: "Зоны Evolution DNS проекта, публичные и приватные: id, имя, домен, видимость, состояние (у " +
			"публичной зоны — и confirm_state), valid, delegated. Только чтение.",
	},
	"dns_zone_get": {
		Short: "Зона DNS по ID: все сведения",
		Long:  "Все сведения об одной зоне DNS по id (список: 'evo dns zones'). Только чтение.",
		Args: map[string]string{
			"id":   "UUID зоны (список: evo dns zones), обязательный",
			"type": "Видимость зоны: public или private. Необязательно: без неё evo определяет видимость по id",
		},
	},
	"dns_zone_create": {
		Short: "Создать зону DNS: публичную или приватную",
		Long: "Создать зону DNS: --type public (по умолчанию) или private. Публичная зона обслуживает запросы " +
			"только после подтверждения владения доменом через делегирование (в ответе — проверочная запись); " +
			"приватная зона разрешается только в привязанном VPC: --vpc-id, иначе VPC проекта по умолчанию (его называет превью).",
		Args: map[string]string{
			"description": "Описание зоны (необязательно)",
			"vpc_id":      "Приватная зона: VPC, к которому она привязана (по умолчанию — VPC проекта по умолчанию)",
			"domain":      "FQDN зоны, например example.com. (обязательный)",
			"name":        "Отображаемое имя зоны (по умолчанию — домен)",
			"type":        "Видимость зоны: public (по умолчанию) или private",
		},
	},
	"dns_zone_delete": {
		Short: "Удалить зону DNS вместе с её записями",
		Long: "Удалить зону DNS по id (список: 'evo dns zones') вместе с её записями. Видимость зоны " +
			"(--type public или private) указывать не обязательно: evo определяет её по id зоны. Приватную зону, " +
			"связанную с VPC, evo не удаляет: Cloud.ru удаляет её только после снятия связей в личном кабинете. " +
			"evo ждёт удаления до 45 с: ответ deleted или deleting с operation_id (evo dns operation).",
		Args: map[string]string{
			"wait": deleteWaitRU,
			"id":   "UUID зоны (список: evo dns zones), обязательный",
			"type": "Видимость зоны: public или private. Необязательно: без неё evo определяет видимость по id",
		},
	},
	"dns_operation": {
		Short: "Состояние асинхронной операции DNS",
		Long: "Состояние асинхронной операции DNS по id — его возвращают создание и удаление зон и записей DNS: " +
			"done — завершена ли операция, error — ошибка, elapsed_sec — сколько она идёт, resource_status — состояние её ресурса. С --wait-seconds команда сама ждёт завершения до N " +
			"секунд и выводит итоговое состояние. Только чтение.",
		Args: map[string]string{
			"operation_id": "UUID операции (обязательный)",
			"wait_seconds": opWaitDescRU,
		},
	},
	"dns_records": {
		Short: "Записи зоны DNS",
		Long: "Ресурсные записи зоны DNS: id, имя, тип, значения, ttl. Только чтение. Видимость зоны (--type " +
			"public или private) указывать не обязательно: evo определяет её по id.",
		Args: map[string]string{
			"type":    "Видимость зоны: public или private. Необязательно: без неё evo определяет видимость по id",
			"zone_id": "UUID зоны (список: evo dns zones), обязательный",
		},
	},
	"dns_record_get": {
		Short: "Запись DNS по ID",
		Long: "Одна запись DNS по id (список: 'evo dns records <zone-id>'). Только чтение. Видимость зоны (--type " +
			"public или private) указывать не обязательно: evo определяет её по id.",
		Args: map[string]string{
			"record_id": "UUID записи (список: evo dns records <zone-id>), обязательный",
			"type":      "Видимость зоны: public или private. Необязательно: без неё evo определяет видимость по id",
		},
	},
	"dns_record_create": {
		Short: "Добавить запись DNS в зону (асинхронно)",
		Long:  "Добавить запись DNS в зону. Операция асинхронная: ход — 'evo dns operation <operation-id>'.",
		Args: map[string]string{
			"name": "Имя записи (хост), например www или @ (обязательный)",
			"record_type": "Тип записи: a, aaaa, cname, mx, ns, spf, txt, caa, srv (в любом регистре; отправляется строчными, " +
				"как принимает API), обязательный",
			"ttl":     "TTL, секунд (по умолчанию 300)",
			"type":    "Видимость зоны: public или private. Необязательно: без неё evo определяет видимость по id",
			"value":   "Одно значение записи (например, IP для записи A). Задаётся --value или --values, не оба",
			"values":  "Несколько значений записи",
			"zone_id": "UUID зоны (список: evo dns zones), обязательный",
		},
	},
	"dns_record_delete": {
		Short: "Удалить запись DNS (асинхронно)",
		Long:  "Удалить запись DNS по id (список: 'evo dns records <zone-id>'). Операция асинхронная.",
		Args: map[string]string{
			"wait":      deleteWaitRU,
			"record_id": "UUID записи (список: evo dns records <zone-id>), обязательный",
			"type":      "Видимость зоны: public или private. Необязательно: без неё evo определяет видимость по id",
		},
	},

	// cert
	"cert_list": {
		Short: "TLS-сертификаты проекта в Certificate Manager",
		Long: "TLS-сертификаты проекта в Certificate Manager; отбор — по имени, DNS-имени, common name, типу и " +
			"состоянию. Только чтение. Так находят сертификат для сайта; TLS завершает ВМ или приложение, которое его " +
			"обслуживает, — балансировщик L4 TCP работает без TLS.",
		Args: map[string]string{
			"common_name": "Часть common name (CN) для отбора (необязательно)",
			"dns_name":    "Одно из DNS-имён сертификата, например shkrbkv.ru (необязательно)",
			"name":        "Имя сертификата или его часть (необязательно)",
			"state":       "CERTIFICATE_STATE_ACTIVE или CERTIFICATE_STATE_EXPIRED (необязательно)",
			"type": "CERTIFICATE_TYPE_IMPORTED, CERTIFICATE_TYPE_SELF_SIGNED или CERTIFICATE_TYPE_LETS_ENCRYPT " +
				"(необязательно)",
		},
	},
	"cert_get": {
		Short: "Сертификат по ID: метаданные, основная версия, срок действия, состояние",
		Long: "Один сертификат Certificate Manager по id: метаданные, основная версия, срок действия, состояние. " +
			"Только чтение.",
		Args: map[string]string{
			"id": "id сертификата (обязательный)",
		},
	},
	"cert_issue_le": {
		Short: "Выпустить бесплатный сертификат Let's Encrypt (асинхронно)",
		Long: "Выпустить бесплатный TLS-сертификат Let's Encrypt для одного или нескольких доменов. Так включают " +
			"HTTPS на сайте (например, shkrbkv.ru). Сертификат ставится в приложение: балансировщик (evo lb create) — " +
			"L4 TCP и TLS не завершает, поэтому HTTPS завершается на ВМ (веб-сервер вроде Caddy или nginx держит " +
			"сертификат или выпускает свой). Зоны " +
			"доменов должны быть заранее делегированы в Evolution DNS, иначе — ошибка ERROR_CODE_NOT_DELEGATED. " +
			"Операция асинхронная: ответ содержит id операции, ход — 'evo cert operation <operation-id>' (до " +
			"done=true).",
		Args: map[string]string{
			"description": "Описание (необязательно)",
			"dns_names":   "Домены для CN и SAN, например shkrbkv.ru и www.shkrbkv.ru (обязательный)",
			"name":        "Отображаемое имя сертификата (обязательный)",
		},
	},
	"cert_import": {
		Short: "Загрузить в Certificate Manager свой сертификат и закрытый ключ",
		Long: "Импортировать в Certificate Manager собственный сертификат пользователя (bring-your-own). " +
			"Сертификат (--server-certificate) и закрытый ключ (--private-key) передаются как PEM — " +
			"как есть или в кодировке Base64.",
		Args: map[string]string{
			"description":              "Описание (необязательно)",
			"intermediate_certificate": "Цепочка промежуточных сертификатов, PEM как есть или в Base64 (необязательно)",
			"name":                     "Отображаемое имя (обязательный)",
			"private_key":              "Закрытый ключ, PEM как есть или в Base64 (обязательный)",
			"server_certificate":       "Сертификат (открытая часть), PEM как есть или в Base64 (обязательный)",
		},
	},
	"cert_renew": {
		Short: "Перевыпустить сертификат Let's Encrypt (асинхронно)",
		Long: "Запустить перевыпуск сертификата Let's Encrypt. Операция асинхронная: ответ содержит id операции, " +
			"ход — 'evo cert operation <operation-id>'.",
		Args: map[string]string{
			"certificate_id": "id сертификата (обязательный)",
		},
	},
	"cert_delete": {
		Short: "Удалить сертификат (безвозвратно)",
		Long: "Удалить сертификат безвозвратно. Если он привязан к балансировщику, удаление может сломать HTTPS; " +
			"удалить всё равно — --force.",
		Args: map[string]string{
			"force": "Удалить, даже если сертификат используют другие сервисы (по умолчанию false)",
			"id":    "id сертификата (обязательный)",
		},
	},
	"cert_usage": {
		Short: "Ресурсы, которые используют сертификат",
		Long: "Ресурсы (балансировщики и их обработчики), которые используют сертификат. Проверяйте перед " +
			"удалением сертификата. Только чтение.",
		Args: map[string]string{
			"id": "id сертификата (обязательный)",
		},
	},
	"cert_versions": {
		Short: "Версии сертификата",
		Long: "Версии сертификата: id, CN, DNS-имена, состояние, срок действия — со всех страниц списка. Только " +
			"чтение.",
		Args: map[string]string{
			"certificate_id": "id сертификата (обязательный)",
		},
	},
	"cert_private_key": {
		Short: "Закрытый ключ версии сертификата — открытым текстом",
		Long: "СЕКРЕТНЫЙ закрытый ключ версии сертификата. privateKey (в живом API — private_key) — текст PEM " +
			"(-----BEGIN …), раскодированный из Base64 ответа API (key_format: pem; base64 — если значение API " +
			"не PEM в Base64). 'evo cert import' принимает ключ в Base64.",
		Args: map[string]string{
			"certificate_id": "id сертификата (обязательный)",
			"confirmed": "Ответ содержит закрытый ключ открытым текстом. Должно быть true; ставится только после согласия " +
				"пользователя",
			"version_id": "Номер версии; 0 или без значения — последняя версия",
		},
	},
	"cert_operation": {
		Short: "Состояние асинхронной операции Certificate Manager",
		Long: "Состояние асинхронной операции Certificate Manager (выпуск или перевыпуск сертификата) по id: " +
			"опрашивайте, пока не станет done=true; elapsed_sec — сколько она идёт, resource_status — состояние её ресурса. С --wait-seconds команда сама ждёт завершения до N секунд и " +
			"выводит итоговое состояние. Только чтение.",
		Args: map[string]string{
			"operation_id": "id операции выпуска или перевыпуска сертификата (обязательный)",
			"wait_seconds": opWaitDescRU,
		},
	},

	// lb, tg
	"lb_list": {
		Short: "Балансировщики нагрузки проекта (L4)",
		Long: "Балансировщики нагрузки Evolution (L4, сетевые) проекта: id, имя, состояние, VPC, внешний IP. " +
			"Только чтение.",
	},
	"lb_get": {
		Short: "Балансировщик по ID: правила, обработчики, адреса, зоны",
		Long: "Все сведения об одном балансировщике по id (список: 'evo lb list'): правила, " +
			"обработчики, адреса, зоны доступности, состояние. Только чтение.",
		Args: map[string]string{
			"id": "UUID балансировщика (список: evo lb list), обязательный",
		},
	},
	"lb_operation": {
		Short: "Состояние асинхронной операции балансировщика",
		Long: "Состояние асинхронной операции балансировщика по id — его возвращают создание и удаление " +
			"балансировщиков и групп целей: done — завершена ли операция, error — ошибка, elapsed_sec — сколько она идёт, resource_status — состояние её ресурса. С --wait-seconds " +
			"команда сама ждёт завершения до N секунд и выводит итоговое состояние. Только чтение.",
		Args: map[string]string{
			"operation_id": "UUID операции (обязательный)",
			"wait_seconds": opWaitDescRU,
		},
	},
	"tg_list": {
		Short: "Группы целей балансировщиков проекта",
		Long:  "Группы целей балансировщиков нагрузки проекта: id, имя, состояние, VPC. Только чтение.",
	},
	"tg_get": {
		Short: "Группа целей по ID: ВМ, VPC, состояние",
		Long: "Сведения об одной группе целей по id (список: 'evo tg list'): ВМ-участники, VPC, " +
			"состояние. Только чтение.",
		Args: map[string]string{
			"id": "UUID группы целей (список: evo tg list), обязательный",
		},
	},
	"tg_create": {
		Short: "Создать группу целей балансировщика (асинхронно)",
		Long: "Создать группу целей — набор ВМ, между которыми балансировщик распределяет трафик. Цель — одна ВМ " +
			"(--vm-id) или несколько (--targets, массив {id}). Сетевой интерфейс ВМ evo находит сам: " +
			"--interface-id нужен, только чтобы задать другой. Цели — приватные ВМ в подсети VPC (создаются с " +
			"'evo vm create --no-public-ip'). Операция асинхронная.",
		Args: map[string]string{
			"interface_id": "UUID сетевого интерфейса (необязательно; по умолчанию evo берёт интерфейс ВМ)",
			"name":         "Имя группы целей, уникальное в проекте (обязательный)",
			"targets":      "Несколько целей: массив {id} (interface_id необязателен, evo находит его сам)",
			"vm_id":        "UUID ВМ — единственной цели; интерфейс evo находит сам. Задаётся --vm-id или --targets, не оба",
		},
	},
	"tg_delete": {
		Short: "Удалить группу целей (асинхронно)",
		Long:  "Удалить группу целей по id (список: 'evo tg list'). Операция асинхронная.",
		Args: map[string]string{
			"wait": deleteWaitRU,
			"id":   "UUID группы целей (обязательный)",
		},
	},
	"lb_create": {
		Short: "Создать балансировщик L4 перед группой целей (платно, асинхронно)",
		Long: "Создать балансировщик нагрузки L4 TCP перед группой целей, платно: он передаёт TCP и TLS не завершает " +
			"(HTTPS завершается на ВМ). Обработчик --port → --target-port с проверкой состояния по TCP. Зона: --zone, " +
			"иначе зона --subnet-id, иначе зона ВМ группы целей, иначе зона из настроек; без зоны — отказ.",
		Args: map[string]string{
			"external":        "Выделить публичный IP; без него балансировщик только внутренний.",
			"hc_port":         "Порт проверки состояния (по умолчанию — --target-port)",
			"name":            "Имя балансировщика",
			"port":            "Порт обработчика",
			"subnet_id":       "UUID или имя подсети (по умолчанию — основная подсеть зоны в VPC)",
			"target_group_id": "UUID группы целей",
			"target_port":     "Порт на целях (по умолчанию — --port)",
			"vpc_id":          "UUID VPC (по умолчанию — VPC группы целей)",
			"zone":            "Зона: имя или UUID",
		},
	},
	"lb_delete": {
		Short: "Удалить балансировщик (асинхронно)",
		Long:  "Удалить балансировщик по id (список: 'evo lb list'). Операция асинхронная.",
		Args: map[string]string{
			"wait": deleteWaitRU,
			"id":   "UUID балансировщика (обязательный)",
		},
	},

	// snat
	"snat_list": {
		Short: "SNAT-шлюзы проекта",
		Long: "SNAT-шлюзы проекта. SNAT-шлюз даёт узлам в приватных подсетях (например, рабочим узлам Managed " +
			"Kubernetes) ИСХОДЯЩИЙ доступ в интернет: он нужен, чтобы скачивать публичные образы контейнеров " +
			"(docker.io и другие) и пакеты apt или обращаться к внешним API. Это не то же, что " +
			"--kube-api-internet кластера: тот открывает входящий доступ к API-серверу. Только чтение.",
	},
	"snat_create": {
		Short: "Создать публичный SNAT-шлюз: исходящий интернет для подсетей зоны (платно)",
		Long: "Создать публичный SNAT-шлюз: исходящий доступ в интернет для каждой МАРШРУТИЗИРУЕМОЙ подсети его " +
			"зоны (изолированная подсеть не обслуживается; ВМ с собственным публичным IP выходит в интернет " +
			"напрямую). Один шлюз на зону; он получает публичный IP, который учитывается в квоте публичных IP и " +
			"освобождается только вместе со шлюзом. Зона — из настроек; другая — --zone (имя) или " +
			"--availability-zone-id. Шлюз платный.\n\n" +
			"'evo k8s create' шлюз не создаёт: кластер Managed Kubernetes поднимается без исходящего доступа. " +
			"Если нагрузкам нужно скачивать публичные образы, создайте шлюз в зоне узлов кластера.",
		Args: map[string]string{
			"availability_zone_id": "UUID зоны доступности (вместо --zone)",
			"name":                 "Имя SNAT-шлюза (обязательный)",
			"zone": "ИМЯ зоны доступности, например ru.AZ-2 (необязательно; evo находит её UUID). По умолчанию — зона из " +
				"настроек",
		},
	},
	"snat_delete": {
		Short: "Удалить SNAT-шлюз (узлы зоны теряют исходящий интернет)",
		Long: "Удалить SNAT-шлюз по id (список: 'evo snat list'). Узлы в этой зоне теряют исходящий " +
			"доступ в интернет.",
		Args: map[string]string{
			"id": "UUID SNAT-шлюза (список: evo snat list), обязательный",
		},
	},

	"subnet_create": {
		Short: "Создать подсеть в VPC",
		Long: "Создать подсеть: частную сеть IPv4 в одной зоне доступности VPC. До вопроса отказывает имя или сеть, " +
			"которые в VPC уже есть, и VPC не из проекта.",
		Args: map[string]string{
			"name":                 "Имя подсети: первая — латинская буква, дальше буквы, цифры, '.', '-', '_'",
			"cidr":                 "Сеть с маской от /8 до /29, например 10.10.0.0/24",
			"vpc_id":               "UUID VPC (по умолчанию — VPC проекта по умолчанию)",
			"zone":                 "Имя зоны, например ru.AZ-1 (по умолчанию — зона из настроек)",
			"availability_zone_id": "UUID зоны (вместо --zone)",
			"routed_network":       "Маршрутизируемая подсеть; false — изолированная",
			"dns_servers":          "Адреса DNS-серверов IPv4; без них имена на ВМ подсети не разрешатся",
			"description":          "Описание",
		},
	},
	"subnet_delete": {
		Short: "Удалить подсеть",
		Long: "Удалить подсеть по id (список: 'evo vpc subnets'). Подсеть, в которой ещё есть ВМ, балансировщики " +
			"или кластеры, отказывает до вопроса.",
		Args: map[string]string{
			"subnet_id": "UUID подсети (список: evo vpc subnets), обязательный",
		},
	},

	// kafka
	"kafka_list": {
		Short: "Кластеры Managed Kafka проекта",
		Long:  "Кластеры Managed Kafka: id, имя, состояние, брокеры, версия. Только чтение.",
	},
	"kafka_get": {
		Short: "Кластер Kafka по ID: состояние, адреса, брокеры, версия",
		Long:  "Кластер Managed Kafka по id: состояние, адреса (kafka_hosts), брокеры, версия. Только чтение.",
		Args: map[string]string{
			"cluster_id": "UUID кластера (список: evo kafka list), обязательный",
		},
	},
	"kafka_versions": {
		Short: "Доступные версии Managed Kafka",
		Long: "Доступные версии Managed Kafka: id и имя. Только чтение. Нужны перед созданием кластера " +
			"('evo kafka create').",
	},
	"kafka_specs": {
		Short: "Конфигурации кластера Kafka для версии: CPU, память, хранилище, брокеры",
		Long: "Конфигурации кластера Managed Kafka для версии: id, CPU, память (ГБ), минимальный объём хранилища, " +
			"наименьшее и наибольшее число брокеров. Только чтение. id конфигурации выбирают для создания " +
			"кластера ('evo kafka create').",
		Args: map[string]string{
			"version_id": "UUID версии (список: evo kafka versions); необязательно — по умолчанию последняя",
			"zone": "Зона доступности: имя (ru.AZ-2) или UUID; необязательно — по умолчанию зона из настроек, иначе " +
				"ru.AZ-2. API нужна зона",
		},
	},
	"kafka_create": {
		Short: "Создать кластер Managed Kafka (платно, асинхронно)",
		Long: "Создать кластер Managed Kafka, платно, асинхронно. Имя, которое уже носит кластер проекта, отклоняется. " +
			"Подсеть по умолчанию — основная подсеть зоны из настроек, иначе первой зоны, где она есть, начиная с " +
			"ru.AZ-2; кластер создаётся в зоне подсети. Брокеров по умолчанию — min_brokers конфигурации, иначе 1, " +
			"без репликации.",
		Args: map[string]string{
			"brokers":          "Число брокеров, 3 — для реплицируемого кластера; проверяется по min_brokers и max_brokers конфигурации, если они есть",
			"cpu":              "vCPU, вместе с --ram: подбирает конфигурацию",
			"description":      "Описание",
			"name":             "Имя кластера, 2–63 символа",
			"ram":              "RAM, ГБ, вместе с --cpu",
			"specification_id": "UUID конфигурации, или --cpu и --ram",
			"storage_gib":      "Диск, ГиБ (по умолчанию — минимум конфигурации)",
			"subnet_id":        "UUID или имя подсети",
			"ui_enabled":       "Включить Kafka UI (по умолчанию false)",
			"version_id":       "UUID версии Kafka (по умолчанию — последняя)",
		},
	},
	"kafka_delete": {
		Short: "Удалить кластер Kafka вместе с данными (асинхронно)",
		Long:  "Удалить кластер Managed Kafka по id вместе с данными. Операция асинхронная.",
		Args: map[string]string{
			"wait":       deleteWaitRU,
			"cluster_id": "UUID кластера (список: evo kafka list), обязательный",
		},
	},
	"kafka_operation": {
		Short: "Состояние асинхронной операции Kafka",
		Long: "Состояние асинхронной операции Managed Kafka по id — его возвращают создание и удаление кластера: " +
			"done — завершена ли операция, error — ошибка, elapsed_sec — сколько она идёт, resource_status — состояние её ресурса. С --wait-seconds команда сама ждёт завершения до N " +
			"секунд и выводит итоговое состояние. Только чтение.",
		Args: map[string]string{
			"operation_id": "UUID операции (обязательный)",
			"wait_seconds": opWaitDescRU,
		},
	},

	// redis
	"redis_list": {
		Short: "Кластеры Managed Redis проекта",
		Long:  "Кластеры Managed Redis: id, имя, состояние, тип, шарды, реплики. Только чтение.",
	},
	"redis_get": {
		Short: "Кластер Redis по ID: состояние, адреса, тип, шарды, реплики",
		Long:  "Кластер Managed Redis по id: состояние, адреса (redis_hosts), тип, шарды, реплики. Только чтение.",
		Args: map[string]string{
			"cluster_id": "UUID кластера (список: evo redis list), обязательный",
		},
	},
	"redis_versions": {
		Short: "Доступные версии Managed Redis",
		Long: "Доступные версии Managed Redis: id и имя. Только чтение. Нужны перед созданием кластера " +
			"('evo redis create').",
	},
	"redis_specs": {
		Short: "Конфигурации кластера Redis для версии: CPU, память, режим, хранилище",
		Long: "Конфигурации кластера Managed Redis для версии: id, CPU, память (ГБ), режим развёртывания, " +
			"минимальный объём хранилища. Только чтение. id конфигурации выбирают для создания кластера " +
			"('evo redis create').",
		Args: map[string]string{
			"version_id": "UUID версии (список: evo redis versions); необязательно — по умолчанию последняя",
			"zone": "Зона доступности: имя (ru.AZ-2) или UUID; необязательно — по умолчанию зона из настроек, иначе " +
				"ru.AZ-2. API нужна зона",
		},
	},
	"redis_create": {
		Short: "Создать кластер Managed Redis (платно, асинхронно)",
		Long: "Создать кластер Managed Redis, платно, асинхронно. Имя, которое уже носит кластер проекта, отклоняется. " +
			"Все узлы — в зоне подсети (по умолчанию — основная подсеть зоны из настроек, иначе другой зоны). " +
			"MASTER_REPLICA (по умолчанию): мастер и две реплики — три платных узла на шард; STANDALONE — один узел.",
		Args: map[string]string{
			"cpu":              "vCPU, вместе с --ram: подбирает конфигурацию",
			"description":      "Описание",
			"name":             "Имя кластера",
			"ram":              "RAM, ГБ, вместе с --cpu",
			"replicas":         "Реплики в шарде БЕЗ мастера: 2 для MASTER_REPLICA (по умолчанию), 0 для STANDALONE; 'evo redis get' считает и мастер",
			"shards":           "Число шардов, 1–8 (по умолчанию 1)",
			"specification_id": "UUID конфигурации, или --cpu и --ram",
			"storage_gb":       "Диск, ГБ (по умолчанию — минимум конфигурации)",
			"subnet_id":        "UUID или имя подсети",
			"type":             "По умолчанию MASTER_REPLICA",
			"version_id":       "UUID версии Redis (по умолчанию — последняя)",
		},
	},
	"redis_delete": {
		Short: "Удалить кластер Redis вместе с данными (асинхронно)",
		Long:  "Удалить кластер Managed Redis по id вместе с данными. Операция асинхронная.",
		Args: map[string]string{
			"wait":       deleteWaitRU,
			"cluster_id": "UUID кластера (список: evo redis list), обязательный",
		},
	},
	"redis_operation": {
		Short: "Состояние асинхронной операции Redis",
		Long: "Состояние асинхронной операции Managed Redis по id — его возвращают создание и удаление кластера: " +
			"done — завершена ли операция, error — ошибка, elapsed_sec — сколько она идёт, resource_status — состояние её ресурса. С --wait-seconds команда сама ждёт завершения до N " +
			"секунд и выводит итоговое состояние. Только чтение.",
		Args: map[string]string{
			"operation_id": "UUID операции (обязательный)",
			"wait_seconds": opWaitDescRU,
		},
	},

	// ar
	"ar_list": {
		Short: "Реестры Artifact Registry проекта",
		Long: "Реестры Artifact Registry проекта: id, имя, тип, состояние, публичный ли, тариф. Только чтение. " +
			"Реестр хранит образы Docker и чарты Helm — источник образов для Container Apps и Managed Kubernetes " +
			"там, где docker.io не подходит.",
	},
	"ar_get": {
		Short: "Реестр Artifact Registry по ID: все сведения",
		Long: "Все сведения об одном реестре Artifact Registry по id (список: 'evo ar list'). Только " +
			"чтение.",
		Args: map[string]string{
			"id": "UUID реестра (список: evo ar list), обязательный",
		},
	},
	"ar_create": {
		Short: "Создать реестр Artifact Registry (платно, асинхронно)",
		Long: "Создать реестр Artifact Registry; имя, которое уже носит реестр проекта ('evo ar list'), " +
			"отклоняется. Тип (--registry-type): DOCKER (по умолчанию), DEBIAN, RPM, GENERIC, PYPI или NPM. " +
			"Реестр платный. Операция асинхронная: ход — 'evo ar operation <operation-id>'; затем образы загружают " +
			"через docker login в <registry>.cr.cloud.ru.",
		Args: map[string]string{
			"name":          "Имя реестра (обязательный)",
			"public":        "Сделать реестр публичным (по умолчанию false)",
			"registry_type": "Тип реестра: DOCKER (по умолчанию), DEBIAN, RPM, GENERIC, PYPI, NPM",
			"tariff":        "Тариф: BASIC (по умолчанию) или PREMIUM",
		},
	},
	"ar_delete": {
		Short: "Удалить реестр вместе с артефактами (асинхронно)",
		Long: "Удалить реестр Artifact Registry по id (список: 'evo ar list') вместе с его артефактами. " +
			"Операция асинхронная.",
		Args: map[string]string{
			"wait": deleteWaitRU,
			"id":   "UUID реестра (список: evo ar list), обязательный",
		},
	},
	"ar_operation": {
		Short: "Состояние асинхронной операции Artifact Registry",
		Long: "Состояние асинхронной операции Artifact Registry по id — его возвращают создание и удаление " +
			"реестра: done — завершена ли операция, error — ошибка, elapsed_sec — сколько она идёт, resource_status — состояние её ресурса. С --wait-seconds команда сама ждёт " +
			"завершения до N секунд и выводит итоговое состояние. Только чтение.",
		Args: map[string]string{
			"operation_id": "UUID операции (обязательный)",
			"wait_seconds": opWaitDescRU,
		},
	},

	// ca
	"ca_list": {
		Short: "Приложения Container Apps проекта",
		Long:  "Приложения Container Apps (бессерверные контейнеры) проекта: имя, состояние. Только чтение.",
	},
	"ca_get": {
		Short: "Приложение Container Apps по имени: состояние, публичный URL",
		Long: "Все сведения о приложении Container Apps по имени, в том числе состояние и публичный URL. Только " +
			"чтение.",
		Args: map[string]string{
			"name": "Имя приложения",
		},
	},
	"ca_deploy": {
		Short: "Развернуть приложение Container Apps из образа контейнера (асинхронно)",
		Long: "Развернуть бессерверное приложение Container Apps из образа контейнера; превью перечисляет все " +
			"настройки, в том числе запуск от root. Операция асинхронная: состояние и URL — 'evo ca get <name>'.",
		Args: map[string]string{
			"cpu":         "CPU, например 1 (по умолчанию 0.1)",
			"description": "Описание",
			"env":         "Переменные окружения по имени",
			"image": "URI образа (Artifact Registry этого проекта или публичный реестр), linux/amd64; приложение " +
				"слушает 0.0.0.0:$PORT",
			"max_instances": "Наибольшее число экземпляров (по умолчанию 1)",
			"memory":        "Память, например 1Gi (по умолчанию 256Mi)",
			"min_instances": "Наименьшее число экземпляров (по умолчанию 0 — масштабирование до нуля). Больше 0 — " +
				"экземпляры работают и оплачиваются постоянно",
			"name":       "Имя приложения, уникальное: метка RFC 1123",
			"port":       "Порт контейнера (по умолчанию 8080). Отклоняются: 1–1024, 8008, 8012, 8013, 8022, 8112, 9090–9092",
			"privileged": "Запустить от root, в привилегированном режиме (по умолчанию false)",
			"public":     "Открыть публичный URL.",
		},
	},
	"ca_delete": {
		Short: "Удалить приложение Container Apps (асинхронно)",
		Long:  "Удалить приложение Container Apps по имени. Операция асинхронная.",
		Args: map[string]string{
			"wait": deleteWaitRU,
			"name": "Имя приложения",
		},
	},
	"ca_logs": {
		Short: "Последние журналы приложения Container Apps",
		Long: "Последние журналы приложения Container Apps: 100 последних строк по времени, старые первыми " +
			"(журналы приложения — stdout и stderr контейнера). С --system — события платформы. Только чтение.",
		Args: map[string]string{
			"name":   "Имя приложения",
			"system": "Системные журналы вместо журналов приложения",
		},
	},
	"ca_start": {
		Short: "Запустить остановленное приложение Container Apps",
		Long:  "Запустить остановленное приложение Container Apps; пока оно работает, оно оплачивается.",
		Args: map[string]string{
			"name": "Имя приложения",
		},
	},
	"ca_stop": {
		Short: "Остановить приложение Container Apps",
		Long:  "Остановить работающее приложение Container Apps: оно перестаёт обслуживать запросы.",
		Args: map[string]string{
			"name": "Имя приложения",
		},
	},

	// billing
	"billing_balance": {
		Short: "Баланс по договору: сумма в рублях, признак активности",
		Long: "Баланс счёта по договору: сумма в рублях, признак активности и исходный объект договора. Только " +
			"чтение. Нужен CLOUDRU_AGREEMENT_ID: баланс принадлежит договору, по проекту его не даёт ни один " +
			"API; id договора видят только администраторы организации и администраторы затрат. Сервер, привязанный " +
			"к проекту (evo mcp --allow-project), отказывает: CONFIG_INVALID. Запрос идёт к " +
			"billing.api.cloud.ru/v1/agreement/{agreement_id}; этого метода нет в опубликованных спецификациях " +
			"API Cloud.ru, поэтому он работает по возможности и может измениться без предупреждения. Так " +
			"проверяют, на сколько ещё хватит баланса. " + readOnlyTaskNoteRU,
	},
	"billing_consumption": {
		Short: "Стоимость потребления за период по сервисам: по договору или по проекту",
		Long: "Стоимость потребления за период — по договору (CLOUDRU_AGREEMENT_ID) или, без него, по проекту; " +
			"поле scope в ответе говорит, по чему. Сводка по сервисам: total (без НДС, сумма amount), " +
			"total_with_nds (с НДС, сумма amount_nds), records, by_service — по убыванию стоимости (без НДС), с cost_with_nds (с НДС). " +
			"Только чтение. Роли: для договора — администратор организации или администратор затрат; для проекта " +
			"— администратор проекта (platform.project.admin). Для FinOps и анализа затрат. " + readOnlyTaskNoteRU,
		Args: map[string]string{
			"end": "Дата окончания, ГГГГ-ММ-ДД (по умолчанию — дата начала)",
			"project_id": "UUID проекта, чьё потребление прочитать (по умолчанию — текущий проект, если CLOUDRU_AGREEMENT_ID " +
				"не задан; с ним — весь договор)",
			"start": "Дата начала, ГГГГ-ММ-ДД (обязательный)",
		},
	},
	"billing_finops": {
		Short: "Отчёт FinOps: распределение затрат, аномалии, темп расходов, запас баланса",
		Long: "Отчёт FinOps (только чтение). INFORM — распределение затрат по сервисам и аномалии относительно " +
			"предыдущего периода той же длины; QUANTIFY — дневной темп расходов, на сколько дней хватит баланса, " +
			"прогнозная дата нулевого баланса и, если задан бюджет, его проверка. Затраты без НДС (amount); total_cost_with_nds, daily_burn_with_nds, cost_with_nds в by_service и " +
			"prev_with_nds/now_with_nds в anomalies — с НДС (amount_nds); запас и бюджет — по затратам без НДС. " +
			"Отбор — как у 'evo billing consumption'; без CLOUDRU_AGREEMENT_ID баланс и запас " +
			"неизвестны. Перенесён из finops_report.py. Для анализа затрат, предупреждений о запасе и проверки " +
			"бюджета. " + readOnlyTaskNoteRU,
		Args: map[string]string{
			"budget": "Месячный бюджет, руб.: прогноз расходов за месяц сравнивается с ним (необязательно)",
			"days":   "Длина окна в днях, если дата начала не задана: 1–366 (по умолчанию 30)",
			"end":    "Дата окончания, ГГГГ-ММ-ДД (по умолчанию — сегодня)",
			"project_id": "UUID проекта (по умолчанию — текущий проект, если CLOUDRU_AGREEMENT_ID не задан; с ним — весь " +
				"договор)",
			"start": "Дата начала, ГГГГ-ММ-ДД (по умолчанию — за --days дней по дату окончания включительно)",
		},
	},

	// audit
	"audit_events": {
		Short: "Журнал аудита проекта: кто, что и когда сделал",
		Long: "Audit Logging — кто что сделал в проекте (только чтение): события управления (создание, удаление и " +
			"изменение ВМ, баз данных, Kubernetes, S3, IAM и другого) с исполнителем, действием, ресурсом и " +
			"временем. Отвечает на вопросы «что произошло на стенде», «кто удалил X», «что недавно менялось». " +
			"Постранично: next_page_token. Доступ к журналам аудита включает поддержка Cloud.ru; до этого API " +
			"отвечает 401, и ответ сообщает, что аудит для проекта не включён.",
		Args: map[string]string{
			"page_size":  "Наибольшее число событий на странице (по умолчанию 100, не больше 500)",
			"page_token": "Токен продолжения — next_page_token предыдущего ответа (необязательно)",
			"since": "Окно назад от текущего момента: 30m, 1h, 3h, 1d или число минут строкой, например \"90\" (по " +
				"умолчанию 1h; лучше короче — для активных проектов API рекомендует не больше 30 минут)",
		},
	},

	// org, iam
	"org_quotas": {
		Short: "Квоты: ресурс, лимит, текущее использование",
		Long: "Использование квот: код ресурса, его имя, единица и продукт (если их называет справочник типов квот), " +
			"лимит, текущее использование — для планирования мощностей. Только " +
			"чтение. Можно ограничить одним проектом (--project-id). Ответ может быть неполным, если у " +
			"сервисного аккаунта недостаточно прав.",
		Args: map[string]string{
			"project_id": "UUID проекта, которым ограничить квоты (необязательно)",
		},
	},
	// auth
	"whoami": {
		Short: "Активный профиль и проверка его ключа",
		Long: "Кем работают команды: профиль, Key ID (маскированный), откуда взяты ключ и проект (профиль, переменная " +
			"окружения, файл проекта, флаг), проект, срок действия токена IAM — из кэша он или только что выдан, — и " +
			"сколько проектов видит ключ (поля visible_projects нет, если Organization API не отдаёт ключу список: это не " +
			"ошибка), и roles — роли ключа на проекте по именам, со сроком у тех, у кого он есть; roles_note говорит, почему " +
			"роли неизвестны (IAM их не сообщил, проект не задан) или что роль на организацию здесь не видна; key_expires_at — " +
			"когда истекает ключ доступа (key_expires_soon — осталось 5 дней или меньше: следующий ключ выпускает человек; " +
			"key_term_note — если срок неизвестен). Только чтение; " +
			"токен у IAM команда запрашивает, только когда действующего токена в кэше нет. Код выхода 2 — " +
			"ключа нет, IAM его отклонил или сломаны настройки.",
	},
	"doctor": {
		Short: "Проверить настройки, ключ доступа и связь с API",
		Long: "Проверки окружения: настройки (профиль, откуда взяты ключ и проект, предупреждения), доступность IAM и " +
			"Compute API, ключ доступа (токен IAM), сколько проектов он видит, его роли на проекте (key roles: skip, если IAM " +
			"их не сообщил, warn — если ролей нет) и срок (key term: warn за 5 дней); команда 'evo doctor' проверяет ещё файлы " +
			"настроек этой машины и их права и kubectl/helm в PATH. Только чтение. В ответе — checks (name, status: ok, " +
			"warn, fail или skip, detail, http_status) и ok; http_status у IAM API и Compute API — ответ корня API: любой ответ, в том " +
			"числе 404, значит «доступен». Код выхода 2 — сломаны настройки или ключ (в том числе не " +
			"задан проект), 1 — не прошли только проверки связи.",
	},
	"iam_service_accounts": {
		Short: "Сервисные аккаунты IAM проекта",
		Long: "Сервисные аккаунты IAM проекта: id, имя, email, тип, включён ли. Только чтение. Так находят UUID " +
			"сервисного аккаунта кластера (--cluster-sa-id команды 'evo k8s create') или проверяют сервисные " +
			"аккаунты.",
		Args: map[string]string{
			"project_id": "UUID проекта (по умолчанию — текущий проект)",
		},
	},

	// metrics
	"metrics_history": {
		Short: "История метрики за недавний период (Service Monitoring)",
		Long: "Service Monitoring: история метрики за недавний период (только чтение). Чтобы понять, здоров ли " +
			"сервис и как загружен CPU, история метрики полезнее, чем повторные 'evo vm get' и 'evo k8s get'. " +
			"Возвращает временные ряды: метки метрики, значения, отметки времени. Данные есть, только если в " +
			"проекте подключён тенант мониторинга; иначе API отвечает 'tenant not found'.",
		Args: map[string]string{
			"match": "Имя метрики или селектор имени PromQL, например cpu_usage или node_load1 (обязательный)",
			"since": "Окно назад от текущего момента: 30m, 1h, 6h, 1d или число минут строкой, например \"90\" (по " +
				"умолчанию 1h; не больше 24h — ограничение API, большее отклоняется)",
		},
	},
	"metrics_scrape": {
		Short: "Текущие значения метрик в текстовом формате Prometheus",
		Long: "Service Monitoring: текущие значения метрик в текстовом формате экспозиции Prometheus — снимок всех " +
			"или отобранных метрик на этот момент (только чтение). Отбор — аргумент [match]; без него — все метрики.",
		Args: map[string]string{
			"match": "Селектор PromQL, например {__name__=~\"cpu.*\"} (необязательно; по умолчанию — все метрики)",
		},
	},

	// stack
	"stack_plan": {
		Short: "Спланировать стек ресурсов по одному манифесту (ничего не создаёт)",
		Long: "Спланировать стек из нескольких ресурсов по ОДНОМУ декларативному манифесту. Для развёртывания из " +
			"нескольких сервисов используйте стек, а не создавайте кластер PostgreSQL, кластер Kubernetes, ВМ, " +
			"группу целей и остальное по одному: всю цепочку выполняет evo. Возвращает job_id и оценку стоимости " +
			"по тарифам; стек создаёт 'evo stack apply <job-id>'. Только чтение: ничего не создаёт.\n\n" +
			"manifest = {name, vpc?{name}, postgres?{name,cpu,ram,storage_gb,no_backup}, " +
			"kafka?{name,cpu,ram,storage_gib,brokers}, redis?{name,cpu,ram,storage_gb,shards,replicas,type}, " +
			"k8s?{name,version,cpu,ram_gb,master_count,kube_api_internet}, registry?{name,registry_type,public}, " +
			"snat?{name,zone}, vm?{name,private,flavor,image,zone}, " +
			"nlb?{name,port,target_port,external,target(default 'vm')}, " +
			"dns?{zone_id,name,record_type,target(default 'lb'→LB public IP)}, secrets?[{path,value}], " +
			"s3?[{bucket,key,content}]}.\n\n" +
			"vm.image (имя или UUID, поиск: 'evo image list') и vm.zone (имя вида ru.AZ-1 или UUID, список: " +
			"'evo vm zones') по умолчанию берутся из CLOUDRU_IMAGE_ID и CLOUDRU_REGION сервера; план находит оба " +
			"и отказывает стеку с ВМ, если чего-то из них нет или оно неизвестно, — до создания чего-либо. " +
			"(Container Apps пока нет: нужен образ из реестра.)",
		Args: map[string]string{
			"manifest": "Декларативное описание стека (формат — в описании)",
		},
	},
	"stack_apply": {
		Short: "Создать ресурсы спланированного стека (платно)",
		Long: "Применить спланированный стек (из 'evo stack plan'). Ресурсы платные. Вызов запускает " +
			"готовые шаги и возвращает краткое состояние; он может идти минутами: шаг ВМ ждёт, пока ВМ станет " +
			"активной (вживую — около 3 минут), а асинхронные ресурсы (PostgreSQL, Kafka, Redis, Kubernetes, " +
			"балансировщик) только запускаются. Затем опрашивайте 'evo stack status <job-id>' с этим job_id, " +
			"пока status не станет done. Пароль, который evo сгенерировал для ВМ шага, — в passwords{шаг} того " +
			"ответа, где ВМ создана, один раз: evo его не хранит. Управлять ресурсами по отдельности не нужно.",
		Args: map[string]string{
			"job_id": "id задания из evo stack plan (обязательный)",
		},
	},
	"stack_status": {
		Short: "Ход развёртывания стека",
		Long: "Ход развёртывания стека, кратко: status, done[], running[], pending[], outputs{postgres, vm, tg, " +
			"lb, …; lb_external_ip — когда у балансировщика появился публичный адрес}, passwords{шаг} — пароль ВМ, " +
			"который evo сгенерировал, только в этом ответе. Для задания, запущенного " +
			"'evo stack apply <job-id>', каждый вызов продвигает развёртывание И ждёт внутри (до " +
			"--wait-seconds), так что на долгое развёртывание хватает нескольких вызовов. Спланированное, но не " +
			"запущенное задание только показывается и никогда не запускается. Развёртывание опрашивайте, пока status " +
			"не станет done (тогда читайте outputs; lb_external_ip есть только у балансировщика с публичным адресом) " +
			"или error. status=deleting (со списком deleting) — удаление не завершено, и stack status его не " +
			"доводит: повторяйте 'evo stack destroy <job-id> --yes', пока ответ не станет destroyed. Задание " +
			"хранится на диске: состояние можно запросить снова по job_id в любой момент.",
		Args: map[string]string{
			"job_id": "id задания (обязательный)",
			"wait_seconds": "Сколько секунд этот вызов может ждать, продвигая развёртывание (по умолчанию и не больше 20). " +
				"Чем больше, тем меньше вызовов для долгого развёртывания",
		},
	},
	"stack_destroy": {
		Short: "Удалить все ресурсы стека (безвозвратно)",
		Long: "Удалить все ресурсы стека — в обратном порядке, идемпотентно. status=destroyed значит, что удалено " +
			"всё. status=deleting (код 0) значит, что ВМ, секреты и объекты S3 удалены, а асинхронные ресурсы из " +
			"списка deleting (PostgreSQL, Kafka, Redis, Kubernetes, балансировщик и другие) облако удаляет ещё " +
			"несколько минут (PostgreSQL вживую — около 5 минут): повторяйте 'evo stack destroy --yes', пока ответ " +
			"не станет destroyed. Удалённый стек отвечает already_destroyed без вопроса; стек в deleting — снова " +
			"превью, и подтверждённый вызов перепроверяет удаления.",
		Args: map[string]string{
			"job_id": "id задания (обязательный)",
		},
	},

	// image
	"image_create": {
		Short: "Создать образ ВМ из загрузочного диска (платно, асинхронно)",
		Long: "Создать образ ВМ из загрузочного диска (хранение платное). Диск должен быть отключён: 'evo disk detach', " +
			"затем состояние available в 'evo disk list'. Образу Маркетплейса нужны --user-data-template и " +
			"--image-metadata с каждой {{ var }} шаблона. Сборка асинхронная: повторяйте 'evo image get', пока образ " +
			"не станет готов (ready=true).",
		Args: map[string]string{
			"description":  "Не больше 255 символов",
			"disk_id":      "UUID диска-источника",
			"display_name": "Имя в консоли",
			"image_metadata": "Поля покупателя для --user-data-template, каждое {name, display_name, display_position, " +
				"field_type, ...}: display_name и display_position обязательны, id и is_required отклоняются",
			"min_cpu":            "Наименьшее число vCPU",
			"min_disk":           "Наименьший загрузочный диск, ГБ: не меньше размера образа",
			"min_ram":            "Наименьший объём RAM, ГБ",
			"name":               "Имя образа, уникальное в проекте",
			"user_data_template": "Шаблон cloud-init (Jinja)",
			"zone":               "Зона: имя или UUID; должна совпадать с зоной диска",
		},
	},
	"image_delete": {
		Short: "Удалить образ ВМ",
		Long:  "Удалить образ ВМ. Работающие ВМ продолжают работать, но новую ВМ из образа уже не создать.",
		Args: map[string]string{
			"image_id": "UUID образа (список: evo image list)",
		},
	},

	// vpc
	"close_ports": {
		Short: "Закрыть входящие порты в группе безопасности",
		Long: "Закрыть входящие порты в группе безопасности Cloud.ru: удалить правила ingress для указанных портов, " +
			"по умолчанию tcp из 0.0.0.0/0, — обратная операция к открытию портов (evo vpc open-ports). Группа — аргумент <security-group> " +
			"(имя или UUID). Удаляются только правила ровно этого порта, протокола и источника; порты без такого " +
			"правила уже закрыты (если не открыт ни один — status already_deleted). Группы безопасности общие для " +
			"зоны доступности: закрытый порт затрагивает каждую ВМ в этой группе.",
		Args: map[string]string{
			"cidr":           "CIDR источника правил (по умолчанию 0.0.0.0/0 — откуда угодно)",
			"ports":          "Порты, которые закрыть, например 8080",
			"protocol":       "Протокол: tcp (по умолчанию) или udp",
			"security_group": "Группа безопасности: имя (например, SSH-access_ru.AZ-2) или UUID (список: evo vpc security-groups)",
		},
	},
	"open_ports": {
		Short: "Открыть входящие порты в группе безопасности",
		Long: "Открыть входящие порты в группе безопасности Cloud.ru: добавить правила ingress, по умолчанию tcp " +
			"из 0.0.0.0/0. Так делают сервис доступным (например, 80 и 443 для веб-сервера). Группа — аргумент " +
			"<security-group> (имя или UUID). Уже открытые порты пропускаются. Группы безопасности общие для " +
			"зоны доступности: открытый порт затрагивает каждую ВМ в этой группе.",
		Args: map[string]string{
			"cidr":           "CIDR источника (по умолчанию 0.0.0.0/0 — откуда угодно): спросите человека, какой источник",
			"ports":          "Порты, которые открыть, например 80,443",
			"protocol":       "Протокол: tcp (по умолчанию) или udp",
			"security_group": "Группа безопасности: имя (например, SSH-access_ru.AZ-2) или UUID (список: evo vpc security-groups)",
		},
	},

	// evo deploy plan, apply, list
	"deploy_plan": {
		Short: "Шаг 1 мастера развёртывания: рассчитать ВМ и её цену, ничего не создавая",
		Long: "Шаг 1 мастера развёртывания, только чтение: подбирает конфигурацию ВМ (flavor, образ, зона, диск, сеть, " +
			"SSH) с ценой за месяц и plan_id (cost.source: API расчёта цен Cloud.ru, иначе оценка по тарифам; лицензия " +
			"образа не учитывается никогда). В режиме auto передайте --cpu, --ram-gb, --disk-gb продукта, остальное " +
			"заполнят значения по умолчанию; в режиме manual — --flavor, --zone, --image, --disk-type. Проверьте план " +
			"и цену, затем 'evo deploy apply <plan-id>'.",
		Args: map[string]string{
			"base_plan_id": "Прежний plan_id, который изменить: его параметры, поверх — только переданные здесь поля; " +
				"возвращается новый plan_id",
			"compose_file": "docker-compose.yml развёртывания compose (задаёт compose_required); с ним образ Astra Linux, " +
				"Альт или РЕД ОС отклоняется: Docker на них не ставится",
			"cpu":         "Нужное число vCPU (auto выберет самый маленький подходящий flavor)",
			"disk_gb":     "Загрузочный диск, ГБ (по умолчанию 10)",
			"disk_type":   "manual: имя типа диска, например SSD",
			"external_ip": "Подключить публичный floating IP (по умолчанию true)",
			"flavor":      "manual: имя flavor. По умолчанию — flavor из настроек, если не заданы --cpu и --ram-gb, иначе автоподбор",
			"image":       "manual: имя или UUID образа. По умолчанию — образ из настроек, иначе Ubuntu 24.04",
			"image_metadata": "Обязательные поля метаданных образа, которые evo не заполняет сам (evo image get <образ>), " +
				"по имени; без них план отклоняется. Хранятся в локальном файле плана",
			"login":   "Пользователь ВМ (по умолчанию user1)",
			"mode":    "auto (по умолчанию) или manual",
			"name":    "Имя ВМ (по умолчанию <product>-<время>)",
			"ports":   "Порты, которые выполнение плана откроет для --cidr в общей группе безопасности зоны, например 80,443; evo vm delete удаляет созданные им правила",
			"cidr":    "CIDR источника правил портов (по умолчанию 0.0.0.0/0 — откуда угодно). Уже открытые правила не меняются",
			"product": "Имя продукта (входит в имя ВМ)",
			"ram_gb":  "Нужный объём RAM, ГБ",
			"ssh_key": "Открытый SSH-ключ. Без него — ключ развёртывания evo из рабочего каталога (.evo/id_ed25519.pub), " +
				"чтобы evo мог зайти по SSH; ключ человека добавьте потом",
			"zone": "manual: зона, имя или UUID. По умолчанию — зона из настроек, иначе ru.AZ-2",
		},
	},
	"deploy_apply": {
		Short: "Шаг 2 мастера развёртывания: выполнить рассчитанный план",
		Long: "Шаг 2 мастера развёртывания: выполнить план, который вернул 'evo deploy plan'. " +
			"Он создаёт ВМ без приложения: проект compose разворачивает 'evo deploy'. " +
			"Выполняйте его только после того, как план и его цена одобрены. Идемпотентно: своя ВМ (в state этой " +
			"папки или созданная этим планом) подхватывается, а не создаётся вторая; чужая ВМ с именем из плана — " +
			"отказ с existing_id. Возвращает vm_id, public_ip, ssh и (один " +
			"раз) сгенерированный пароль. С --wait=false отвечает сразу: статус creating, состояние ВМ и пароль; " +
			"повтор той же команды подхватывает ВМ и доделывает её.",
		Args: map[string]string{
			"plan_id": "plan_id, который вернул evo deploy plan (обязательный)",
			"wait": "Ждать ли ВМ: по умолчанию команда ждёт, пока ВМ запустится; --wait=false — ответ сразу, как облако " +
				"приняло создание (статус creating), закончит повтор той же командой",
		},
	},
	"list": {
		Short: "Локальные записи развёртываний этого проекта",
		Long: "Все записанные развёртывания этого проекта: IP ВМ, состояние, flavor, время создания. Читает " +
			"локальное состояние, без обращений к API.",
	},

	// The engine commands (evo deploy, status, logs, list-images; evo org
	// projects)
	"deploy": {
		Short: "Развернуть локальный проект docker-compose на ВМ Cloud.ru",
		Long: "Развернуть локальный проект docker-compose на ВМ Cloud.ru evo-<project_name> (project_name из .evo.yaml, " +
			"иначе имя каталога), платно, если создаёт ВМ (с публичным floating IP): загрузить файлы compose по SSH и запустить их; порты compose открываются для --cidr в " +
			"общей группе безопасности зоны. Проект — каталог, где запущен evo (у сервера MCP — каталог его запуска); " +
			"compose-файл другого развёртывания отклоняется. Подтверждение нужно и для обновления (в превью — compose_changed).",
		Args: map[string]string{
			"compose_file": "Файл compose (по умолчанию docker-compose.yml)",
			"compose_hash": "Хеш compose-файла, на который дано согласие (next_args незаконченного deploy); файл другой — снова превью",
			"cidr":         "CIDR источника правил портов (по умолчанию 0.0.0.0/0 — откуда угодно). Уже открытые правила не меняются",
			"disk_size":    "Загрузочный диск, ГБ (по умолчанию 10, или минимум образа)",
			"flavor":       "Flavor ВМ или auto (по умолчанию)",
			"force":        "Развернуть заново неизменный compose",
			"image":        "Загрузочный образ, Ubuntu или Debian (не Astra Linux, Альт, РЕД ОС)",
			"image_id":     "UUID загрузочного образа",
			"project_id":   "UUID проекта",
			"region":       "UUID зоны",
		},
	},
	"status": {
		Short: "Состояние текущего развёртывания",
		Long: "Состояние текущего развёртывания по локальной записи: ID ВМ, публичный IP, flavor, регион, " +
			"работоспособность и открытые порты. Только чтение. По умолчанию спрашивает у облака и живое " +
			"состояние ВМ (один GET, не дольше 8 с) и добавляет cloud_checked, cloud_state, " +
			"not_found_in_cloud=true (ВМ удалили в обход evo: запись убирает 'evo destroy') и cloud_error с " +
			"cloud_reason (почему облако не проверено — фраза и код ошибки; локальное состояние возвращается и " +
			"тогда). Если облако ответило, vm_name, zone_name и flavor_name называют ВМ, её зону и flavor.",
		Args: map[string]string{
			"refresh": "Спросить у облака живое состояние ВМ; без этого — только локальное состояние, без обращения к API.",
		},
	},
	"logs": {
		Short: "Журналы контейнеров развёрнутой ВМ",
		Long:  "Журналы контейнеров развёрнутой ВМ: 'docker compose logs' по SSH.",
		Args: map[string]string{
			"service": "Только этот сервис compose (по умолчанию — все сервисы)",
			"tail":    "Сколько последних строк вернуть (по умолчанию 100, 0 — все)",
		},
	},
	"org_projects": {
		Short: "Проекты Cloud.ru, которые видит ключ доступа",
		Long: "Проекты Cloud.ru, которые видит ключ доступа: id, имя, состояние, подразделение организации, — и " +
			"активный проект (active). Только " +
			"чтение. Если Organization API не показывает ключу ни одного проекта (сервисный аккаунт уровня " +
			"проекта), это не ошибка: в ответе projects: null, активный проект и примечание.",
	},
	"list_images": {
		Short: "Загрузочные образы ВМ (UUID и имя)",
		Long: "Доступные загрузочные образы ВМ Cloud.ru: UUID и имя. UUID образа передаётся в --image-id команд " +
			"'evo deploy' и 'evo vm create'. Удобнее 'evo image list': он ищет по имени и выводит компактные " +
			"строки.",
	},
}

// deleteWaitRU is the wait argument of a delete that reads its outcome
// (D74).
const deleteWaitRU = "Ждать ли итог удаления: по умолчанию до ~45 с — deleted, отказ платформы или deleting со следующим шагом; " +
	"--wait=false — ответ deleting с operation_id сразу, без чтения итога"
