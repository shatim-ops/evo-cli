#!/usr/bin/env bash
# Чек-лист PR по выводу classify.sh (stdin): что сделать сверх CI для
# каждого класса и какие области cloud-e2e проверить вживую. Источник —
# матрица проверок по типу изменения (docs/internal/audits/process-1.2.1.md,
# §4). Печатает Markdown для summary прогона; ничего не блокирует.
set -euo pipefail

classes='' before='' after='' n=0
while IFS='=' read -r k v; do
	case $k in
	classes) classes=$v ;;
	before) before=$v ;;
	after) after=$v ;;
	esac
done

item() { printf -- '- [ ] %s\n' "$1"; n=$((n + 1)); }
flags() { # области → флажки cloud-e2e; e2e — основной прогон, без флажка
	local a out=''
	for a in $1; do
		case $a in
		e2e) out+="${out:+, }основной прогон (флажки не нужны)" ;;
		*) out+="${out:+, }\`${a}_smoke\`" ;;
		esac
	done
	printf '%s' "$out"
}

echo "### Класс изменения: ${classes:-код без отдельного класса}"
echo
for c in $classes; do
	case $c in
	policy)
		item "Политика и согласие: новый путь мимо гейта — свой тест; новая декларация — свой тест-инвариант рядом с \`TestHTTPPolicy_ClosedList\`, \`TestReadOnly_*\`, \`TestProjectBound_*\`."
		item "Политика и согласие: бриф «атака» (Grok или Cursor) до слияния или до тега minor — решение владельца." ;;
	secrets)
		item "Секреты: тест, что значение секрета фейка не попадает ни в один вывод — stdout, stderr, лог, state, ответ MCP."
		item "Секреты: внешний взгляд Fable или Grok до тега." ;;
	transport)
		item "Транспорт: тест сбоя на шаге (обрыв, 429/5xx, таймаут) — ресурс учтён, состояние цело." ;;
	output)
		item "Вывод: \`--json\` команды равен ответу инструмента; примеры в docs перегенерированы." ;;
	tools)
		item "Инструменты: изменяющий — превью с ценой или \`tariff_url\`, согласие, \`--read-only\`, \`--allow-project\`; новый — \`CLISpec\`, RU/EN, \`docs/tools.md\` и \`docs/commands.md\` перегенерированы." ;;
	deps)
		item "Зависимости: govulncheck зелёный; экшены и образы закреплены по SHA или digest." ;;
	ci)
		item "CI: \`permissions\` минимальные; текст событий не попадает в \`run\`; код PR не видит секретов." ;;
	docs)
		item "Документация: без внутренних сведений Cloud.ru, реальных ID, IP и ключей." ;;
	breaking)
		item "Ломающее: «Миграция» в CHANGELOG, запись D в DECISIONS, строка в RELEASING — по правилу \`docs/RELEASING.md\`."
		item "Ломающее: полный аудит Fable перед тегом." ;;
	esac
done
if [ -n "$before" ]; then
	item "cloud-e2e до слияния (дешёвые области): Actions → cloud-e2e → Run workflow с ветки PR — $(flags "$before")."
fi
if [ -n "$after" ]; then
	item "cloud-e2e после слияния из \`main\` (дорогие области): $(flags "$after")."
fi
if [ "$n" = 0 ]; then
	echo "Дополнительных проверок сверх CI не нужно."
fi
