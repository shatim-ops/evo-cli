#!/usr/bin/env bash
# Прогоны cloud-e2e, которые запустил cloud-e2e-weekly (github-actions[bot]) и
# которые до сих пор ждут одобрения окружения (status waiting), отменяются
# перед новым недельным запуском: неодобренный набор или уборка не держит
# группу concurrency «cloud-e2e» дольше недели. Ожидание одобрения у GitHub —
# до 30 дней, своего тайм-аута у job нет. Прогоны 177 (07.11) и 180 (09.11),
# прогон 41194687814 (31.10) блокировали живые прогоны cloud-e2e (бэклог
# «Процесса» 1.8). Прогоны, запущенные человеком, не трогаются.
#
#   weekly_stale.sh REPO  — отменить; печатает «отменён <id> (<дата>)».
set -euo pipefail
repo=${1:?репозиторий}
# Сбой чтения или отмены не срывает недельный запуск: новый прогон встанет
# в очередь concurrency, как и без этого шага (ревью Qwen #1111).
ids=$(gh api "repos/$repo/actions/workflows/cloud-e2e.yml/runs?status=waiting&per_page=100" \
	--jq '.workflow_runs[] | select(.triggering_actor.login == "github-actions[bot]") | "\(.id) \(.created_at)"') || {
	echo "::warning::список ждущих прогонов cloud-e2e не прочитан — отмена пропущена"
	exit 0
}
[ -n "$ids" ] || { echo "ждущих прогонов cloud-e2e от github-actions[bot] нет"; exit 0; }
while read -r id at; do
	[ -n "$id" ] || continue
	if gh api -X POST "repos/$repo/actions/runs/$id/cancel" >/dev/null; then
		echo "отменён $id ($at): ждал одобрения окружения с прошлого недельного запуска"
	else
		echo "::warning::прогон $id ($at) не отменён — он держит группу cloud-e2e, отмените вручную"
	fi
done <<<"$ids"
