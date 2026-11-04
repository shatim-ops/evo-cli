#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
# SPDX-FileContributor: Тимур Шакирбеков
# SPDX-License-Identifier: Apache-2.0
#
# ai-factory-probe: сырой отчёт — в закрытый evo-internal, в ветку
# probe/ai-factory-<run>, и никуда больше (условие владельца 04.11): main
# evo-internal не трогается, PR открывает «Процесс», сливает «релиз».
#
#   aif_push.sh КАТАЛОГ RUN
#
# Токен — EVO_INTERNAL_PUSH_TOKEN, в git идёт заголовком через окружение
# (GIT_CONFIG_*), а не в URL и не в argv: ни в .git/config, ни в выводе
# git, ни в списке процессов его нет. AIF_INTERNAL_REMOTE — адрес
# репозитория (проверка подставляет локальный).
set -euo pipefail
dir=${1:?каталог отчёта}
run=${2:?номер прогона}
[[ "$run" =~ ^[0-9]{1,20}$ ]] || { echo "::error::номер прогона — число: $run"; exit 1; }
branch="probe/ai-factory-$run"
remote=${AIF_INTERNAL_REMOTE:-https://github.com/shatim-ops/evo-internal.git}
token=${EVO_INTERNAL_PUSH_TOKEN:?нет EVO_INTERNAL_PUSH_TOKEN}
echo "::add-mask::$token"
basic=$(printf 'x-access-token:%s' "$token" | base64 -w0)
echo "::add-mask::$basic"
# push.negotiate — выключен: глобальный true раннера или машины ломает push
# в новый репозиторий без истории («expected acknowledgments»).
export GIT_CONFIG_COUNT=2 GIT_CONFIG_KEY_0=http.extraHeader GIT_CONFIG_VALUE_0="Authorization: Basic $basic" \
	GIT_CONFIG_KEY_1=push.negotiate GIT_CONFIG_VALUE_1=false
export GIT_TERMINAL_PROMPT=0
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
git init -q "$work/repo"
cd "$work/repo"
git config user.name "ai-factory-probe"
git config user.email "ai-factory-probe@users.noreply.github.com"
git remote add origin "$remote"
# Ветка — от main evo-internal, но main только читается: push идёт одним
# refspec на refs/heads/$branch, и ветка с таким именем должна быть новой.
git fetch -q origin main
if git ls-remote --exit-code --heads origin "$branch" >/dev/null 2>&1; then
	echo "::error::ветка $branch уже есть в evo-internal — прогон не перезаписывает чужой отчёт"
	exit 1
fi
git checkout -q -b "$branch" FETCH_HEAD
dest="runs/ai-factory-probe-$run"
mkdir -p "$dest"
cp -R "$dir"/. "$dest"/
git add "$dest"
git commit -q -m "ai-factory-probe: сырой отчёт прогона $run (фаза discover)"
git push -q origin "HEAD:refs/heads/$branch"
echo "отчёт — evo-internal, ветка $branch: файлов $(find "$dest" -type f | wc -l)"
