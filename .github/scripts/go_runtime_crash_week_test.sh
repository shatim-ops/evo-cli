#!/usr/bin/env bash
# Проверка go_runtime_crash_week.sh (job ci) на поддельном gh: шаг повтора
# после сбоя рантайма Go, который выполнялся (success или failure) в любой
# попытке job ci за 7 дней, — случай; пропущенный — нет; случаев не меньше
# порога — код 1 (#370, вариант 1).
set -uo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0
step='go test: повтор после сбоя рантайма Go'

# Поддельный gh api: список прогонов ci.yml — runs.json, задания прогона N —
# jobs.N.json; каждый адрес — строка в calls.
mkdir -p "$work/bin"
cat >"$work/bin/gh" <<'FAKE'
#!/usr/bin/env bash
d=$FAKE_DIR
url=${*: -1}
echo "$url" >>"$d/calls"
case "$url" in
*/actions/workflows/ci.yml/runs\?*) cat "$d/runs.json" ;;
*/actions/runs/*/jobs\?*)
	id=${url#*/actions/runs/}
	cat "$d/jobs.${id%%/*}.json"
	;;
*) exit 1 ;;
esac
FAKE
chmod +x "$work/bin/gh"

job() { # ИМЯ ИТОГ_ШАГА_ПОВТОРА
	printf '{"name":"%s","html_url":"https://example.invalid/job","steps":[{"name":"go test","conclusion":"success"},{"name":"%s","conclusion":"%s"}]}' "$1" "$step" "$2"
}
case_() { # ИМЯ ПОРОГ КОД; jobs.N.json уже в $FAKE_DIR
	local rc
	PATH="$work/bin:$PATH" GITHUB_REPOSITORY=o/r GITHUB_STEP_SUMMARY="$FAKE_DIR/summary" \
		"$here/go_runtime_crash_week.sh" "$2" >"$FAKE_DIR/out" 2>&1
	rc=$?
	if [ "$rc" != "$3" ]; then
		printf 'FAIL %s: код %s (ждём %s)\n' "$1" "$rc" "$3"
		sed 's/^/  | /' "$FAKE_DIR/out"
		fail=1
	fi
	if ! grep -q 'created=%3E%3D[0-9-]*T' "$FAKE_DIR/calls" || ! grep -q 'jobs?filter=all' "$FAKE_DIR/calls"; then
		printf 'FAIL %s: не те адреса API:\n' "$1"
		sed 's/^/  | /' "$FAKE_DIR/calls"
		fail=1
	fi
}
fresh() {
	export FAKE_DIR="$work/$1"
	mkdir -p "$FAKE_DIR"
	echo '{"workflow_runs":[{"id":1},{"id":2},{"id":3}]}' >"$FAKE_DIR/runs.json"
}

# Два случая (success и failure), пропуски не в счёт: ниже порога 3.
fresh below
echo "{\"jobs\":[$(job 'test (windows-latest)' success),$(job 'test (ubuntu-latest)' skipped)]}" >"$FAKE_DIR/jobs.1.json"
echo "{\"jobs\":[$(job 'test (windows-latest)' failure)]}" >"$FAKE_DIR/jobs.2.json"
echo "{\"jobs\":[$(job 'test (windows-latest)' skipped)]}" >"$FAKE_DIR/jobs.3.json"
case_ below 3 0

# Три случая — порог: красный.
fresh limit
echo "{\"jobs\":[$(job 'test (windows-latest)' success),$(job 'test (macos-latest)' success)]}" >"$FAKE_DIR/jobs.1.json"
echo "{\"jobs\":[$(job 'test (windows-latest)' success)]}" >"$FAKE_DIR/jobs.2.json"
echo '{"jobs":[]}' >"$FAKE_DIR/jobs.3.json"
case_ limit 3 1
grep -q 'случаев 3' "$FAKE_DIR/summary" || {
	echo "FAIL limit: в сводке нет «случаев 3»"
	fail=1
}

# Список прогонов не прочитан — красный, а не «0 случаев».
fresh no-api
rm "$FAKE_DIR/runs.json"
echo '{"jobs":[]}' >"$FAKE_DIR/jobs.1.json"
PATH="$work/bin:$PATH" GITHUB_REPOSITORY=o/r GITHUB_STEP_SUMMARY="$FAKE_DIR/summary" \
	"$here/go_runtime_crash_week.sh" 3 >"$FAKE_DIR/out" 2>&1 && {
	echo "FAIL no-api: код 0 без списка прогонов"
	fail=1
}

[ "$fail" = 0 ] && echo "go_runtime_crash_week: ok"
exit "$fail"
