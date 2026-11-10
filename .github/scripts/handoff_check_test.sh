#!/usr/bin/env bash
# Проверка handoff_check.sh на временном репозитории: релизный PR, PR после
# тега без передачи и с ней, окно передачи в 24 часа, X.Y.0 с передачей X.Y.md.
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
# Коммит выпуска — за трое суток до тега: окно считается от тега (tag.yml
# ставит аннотированный, git tag -a), а не от коммита.
git add -A && GIT_COMMITTER_DATE="$(date -d '-3 days' -R)" git commit -qm release && base=$(git rev-parse HEAD)
check "PR после тега без передачи" 1
git tag -a v1.4.1 -m v1.4.1
tagged=$(date +%s)
HANDOFF_NOW=$((tagged + 23 * 3600)) check "без передачи, 23 часа после тега" 1
HANDOFF_NOW=$((tagged + 25 * 3600)) check "без передачи, 25 часов после тега: окно закрыто" 0
touch docs/internal/handoff/1.4.1.md
check "PR с передачей" 0
log $'## [1.5.0] — 2026-11-01\n\n## [1.4.1] — 2026-10-28\n'
git add -A && git commit -qm 1.5.0 && base=$(git rev-parse HEAD)
touch docs/internal/handoff/1.5.md
check "X.Y.0 — передача X.Y.md" 0
# От 1.8 передача называет фазу 0 следующей версии: раздел «Следующая
# версия», строка «Фаза 0» и ссылка на бриф briefs/phase-0.md (строка плана
# 1.8 «Страховка фазы 0»). Передачи до 1.8 — как были.
log $'## [1.8.0] — 2026-11-12\n\n## [1.5.0] — 2026-11-01\n'
git add -A && git commit -qm 1.8.0 && base=$(git rev-parse HEAD)
printf '# 1.8\n\n## Следующая версия\n\n1.9 — сравнение с CLI.\n' >docs/internal/handoff/1.8.md
check "1.8: передача без фазы 0" 1
printf '# 1.8\n\n## Фаза 0\n\nсм. briefs/phase-0.md\n\n## Следующая версия\n\n1.9.\n' >docs/internal/handoff/1.8.md
check "1.8: фаза 0 не в разделе «Следующая версия»" 1
printf '# 1.8\n\n## Следующая версия\n\n- Фаза 0 — до работы над 1.9.\n' >docs/internal/handoff/1.8.md
check "1.8: фаза 0 без ссылки на бриф" 1
printf '# 1.8\n\n## Следующая версия\n\n- Фаза 0 — до работы над 1.9.\n- Бриф: briefs/phase-0.md\n' >docs/internal/handoff/1.8.md
check "1.8: ссылка на бриф не в строке фазы 0" 1
printf '# 1.8\n\n## Риски\n\nФаза 0: briefs/phase-0.md\n' >docs/internal/handoff/1.8.md
check "1.8: раздела «Следующая версия» нет" 1
printf '# 1.8\n\n## Следующая версия\n\n- Фаза 0 — до работы над 1.9: docs/internal/briefs/phase-0.md.\n\n## Риски\n' >docs/internal/handoff/1.8.md
check "1.8: фаза 0 со ссылкой на бриф" 0
printf '# 1.5\n\n## Следующая версия\n\n1.6.\n' >docs/internal/handoff/1.5.md
log $'## [1.5.0] — 2026-11-01\n'
git add -A && git commit -qm back-1.5 && base=$(git rev-parse HEAD)
check "1.5: передача до 1.8 без фазы 0" 0
log ''
check "раздела выпуска нет" 0
exit "$fail"
