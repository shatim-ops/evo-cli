# Этап 10 — Переименование `cloudru-vm` → `evo`

Решение D2. **Предусловие:** владелец письменно подтвердил имя `evo`
(согласование бренда внутри Cloud.ru). Без подтверждения этап не начинать;
запасной вариант — `evo-infra`, бриф тот же с заменой имени.

Делать после этапа 7 (релизный пайплайн уже существует и меняется одной строкой)
и до первого публичного тега `v1.0.0`.

## Задачи

1. **Путь модуля:** `github.com/shatim-ops/cloudru-vm-cli` → новое имя
   репозитория/модуля (владелец переименовывает репозиторий на GitHub;
   `go.mod` + все импорты через `gofmt -r` или `sed`, затем `go build ./...`).
2. **Бинарь:** `cmd/cloudru-vm` → `cmd/evo`; `Use: "evo"`; goreleaser `binary: evo`.
3. **Alias на переходный релиз:** второй артефакт `cloudru-vm`, который при
   запуске печатает в stderr одну строку «cloudru-vm переименован в evo, alias
   будет удалён в v1.1» и делегирует в тот же `main`. Реализация — один и тот
   же бинарь, поведение по `filepath.Base(os.Args[0])`; в goreleaser — два
   `builds` с разными `binary`, или symlink в архиве.
4. **Конфиг и каталоги:** `os.UserConfigDir()/evo/`; при старте, если есть
   `…/cloudru-vm/` и нет `…/evo/` — скопировать один раз с сообщением.
   Проектный файл `.cloudru-vm.yaml` → `.evo.yaml` с чтением старого имени как
   fallback (предупреждение). Каталог `./.cloudru-vm/` (SSH-ключ, state) →
   `./.evo/` с тем же fallback. Env-переменные `CLOUDRU_*` **не** переименовывать
   (это имя провайдера, не инструмента).
5. **MCP:** `serverInfo.name = "evo"`; инструкция `claude mcp add … evo -- evo mcp`.
6. **Строки:** `grep -rn 'cloudru-vm' --include=*.go --include=*.md --include=*.yaml`
   → остаются только: alias, fallback-пути, раздел «Переименование» в README.

## Не делать

- Не переименовывать env-переменные, имена MCP-инструментов и группы.
- Не ломать чтение старых конфигов и state в течение одного минорного релиза.

## Критерии приёмки

```
go build ./... && go test -race ./...
./dist/…/evo version                      # evo <ver>
./dist/…/cloudru-vm version 2>&1 | head -1 # предупреждение об alias, затем работает
cd old-project && evo status              # читает ./.cloudru-vm/state.json с предупреждением
grep -rn 'cloudru-vm' --include=*.go . | grep -v -e alias -e legacy   # пусто
```
