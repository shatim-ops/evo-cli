# Сценарные скиллы evo

Пять скиллов для ИИ-агентов (Claude Code, Cursor, Codex, Gemini CLI и
других) в формате [Agent Skills](https://agentskills.io/specification): по
одному каталогу с `SKILL.md`. Скилл ведёт агента по сценарию на Cloud.ru
Evolution через evo — по шагам, с остановкой на согласие человека перед
каждым платным или разрушительным действием.

| Скилл | Сценарий |
|---|---|
| `evo-scenarios` | с чего начать: какой скилл и какие группы инструментов (`--toolsets`) нужны, путаница имён, долгие операции, ответы на удаление |
| `evo-deploy` | docker-compose-проект на ВМ: превью, развёртывание, статус, логи, обновление, удаление |
| `evo-vm` | ВМ с публичным адресом, открыть и закрыть порт, floating IP, удалить ВМ |
| `evo-postgres` | Managed PostgreSQL подешевле, пользователь и база, строка подключения без пароля, удаление |
| `evo-cleanup` | найти созданное по префиксу имени, удалить в порядке зависимостей, отчёт о том, что осталось и сколько стоит |

Скиллы написаны под evo 1.3 и по-английски: их читает агент.

## Чем отличаются от встроенного скилла `evo`

Встроенный скилл (`evo agent skill`, `evo agent setup --client <клиент>
--skill`, текст — [docs/agent-skill.md](../docs/agent-skill.md)) — правила
работы с evo: `--json`, согласие, превью, ошибки. Он генерируется из
бинаря и всегда совпадает с ним. Сценарные скиллы его не заменяют и не
повторяют: они ссылаются на его правила и добавляют порядок шагов
сценария, точки остановки, ожидание и уборку. Ставьте оба.

## Установка

Ставьте только с тега официального репозитория: скилл — текст, который
агент исполняет. Подписи набора пока нет. Команды ниже заработают после
выпуска тега `v1.3.0`.

**Копированием с тега** (любой агент):

```sh
git clone --branch v1.3.0 --depth 1 https://github.com/shatim-ops/evo-cli /tmp/evo-cli
cp -r /tmp/evo-cli/skills/evo-* <каталог скиллов агента>/
```

| Агент | В проекте | Для пользователя |
|---|---|---|
| Claude Code | `.claude/skills/` | `~/.claude/skills/` |
| Cursor | `.agents/skills/` (или `.cursor/skills/`) | `~/.cursor/skills/` |
| Codex | `.agents/skills/` | `~/.agents/skills/` |
| Gemini CLI | `.agents/skills/` | `~/.gemini/skills/` |

Другие агенты — по их документации о каталоге скиллов.

**Установщиком `skills`** (Node.js), по одному скиллу с тега:

```sh
npx skills add https://github.com/shatim-ops/evo-cli/tree/v1.3.0/skills/evo-scenarios
npx skills add https://github.com/shatim-ops/evo-cli/tree/v1.3.0/skills/evo-deploy
```

и так же `evo-vm`, `evo-postgres`, `evo-cleanup`. Команда
`npx skills add shatim-ops/evo-cli` без пути ставит набор с `main`, который
может быть новее вашего бинаря.

## Проверка версии

`evo version --json`, поле `version`: его major.minor должен совпадать с
`metadata.evo-version` в `SKILL.md`. Скилл `evo-scenarios` проверяет это
первым шагом и при расхождении предупреждает.

## Удаление

Удалите каталоги `evo-*` из каталога скиллов агента. Встроенный скилл
`evo` от этого не меняется.

## См. также

- [docs/agent-skill.md](../docs/agent-skill.md) — правила встроенного скилла
- [docs/mcp.md](../docs/mcp.md) — MCP-сервер evo, группы инструментов, ограничения
