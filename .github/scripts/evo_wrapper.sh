#!/usr/bin/env bash
# Обёртка evo для публичных логов cloud-e2e и api-probe (репозиторий
# открыт): stderr evo, который идёт в лог или в канал (`2>&1 | jq`), — через
# redact.sed; stdout — как есть (его читает jq). Одна точка вместо маски у
# каждого вызова: ошибка облака из `k8s create` ушла в лог без маски
# (k8s-smoke, 22.10).
#
# stderr в файл — как есть и синхронно: шаг читает файл сразу после вызова
# (reason ошибки, ID своего кластера в отказе), и асинхронный sed отдал бы
# его недописанным; в лог такой файл выводится через redact.sed (ревью #253).
# exec — код выхода и сигнал (Ctrl-C у `logs -f`) достаются evo; sed маски INT
# не слышит: timeout шлёт его всей группе, и без этого evo после Ctrl-C писал
# бы в закрытый канал (код 141 вместо 130).
# Вызов: evo_wrapper.sh <настоящий evo> <redact.sed> <куда положить обёртку>
set -euo pipefail
real=$1 redact=$2 out=$3
cat > "$out" <<WRAP
#!/usr/bin/env bash
if [ -f /dev/stderr ]; then exec "$real" "\$@"; fi
exec "$real" "\$@" 2> >(trap '' INT; exec sed -u -E -f "$redact" >&2)
WRAP
chmod +x "$out"
