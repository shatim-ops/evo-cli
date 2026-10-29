#!/usr/bin/env bash
# Шаг 6 docs/RELEASING.md: передача дел — после тега и до слияния следующего
# PR в main. handoff_check.sh БАЗА [КАТАЛОГ]: PR, который не добавляет новый
# раздел выпуска в CHANGELOG.md, проходит, только если у последней выпущенной
# версии есть docs/internal/handoff/<версия>.md (для X.Y.0 подходит и
# X.Y.md). PR с новым разделом выпуска — релизный, передачи у него ещё нет.
# Найдено ретро 1.4.1: после тега v1.4.1 слиты три PR до передачи (#454,
# #457, #456), правило держалось только на памяти сессии.
#
# Окно передачи — не дольше 24 часов после тега v<версия> (решение владельца
# 29.10, #578): прошли сутки — PR проходит с предупреждением, недописанное
# релиз дописывает следующим PR. Время тега — дата создания тега (у
# аннотированного — тегера, у лёгкого — коммита); тега нет — окно не
# началось. HANDOFF_NOW (секунды Unix) подменяет «сейчас» в проверке скрипта.
#
# Код выхода: 0 — можно сливать; 1 — нет передачи; 2 — неверный вызов.
set -euo pipefail
base=${1:?нужна база PR}
dir=${2:-.}
cd "$dir"
latest() { sed -nE 's/^## \[([0-9]+\.[0-9]+\.[0-9]+)\].*/\1/p' | head -n1; }
head_v=$(latest <CHANGELOG.md)
base_v=$(git show "$base:CHANGELOG.md" 2>/dev/null | latest) || base_v=
if [ -z "$head_v" ]; then
	echo "в CHANGELOG.md нет раздела выпуска — проверять нечего"
	exit 0
fi
if [ "$head_v" != "$base_v" ]; then
	echo "PR добавляет раздел выпуска $head_v — релизный PR, передача пишется после тега"
	exit 0
fi
f="docs/internal/handoff/$head_v.md"
short="docs/internal/handoff/${head_v%.0}.md"
if [ -f "$f" ] || { [ "${head_v%.0}" != "$head_v" ] && [ -f "$short" ]; }; then
	echo "передача $head_v есть"
	exit 0
fi
tagged=$(git for-each-ref --format='%(creatordate:unix)' "refs/tags/v$head_v")
now=${HANDOFF_NOW:-$(date +%s)}
if [ -n "$tagged" ] && [ $((now - tagged)) -gt 86400 ]; then
	echo "::warning::нет $f, но с тега v$head_v прошло больше 24 часов — окно передачи закрыто, очередь открыта; релиз дописывает передачу следующим PR (docs/RELEASING.md, шаг 6)"
	exit 0
fi
echo "::error::нет $f: версия $head_v выпущена, а передачи нет — до неё PR в main не сливаются (docs/RELEASING.md, шаг 6)"
exit 1
