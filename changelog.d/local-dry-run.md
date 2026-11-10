### Исправлено

- **`--dry-run` больше не меняет ничего у локальных команд:** `evo logout`, `evo configure delete`, `evo configure`, `evo configure s3`, `evo diag on` и `evo diag off` под `--dry-run` (и с `--yes`) показывают превью (`"status":"dry_run"` с `--json`) и ничего не пишут и не удаляют. Раньше `evo logout --dry-run --yes` отзывал ключ в облаке и удалял профиль, а остальные команды записывали или удаляли профиль либо настройку журнала.
