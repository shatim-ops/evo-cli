#!/usr/bin/env bash
# Обёртка evo для публичных логов cloud-e2e и api-probe (репозиторий
# открыт): stderr evo — через redact.sed, stdout — как есть (его читает jq).
# Одна точка вместо маски у каждого вызова: ошибка облака из `k8s create`
# ушла в лог без маски (k8s-smoke, 22.10). exec — код выхода и сигнал
# (Ctrl-C у `logs -f`) достаются evo; sed маски INT не слышит: timeout шлёт
# его всей группе, и без этого evo после Ctrl-C писал бы в закрытый канал
# (код 141 вместо 130). E2E_RAW_STDERR=1 — stderr как есть;
# только когда он уходит в файл, который шаг разбирает по ID
# (TestWorkflows_CloudE2EStderrRedacted).
# Вызов: evo_wrapper.sh <настоящий evo> <redact.sed> <куда положить обёртку>
set -euo pipefail
real=$1 redact=$2 out=$3
cat > "$out" <<WRAP
#!/usr/bin/env bash
if [ "\${E2E_RAW_STDERR:-}" = 1 ]; then exec "$real" "\$@"; fi
exec "$real" "\$@" 2> >(trap '' INT; exec sed -u -E -f "$redact" >&2)
WRAP
chmod +x "$out"
