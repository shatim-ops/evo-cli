#!/usr/bin/env bash
# Класс изменения PR по путям (аудит процессов 1.2.1, PR 8): что затронуто
# и какие проверки этому классу нужны сверх CI (матрица — отчёт аудита, §4).
# Пути PR — только данные: читаются построчно, сравниваются с шаблонами и
# не печатаются. Вызов: список изменённых путей на stdin; MIGRATION=1, если
# PR добавляет раздел «Миграция» в CHANGELOG; REGISTRY — классы по diff
# реестра (classify_registry.sh) через пробел. Печатает строки:
#   classes=<классы через пробел>
#   before=<области cloud-e2e до слияния: дешёвые — ВМ, DNS, S3, секреты>
#   after=<области после слияния из main: остальные, k8s и DBaaS>
#   macos=<yes | no: no — только если все пути — docs/internal/*.md>
#   wintools=<yes | no: yes — PR трогает код путей в internal/tools>
# Порог «до/после» — решение владельца В5 (22.10). macos=no снимает
# test (macos-latest) с PR (CLAUDE.md §4, решение владельца 03.11: у GitHub
# не больше 5 macOS-задач одновременно); пустой список — macos=yes.
# wintools=yes добавляет internal/tools к пакетам Windows на PR
# (go_test_ci.sh): по решению владельца 29.10 PR гоняет на Windows только
# пакеты с кодом Windows, и internal/tools в них нет — тест, разбиравший
# путь как строку со слэшем, прошёл PR и шесть часов держал main красным
# (12.11, #1473 → #1535; план 1.9, «CI: main красный на Windows…», М4).
# Код путей — это слой намерений (internal/tools/intent*.go,
# internal/intent) и любой файл internal/tools, который импортирует
# path/filepath: для этого скрипт читает сам файл по пути из diff — только
# grep по строке импорта, файл не выполняется. Часы (разрешение таймера
# Windows) по пути не узнать — их правило не видит.
set -euo pipefail

declare -A cls=() before=() after=()
macos='' wintools=''
add() { cls[$1]=1; }
area() {
	case $1 in
	e2e | dns | s3 | secrets) before[$1]=1 ;;
	*) after[$1]=1 ;;
	esac
}

while IFS= read -r p; do
	[ -n "$p" ] || continue
	# macOS is off only if every path is internal docs: the first such path
	# sets no, any other path sets yes for good (no never replaces yes).
	case $p in
	docs/internal/*.md) macos=${macos:-no} ;;
	*) macos=yes ;;
	esac
	# Before the tests are set aside: a test takes a path apart too.
	case $p in
	internal/tools/intent*.go | internal/intent/*.go) wintools=yes ;;
	internal/tools/*.go)
		if [ -f "$p" ] && grep -q '"path/filepath"' -- "$p"; then wintools=yes; fi
		;;
	esac
	case $p in
	*_test.go) add tests; continue ;;
	*.md) add docs; continue ;;
	go.mod | go.sum | Dockerfile) add deps; continue ;;
	.github/* | .goreleaser.yaml | install.sh) add ci; continue ;;
	esac
	case $p in
	internal/mcp/http_policy.go | internal/mcp/readonly.go | internal/mcp/http.go | \
		internal/tools/registry.go | internal/tools/project_bound.go | internal/tools/argcheck.go | \
		internal/tools/argrules.go | internal/confirm/* | internal/cli/mcp.go) add policy ;;
	internal/mcp/http_redact.go | internal/tools/creds.go | internal/config/* | internal/auth/* | internal/cli/credentials.go | internal/log/*) add secrets ;;
	internal/httpretry/* | internal/provision/adapter.go | internal/tools/opwait.go) add transport ;;
	internal/output/* | internal/cli/text_views.go | internal/cli/billing_views.go | internal/cli/cards.go) add output ;;
	esac
	case $p in
	internal/tools/tools*.go | internal/tools/cli_spec.go | internal/tools/descriptions_ru.go) add tools ;;
	esac
	case $p in
	internal/engine/* | internal/provision/* | internal/deploy/* | internal/deployplan/* | internal/bootstrap/* | \
		internal/sgrules/* | internal/compose/* | \
		internal/state/* | internal/tools/tools_deploy.go | internal/tools/tools_vm.go) area e2e ;;
	internal/dns/* | internal/tools/tools_dns.go) area dns ;;
	internal/s3/* | internal/tools/tools_s3.go) area s3 ;;
	internal/secret/* | internal/certmanager/* | internal/tools/tools_secret.go | internal/tools/tools_certmanager.go) area secrets ;;
	internal/vpc/* | internal/nlb/* | internal/tools/tools_vpc.go | internal/tools/tools_nlb.go | internal/tools/tools_snat.go | \
		internal/tools/tools_sg*.go | internal/tools/tools_subnet.go) area net ;;
	internal/images/* | internal/tools/tools_disk.go | internal/tools/tools_image*.go) area disk ;;
	internal/containerapp/* | internal/artifact/* | internal/tools/tools_ca.go | internal/tools/tools_ar.go) area ca ;;
	internal/stack/* | internal/tools/tools_stack.go) area stack ;;
	internal/mcp/*) area mcp ;;
	internal/mk8s/* | internal/kubetools/* | internal/tools/tools_k8s*.go | internal/tools/tools_kube.go) area k8s ;;
	internal/postgresql/* | internal/dbaas/* | internal/tools/tools_pg.go | internal/tools/tools_kafka.go | internal/tools/tools_redis.go) area dbaas ;;
	esac
done

[ "${MIGRATION:-}" != 1 ] || add breaking
# Классы по diff реестра (classify_registry.sh), если CI их посчитал.
for c in ${REGISTRY:-}; do add "$c"; done

keys() { printf '%s\n' "$@" | sed '/^$/d' | sort | tr '\n' ' ' | sed 's/ $//'; }
echo "classes=$(keys "${!cls[@]}")"
echo "before=$(keys "${!before[@]}")"
echo "after=$(keys "${!after[@]}")"
# No paths at all: nothing shows the PR is only internal docs.
echo "macos=${macos:-yes}"
echo "wintools=${wintools:-no}"
