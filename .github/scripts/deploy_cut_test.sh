#!/usr/bin/env bash
# Проверка deploy_cut.sh (cloud-e2e, job e2e) без облака: evo — общая
# заглушка fake_evo.sh со сценарием deploy, ssh — заглушка в PATH. evo
# deploy пишет state и «держит этап»; ssh отвечает состоянием этапа из
# файла. Случаи: обрыв посреди этапа и повтор — ok;
# этап lost после обрыва, код обрыва не 130, повтор с «прервался на ВМ» —
# ошибка; deploy кончился раньше, чем этап пойман, — предупреждение, ok.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fail=0
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"

# shellcheck source-path=SCRIPTDIR source=fake_evo.sh
. "$here/fake_evo.sh"
fake_evo_install "$work/bin"
# evo deploy — сценарий общей заглушки (fake_evo_run) на Python: Go evo
# ставит обработчик SIGINT сам (signal.NotifyContext), даже если фоновый
# запуск bash его игнорирует; у bash-сценария trap на такой сигнал не встал
# бы. Ставится в FAKE_DIR каждого случая (run).
deploy_py=$(cat <<'PY'
#!/usr/bin/env python3
import json, os, signal, sys, time
f = os.environ["FAKE_DIR"]
n = int(open(f + "/calls").read()) + 1 if os.path.exists(f + "/calls") else 1
open(f + "/calls", "w").write(str(n))
mode = open(f + "/mode").read().strip()
def on_int(*_):
    open(f + "/cut", "w").write("1")
    if mode == "lost":
        open(f + "/stage", "w").write("lost")
    sys.exit(int(os.environ.get("INT_CODE", "130")))
signal.signal(signal.SIGINT, on_int)
os.makedirs(".evo", exist_ok=True)
json.dump({"deployments": [{"vm_ip": "203.0.113.7", "vm_id": "00000000-0000-4000-8000-0000000000aa", "ssh_user": "ubuntu"}]}, open(".evo/state.json", "w"))
open(".evo/id_ed25519", "w").write("key")
if n == 1 and mode != "fast":
    open(f + "/stage", "w").write("running")
    time.sleep(60)
    sys.exit(0)
open(f + "/stage", "w").write("done")
print("evo: этап docker прервался на ВМ" if mode == "relost" and n > 1 else "evo: развёрнуто")
PY
)
# ssh: последний аргумент — команда на ВМ.
cat >"$work/bin/ssh" <<'SH'
#!/usr/bin/env bash
cmd=${*: -1}
st=$(cat "$FAKE_DIR/stage" 2>/dev/null || echo none)
if grep -q ssh-down "$FAKE_DIR/mode" && [ -f "$FAKE_DIR/calls" ] && [ "$st" = running ] && [ -f "$FAKE_DIR/cut" ]; then
	echo "ssh: connect to host 203.0.113.7 port 22: Connection timed out" >&2
	exit 255
fi
case "$cmd" in
*"/docker/state"*) echo "$st" ;;
*"/compose-up/state"*) echo none ;;
*"evo-stage.sh status docker"*) printf 'state=%s\nexit=0\n' "$st" ;;
*) echo "setsid=есть flock=есть base64=есть nohup=есть" ;;
esac
SH
chmod +x "$work/bin/ssh"

# run ИМЯ РЕЖИМ ОЖИДАЕМЫЙ_КОД [INT_CODE]
run() {
	local d="$work/$1" rc
	mkdir -p "$d/app" "$d/fake"
	printf '%s\n' "$2" >"$d/fake/mode"
	printf '%s\n' "$deploy_py" | FAKE_DIR="$d/fake" fake_evo_run deploy
	set +e
	(cd "$d/app" && PATH="$work/bin:$PATH" FAKE_DIR="$d/fake" INT_CODE="${4:-130}" CUT_WAIT=30 RUNNER_TEMP="$d" \
		"$here/deploy_cut.sh") >"$d/out" 2>&1
	rc=$?
	set -e
	if [ "$rc" != "$3" ]; then
		printf 'FAIL %s: код %s, ожидался %s\n' "$1" "$rc" "$3"
		sed 's/^/  /' "$d/out"
		fail=1
	fi
}
run cut-and-repeat running 0
run lost-after-cut lost 1
run int-not-130 running 1 1
run repeat-lost relost 1
run deploy-before-caught fast 0
run ssh-down-after-cut ssh-down 1
grep -q 'ssh: connect to host <ip> port 22: Connection timed out' "$work/ssh-down-after-cut/out" ||
	{ echo "FAIL ssh-down-after-cut: нет причины ssh (через маску)"; sed 's/^/  /' "$work/ssh-down-after-cut/out"; fail=1; }
grep -q '::warning::' "$work/deploy-before-caught/out" ||
	{ echo "FAIL deploy-before-caught: нет предупреждения"; fail=1; }
! grep -q '203.0.113.7' "$work/cut-and-repeat/out" ||
	grep -q '::add-mask::203.0.113.7' "$work/cut-and-repeat/out" ||
	{ echo "FAIL cut-and-repeat: IP без маски"; fail=1; }
[ "$fail" = 0 ] && echo "deploy_cut: ok"
exit "$fail"
