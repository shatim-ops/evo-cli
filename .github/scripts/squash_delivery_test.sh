#!/usr/bin/env bash
# Проверка squash_delivery.sh на временном репозитории: доставленный squash
# (форма #1888), поздний коммит после слитой головы (форма #1460 → #1495),
# независимая правка main до squash, поздний коммит, доставленный другим
# PR, squash не той головы, конфликт слияния, неверный вызов.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fail=0
repo=$(mktemp -d)
trap 'rm -rf "$repo"' EXIT
cd "$repo"
git init -q -b main .
git config user.email t@example.com
git config user.name t
put() { printf '%s\n' "$2" >"$1"; git add "$1"; git commit -qm "$3"; }
squash() { # ветка, сообщение: squash головы ветки в main
	git merge -q --squash "$1" >/dev/null 2>&1 && git commit -qm "$2"
}
check() { # имя, ожидаемый код, ожидаемая строка, аргументы
	local name=$1 want=$2 line=$3 rc=0 out
	shift 3
	out=$("$here/squash_delivery.sh" "$@" 2>&1) || rc=$?
	if [ "$rc" != "$want" ]; then
		echo "FAIL $name: код $rc, ждали $want"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1
	elif ! grep -qF -- "$line" <<<"$out"; then
		echo "FAIL $name: нет строки «$line»"; printf '%s\n' "$out" | sed 's/^/     /'; fail=1
	else
		echo "ok   $name"
	fi
}
put a.txt a0 base
put b.txt b0 base-b

# Форма #1888: один коммит, слит squash; буквальный git log его показывает.
git checkout -q -b one main
put a.txt a1 "one: a1"
h1=$(git rev-parse HEAD)
git checkout -q main
squash one "one (#1)"
s1=$(git rev-parse HEAD)
check "доставленный squash: голова целиком" 0 "доставлена: дерево squash" "$s1" "$h1"
check "доставленный squash: ветка без поздних" 0 "доставлена" "$s1" "$h1" one main
[ -n "$(git log --oneline main..one)" ] || { echo "FAIL буквальная команда правила не показала коммит — случай не тот"; fail=1; }

# Независимая правка main между веткой и squash.
git checkout -q -b two main
put b.txt b1 "two: b1"
h2=$(git rev-parse HEAD)
git checkout -q main
put c.txt c0 "main: c0"
squash two "two (#2)"
s2=$(git rev-parse HEAD)
check "независимая правка main до squash" 0 "доставлена" "$s2" "$h2" two main

# Форма #1460: слита голова, после слияния в ветку пришёл поздний коммит
# и merge main; поздний в squash не попал.
git checkout -q -b three main
put d.txt d0 "three: d0"
h3=$(git rev-parse HEAD)
git checkout -q main
squash three "three (#3)"
s3=$(git rev-parse HEAD)
put e.txt e0 "main: e0"
git checkout -q three
git merge -q --no-edit main
put d.txt d1 "three: поздний d1"
git checkout -q main
check "поздний коммит не доставлен" 1 "поздний" "$s3" "$h3" three main
check "поздний коммит назван новым PR" 1 "не доставлен: новым PR от main" "$s3" "$h3" three main
check "голова с поздним — сама доставлена" 1 "доставлена: дерево squash" "$s3" "$h3" three main

# Тот же поздний коммит доставлен другим PR (cherry-pick в main).
git cherry-pick "$(git rev-parse three)" >/dev/null
check "поздний, доставленный другим PR" 0 "доставлен другим коммитом main" "$s3" "$h3" three main

# Squash слил не ту голову: в main ушла старая, названа новая.
git checkout -q -b four main
put f.txt f0 "four: f0"
old=$(git rev-parse HEAD)
put f.txt f1 "four: f1"
h4=$(git rev-parse HEAD)
git checkout -q main
git checkout -q -b tmp "$old"
git checkout -q main
squash tmp "four (#4) — старая голова"
s4=$(git rev-parse HEAD)
check "squash не той головы" 1 "доставлена не так" "$s4" "$h4"
check "squash не той головы называет файл" 1 "  f.txt" "$s4" "$h4"

# Конфликт: ветка и main правят одну строку, squash разрешил вручную.
git checkout -q -b five main
put a.txt a5 "five: a5"
h5=$(git rev-parse HEAD)
git checkout -q main
put a.txt a6 "main: a6"
put a.txt a7 "five (#5) — разрешено вручную"
s5=$(git rev-parse HEAD)
check "конфликт — разбор вручную" 3 "разбор вручную" "$s5" "$h5"

# Не squash: коммит слияния.
git checkout -q -b six main
put g.txt g0 "six: g0"
git checkout -q main
git merge -q --no-ff --no-edit six
check "коммит слияния — разбор вручную" 3 "не один родитель" "$(git rev-parse HEAD)" six

check "без аргументов" 2 "вызов:" 
check "нет коммита" 2 "нет коммита" deadbeef "$h1"
exit "$fail"
