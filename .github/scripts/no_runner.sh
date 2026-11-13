#!/usr/bin/env bash
# no_runner.sh (workflow no-runner): job прогона ci, которому GitHub не
# выдал раннер, повторяется один раз — без «релиза» вручную. Признак (12–13.11
# — 7 из 7 случаев, 03.11 — 3 из 3; все — test (macos-latest)): отмена, раннера
# нет, ни одного шага, отмена через 15 мин после создания job. Повтор помогал
# каждый раз (10 из 10). Нагрузка нашей очереди с отменами не связана: 1–10
# прогонов ci за 15 мин до отмены при медиане 4 — гипотеза «сбой выдачи
# раннера у GitHub».
#
# Повтор — только первой попытки и только когда все исходы, кроме успеха и
# пропуска, — такие отмены: настоящий провал рядом повторяет «релиз» по
# правилу 2 CLAUDE.md, а не автомат. Голова PR сдвинулась — не повторяет:
# новый прогон и так идёт. Вторая отмена без раннера — «релизу» вручную.
# Строка ::notice:: «повторены» — счёт «после» для бэклога «Процесса».
#
# Вход (env, от workflow): GH_REPO, RUN_ID, RUN_ATTEMPT, EVENT, HEAD_SHA,
# HEAD_BRANCH; gh — с GH_TOKEN (actions: write).
set -euo pipefail
: "${GH_REPO:?}" "${RUN_ID:?}" "${RUN_ATTEMPT:?}" "${EVENT:?}" "${HEAD_SHA:?}" "${HEAD_BRANCH:?}"

# Waited this long (seconds) before the cancel: GitHub cancels a job given no
# runner 15 min after it was made; a cancel by a push or by hand comes at any
# time, mostly sooner.
STARVED_AFTER=840

if [ "$RUN_ATTEMPT" != 1 ]; then
	echo "попытка $RUN_ATTEMPT: автомат повторяет только первую; дальше — «релиз» вручную (CLAUDE.md §0, правило 2)"
	exit 0
fi
if ! jobs=$(gh api "repos/$GH_REPO/actions/runs/$RUN_ID/attempts/1/jobs?per_page=100" --jq '.jobs' 2>&1); then
	echo "список job прогона не прочитан — повтора нет: $(printf '%s' "$jobs" | tail -c 200)"
	exit 0
fi
starved=$(jq -r --argjson after "$STARVED_AFTER" '[.[] | select(.conclusion == "cancelled" and (.runner_id // 0) == 0 and ((.steps // []) | length) == 0
	and ((.completed_at | fromdateiso8601) - (.created_at | fromdateiso8601)) >= $after) | .name] | join(", ")' <<<"$jobs")
others=$(jq -r --argjson after "$STARVED_AFTER" '[.[] | select(.conclusion != "success" and .conclusion != "skipped" and .conclusion != "neutral")
	| select((.conclusion == "cancelled" and (.runner_id // 0) == 0 and ((.steps // []) | length) == 0
		and ((.completed_at | fromdateiso8601) - (.created_at | fromdateiso8601)) >= $after) | not)
	| "\(.name): \(.conclusion // .status)"] | join(", ")' <<<"$jobs")
if [ -z "$starved" ]; then
	echo "job без раннера нет — повтора нет${others:+ (не успех: $others)}"
	exit 0
fi
if [ -n "$others" ]; then
	echo "без раннера: $starved; рядом не успех: $others — повтора нет, решение о повторе — «релиза» (CLAUDE.md §0, правило 2)"
	exit 0
fi
if [ "$EVENT" = pull_request ]; then
	head=$(gh api "repos/$GH_REPO/git/ref/heads/$HEAD_BRANCH" --jq '.object.sha' 2>/dev/null) || head=
	if [ "$head" != "$HEAD_SHA" ]; then
		echo "без раннера: $starved; голова ветки ${head:+сдвинулась}${head:-не прочитана} — повтора нет: новый пуш идёт своим прогоном"
		exit 0
	fi
fi
gh api -X POST "repos/$GH_REPO/actions/runs/$RUN_ID/rerun-failed-jobs" >/dev/null
echo "::notice::no-runner: повторены job без раннера — $starved (прогон $RUN_ID, $EVENT)"
