package cli

// The tree commands are made from the tools, whose descriptions are English:
// agents read them through MCP. The Russian help of a tree command takes its
// summary from here; a tool missing from the map falls back to the first
// sentence of its English description (TestTrees_RussianShorts keeps the map
// complete).
//
// The trees vm and disk are Russian throughout: the long help (toolLongRU)
// and every flag (flagShortRU) too, written for the CLI — consent is the
// prompt or --yes, not confirmed=true, and other tools are named by their
// commands (TestTrees_RussianFlags keeps these complete). The other trees
// show the tool's English description and flags, as MCP does (v1.1).

// treeGroupShortRU is the Russian summary of each tree.
var treeGroupShortRU = map[string]string{
	"vm":          "Виртуальные машины",
	"image":       "Образы ВМ: операционные системы и продукты маркетплейса",
	"marketplace": "Витрина: образы ВМ и плагины Kubernetes с командой развёртывания каждого",
	"disk":        "Диски",
	"vpc":         "Сети: VPC, маршруты, подсети, группы безопасности, floating IP",
	"k8s":         "Кластеры Managed Kubernetes",
	"pg":          "Managed PostgreSQL: кластеры, базы данных, пользователи, резервные копии",
	"s3":          "Object Storage: бакеты и объекты",
	"secret":      "Secret Manager: секреты",
}

