#!/bin/sh
# Самостоятельный запуск: scripts/test-net-cleanup.sh. Использует те же
# заглушки evo и curl, что шаг Linux CI, и возвращает 77 вне Linux.
set -eu
if [ "$(uname -s)" != Linux ]; then
    echo 'Проверка уборки сети на заглушках рассчитана на Linux.' >&2
    exit 77
fi
for tool in bash jq timeout; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        echo "Для проверки уборки сети требуется $tool." >&2
        exit 1
    fi
done
cd "$(dirname "$0")/.."
exec timeout -k 5 120 .github/scripts/cloud_live_net_cleanup_test.sh
