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
// as the commands are (cliSpecs, then CLIPending). New sets Tool.ShortRU,
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
	"provision": {
		Short: "Создать ВМ (по умолчанию с floating IP; без внешнего адреса — --no-public-ip)",
		Long: "Создать ВМ без docker-compose. По умолчанию ВМ получает floating IP (внешний адрес); с --no-public-ip " +
			"создаётся приватная ВМ — только адрес в подсети (интерфейс типа regular), доступна лишь внутри VPC; так создают " +
			"узлы за балансировщиком. Подсеть и группа безопасности подключаются сами.\n\n" +
			"Образ — --image (имя, отображаемое имя или UUID; ищется как в 'evo image get': при нескольких совпадениях — ошибка " +
			"со списком кандидатов) или --image-id (UUID). Подходит любой образ ОС, доступный проекту, в том числе Astra Linux, " +
			"Альт и РЕД ОС; образ общего каталога, недоступный проекту, отклоняется до создания чего-либо.\n\n" +
			"Поля метаданных образа (SSH-ключ, имя ВМ, логин, случайный пароль) evo заполняет сам. Обязательное поле, которое " +
			"evo заполнить не может ('evo image get <образ>' показывает такие поля), задаётся через --meta, иначе команда " +
			"отказывает до создания ВМ.\n\n" +
			"--port открывает входящие порты в группе безопасности ВМ после её загрузки; 'evo vm delete' закрывает их. " +
			"Для docker-compose проекта — 'evo deploy'. ВМ платная.",
		Args: map[string]string{
			"disk_size": "Размер загрузочного диска, ГБ (по умолчанию 10, но не меньше минимума образа)",
			"flavor": "Flavor ВМ: имя или UUID, например lowcost10-2-4 (список: evo vm flavors). По умолчанию — " +
				"самый маленький lowcost не бесплатного уровня",
			"image": "Образ: точное имя, отображаемое имя или UUID, например Ubuntu-24.04 (поиск: evo image list). " +
				"Задаётся --image или --image-id, не оба",
			"image_id": "UUID образа (см. evo image list, evo image get)",
			"image_metadata": "Поле метаданных образа, например admin_password=… — обязательные поля, которые evo не " +
				"заполняет сам (evo image get <образ>). Перекрывает заполненное evo; поле, которого в образе нет, отклоняется",
			"name":         "Имя ВМ, например hermes-agent",
			"no_public_ip": "Приватная ВМ: без floating IP, интерфейс типа regular — для узлов за балансировщиком и внутренних серверов",
			"ports": "Входящий порт, открываемый в группе безопасности ВМ для 0.0.0.0/0: 80, 8080/tcp, 53/udp " +
				"(по умолчанию tcp). Группа общая для ВМ зоны; evo vm delete удаляет правила, созданные evo",
			"project_id": "UUID проекта Cloud.ru",
			"region":     "UUID зоны доступности (список: evo vm zones)",
			"subnet_id":  "UUID подсети для интерфейса (по умолчанию — подсеть в зоне ВМ; список: evo vpc subnets)",
		},
	},
	"destroy": {
		Short: "Удалить ВМ и все данные на ней (безвозвратно)",
		Long: "Удалить ВМ и все данные на ней безвозвратно. ВМ задаётся UUID или именем; превью называет ВМ (id, имя, зону, " +
			"состояние, адрес) и floating IP, который будет освобождён. Несуществующая ВМ — код 4 уже на превью; имя, которое носят " +
			"несколько ВМ, — отказ со списком кандидатов: укажите UUID. Floating IP ВМ по умолчанию " +
			"освобождается тоже; оставить адрес — --release-ip=false. Правила группы безопасности, которые evo открыл для ВМ " +
			"('evo deploy', 'evo vm create --port'), удаляются после неё, если порт не нужен другому развёртыванию из " +
			"локального состояния; правило, которое удалить не удалось, попадает в отчёт, и следующее удаление попробует снова. " +
			"Если Cloud.ru принял удаление, но ВМ осталась в списке, — код 5: floating IP, правила и запись остаются до повторного удаления.",
		Args: map[string]string{
			"name":       "Имя ВМ, которую удалить (вместо vm_id)",
			"release_ip": "Освободить и floating IP ВМ.",
			"vm_id":      "UUID ВМ, которую удалить (список: evo vm list)",
		},
	},
	"list_zones": {
		Short: "Зоны доступности (UUID и имя)",
		Long: "Зоны доступности: UUID и имя. UUID зоны передаётся в --region, например 'evo vm create --region <UUID>'. " +
			"Только чтение.",
	},
	"list_flavors": {
		Short: "Flavor ВМ: vCPU, RAM, бесплатный уровень, оценка цены за месяц",
		Long: "Flavor ВМ: имя, vCPU, RAM, признак бесплатного уровня и оценка цены за месяц. Имя flavor передаётся " +
			"в --flavor команд 'evo vm create' и 'evo deploy'. Только чтение.",
	},

	// image
	"image_list": {
		Short: "Поиск образов: ОС (public), маркетплейс (marketplace), образы проекта (private)",
		Long: "Поиск по каталогу образов ВМ: образы ОС (тип public), продукты Маркетплейса Cloud.ru (marketplace) " +
			"и собственные образы проекта (private). Запрос ищет подстроку в имени или отображаемом имени без учёта регистра. " +
			"Образы, не готовые ни в одной зоне, и образы общего каталога, недоступные проекту, скрыты; --all показывает и их " +
			"(недоступные — с пометкой). В строке — id, имя, отображаемое имя, тип, зоны, где образ готов, и минимальный размер " +
			"диска; образы ОС идут первыми. Только чтение. Какие поля нужны ВМ из образа — 'evo image get <образ>'; " +
			"имя или id образа передаётся в --image или --image-id команды 'evo vm create'.",
		Args: map[string]string{
			"all": "Показать и образы, скрытые по умолчанию: не готовые ни в одной зоне и недоступные проекту " +
				"(общий каталог содержит тестовые и неисправные образы и образы, из которых ВМ этого проекта не создать)",
			"query": "Подстрока имени или отображаемого имени образа без учёта регистра, например astra или ubuntu. По " +
				"умолчанию — весь каталог",
			"type": "Только этот тип: public (образы ОС), marketplace (продукты Маркетплейса Cloud.ru) или private " +
				"(образы проекта). По умолчанию — все три",
			"zone": "Только образы, готовые в этой зоне доступности: имя (ru.AZ-1, AZ-1) или UUID",
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
		Long: "Витрина того, что Cloud.ru предлагает развернуть, одним списком: образы ВМ общего каталога " +
			"(образы ОС и продукты Маркетплейса Cloud.ru; собственные образы проекта не входят) и плагины Managed Kubernetes. " +
			"Только чтение.\n\n" +
			"Вид (--kind): vm-os — образ ОС (тип public или образ маркетплейса, имя которого начинается с названия семейства ОС: " +
			"Ubuntu, Debian, CentOS, Rocky, Alma, Fedora, openSUSE, Astra, Alt/Альт, RED OS/РЕД ОС, SberLinux, ROSA/РОСА, " +
			"Windows; это эвристика — в каталоге такого поля нет), vm-app — любой другой образ маркетплейса (например, " +
			"nginx-ubuntu-22.04), k8s-addon — плагин Kubernetes. Запрос ищет подстроку в имени или отображаемом имени " +
			"(у плагинов — и в описании) без учёта регистра. Образы, недоступные проекту, и образы, не готовые ни в одной зоне, " +
			"скрыты; --all показывает и их.\n\n" +
			"У каждой строки — команда развёртывания: для образа — 'evo vm create <имя-ВМ> --image <образ>' (по имени, а при " +
			"повторяющихся именах — по UUID), для плагина — 'evo k8s add-addon <cluster-id> <плагин>' (версии плагина — " +
			"'evo k8s addon-versions <плагин>'); у образа, недоступного проекту, команды нет. Платность известна только " +
			"у плагинов: платный ли образ или его лицензия, неизвестно — расчёт цены даёт 0 для любого образа. Порядок — " +
			"по виду, затем по отображаемому имени. Если один каталог прочитать не удалось, список строится по другому " +
			"с предупреждением; ошибка — только если не прочитан ни один.",
		Args: map[string]string{
			"all": "Показать и образы, скрытые по умолчанию: недоступные проекту (без команды развёртывания) " +
				"и не готовые ни в одной зоне",
			"kind": "Только этот вид: vm-os (образы ОС), vm-app (продукты Маркетплейса на ВМ) или k8s-addon " +
				"(плагины Managed Kubernetes). По умолчанию — все три; для видов ВМ каталог плагинов не читается, для k8s-addon — " +
				"каталог образов",
			"query": "Подстрока имени или отображаемого имени (у плагинов — и описания) без учёта регистра, например " +
				"astra, nginx, cert. По умолчанию — всё",
		},
	},

	// disk
	"disk_list": {
		Short: "Диски: id, имя, размер, состояние, тип, зона, к каким ВМ подключены",
		Long:  "Диски проекта: id, имя, размер (ГБ), состояние, тип, зона и ВМ, к которым диск подключён. Только чтение.",
	},
	"list_disk_types": {
		Short: "Типы дисков (UUID и имя, например SSD или HDD)",
		Long: "Типы дисков: UUID и имя (например, SSD или HDD). Имя или UUID типа передаётся в --disk-type " +
			"команды 'evo disk create'. Только чтение.",
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
		Long: "Создать VPC (виртуальное частное облако). Операция асинхронная: ход — " +
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
			"id": "UUID VPC (список: evo vpc list), обязательный",
		},
	},
	"vpc_operation": {
		Short: "Состояние асинхронной операции VPC",
		Long: "Состояние асинхронной операции VPC по id — его возвращают 'evo vpc create', 'evo vpc delete', " +
			"'evo vpc route-create' и 'evo vpc route-delete': done — завершена ли операция, error — ошибка. С --wait-seconds " +
			"команда сама ждёт завершения до N секунд и выводит итоговое состояние. Только чтение.",
		Args: map[string]string{
			"operation_id": "UUID операции (обязательный)",
			"wait_seconds": "Ждать итогового состояния до N секунд (не больше 300), опрашивая операцию " +
				"с нарастающим интервалом. Без флага — одна проверка",
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
		Long: "Подсети проекта: UUID, имя, зона доступности. UUID подсети передаётся в --subnet-id, например " +
			"'evo vm create --subnet-id <UUID>'. Только чтение.",
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
		Long: "Публичные IP-адреса (floating IP) проекта: адрес, состояние, свободен ли адрес для привязки. " +
			"Только чтение.",
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
			"('evo tools call snat_delete').",
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
		Long: "Доступные версии Kubernetes (по каналам выпуска), flavor узлов и зоны для кластеров Managed Kubernetes. " +
			"Только чтение. Нужны, чтобы задать флаги 'evo k8s plan' вручную.",
	},
	"k8s_plan": {
		Short: "Шаг 1: рассчитать конфигурацию кластера, ничего не создавая",
		Long: "Шаг 1 создания кластера Managed Kubernetes: рассчитать конфигурацию — версию Kubernetes, flavor и число " +
			"мастер-узлов, подсеть, сервисный аккаунт, — ничего не создавая. Что не задано флагами, evo подбирает сам. " +
			"Проверьте расчёт и создайте кластер командой 'evo k8s create' с теми же флагами.",
		Args: map[string]string{
			"cluster_sa_id":     "UUID сервисного аккаунта кластера (по умолчанию — сервисный аккаунт ключа доступа evo)",
			"cpu":               "vCPU мастер-узла для автоподбора flavor (по умолчанию 2)",
			"flavor":            "Flavor мастер-узлов (список: evo k8s options); заменяет --cpu и --ram-gb",
			"kube_api_internet": "Опубликовать kube-apiserver в интернет.",
			"master_count":      "Число мастер-узлов: 1, 3 или 5 (по умолчанию 1); другие значения отклоняются",
			"name":              "Имя кластера, 3–60 символов (по умолчанию k8s-<значение --product>)",
			"product":           "Назначение кластера (используется в имени)",
			"ram_gb":            "RAM мастер-узла, ГБ, для автоподбора flavor (по умолчанию 4)",
			"subnet_id": "UUID подсети для приватного VIP мастер-узлов, в зоне кластера (по умолчанию — основная " +
				"подсеть зоны). Подсеть другой зоны отклоняется",
			"version": "Версия Kubernetes, например v1.34.6 (по умолчанию — новейшая STABLE; список: evo k8s options)",
			"zone": "Зона доступности мастер-узлов: имя или UUID, например ru.AZ-2 (по умолчанию — зона --subnet-id, " +
				"иначе зона по умолчанию сервиса)",
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
			"subnet_id":         "UUID подсети для VIP мастер-узлов, в зоне кластера (как в evo k8s plan)",
			"version":           "Версия Kubernetes (как в evo k8s plan)",
			"zone":              "Зона: имя или UUID (как в evo k8s plan)",
		},
	},
	"k8s_delete": {
		Short: "Удалить кластер Managed Kubernetes (асинхронно)",
		Long:  "Удалить кластер Managed Kubernetes безвозвратно. Операция асинхронная.",
		Args: map[string]string{
			"cluster_id": "UUID кластера",
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
		Long: "Добавить в кластер Managed Kubernetes группу рабочих узлов — на них запускаются поды; в новом " +
			"кластере рабочих узлов нет. Группа узлов платная. Операция асинхронная. Flavor (по --cpu и --ram-gb или по имени), " +
			"подсеть узлов и группу безопасности evo подбирает сам. Сервис групп узлов Cloud.ru иногда отвечает " +
			"503 'no healthy upstream' — это временный сбой: повторите команду.",
		Args: map[string]string{
			"cluster_id":      "UUID кластера (список: evo k8s list)",
			"count":           "Число рабочих узлов, постоянное (по умолчанию 2)",
			"cpu":             "vCPU узла для автоподбора flavor (по умолчанию 2)",
			"disk_gb":         "Диск узла, ГБ, не меньше 10 (по умолчанию 20)",
			"flavor":          "Flavor узлов (список: evo k8s options); заменяет --cpu и --ram-gb",
			"name":            "Имя группы узлов, 4–60 символов (по умолчанию nodepool-1)",
			"nodes_subnet_id": "UUID подсети рабочих узлов (по умолчанию — подходящая подсеть зоны)",
			"ram_gb":          "RAM узла, ГБ, для автоподбора flavor (по умолчанию 4)",
			"security_group_id": "UUID группы безопасности узлов (по умолчанию — группа зоны с доступом " +
				"по SSH)",
			"zone": "Зона (по умолчанию — зона мастер-узлов кластера; если кластер её не сообщает — отказ)",
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
			"(SNAT-шлюз: 'evo tools call snat_create'), а CRD (например, ServiceMonitor) должны существовать до объектов, " +
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
			"идемпотентны (helm upgrade --install --wait). --repo-url сначала добавляет репозиторий чартов. Значения — " +
			"--values (содержимое values.yaml) и --set (ключ=значение). Изменяющие действия — с подтверждением. Подходит " +
			"для типовых продуктов (например, kube-prometheus-stack, базы данных). Узлам нужен выход в интернет, чтобы " +
			"скачать образы чарта (SNAT-шлюз: 'evo tools call snat_create'); размер группы узлов подбирайте под чарт: " +
			"стекам мониторинга нужны узлы от 8 ГБ.",
		Args: map[string]string{
			"action":     "install, upgrade, uninstall, list или status",
			"chart":      "Чарт, например prometheus-community/kube-prometheus-stack (для install и upgrade)",
			"cluster_id": "UUID кластера",
			"confirmed": "Только для install, upgrade и uninstall. Должно быть true; ставится только после согласия " +
				"пользователя",
			"namespace": "Namespace (создаётся при install и upgrade)",
			"release":   "Имя релиза Helm (для install, upgrade, uninstall и status)",
			"repo_url": "URL репозитория чартов: сначала добавить и обновить его; имя репозитория — из префикса " +
				"<repo>/ в --chart",
			"set":     "Значение Helm, как в helm --set: ключ=значение (необязательно)",
			"values":  "Содержимое values.yaml (необязательно)",
			"version": "Версия чарта (необязательно; по умолчанию — последняя)",
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
		Long: "Установить версию плагина Managed Kubernetes в кластер. Превью показывает выбранную версию " +
			"(по умолчанию — новейшая ACTIVE), namespace, требования версии и проверки кластера: версию Kubernetes и группы " +
			"узлов против требований плагина. Что определить не удалось, перечислено в not_checked; явное несоответствие — " +
			"отказ до установки.\n\n" +
			"Операция асинхронная: ответ — состояние installing с operation_id и cluster_addon_id. Готовность — " +
			"'evo k8s kubectl <cluster-id> get pods -n <namespace>' (или -A): поды плагина должны стать Running и Ready " +
			"(поды заданий Job, например cert-manager-startupapicheck, завершаются в Completed). Поды в ImagePullBackOff " +
			"могут означать, что у кластера нет выхода в интернет: в Cloud.ru для этого нужен публичный SNAT-шлюз в зоне " +
			"узлов ('evo tools call snat_create'). --resolve-mode OVERWRITE может заменить объекты, уже созданные в кластере.",
		Args: map[string]string{
			"cluster_id": "UUID кластера (список: evo k8s list)",
			"config": "Настройки плагина по схеме его версии (evo k8s addon-versions <плагин> --full): объект " +
				"JSON или строка с объектом JSON. Cloud.ru подставляет в них {{clusterID}}, {{clusterName}}, " +
				"{{productInstanceID}}, {{nodeCIDR}} и {{serviceCIDR}}. По умолчанию облако берёт последние настройки плагина " +
				"в кластере (при первой установке — настройки по умолчанию)",
			"name": "Имя плагина из evo k8s addons",
			"resolve_mode": "Что делать с объектами плагина, уже созданными в кластере вручную: NONE — оставить " +
				"(установка тогда может не пройти), OVERWRITE — сбросить к рекомендуемым значениям. По умолчанию NONE",
			"version": "Версия приложения из evo k8s addon-versions. Облако устанавливает плагин по версии " +
				"приложения и само выбирает addon_version, поэтому addon_version принимается, только если она новейшая " +
				"для своей версии приложения. По умолчанию — новейшая ACTIVE",
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
			"строка подключения, подсети, состояние. Только чтение.",
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
		Long: "Создать кластер Managed PostgreSQL. Имя, которое уже носит кластер проекта ('evo pg list'), отклоняется. " +
			"evo подбирает версию (по умолчанию — последняя), конфигурацию (по --specification-id или по --cpu и --ram; " +
			"если подходящей нет — ошибка со списком конфигураций), хранилище (по умолчанию — минимум конфигурации) и подсеть " +
			"(по умолчанию — первая подсеть проекта). Кластер платный. Операция асинхронная и занимает несколько минут: " +
			"ход — 'evo pg operation <operation-id>'.",
		Args: map[string]string{
			"backup_retention_days": "Срок хранения автоматических резервных копий, дней (по умолчанию 14, " +
				"не больше 30)",
			"backup_schedule": "Расписание автоматических резервных копий в формате CRON (по умолчанию " +
				"'0 3 * * 0' — по воскресеньям в 03:00; не чаще раза в 6 часов)",
			"cpu":              "Число vCPU — для автоподбора конфигурации, если --specification-id не задан",
			"description":      "Описание кластера (необязательно)",
			"initial_database": "Имя базы данных, которая создаётся вместе с кластером (по умолчанию defaultdb)",
			"instances": "Число узлов: 1–2 для standard, 1–3 для business (проверяется по конфигурации). " +
				"По умолчанию 1",
			"name": "Имя кластера (обязательный)",
			"no_backup": "Без автоматических резервных копий. По умолчанию они включены, и после создания кластера " +
				"это не изменить; кластер без резервных копий отклоняет evo pg backup-create и evo pg backups с ошибкой " +
				"'backup disabled'",
			"ram":              "RAM, ГБ — вместе с --cpu для автоподбора конфигурации",
			"specification_id": "UUID конфигурации (список: evo pg specs). Необязателен, если заданы --cpu и --ram",
			"storage_gb":       "Размер основного диска данных, ГБ (по умолчанию — минимум конфигурации)",
			"subnet_id":        "UUID подсети, определяет зону доступности (по умолчанию — первая подсеть проекта)",
			"version":          "Версия PostgreSQL (список: evo pg versions; по умолчанию — последняя)",
		},
	},
	"pg_delete": {
		Short: "Удалить кластер PostgreSQL вместе с данными и всеми резервными копиями (асинхронно)",
		Long: "Удалить кластер Managed PostgreSQL по id (список: 'evo pg list') вместе с данными и всеми резервными " +
			"копиями — восстановить их нельзя. Операция асинхронная.",
		Args: map[string]string{
			"cluster_id": "То же, что id",
			"id":         "UUID кластера (список: evo pg list), обязательный (синоним: cluster_id)",
		},
	},
	"pg_operation": {
		Short: "Состояние асинхронной операции PostgreSQL",
		Long: "Состояние асинхронной операции Managed PostgreSQL по id — его возвращают 'evo pg create', " +
			"'evo pg delete' и 'evo pg backup-create': done — завершена ли операция, error — ошибка. С --wait-seconds " +
			"команда сама ждёт завершения до N секунд и выводит итоговое состояние. Только чтение.",
		Args: map[string]string{
			"operation_id": "UUID операции (обязательный)",
			"wait_seconds": "Ждать итогового состояния до N секунд (не больше 300), опрашивая операцию " +
				"с нарастающим интервалом. Без флага — одна проверка",
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
			"'evo pg users <cluster-id>').",
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
		Long:  "Создать пользователя в кластере PostgreSQL. Пароль в ответе не выводится.",
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
			"Повторное создание своего бакета ошибкой не считается.",
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

	// Without a command yet (CLIPending), by the PR that gives them one.
	// PR 3a: dns
	"dns_zones": {
		Short: "Зоны DNS проекта: публичные и приватные",
		Long: "Зоны Evolution DNS проекта, публичные и приватные: id, имя, домен, видимость, состояние (у " +
			"публичной зоны — и confirm_state), valid, delegated. Только чтение.",
	},
	"dns_zone_get": {
		Short: "Зона DNS по ID: все сведения",
		Long:  "Все сведения об одной зоне DNS по id (список: 'evo tools call dns_zones'). Только чтение.",
		Args: map[string]string{
			"id":   "UUID зоны (список: evo tools call dns_zones), обязательный",
			"type": "Видимость зоны: public или private. Необязательно: без неё evo определяет видимость по id",
		},
	},
	"dns_zone_create": {
		Short: "Создать зону DNS: публичную или приватную",
		Long: "Создать зону DNS: --type public (по умолчанию) или private. Публичная зона обслуживает запросы " +
			"только после подтверждения владения доменом через делегирование (в ответе — проверочная запись); " +
			"приватная зона — внутренняя.",
		Args: map[string]string{
			"description": "Описание зоны (необязательно)",
			"domain":      "FQDN зоны, например example.com. (обязательный)",
			"name":        "Отображаемое имя зоны (по умолчанию — домен)",
			"type":        "Видимость зоны: public (по умолчанию) или private",
		},
	},
	"dns_zone_delete": {
		Short: "Удалить зону DNS вместе с её записями (асинхронно)",
		Long: "Удалить зону DNS по id (список: 'evo tools call dns_zones') вместе с её записями. Операция " +
			"асинхронная. Видимость зоны (--type public или private) указывать не обязательно: evo определяет её " +
			"по id зоны.",
		Args: map[string]string{
			"id":   "UUID зоны (список: evo tools call dns_zones), обязательный",
			"type": "Видимость зоны: public или private. Необязательно: без неё evo определяет видимость по id",
		},
	},
	"dns_operation": {
		Short: "Состояние асинхронной операции DNS",
		Long: "Состояние асинхронной операции DNS по id — его возвращают создание и удаление зон и записей DNS: " +
			"done — завершена ли операция, error — ошибка. С --wait-seconds команда сама ждёт завершения до N " +
			"секунд и выводит итоговое состояние. Только чтение.",
		Args: map[string]string{
			"operation_id": "UUID операции (обязательный)",
			"wait_seconds": "Ждать итогового состояния до N секунд (не больше 300), опрашивая операцию с нарастающим интервалом. " +
				"Без флага — одна проверка",
		},
	},
	"dns_records": {
		Short: "Записи зоны DNS",
		Long: "Ресурсные записи зоны DNS: id, имя, тип, значения, ttl. Только чтение. Видимость зоны (--type " +
			"public или private) указывать не обязательно: evo определяет её по id.",
		Args: map[string]string{
			"type":    "Видимость зоны: public или private. Необязательно: без неё evo определяет видимость по id",
			"zone_id": "UUID зоны (список: evo tools call dns_zones), обязательный",
		},
	},
	"dns_record_get": {
		Short: "Запись DNS по ID",
		Long: "Одна запись DNS по id (список: 'evo tools call dns_records'). Только чтение. Видимость зоны (--type " +
			"public или private) указывать не обязательно: evo определяет её по id.",
		Args: map[string]string{
			"record_id": "UUID записи (список: evo tools call dns_records), обязательный",
			"type":      "Видимость зоны: public или private. Необязательно: без неё evo определяет видимость по id",
		},
	},
	"dns_record_create": {
		Short: "Добавить запись DNS в зону (асинхронно)",
		Long:  "Добавить запись DNS в зону. Операция асинхронная: ход — 'evo tools call dns_operation'.",
		Args: map[string]string{
			"name": "Имя записи (хост), например www или @ (обязательный)",
			"record_type": "Тип записи: a, aaaa, cname, mx, ns, spf, txt, caa, srv (в любом регистре; отправляется строчными, " +
				"как принимает API), обязательный",
			"ttl":     "TTL, секунд (по умолчанию 300)",
			"type":    "Видимость зоны: public или private. Необязательно: без неё evo определяет видимость по id",
			"value":   "Одно значение записи (например, IP для записи A). Задаётся --value или --values, не оба",
			"values":  "Несколько значений записи",
			"zone_id": "UUID зоны (список: evo tools call dns_zones), обязательный",
		},
	},
	"dns_record_delete": {
		Short: "Удалить запись DNS (асинхронно)",
		Long:  "Удалить запись DNS по id (список: 'evo tools call dns_records'). Операция асинхронная.",
		Args: map[string]string{
			"record_id": "UUID записи (список: evo tools call dns_records), обязательный",
			"type":      "Видимость зоны: public или private. Необязательно: без неё evo определяет видимость по id",
		},
	},

	// PR 3a: cert
	"cert_list": {
		Short: "TLS-сертификаты проекта в Certificate Manager",
		Long: "TLS-сертификаты проекта в Certificate Manager; отбор — по имени, DNS-имени, common name, типу и " +
			"состоянию. Только чтение. Так находят сертификат для HTTPS-балансировщика или сайта.",
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
			"HTTPS на сайте (например, shkrbkv.ru): выпуск, затем привязка к обработчику балансировщика. Зоны " +
			"доменов должны быть заранее делегированы в Evolution DNS, иначе — ошибка ERROR_CODE_NOT_DELEGATED. " +
			"Операция асинхронная: ответ содержит id операции, ход — 'evo tools call cert_operation' (до " +
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
			"Сертификат (--server-certificate) и закрытый ключ (--private-key) передаются как PEM в кодировке " +
			"Base64.",
		Args: map[string]string{
			"description":              "Описание (необязательно)",
			"intermediate_certificate": "Цепочка промежуточных сертификатов, PEM в Base64 (необязательно)",
			"name":                     "Отображаемое имя (обязательный)",
			"private_key":              "Закрытый ключ, PEM в Base64 (обязательный)",
			"server_certificate":       "Сертификат (открытая часть), PEM в Base64 (обязательный)",
		},
	},
	"cert_renew": {
		Short: "Перевыпустить сертификат Let's Encrypt (асинхронно)",
		Long: "Запустить перевыпуск сертификата Let's Encrypt. Операция асинхронная: ответ содержит id операции, " +
			"ход — 'evo tools call cert_operation'.",
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
			"не PEM в Base64). 'evo tools call cert_import' принимает ключ в Base64.",
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
			"опрашивайте, пока не станет done=true. С --wait-seconds команда сама ждёт завершения до N секунд и " +
			"выводит итоговое состояние. Только чтение.",
		Args: map[string]string{
			"operation_id": "id операции выпуска или перевыпуска сертификата (обязательный)",
			"wait_seconds": "Ждать итогового состояния до N секунд (не больше 300), опрашивая операцию с нарастающим интервалом. " +
				"Без флага — одна проверка",
		},
	},

	// PR 3a: lb, tg
	"lb_list": {
		Short: "Балансировщики нагрузки проекта (L4)",
		Long: "Балансировщики нагрузки Evolution (L4, сетевые) проекта: id, имя, состояние, VPC, внешний IP. " +
			"Только чтение.",
	},
	"lb_get": {
		Short: "Балансировщик по ID: правила, обработчики, адреса, зоны",
		Long: "Все сведения об одном балансировщике по id (список: 'evo tools call lb_list'): правила, " +
			"обработчики, адреса, зоны доступности, состояние. Только чтение.",
		Args: map[string]string{
			"id": "UUID балансировщика (список: evo tools call lb_list), обязательный",
		},
	},
	"lb_operation": {
		Short: "Состояние асинхронной операции балансировщика",
		Long: "Состояние асинхронной операции балансировщика по id — его возвращают создание и удаление " +
			"балансировщиков и групп целей: done — завершена ли операция, error — ошибка. С --wait-seconds " +
			"команда сама ждёт завершения до N секунд и выводит итоговое состояние. Только чтение.",
		Args: map[string]string{
			"operation_id": "UUID операции (обязательный)",
			"wait_seconds": "Ждать итогового состояния до N секунд (не больше 300), опрашивая операцию с нарастающим интервалом. " +
				"Без флага — одна проверка",
		},
	},
	"tg_list": {
		Short: "Группы целей балансировщиков проекта",
		Long:  "Группы целей балансировщиков нагрузки проекта: id, имя, состояние, VPC. Только чтение.",
	},
	"tg_get": {
		Short: "Группа целей по ID: ВМ, VPC, состояние",
		Long: "Сведения об одной группе целей по id (список: 'evo tools call tg_list'): ВМ-участники, VPC, " +
			"состояние. Только чтение.",
		Args: map[string]string{
			"id": "UUID группы целей (список: evo tools call tg_list), обязательный",
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
		Long:  "Удалить группу целей по id (список: 'evo tools call tg_list'). Операция асинхронная.",
		Args: map[string]string{
			"id": "UUID группы целей (обязательный)",
		},
	},
	"lb_create": {
		Short: "Создать балансировщик L4 перед группой целей (платно, асинхронно)",
		Long: "Создать балансировщик нагрузки L4 перед группой целей. evo определяет VPC (по группе целей), зону " +
			"(--zone, иначе зона --subnet-id, иначе зона ВМ группы целей, иначе зона из настроек; без зоны — " +
			"отказ) и подсеть (основная подсеть зоны в этом VPC) и создаёт правило с обработчиком (--port → " +
			"--target-port) и проверкой состояния по TCP. По умолчанию балансировщик получает публичный IP " +
			"(--external). Балансировщик платный. Операция асинхронная: ход — 'evo tools call lb_operation'.",
		Args: map[string]string{
			"external":  "Выделить публичный IP; без него балансировщик только внутренний.",
			"hc_port":   "Порт проверки состояния (по умолчанию — --target-port)",
			"name":      "Имя балансировщика (обязательный)",
			"port":      "Порт обработчика, на который балансировщик принимает трафик (обязательный)",
			"subnet_id": "UUID подсети в зоне балансировщика (по умолчанию — основная подсеть зоны в VPC балансировщика)",
			"target_group_id": "UUID группы целей, на которую балансировщик распределяет трафик (список: evo tools call tg_list), " +
				"обязательный",
			"target_port": "Порт на целях, куда перенаправляется трафик (по умолчанию — --port)",
			"vpc_id":      "UUID VPC (по умолчанию — VPC группы целей)",
			"zone": "Зона доступности, которую обслуживает балансировщик: имя или UUID (список: evo vm zones). По " +
				"умолчанию — зона --subnet-id, иначе зона ВМ группы целей, иначе зона из настроек",
		},
	},
	"lb_delete": {
		Short: "Удалить балансировщик (асинхронно)",
		Long:  "Удалить балансировщик по id (список: 'evo tools call lb_list'). Операция асинхронная.",
		Args: map[string]string{
			"id": "UUID балансировщика (обязательный)",
		},
	},

	// PR 3a: snat
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
		Long: "Удалить SNAT-шлюз по id (список: 'evo tools call snat_list'). Узлы в этой зоне теряют исходящий " +
			"доступ в интернет.",
		Args: map[string]string{
			"id": "UUID SNAT-шлюза (список: evo tools call snat_list), обязательный",
		},
	},

	// PR 3b: kafka
	"kafka_list": {
		Short: "Кластеры Managed Kafka проекта",
		Long:  "Кластеры Managed Kafka: id, имя, состояние, брокеры, версия. Только чтение.",
	},
	"kafka_get": {
		Short: "Кластер Kafka по ID: состояние, адреса, брокеры, версия",
		Long:  "Кластер Managed Kafka по id: состояние, адреса (kafka_hosts), брокеры, версия. Только чтение.",
		Args: map[string]string{
			"cluster_id": "UUID кластера (список: evo tools call kafka_list), обязательный",
		},
	},
	"kafka_versions": {
		Short: "Доступные версии Managed Kafka",
		Long: "Доступные версии Managed Kafka: id и имя. Только чтение. Нужны перед созданием кластера " +
			"('evo tools call kafka_create').",
	},
	"kafka_specs": {
		Short: "Конфигурации кластера Kafka для версии: CPU, память, хранилище, брокеры",
		Long: "Конфигурации кластера Managed Kafka для версии: id, CPU, память (ГБ), минимальный объём хранилища, " +
			"наименьшее и наибольшее число брокеров. Только чтение. id конфигурации выбирают для создания " +
			"кластера ('evo tools call kafka_create').",
		Args: map[string]string{
			"version_id": "UUID версии (список: evo tools call kafka_versions); необязательно — по умолчанию последняя",
			"zone": "Зона доступности: имя (ru.AZ-2) или UUID; необязательно — по умолчанию зона из настроек, иначе " +
				"ru.AZ-2. API нужна зона",
		},
	},
	"kafka_create": {
		Short: "Создать кластер Managed Kafka (платно, асинхронно)",
		Long: "Создать кластер Managed Kafka. Имя, которое уже носит кластер проекта " +
			"('evo tools call kafka_list'), отклоняется. evo подбирает версию (последнюю), подсеть (основную " +
			"подсеть зоны из настроек, иначе первой зоны, где она есть, начиная с ru.AZ-2) и по ней — зону " +
			"кластера, конфигурацию этой зоны (по --specification-id или по --cpu и --ram; если подходящей нет — " +
			"ошибка со списком конфигураций), хранилище (минимум конфигурации) и число брокеров (min_brokers " +
			"конфигурации; живой API его не сообщает, тогда 1 — без репликации). Кластер платный. Операция " +
			"асинхронная: ход — 'evo tools call kafka_operation'.",
		Args: map[string]string{
			"brokers": "Число брокеров. По умолчанию — min_brokers конфигурации, иначе 1 (один брокер — без репликации; для " +
				"реплицируемого кластера — 3). Проверяется по min_brokers и max_brokers конфигурации, если они есть",
			"cpu":              "Число vCPU — для автоподбора конфигурации, если --specification-id не задан",
			"description":      "Описание (необязательно)",
			"name":             "Имя кластера, 2–63 символа (обязательный)",
			"ram":              "RAM, ГБ — вместе с --cpu",
			"specification_id": "UUID конфигурации (список: evo tools call kafka_specs). Необязателен, если заданы --cpu и --ram",
			"storage_gib":      "Размер диска, ГиБ (по умолчанию — минимум конфигурации)",
			"subnet_id": "UUID подсети (список: evo vpc subnets). По умолчанию — основная подсеть зоны из настроек. Кластер " +
				"создаётся в зоне подсети",
			"ui_enabled": "Включить Kafka UI (по умолчанию false)",
			"version_id": "UUID версии Kafka (список: evo tools call kafka_versions). По умолчанию — последняя",
		},
	},
	"kafka_delete": {
		Short: "Удалить кластер Kafka вместе с данными (асинхронно)",
		Long:  "Удалить кластер Managed Kafka по id вместе с данными. Операция асинхронная.",
		Args: map[string]string{
			"cluster_id": "UUID кластера (список: evo tools call kafka_list), обязательный",
		},
	},
	"kafka_operation": {
		Short: "Состояние асинхронной операции Kafka",
		Long: "Состояние асинхронной операции Managed Kafka по id — его возвращают создание и удаление кластера: " +
			"done — завершена ли операция, error — ошибка. С --wait-seconds команда сама ждёт завершения до N " +
			"секунд и выводит итоговое состояние. Только чтение.",
		Args: map[string]string{
			"operation_id": "UUID операции (обязательный)",
			"wait_seconds": "Ждать итогового состояния до N секунд (не больше 300), опрашивая операцию с нарастающим интервалом. " +
				"Без флага — одна проверка",
		},
	},

	// PR 3b: redis
	"redis_list": {
		Short: "Кластеры Managed Redis проекта",
		Long:  "Кластеры Managed Redis: id, имя, состояние, тип, шарды, реплики. Только чтение.",
	},
	"redis_get": {
		Short: "Кластер Redis по ID: состояние, адреса, тип, шарды, реплики",
		Long:  "Кластер Managed Redis по id: состояние, адреса (redis_hosts), тип, шарды, реплики. Только чтение.",
		Args: map[string]string{
			"cluster_id": "UUID кластера (список: evo tools call redis_list), обязательный",
		},
	},
	"redis_versions": {
		Short: "Доступные версии Managed Redis",
		Long: "Доступные версии Managed Redis: id и имя. Только чтение. Нужны перед созданием кластера " +
			"('evo tools call redis_create').",
	},
	"redis_specs": {
		Short: "Конфигурации кластера Redis для версии: CPU, память, режим, хранилище",
		Long: "Конфигурации кластера Managed Redis для версии: id, CPU, память (ГБ), режим развёртывания, " +
			"минимальный объём хранилища. Только чтение. id конфигурации выбирают для создания кластера " +
			"('evo tools call redis_create').",
		Args: map[string]string{
			"version_id": "UUID версии (список: evo tools call redis_versions); необязательно — по умолчанию последняя",
			"zone": "Зона доступности: имя (ru.AZ-2) или UUID; необязательно — по умолчанию зона из настроек, иначе " +
				"ru.AZ-2. API нужна зона",
		},
	},
	"redis_create": {
		Short: "Создать кластер Managed Redis (платно, асинхронно)",
		Long: "Создать кластер Managed Redis. Имя, которое уже носит кластер проекта " +
			"('evo tools call redis_list'), отклоняется. evo подбирает версию (последнюю), подсеть (как для " +
			"Kafka) и по ней — зону кластера, конфигурацию этой зоны (по --specification-id или по --cpu и " +
			"--ram; если подходящей нет — ошибка со списком конфигураций). Тип MASTER_REPLICA (по умолчанию): в " +
			"каждом шарде мастер и две реплики, все в зоне подсети — Single-AZ; API до сих пор отклонял " +
			"STANDALONE.\n\n" +
			"--replicas здесь — реплики БЕЗ мастера (поле replicas запроса на создание, 2), а сведения о " +
			"кластере ('evo tools call redis_get', 'evo tools call redis_list') показывают replicas как число " +
			"узлов в шарде вместе с мастером (3). Кластер платный. Операция асинхронная: ход — " +
			"'evo tools call redis_operation'.",
		Args: map[string]string{
			"cpu":         "Число vCPU — для автоподбора конфигурации, если --specification-id не задан",
			"description": "Описание (необязательно)",
			"name":        "Имя кластера (обязательный)",
			"ram":         "RAM, ГБ — вместе с --cpu",
			"replicas": "Реплики в шарде БЕЗ мастера — поле replicas запроса на создание. Для MASTER_REPLICA — 2 (по " +
				"умолчанию: мастер и две реплики, всего 3 узла — это число сведения о кластере показывают как " +
				"replicas); для STANDALONE — 0",
			"shards":           "Число шардов, 1–8 (по умолчанию 1)",
			"specification_id": "UUID конфигурации (список: evo tools call redis_specs). Необязателен, если заданы --cpu и --ram",
			"storage_gb":       "Размер диска, ГБ (по умолчанию — минимум конфигурации)",
			"subnet_id": "UUID подсети (список: evo vpc subnets). По умолчанию — основная подсеть зоны из настроек. Кластер " +
				"создаётся в зоне подсети",
			"type":       "MASTER_REPLICA (по умолчанию) или STANDALONE (API пока его отклоняет)",
			"version_id": "UUID версии Redis (список: evo tools call redis_versions). По умолчанию — последняя",
		},
	},
	"redis_delete": {
		Short: "Удалить кластер Redis вместе с данными (асинхронно)",
		Long:  "Удалить кластер Managed Redis по id вместе с данными. Операция асинхронная.",
		Args: map[string]string{
			"cluster_id": "UUID кластера (список: evo tools call redis_list), обязательный",
		},
	},
	"redis_operation": {
		Short: "Состояние асинхронной операции Redis",
		Long: "Состояние асинхронной операции Managed Redis по id — его возвращают создание и удаление кластера: " +
			"done — завершена ли операция, error — ошибка. С --wait-seconds команда сама ждёт завершения до N " +
			"секунд и выводит итоговое состояние. Только чтение.",
		Args: map[string]string{
			"operation_id": "UUID операции (обязательный)",
			"wait_seconds": "Ждать итогового состояния до N секунд (не больше 300), опрашивая операцию с нарастающим интервалом. " +
				"Без флага — одна проверка",
		},
	},

	// PR 3b: ar
	"ar_list": {
		Short: "Реестры Artifact Registry проекта",
		Long: "Реестры Artifact Registry проекта: id, имя, тип, состояние, публичный ли, тариф. Только чтение. " +
			"Реестр хранит образы Docker и чарты Helm — источник образов для Container Apps и Managed Kubernetes " +
			"там, где docker.io не подходит.",
	},
	"ar_get": {
		Short: "Реестр Artifact Registry по ID: все сведения",
		Long: "Все сведения об одном реестре Artifact Registry по id (список: 'evo tools call ar_list'). Только " +
			"чтение.",
		Args: map[string]string{
			"id": "UUID реестра (список: evo tools call ar_list), обязательный",
		},
	},
	"ar_create": {
		Short: "Создать реестр Artifact Registry (платно, асинхронно)",
		Long: "Создать реестр Artifact Registry; имя, которое уже носит реестр проекта ('evo tools call ar_list'), " +
			"отклоняется. Тип (--registry-type): DOCKER (по умолчанию), DEBIAN, RPM, GENERIC, PYPI или NPM. " +
			"Реестр платный. Операция асинхронная: ход — 'evo tools call ar_operation'; затем образы загружают " +
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
		Long: "Удалить реестр Artifact Registry по id (список: 'evo tools call ar_list') вместе с его артефактами. " +
			"Операция асинхронная.",
		Args: map[string]string{
			"id": "UUID реестра (список: evo tools call ar_list), обязательный",
		},
	},
	"ar_operation": {
		Short: "Состояние асинхронной операции Artifact Registry",
		Long: "Состояние асинхронной операции Artifact Registry по id — его возвращают создание и удаление " +
			"реестра: done — завершена ли операция, error — ошибка. С --wait-seconds команда сама ждёт " +
			"завершения до N секунд и выводит итоговое состояние. Только чтение.",
		Args: map[string]string{
			"operation_id": "UUID операции (обязательный)",
			"wait_seconds": "Ждать итогового состояния до N секунд (не больше 300), опрашивая операцию с нарастающим интервалом. " +
				"Без флага — одна проверка",
		},
	},

	// PR 3b: ca
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
		Long: "Развернуть (создать и запустить) приложение Container Apps из образа контейнера — бессерверно, без " +
			"нагрузки приложение масштабируется до нуля. Превью (will_create) перечисляет все настройки, в том " +
			"числе работает ли контейнер от root. Имя, порт и число экземпляров проверяются до подтверждения. " +
			"Операция асинхронная: ответ содержит operation_id; состояние и публичный URL — " +
			"'evo tools call ca_get'. Подходит для веб-сервисов и API без состояния, когда не нужна своя ВМ.",
		Args: map[string]string{
			"cpu":         "Запрос CPU, например 0.1 или 1 (по умолчанию 0.1)",
			"description": "Описание (необязательно)",
			"env":         "Переменные окружения: объект имя→значение",
			"image": "URI образа контейнера из Artifact Registry этого проекта (или публичного реестра другого проекта), " +
				"linux/amd64, например <registry>.cr.cloud.ru/<repository>:<tag>. Приложение должно слушать " +
				"0.0.0.0:$PORT",
			"max_instances": "Наибольшее число экземпляров (по умолчанию 1, не меньше 1)",
			"memory":        "Запрос памяти, например 256Mi или 1Gi (по умолчанию 256Mi)",
			"min_instances": "Наименьшее число экземпляров (по умолчанию 0 — масштабирование до нуля); не больше --max-instances",
			"name":          "Имя приложения: метка RFC 1123 (строчные буквы, цифры и '-', 1–63 символа), уникальное",
			"port": "Порт контейнера, который слушает приложение (по умолчанию 8080). Порты, занятые платформой, " +
				"отклоняются: 1–1024, 8008, 8012, 8013, 8022, 8112, 9090, 9091, 9092",
			"privileged": "Запустить контейнер в привилегированном режиме, от root (по умолчанию false: от UID 1000). Только " +
				"для образов, которым нужен root; превью об этом сообщает",
			"public": "Открыть публичный URL.",
		},
	},
	"ca_delete": {
		Short: "Удалить приложение Container Apps (асинхронно)",
		Long:  "Удалить приложение Container Apps по имени. Операция асинхронная.",
		Args: map[string]string{
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

	// PR 3c: billing
	"billing_balance": {
		Short: "Баланс по договору: сумма в рублях, признак активности",
		Long: "Баланс счёта по договору: сумма в рублях, признак активности и исходный объект договора. Только " +
			"чтение. Нужен CLOUDRU_AGREEMENT_ID: баланс принадлежит договору, по проекту его не даёт ни один " +
			"API; id договора видят только администраторы организации и администраторы затрат. Запрос идёт к " +
			"billing.api.cloud.ru/v1/agreement/{agreement_id}; этого метода нет в опубликованных спецификациях " +
			"API Cloud.ru, поэтому он работает по возможности и может измениться без предупреждения. Так " +
			"проверяют, на сколько ещё хватит баланса.",
	},
	"billing_consumption": {
		Short: "Стоимость потребления за период по сервисам: по договору или по проекту",
		Long: "Стоимость потребления за период — по договору (CLOUDRU_AGREEMENT_ID) или, без него, по проекту; " +
			"поле scope в ответе говорит, по чему. Сводка по сервисам: total (без НДС, сумма amount), " +
			"total_with_nds (с НДС, сумма amount_nds), records, by_service — по убыванию стоимости (без НДС). " +
			"Только чтение. Роли: для договора — администратор организации или администратор затрат; для проекта " +
			"— администратор проекта (platform.project.admin). Для FinOps и анализа затрат.",
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
			"прогнозная дата нулевого баланса и, если задан бюджет, его проверка. Затраты без НДС (amount). " +
			"Отбор — как у 'evo tools call billing_consumption'; без CLOUDRU_AGREEMENT_ID баланс и запас " +
			"неизвестны. Перенесён из finops_report.py. Для анализа затрат, предупреждений о запасе и проверки " +
			"бюджета.",
		Args: map[string]string{
			"budget": "Месячный бюджет, руб.: прогноз расходов за месяц сравнивается с ним (необязательно)",
			"days":   "Длина окна в днях, если дата начала не задана: 1–366 (по умолчанию 30)",
			"end":    "Дата окончания, ГГГГ-ММ-ДД (по умолчанию — сегодня)",
			"project_id": "UUID проекта (по умолчанию — текущий проект, если CLOUDRU_AGREEMENT_ID не задан; с ним — весь " +
				"договор)",
			"start": "Дата начала, ГГГГ-ММ-ДД (по умолчанию — за --days дней по дату окончания включительно)",
		},
	},

	// PR 3c: audit
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

	// PR 3c: org, iam
	"org_quotas": {
		Short: "Квоты: ресурс, лимит, текущее использование",
		Long: "Использование квот: ресурс, лимит, текущее использование — для планирования мощностей. Только " +
			"чтение. Можно ограничить одним проектом (--project-id). Ответ может быть неполным, если у " +
			"сервисного аккаунта недостаточно прав.",
		Args: map[string]string{
			"project_id": "UUID проекта, которым ограничить квоты (необязательно)",
		},
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

	// PR 3c: metrics
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
			"или отобранных метрик на этот момент (только чтение). Отбор — --match; без него — все метрики.",
		Args: map[string]string{
			"match": "Селектор PromQL, например {__name__=~\"cpu.*\"} (необязательно; по умолчанию — все метрики)",
		},
	},

	// PR 3c: stack
	"stack_plan": {
		Short: "Спланировать стек ресурсов по одному манифесту (ничего не создаёт)",
		Long: "Спланировать стек из нескольких ресурсов по ОДНОМУ декларативному манифесту. Для развёртывания из " +
			"нескольких сервисов используйте стек, а не создавайте кластер PostgreSQL, кластер Kubernetes, ВМ, " +
			"группу целей и остальное по одному: всю цепочку выполняет evo. Возвращает job_id и оценку стоимости " +
			"по тарифам; стек создаёт 'evo tools call stack_apply'. Только чтение: ничего не создаёт.\n\n" +
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
		Long: "Применить спланированный стек (из 'evo tools call stack_plan'). Ресурсы платные. Вызов запускает " +
			"готовые шаги и возвращает краткое состояние; он может идти минутами: шаг ВМ ждёт, пока ВМ станет " +
			"активной (вживую — около 3 минут), а асинхронные ресурсы (PostgreSQL, Kafka, Redis, Kubernetes, " +
			"балансировщик) только запускаются. Затем опрашивайте 'evo tools call stack_status' с этим job_id, " +
			"пока status не станет done. Управлять ресурсами по отдельности не нужно.",
		Args: map[string]string{
			"job_id": "id задания из evo tools call stack_plan (обязательный)",
		},
	},
	"stack_status": {
		Short: "Ход развёртывания стека",
		Long: "Ход развёртывания стека, кратко: status, done[], running[], pending[], outputs{postgres, vm, tg, " +
			"lb, …; lb_external_ip — когда у балансировщика появился публичный адрес}. Для задания, запущенного " +
			"'evo tools call stack_apply', каждый вызов продвигает развёртывание И ждёт внутри (до " +
			"--wait-seconds), так что на долгое развёртывание хватает нескольких вызовов. Спланированное, но не " +
			"запущенное задание только показывается и никогда не запускается. Повторяйте, пока status не станет " +
			"done (тогда читайте outputs; lb_external_ip есть только у балансировщика с публичным адресом) или " +
			"error. Задание хранится на диске: состояние можно запросить снова по job_id в любой момент.",
		Args: map[string]string{
			"job_id": "id задания (обязательный)",
			"wait_seconds": "Сколько секунд этот вызов может ждать, продвигая развёртывание (по умолчанию 90, не больше 240). " +
				"Чем больше, тем меньше вызовов для долгого развёртывания",
		},
	},
	"stack_destroy": {
		Short: "Удалить все ресурсы стека (безвозвратно)",
		Long: "Удалить все ресурсы стека — в обратном порядке, идемпотентно. status=destroyed значит, что каждое " +
			"удаление принято: ВМ, секреты и объекты S3 удалены, а асинхронные ресурсы из списка deleting " +
			"(PostgreSQL, Kafka, Redis, Kubernetes, балансировщик и другие) облако удаляет ещё несколько минут " +
			"(PostgreSQL вживую — около 5 минут); проверьте их через 'evo pg list', 'evo k8s list' и подобные " +
			"команды.",
		Args: map[string]string{
			"job_id": "id задания (обязательный)",
		},
	},

	// PR 3c: image
	"image_create": {
		Short: "Создать образ ВМ из загрузочного диска (платно, асинхронно)",
		Long: "Создать многоразовый образ ВМ ИЗ ЗАГРУЗОЧНОГО ДИСКА — последний шаг подготовки распространяемого " +
			"образа (например, продукта Маркетплейса): создайте ВМ, установите продукт, очистите систему, " +
			"остановите ВМ и снимите образ с её загрузочного диска. Диск должен быть загрузочным И отключённым: " +
			"выключите ВМ, отключите загрузочный диск ('evo disk detach') и дождитесь состояния available в " +
			"'evo disk list'. Остановить ВМ недостаточно: пока диск подключён, API отвечает " +
			"invalid_disk_state_to_create_image.\n\n" +
			"Для образа Маркетплейса передайте и --user-data-template (шаблон cloud-init на Jinja), и " +
			"--image-metadata с объявлением каждой {{ var }} шаблона: так SSH-ключ или пароль покупателя " +
			"попадает в гостевую систему. Сборка асинхронная (около 0,7 ГБ в минуту): повторяйте " +
			"'evo image get', пока образ не станет готов (ready=true). Хранение образа платное.",
		Args: map[string]string{
			"description": "Описание в свободной форме, НЕ БОЛЬШЕ 255 символов (длиннее API отклоняет с too_long). Достаточно " +
				"продукта, закреплённой версии и digest; юридические оговорки — в карточке Маркетплейса, а не здесь",
			"disk_id":      "UUID загрузочного диска-источника (список: evo disk list)",
			"display_name": "Понятное имя, которое показывает консоль",
			"image_metadata": "Поля, которые заполняет покупатель; они подставляются в --user-data-template, и каждая {{ var }} " +
				"шаблона должна быть объявлена здесь. Элемент: {name, display_name, display_position, field_type, " +
				"default_value, sensitive, mandatory_group} — display_name и display_position ОБЯЗАТЕЛЬНЫ, а 'id' и " +
				"'is_required' отклоняются (схема записи отличается от того, что показывает evo image get). " +
				"field_type — типизированные значения платформы (public_key, linux_password, linux_login, hostname, " +
				"flag); public_key и linux_password поместите в одну mandatory_group ('authentication'), чтобы " +
				"доступ давало любое из них",
			"min_cpu":  "Наименьшее число vCPU, с которым ВМ загрузится из образа",
			"min_disk": "Наименьший загрузочный диск, ГБ — не меньше размера образа, иначе ВМ из него не создать",
			"min_ram":  "Наименьший объём RAM, ГБ",
			"name":     "Имя образа (техническое, уникальное в проекте)",
			"user_data_template": "Шаблон cloud-init (Jinja), который подставляется при создании ВМ из образа. Обязателен для образов " +
				"Маркетплейса",
			"zone": "Зона доступности, в которой собирать образ: имя или UUID. По умолчанию — зона из настроек, а если " +
				"она не задана — зона диска-источника. Должна совпадать с зоной диска",
		},
	},
	"image_delete": {
		Short: "Удалить образ ВМ",
		Long:  "Удалить образ ВМ. Работающие ВМ продолжают работать, но новую ВМ из образа уже не создать.",
		Args: map[string]string{
			"image_id": "UUID образа (список: evo image list)",
		},
	},

	// PR 3c: open_ports, deploy_plan, deploy_apply, list
	"open_ports": {
		Short: "Открыть входящие порты в группе безопасности",
		Long: "Открыть входящие порты в группе безопасности Cloud.ru: добавить правила ingress, по умолчанию tcp " +
			"из 0.0.0.0/0. Так делают сервис доступным (например, 80 и 443 для веб-сервера). Группа задаётся " +
			"--security-group (имя или UUID). Уже открытые порты пропускаются. Группы безопасности общие для " +
			"зоны доступности: открытый порт затрагивает каждую ВМ в этой группе.",
		Args: map[string]string{
			"cidr":           "CIDR источника (по умолчанию 0.0.0.0/0 — откуда угодно)",
			"ports":          "Порты, которые открыть, например 80,443",
			"protocol":       "Протокол: tcp (по умолчанию) или udp",
			"security_group": "Группа безопасности: имя (например, SSH-access_ru.AZ-2) или UUID (список: evo vpc security-groups)",
		},
	},
	"deploy_plan": {
		Short: "Шаг 1 мастера развёртывания: рассчитать ВМ и её цену, ничего не создавая",
		Long: "Шаг 1 мастера развёртывания (только чтение, ничего не создаёт). Подбирает полную конфигурацию ВМ — " +
			"flavor, образ, зону, диск, сеть, SSH — и возвращает её вместе с ЦЕНОЙ ЗА МЕСЯЦ и plan_id " +
			"(cost.source: API расчёта цен Cloud.ru или оценка по тарифам, если API цены не дал; лицензия образа " +
			"не учитывается никогда). В режиме auto передайте требования продукта (--cpu, --ram-gb, --disk-gb из " +
			"его README), остальное заполнят значения по умолчанию; в режиме manual — явный выбор (--flavor, " +
			"--zone, --image, --disk-type). Проверьте план и цену, затем выполните план: " +
			"'evo tools call deploy_apply' с этим plan_id.",
		Args: map[string]string{
			"base_plan_id": "Необязательно: прежний plan_id, который нужно ИЗМЕНИТЬ. Берутся параметры того плана, поверх — " +
				"только переданные здесь поля (например, одно --disk-gb); возвращается новый plan_id. Так меняют " +
				"одно значение, не повторяя остальные",
			"compose_file": "Путь к docker-compose.yml, если это развёртывание compose (задаёт compose_required). С ним образ " +
				"Astra Linux, Альт или РЕД ОС отклоняется (USAGE, плана нет): установочный скрипт Docker их не " +
				"поддерживает",
			"cpu":         "Нужное число vCPU (автоподбор выберет самый маленький подходящий flavor)",
			"disk_gb":     "Размер загрузочного диска, ГБ (по умолчанию 10)",
			"disk_type":   "manual: имя типа диска, например SSD (список: evo disk types)",
			"external_ip": "Подключить публичный floating IP (по умолчанию true)",
			"flavor": "manual: точное имя flavor (список: evo vm flavors). По умолчанию — flavor из настроек (профиль, " +
				".evo.yaml, CLOUDRU_FLAVOR), если не заданы --cpu и --ram-gb, иначе — автоподбор",
			"image": "manual: имя или UUID образа (поиск: evo image list). По умолчанию — образ из настроек (image или " +
				"image_id: профиль, .evo.yaml), иначе Ubuntu 24.04",
			"image_metadata": "Значения полей метаданных образа по имени — обязательные поля, которые evo не заполняет сам (evo " +
				"image get <образ>). Без них план отклоняется. Хранятся в локальном файле плана до его выполнения",
			"login": "Пользователь для входа на ВМ (по умолчанию user1)",
			"mode":  "auto (значения по умолчанию и требования) или manual (явный выбор). По умолчанию auto",
			"name":  "Имя ВМ (по умолчанию <product>-<время>)",
			"ports": "Порты продукта, которые открыть в группе безопасности ВМ (например, 80,443 для веб-сервера — из " +
				"README). Выполнение плана открывает их само; evo vm delete удаляет созданные им правила",
			"product": "Имя разворачиваемого продукта или приложения (для имени ВМ и отчётов)",
			"ram_gb":  "Нужный объём RAM, ГБ (автоподбор)",
			"ssh_key": "Полный открытый SSH-ключ для ВМ (например, 'ssh-ed25519 AAAA... user'). Без него на ВМ попадает " +
				"ключ развёртывания evo из рабочего каталога (.evo/id_ed25519.pub), чтобы evo мог зайти по SSH; ключ " +
				"пользователя добавьте в authorized_keys потом",
			"zone": "manual: зона доступности, имя или UUID (список: evo vm zones). По умолчанию — зона из настроек " +
				"(профиль, .evo.yaml, CLOUDRU_REGION), иначе ru.AZ-2",
		},
	},
	"deploy_apply": {
		Short: "Шаг 2 мастера развёртывания: выполнить рассчитанный план",
		Long: "Шаг 2 мастера развёртывания: выполнить план, который вернул 'evo tools call deploy_plan'. " +
			"Выполняйте его только после того, как план и его цена одобрены. Идемпотентно: если ВМ с именем из " +
			"плана уже есть, она подхватывается, а не создаётся вторая. Возвращает vm_id, public_ip, ssh и (один " +
			"раз) сгенерированный пароль.",
		Args: map[string]string{
			"plan_id": "plan_id, который вернул evo tools call deploy_plan (обязательный)",
		},
	},
	"list": {
		Short: "Локальные записи развёртываний этого проекта",
		Long: "Все записанные развёртывания этого проекта: IP ВМ, состояние, flavor, время создания. Читает " +
			"локальное состояние, без обращений к API.",
	},

	// PR 5: the engine commands
	"deploy": {
		Short: "Развернуть локальный проект docker-compose на ВМ Cloud.ru",
		Long: "Развернуть локальный проект docker-compose на ВМ Cloud.ru: создать ВМ (размер подбирается сам), " +
			"назначить публичный floating IP, загрузить файлы compose по SSH и выполнить 'docker compose up -d'. " +
			"Существующая ВМ используется снова, а развёртывание без изменений пропускается, если не задан " +
			"--force. Нужен ключ доступа Cloud.ru (профиль из 'evo configure' или CLOUDRU_KEY_ID и " +
			"CLOUDRU_SECRET), а проект, зона и образ — из переменных окружения, настроек или аргументов. Платно, " +
			"если создаётся ВМ.",
		Args: map[string]string{
			"compose_file": "Путь к файлу docker-compose (по умолчанию docker-compose.yml)",
			"disk_size":    "Размер загрузочного диска, ГБ (по умолчанию 10, но не меньше минимума образа)",
			"flavor":       "Flavor ВМ или auto — самый маленький подходящий (по умолчанию auto)",
			"force":        "Развернуть заново, даже если хеш compose не изменился",
			"image": "Загрузочный образ: точное имя, отображаемое имя или UUID (Ubuntu или Debian: Astra Linux, Альт и " +
				"РЕД ОС отклоняются — на них не установить Docker). Задаётся --image или --image-id, не оба",
			"image_id":   "UUID загрузочного образа (см. evo image list, evo image get)",
			"project_id": "UUID проекта Cloud.ru",
			"region":     "UUID зоны доступности (список: evo vm zones)",
		},
	},
	"status": {
		Short: "Состояние текущего развёртывания",
		Long: "Состояние текущего развёртывания по локальной записи: ID ВМ, публичный IP, flavor, регион, " +
			"работоспособность и открытые порты. Только чтение. По умолчанию спрашивает у облака и живое " +
			"состояние ВМ (один GET, не дольше 8 с) и добавляет cloud_checked, cloud_state, " +
			"not_found_in_cloud=true (ВМ удалили в обход evo: запись убирает 'evo destroy') и cloud_error с " +
			"cloud_reason (почему облако не проверено — фраза и код ошибки; локальное состояние возвращается и " +
			"тогда).",
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
		Long: "Проекты Cloud.ru, которые видит ключ доступа: id, имя, состояние, подразделение организации. Только " +
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
