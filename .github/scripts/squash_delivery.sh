#!/usr/bin/env bash
# Сверка доставки squash (CLAUDE.md §0, правило 2; находка Астры #1968).
#
# squash_delivery.sh SQUASH ГОЛОВА [ВЕТКА [MAIN]]
#   SQUASH — коммит PR в main; ГОЛОВА — голова PR, которую слили (поле
#   head.sha слитого PR, а не нынешний конец ветки); ВЕТКА — конец ветки
#   сейчас; MAIN — main сейчас (по умолчанию origin/main).
#
# `git log origin/main..<ветка>` — только список кандидатов: squash меняет
# граф, и исходные коммиты PR не становятся предками main, даже когда их
# правки доставлены (#1888). Доставку решают две проверки:
#
# 1. Голова доставлена целиком, если дерево squash равно ожидаемому —
#    слиянию родителя squash с головой (`git merge-tree --write-tree`).
#    Независимые правки main до squash в ожидаемое дерево входят сами.
#    Слияние с конфликтом — разбор вручную: как его разрешили, скрипт не
#    знает.
# 2. Поздние коммиты — на ветке после головы (ВЕТКА ^ГОЛОВА ^MAIN, без
#    коммитов слияния): их в squash нет (#1460: поздний срез ушёл новым
#    PR #1495). Коммит, чей patch-id уже есть в main после squash, назван
#    доставленным другим PR.
#
# Код выхода: 0 — доставлено, поздних нет; 1 — не доставлено (дерево не то
# или есть поздние коммиты); 3 — нужен разбор вручную (конфликт слияния,
# у squash не один родитель); 2 — неверный вызов.
set -euo pipefail
squash=${1:-}
head=${2:-}
branch=${3:-}
main=${4:-origin/main}
if [ -z "$squash" ] || [ -z "$head" ]; then
	echo "вызов: squash_delivery.sh SQUASH ГОЛОВА [ВЕТКА [MAIN]]" >&2
	exit 2
fi
for c in "$squash" "$head" ${branch:+"$branch"} ${branch:+"$main"}; do
	git rev-parse -q --verify "$c^{commit}" >/dev/null || { echo "нет коммита $c" >&2; exit 2; }
done
parents=$(git rev-list --parents -n1 "$squash" | wc -w)
if [ "$parents" != 2 ]; then
	echo "разбор вручную: у $squash не один родитель — это не squash"
	exit 3
fi
rc=0
if ! want=$(git merge-tree --write-tree "$squash^" "$head" 2>/dev/null); then
	echo "разбор вручную: голова $head сливается с родителем squash с конфликтом"
	rc=3
else
	want=$(printf '%s\n' "$want" | head -n1)
	if [ "$want" = "$(git rev-parse "$squash^{tree}")" ]; then
		echo "голова $(git rev-parse --short "$head") доставлена: дерево squash $(git rev-parse --short "$squash") — ожидаемое"
	else
		echo "голова $(git rev-parse --short "$head") доставлена не так: файлы, где squash и ожидаемое дерево расходятся:"
		git diff --name-only "$want" "$squash^{tree}" | sed 's/^/  /'
		rc=1
	fi
fi
if [ -n "$branch" ]; then
	since=$(git rev-list -n1 "$squash")
	late=$(git rev-list --reverse --no-merges "$branch" "^$head" "^$main")
	for c in $late; do
		id=$(git diff "$c^" "$c" | git patch-id --stable | cut -d' ' -f1)
		found=
		for m in $(git rev-list --no-merges "$since..$main"); do
			if [ "$(git diff "$m^" "$m" | git patch-id --stable | cut -d' ' -f1)" = "$id" ]; then
				found=$m
				break
			fi
		done
		if [ -n "$found" ]; then
			echo "поздний $(git log -1 --format='%h %s' "$c") — доставлен другим коммитом main $(git rev-parse --short "$found")"
		else
			echo "поздний $(git log -1 --format='%h %s' "$c") — не доставлен: новым PR от main"
			[ "$rc" = 0 ] && rc=1
		fi
	done
fi
exit "$rc"
