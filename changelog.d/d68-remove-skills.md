### Миграция

- **Сценарных скиллов в репозитории больше нет** (DECISIONS D68; решения владельца 30.10 и 03.11).
  Было: каталог `skills/` с `evo-scenarios`, `evo-deploy`, `evo-vm`, `evo-postgres` и `evo-cleanup` ставили в агента вручную (промпт из `skills/README.md` или `npx skills add …/skills/evo-*`). `instructions` MCP при `initialize` отсылали к ним: «… follow them».
  Стало: их знание — в самом evo. Факты, без которых вызов неверен или опасен, — в описаниях инструментов. Справочное и порядок вызовов — во встроенном скилле `evo` и в заметках карточек групп (`evo agent skill`, `evo agent skill --group <группа>`; их пишет `evo agent setup --skill`). Рецептов задачи и сценария «убери за собой» в evo нет: что делать для задачи и что удалять, решают агент и человек. `instructions` больше не называют скиллы.
  Что поменять: удалите скопированные `evo-*` из каталога скиллов агента (например, `~/.claude/skills/evo-deploy`): они больше не обновляются и с 1.7 расходятся с evo (`wait_seconds` до 45 с, `next_tool`, `cidr`). Встроенный скилл поставьте или обновите командой `evo agent setup --client <клиент> --skill` (или `--skill-only`).

### Удалено

- Каталог `skills/` (`evo-scenarios`, `evo-deploy`, `evo-vm`, `evo-postgres`, `evo-cleanup`, `skills/README.md`) и фраза о нём в `instructions` MCP.
