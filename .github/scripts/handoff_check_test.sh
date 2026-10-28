#!/usr/bin/env bash
# Проверка handoff_check.sh на временном репозитории: релизный PR, PR после
# тега без передачи и с ней, X.Y.0 с передачей X.Y.md.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fail=0
repo=$(mktemp -d)
trap 'rm -rf "$repo"' EXIT
cd "$repo"
git init -q -b main .
git config user.email t@example.com
git config user.name t
mkdir -p docs/internal/handoff
log() { printf '# Changelog\n\n## [Не выпущено]\n\n%s' "$1" >CHANGELOG.md; }
log $'## [1.4.0] — 2026-10-27\n'
touch docs/internal/handoff/1.4.0.md
git add -A && git commit -qm base
base=$(git rev-parse HEAD)
check() { # имя, ожидаемый код
	local rc=0
	"$here/handoff_check.sh" "$base" "$repo" >/dev/null || rc=$?
	if [ "$rc" = "$2" ]; then echo "ok   $1"; else echo "FAIL $1: код $rc, ждали $2"; fail=1; fi
}
check "передача последней версии есть" 0
log $'## [1.4.1] — 2026-10-28\n\n## [1.4.0] — 2026-10-27\n'
check "релизный PR: новый раздел выпуска" 0
git add -A && git commit -qm release && base=$(git rev-parse HEAD)
check "PR после тега без передачи" 1
touch docs/internal/handoff/1.4.1.md
check "PR с передачей" 0
log $'## [1.5.0] — 2026-11-01\n\n## [1.4.1] — 2026-10-28\n'
git add -A && git commit -qm 1.5.0 && base=$(git rev-parse HEAD)
touch docs/internal/handoff/1.5.md
check "X.Y.0 — передача X.Y.md" 0
log ''
check "раздела выпуска нет" 0
exit "$fail"
