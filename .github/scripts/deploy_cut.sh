#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
# SPDX-FileContributor: Тимур Шакирбеков
# SPDX-License-Identifier: Apache-2.0
#
# cloud-e2e, job e2e: evo deploy, оборванный посреди этапа на ВМ, и его
# повтор (D69, вариант C; строка плана 1.7 «cloud-e2e deploy на rc.1
# обязателен»). Путь CLI тоже идёт через этапы на ВМ (internal/vmstage):
# этап отсоединён от сессии SSH (setsid nohup, flock), обрыв evo его не
# убивает, повтор deploy его подхватывает, а не запускает снова.
#
#   deploy_cut.sh   — из каталога проекта (e2e-app) после evo init
#
# Шаги:
#   1. evo deploy --yes в фоне; ждём, пока на ВМ этап docker или compose-up
#      станет running (ключ развёртывания .evo/id_ed25519, пользователь из
#      state, до CUT_WAIT секунд);
#   2. SIGINT evo — код 130, как у logs -f;
#   3. этап на ВМ жив: running или done, а не lost (lost — процесс этапа
#      умер с сессией: KillUserProcesses, нет setsid); setsid, flock,
#      base64 — есть в образе;
#   4. evo deploy --yes ещё раз — код 0, без «этап прервался на ВМ».
# Этап кончился раньше, чем пойман, — предупреждение, шаги 2–3 пропущены,
# шаг 4 обязателен. Лог публичный: вывод evo и ВМ — через redact.sed, IP и
# id ВМ маскируются.
set -uo pipefail
here=$(cd "$(dirname "$0")" && pwd)
redact() { sed -E -f "$here/redact.sed"; }
CUT_WAIT=${CUT_WAIT:-1500}
fail=0
err() { echo "::error::deploy с обрывом: $*"; fail=1; }

evo deploy --yes >deploy1.txt 2>&1 &
pid=$!
kh="${RUNNER_TEMP:-/tmp}/deploy-cut-kh"
ip= user= caught= start=$SECONDS
sshlog="${RUNNER_TEMP:-/tmp}/deploy-cut-ssh.err"
# vm_ssh КОМАНДА — команда на ВМ ключом развёртывания; stdout, код ssh;
# stderr последнего вызова — в $sshlog (ssh_why). Ключ хоста — при первом
# входе, как у самого evo: ВМ создана этим прогоном, файл known_hosts —
# свой на прогон.
vm_ssh() {
	ssh -i .evo/id_ed25519 -o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new \
		-o UserKnownHostsFile="$kh" "$user@$ip" "$1" 2>"$sshlog"
}
# ssh_why — последняя строка stderr ssh через маску: почему ВМ не ответила.
ssh_why() { tail -1 "$sshlog" 2>/dev/null | redact | cut -c1-200; }
while kill -0 "$pid" 2>/dev/null && [ $((SECONDS - start)) -lt "$CUT_WAIT" ]; do
	if [ -z "$ip" ] && [ -s .evo/state.json ]; then
		ip=$(jq -r '.deployments[0].vm_ip // empty' .evo/state.json 2>/dev/null)
		user=$(jq -r '.deployments[0].ssh_user // "ubuntu"' .evo/state.json 2>/dev/null)
		vid=$(jq -r '.deployments[0].vm_id // empty' .evo/state.json 2>/dev/null)
		[ -z "$ip" ] || echo "::add-mask::$ip"
		[ -z "$vid" ] || echo "::add-mask::$vid"
	fi
	if [ -n "$ip" ] && [ -f .evo/id_ed25519 ]; then
		for s in docker compose-up; do
			if [ "$(vm_ssh "cat ~/.evo/stage/$s/state 2>/dev/null")" = running ]; then
				caught=$s
				break 2
			fi
		done
	fi
	sleep 3
done

if [ -n "$caught" ]; then
	echo "этап $caught — running на ВМ через $((SECONDS - start)) с: обрыв evo deploy"
	kill -INT "$pid"
	wait "$pid"
	rc=$?
	redact <deploy1.txt | tail -5
	[ "$rc" = 130 ] && echo "обрыв: код 130" || err "обрыв (SIGINT): код $rc, ожидался 130"
	sleep 5
	# status скрипта этапа: running с держателем flock — процесс жив;
	# running без держателя — lost.
	st=$(vm_ssh "sh ~/.evo/stage/evo-stage.sh status $caught" | sed -n 's/^state=//p' | head -1)
	echo "этап $caught после обрыва: ${st:-не прочитан}"
	case "$st" in
	running | done) ;;
	*) err "этап $caught после обрыва — ${st:-не прочитан${sshlog:+ (ssh: $(ssh_why))}}, ожидался running или done" ;;
	esac
	tools=$(vm_ssh 'for c in setsid flock base64 nohup; do command -v "$c" >/dev/null && printf "%s=есть " "$c" || printf "%s=нет " "$c"; done')
	echo "образ: $tools"
	kup=$(vm_ssh 'loginctl show-user "$(id -un)" -p KillUserProcesses 2>/dev/null || grep -h "^KillUserProcesses" /etc/systemd/logind.conf /etc/systemd/logind.conf.d/*.conf 2>/dev/null')
	echo "logind: ${kup:-KillUserProcesses не задан (по умолчанию no)}"
else
	if kill -0 "$pid" 2>/dev/null; then
		kill -INT "$pid"
		wait "$pid"
		redact <deploy1.txt | tail -20
		err "этап на ВМ не пойман за $CUT_WAIT с, evo deploy оборван; ssh: $(ssh_why)"
	else
		wait "$pid"
		rc=$?
		redact <deploy1.txt | tail -5
		echo "::warning::deploy с обрывом: deploy кончился (код $rc) раньше, чем этап пойман в running — обрыв не проверен, повтор — ниже"
	fi
fi

# Повтор: подхватывает этап (или видит его конец), а не запускает заново.
evo deploy --yes >deploy2.txt 2>&1
rc=$?
redact <deploy2.txt | tail -15
[ "$rc" = 0 ] || err "повтор deploy: код $rc"
! grep -Eq 'прервался на ВМ|was interrupted on the VM' deploy2.txt ||
	err "повтор deploy: этап прервался на ВМ (lost) — процесс этапа не пережил обрыв"
exit "$fail"
