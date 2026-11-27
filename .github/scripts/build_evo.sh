#!/usr/bin/env bash
# Сборка evo в job cloud-e2e и api-probe: бинарь — evo-real, в PATH —
# обёртка evo, у которой stderr идёт через redact.sed (evo_wrapper.sh).
set -euo pipefail
bin="$RUNNER_TEMP/evo-bin"
mkdir -p "$bin"
if [[ -n "${E2E_PRODUCT_SHA:-}" ]]; then
	# Собираем продукт из проверенного коммита, скрипты прогона остаются в workspace.
	src="$RUNNER_TEMP/evo-product"
	git worktree add --quiet --detach "$src" "$E2E_PRODUCT_SHA"
	cd "$src"
	printf 'Сборка кандидата %s\n' "$(git rev-parse HEAD)"
fi
go build -o "$bin/evo-real" ./cmd/evo
"$GITHUB_WORKSPACE/.github/scripts/evo_wrapper.sh" "$bin/evo-real" "$GITHUB_WORKSPACE/.github/scripts/redact.sed" "$bin/evo"
echo "$bin" >> "$GITHUB_PATH"