// toolShortRU is the Russian summary of each tool used by a tree command.
var toolShortRU = map[string]string{
	"vm_list":      "Список ВМ проекта: id, имя, состояние, зона, время создания",
	"vm_get":       "ВМ по ID или имени: внутренний и внешний IP, состояние, интерфейсы",
	"provision":    "Создать ВМ (по умолчанию с floating IP; без внешнего адреса — --no-public-ip)",
	"destroy":      "Удалить ВМ и все данные на ней (безвозвратно)",
	"list_zones":   "Зоны доступности (UUID и имя)",
	"list_flavors": "Flavor ВМ: vCPU, RAM, бесплатный уровень, оценка цены за месяц",

	"image_list": "Поиск образов: ОС (public), маркетплейс (marketplace), образы проекта (private)",
	"image_get":  "Образ по имени или UUID: минимум ресурсов, зоны, поля и кто их заполнит, команда создания ВМ",

	"marketplace_list": "Витрина: ОС, продукты маркетплейса и плагины Kubernetes одним списком, с командой развёртывания",

	"disk_list":       "Диски: id, имя, размер, состояние, тип, зона, к каким ВМ подключены",
	"list_disk_types": "Типы дисков (UUID и имя, например SSD или HDD)",
	"disk_create":     "Создать диск (с --attach-to — в зоне ВМ и сразу подключить)",
	"disk_attach":     "Подключить диск к ВМ (диск и ВМ должны быть в одной зоне)",
	"disk_detach":     "Отключить диск от ВМ",
	"disk_resize":     "Увеличить диск (только рост; затем расширьте раздел и файловую систему на ВМ)",
	"disk_delete":     "Удалить диск и все данные на нём (безвозвратно)",

	"vpc_list":             "Список VPC проекта",
	"vpc_get":              "VPC по ID: все сведения",
	"vpc_create":           "Создать VPC (асинхронно)",
	"vpc_delete":           "Удалить VPC (асинхронно; сначала удалите его подсети и ресурсы)",
	"vpc_operation":        "Состояние асинхронной операции VPC",
	"vpc_routes":           "Статические маршруты VPC",
	"vpc_route_create":     "Добавить статический маршрут в VPC (асинхронно)",
	"vpc_route_delete":     "Удалить статический маршрут из VPC (асинхронно)",
	"list_subnets":         "Подсети: UUID, имя, зона доступности",
	"list_security_groups": "Группы безопасности (с --rules — вместе с правилами)",
	"list_floating_ips":    "Floating IP: адрес, состояние, свободен ли",
	"fip_attach":           "Привязать свободный floating IP к основному интерфейсу ВМ",
	"release_floating_ip":  "Освободить floating IP (адрес, занятый ВМ или SNAT-шлюзом, не освобождается)",

	"k8s_list":         "Кластеры Managed Kubernetes проекта",
	"k8s_get":          "Кластер Managed Kubernetes по ID",
	"k8s_options":      "Версии Kubernetes, flavor узлов и зоны для кластеров",
	"k8s_plan":         "Шаг 1: рассчитать конфигурацию кластера, ничего не создавая",
	"k8s_create":       "Шаг 2: создать кластер по расчёту k8s plan (платно, асинхронно, около 10 минут)",
	"k8s_delete":       "Удалить кластер Managed Kubernetes (асинхронно)",
	"k8s_kubeconfig":   "kubeconfig кластера (по умолчанию со встроенным токеном примерно на час)",
	"k8s_nodepools":    "Группы узлов кластера",
	"k8s_add_nodepool": "Добавить группу рабочих узлов (платно, асинхронно)",
	"k8s_kubectl":      "Команда kubectl в кластере (изменяющие — с подтверждением)",
	"k8s_apply":        "Применить манифест Kubernetes (как kubectl apply -f -)",
	"k8s_helm":         "Helm в кластере: install, upgrade, uninstall, list, status",
	// Addons of a cluster.
	"k8s_addons":         "Каталог плагинов Kubernetes: поиск по имени и описанию, категория, платность",
	"k8s_addon_versions": "Версии плагина: режим поддержки, требования к кластеру, схема настроек",
	"k8s_add_addon":      "Установить плагин в кластер (асинхронно; версия по умолчанию — последняя активная)",

	"pg_list":          "Кластеры Managed PostgreSQL проекта",
	"pg_get":           "Кластер PostgreSQL по ID: версия, хранилище, строка подключения, состояние",
	"pg_versions":      "Доступные версии PostgreSQL",
	"pg_specs":         "Конфигурации кластера PostgreSQL для версии: CPU, память, минимальный объём",
	"pg_create":        "Создать кластер PostgreSQL (платно, асинхронно)",
	"pg_delete":        "Удалить кластер PostgreSQL вместе с данными и всеми резервными копиями (асинхронно)",
	"pg_operation":     "Состояние асинхронной операции PostgreSQL",
	"pg_databases":     "Базы данных кластера",
	"pg_db_create":     "Создать базу данных",
	"pg_db_delete":     "Удалить базу данных",
	"pg_users":         "Пользователи кластера и их роли",
	"pg_user_create":   "Создать пользователя (пароль не выводится)",
	"pg_user_delete":   "Удалить пользователя",
	"pg_backups":       "Резервные копии кластера",
	"pg_backup_create": "Создать резервную копию вручную (асинхронно)",

	"s3_buckets":       "Бакеты Object Storage",
	"s3_ls":            "Объекты бакета: ключ, размер, время изменения",
	"s3_get":           "Скачать объект до 10 МиБ (небольшой текст — как есть, остальное — в base64; больший объект — через S3-клиент или консоль)",
	"s3_put":           "Загрузить объект (существующий ключ перезаписывается)",
	"s3_rm":            "Удалить объект",
	"s3_bucket_create": "Создать бакет",
	"s3_bucket_delete": "Удалить пустой бакет",

	"secret_list":        "Секреты проекта (без значений)",
	"secret_get":         "Метаданные секрета: папка, метки, версии, ключ KMS (без значения)",
	"secret_value":       "Расшифрованное значение секрета",
	"secret_create":      "Создать секрет",
	"secret_add_version": "Добавить новую версию значения секрета (ротация)",
	"secret_delete":      "Удалить секрет со всеми версиями",
}

