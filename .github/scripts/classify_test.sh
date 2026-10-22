#!/usr/bin/env bash
# Проверка classify.sh и classify_summary.sh на образцах путей: класс,
# области cloud-e2e и чек-лист.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fail=0
check() { # имя, ожидаемый вывод, пути через пробел [, MIGRATION]
	local got
	got=$(tr ' ' '\n' <<<"$3" | MIGRATION=${4:-} "$here/classify.sh")
	if [ "$got" = "$2" ]; then echo "ok   $1"; else
		echo "FAIL $1"; echo "  ждали: $2" | tr '\n' '|'; echo; echo "  вышло: $got" | tr '\n' '|'; echo
		fail=1
	fi
}
nl=$'\n'
check "только документация" "classes=docs${nl}before=${nl}after=" "README.md docs/mcp.md"
check "политика и mcp" "classes=policy${nl}before=${nl}after=mcp" "internal/mcp/http_policy.go"
check "s3 — до слияния" "classes=tools${nl}before=s3${nl}after=" "internal/tools/tools_s3.go"
check "dns и секреты — до слияния" "classes=tools${nl}before=dns secrets${nl}after=" "internal/tools/tools_dns.go internal/secret/client.go internal/tools/tools_certmanager.go"
check "сеть, диски, ca, стек — после" "classes=tools${nl}before=${nl}after=ca disk net stack" "internal/nlb/nlb.go internal/tools/tools_disk.go internal/containerapp/app.go internal/stack/apply.go internal/tools/tools_snat.go"
check "k8s и pg — после" "classes=tools${nl}before=${nl}after=dbaas k8s" "internal/tools/tools_k8s.go internal/tools/tools_pg.go"
check "движок — e2e до слияния" "classes=${nl}before=e2e${nl}after=" "internal/engine/deploy.go internal/sgrules/sgrules.go"
check "ключи — секреты" "classes=secrets${nl}before=${nl}after=" "internal/tools/creds.go"
check "адаптер: транспорт и e2e" "classes=transport${nl}before=e2e${nl}after=" "internal/provision/adapter.go"
check "тесты не зовут облако" "classes=tests${nl}before=${nl}after=" "internal/engine/deploy_test.go internal/tools/tools_s3_test.go"
check "секреты и mcp" "classes=secrets${nl}before=${nl}after=mcp" "internal/mcp/http_redact.go"
check "зависимости и CI" "classes=ci deps${nl}before=${nl}after=" "go.mod go.sum .github/workflows/ci.yml"
check "ломающее" "classes=breaking docs${nl}before=${nl}after=" "CHANGELOG.md" 1
check "пусто" "classes=${nl}before=${nl}after=" ""
# Чек-лист: области превращаются во флажки cloud-e2e, тесты — без пунктов.
has() { # имя, пути через пробел, ожидаемые строки чек-листа
	local name=$1 paths=$2 summary want
	shift 2
	summary=$(tr ' ' '\n' <<<"$paths" | "$here/classify.sh" | "$here/classify_summary.sh")
	for want in "$@"; do
		if ! grep -qF "$want" <<<"$summary"; then
			echo "FAIL $name: нет «$want»"; echo "$summary"; fail=1; return
		fi
	done
	echo "ok   $name"
}
has "чек-лист: флажки cloud-e2e" "internal/tools/tools_dns.go internal/engine/deploy.go internal/tools/tools_pg.go" \
	"с ветки PR — \`dns_smoke\`, основной прогон (флажки не нужны)." \
	"из \`main\` (дорогие области): \`dbaas_smoke\`."
has "чек-лист: только тесты" "internal/engine/deploy_test.go" "Дополнительных проверок сверх CI не нужно."
# Каждая область, кроме основного прогона e2e, — флажок <область>_smoke
# cloud-e2e: чек-лист не назовёт флажок, которого нет.
while read -r a; do
	[ "$a" = e2e ] && continue
	if ! grep -q "^      ${a}_smoke:" "$here/../workflows/cloud-e2e.yml"; then
		echo "FAIL область $a: нет входа ${a}_smoke в cloud-e2e.yml"; fail=1
	fi
done < <(grep -o 'area [a-z0-9]*' "$here/classify.sh" | cut -d' ' -f2 | sort -u)
[ "$fail" = 0 ] && echo "classify: все случаи верны"
exit "$fail"