// toolLongRU is the Russian long help of a tree command, in place of the
// tool's English description.
var toolLongRU = map[string]string{
	"vm_list": "Все ВМ проекта по данным облака, а не по локальным записям развёртываний: id, имя, состояние, " +
		"зона доступности и время создания. Только чтение. IP-адреса и интерфейсы одной ВМ — 'evo vm get <ВМ>'.",
	"vm_get": "Сведения о ВМ из облака по UUID или имени: внутренний IP (private_ip), внешний или floating IP (public_ip), " +
		"состояние и все сетевые интерфейсы. В отличие от 'evo status', который читает локальную запись развёртывания, " +
		"показывает и внутренний IP — его, например, прописывают в настройки другого сервиса. Только чтение.",
	"provision": "Создать ВМ без docker-compose. По умолчанию ВМ получает floating IP (внешний адрес); с --no-public-ip " +
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
	"destroy": "Удалить ВМ и все данные на ней безвозвратно. ВМ задаётся UUID или именем; превью называет ВМ (id, имя, зону, " +
		"состояние, адрес) и floating IP, который будет освобождён. Несуществующая ВМ — код 4 уже на превью; имя, которое носят " +
		"несколько ВМ, — отказ со списком кандидатов: укажите UUID. Floating IP ВМ по умолчанию " +
		"освобождается тоже; оставить адрес — --release-ip=false. Правила группы безопасности, которые evo открыл для ВМ " +
		"('evo deploy', 'evo vm create --port'), удаляются после неё, если порт не нужен другому развёртыванию из " +
		"локального состояния; правило, которое удалить не удалось, попадает в отчёт, и следующее удаление попробует снова. " +
		"Если Cloud.ru принял удаление, но ВМ осталась в списке, — код 5: floating IP, правила и запись остаются до повторного удаления.",
	"list_zones": "Зоны доступности: UUID и имя. UUID зоны передаётся в --region, например 'evo vm create --region <UUID>'. " +
		"Только чтение.",
	"list_flavors": "Flavor ВМ: имя, vCPU, RAM, признак бесплатного уровня и оценка цены за месяц. Имя flavor передаётся " +
		"в --flavor команд 'evo vm create' и 'evo deploy'. Только чтение.",

	"disk_list": "Диски проекта: id, имя, размер (ГБ), состояние, тип, зона и ВМ, к которым диск подключён. Только чтение.",
	"list_disk_types": "Типы дисков: UUID и имя (например, SSD или HDD). Имя или UUID типа передаётся в --disk-type " +
		"команды 'evo disk create'. Только чтение.",
	"disk_create": "Создать диск. С --attach-to <ВМ> диск создаётся в зоне этой ВМ и сразу к ней подключается: диск и ВМ " +
		"должны быть в одной зоне. Подключённый диск — новое блочное устройство, файловая система ВМ от него не растёт: " +
		"разметьте, отформатируйте и смонтируйте его на ВМ. Увеличить существующий диск — 'evo disk resize'. Диск платный.",
	"disk_attach": "Подключить существующий диск к ВМ (--vm — имя или UUID ВМ). Диск и ВМ должны быть в одной зоне; " +
		"зону и id диска показывает 'evo disk list'. Подключённый диск — новое блочное устройство: разметьте, " +
		"отформатируйте и смонтируйте его на ВМ.",
	"disk_detach": "Отключить диск от ВМ (--vm — ВМ, к которой он подключён). Загрузочный или используемый диск не " +
		"отключайте: сначала отмонтируйте файловую систему на ВМ.",
	"disk_resize": "Увеличить диск на месте, до 16384 ГБ (16 ТБ). Только рост: уменьшение и диски бесплатного уровня облако " +
		"отклоняет (бесплатный загрузочный диск не увеличить — пересоздайте ВМ с платным диском). После увеличения " +
		"расширьте раздел и файловую систему на ВМ (growpart и resize2fs).",
	"disk_delete": "Удалить диск и все данные на нём безвозвратно. Подключённый диск сначала отключите: 'evo disk detach'.",
}

// flagShortRU is the Russian help of a tree command's flag, by
// "tool.argument". The notes on repeatable flags and on a flag that
// defaults to true are added to it, as to the English one.
var flagShortRU = map[string]string{
	"provision.flavor": "Flavor ВМ: имя или UUID, например lowcost10-2-4 (список: evo vm flavors). По умолчанию — " +
		"самый маленький lowcost не бесплатного уровня",
	"provision.image": "Образ: точное имя, отображаемое имя или UUID, например Ubuntu-24.04 (поиск: evo image list). " +
		"Задаётся --image или --image-id, не оба",
	"provision.image_id": "UUID образа (см. evo image list, evo image get)",
	"provision.image_metadata": "Поле метаданных образа, например admin_password=… — обязательные поля, которые evo не " +
		"заполняет сам (evo image get <образ>). Перекрывает заполненное evo; поле, которого в образе нет, отклоняется",
	"provision.ports": "Входящий порт, открываемый в группе безопасности ВМ для 0.0.0.0/0: 80, 8080/tcp, 53/udp " +
		"(по умолчанию tcp). Группа общая для ВМ зоны; evo vm delete удаляет правила, созданные evo",
	"provision.disk_size":    "Размер загрузочного диска, ГБ (по умолчанию 10, но не меньше минимума образа)",
	"provision.subnet_id":    "UUID подсети для интерфейса (по умолчанию — подсеть в зоне ВМ; список: evo vpc subnets)",
	"provision.no_public_ip": "Приватная ВМ: без floating IP, интерфейс типа regular — для узлов за балансировщиком и внутренних серверов",
	"destroy.release_ip":     "Освободить и floating IP ВМ.",

	"disk_create.size_gb":   "Размер диска, ГБ (обязательный; не больше 16384)",
	"disk_create.disk_type": "Тип диска: имя или UUID (список: evo disk types). По умолчанию — SSD",
	"disk_create.attach_to": "ВМ (имя или UUID): диск создаётся в её зоне и сразу к ней подключается",
	"disk_create.zone":      "Зона доступности: имя или UUID, вместо --attach-to (по умолчанию — зона из --region или профиля)",
	"disk_attach.vm":        "ВМ: имя или UUID",
	"disk_detach.vm":        "ВМ, к которой подключён диск: имя или UUID",
	"disk_resize.size_gb":   "Новый размер, ГБ: больше текущего, не больше 16384",
}

// russianTrees are the trees whose human tables speak Russian too: the
// column names, да/нет for booleans and the note under a list (the README
// promises the vm and disk trees in Russian). --json and MCP keep the
// tool's keys and English notes.
var russianTrees = map[string]bool{"vm": true, "disk": true}

// columnHeaderRU is the Russian title of a column of those tables, by the
// key of the tool's answer; a key without one is shown in capitals.
var columnHeaderRU = map[string]string{
	"id":              "ID",
	"name":            "Имя",
	"state":           "Состояние",
	"status":          "Состояние",
	"zone":            "Зона",
	"created_time":    "Создана",
	"vcpus":           "vCPU",
	"ram_gb":          "RAM, ГБ",
	"est_monthly_rub": "≈ руб./мес",
	"free_tier":       "Бесплатный уровень",
	"size_gb":         "Размер, ГБ",
	"disk_type":       "Тип",
	"attached_to":     "Подключён к",
	"enabled":         "Открыта",
}

// listNoteRU is the Russian text of a note a tool puts under its list, by
// its English text (the MCP answer keeps the English one).
var listNoteRU = map[string]string{
	"est_monthly_rub is a rate-card estimate incl. 10GB SSD, not a billed price": "Цена — оценка по тарифам, с диском SSD 10 ГБ; это не счёт.",
}

// noteLabelRU names the other notes under a list.
var noteLabelRU = map[string]string{"note": "Примечание", "warning": "Предупреждение", "hint": "Подсказка"}
