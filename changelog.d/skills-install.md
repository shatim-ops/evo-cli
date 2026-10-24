### Изменено

- **Установка сценарных скиллов — первым способом через агента**
  (`skills/README.md`). Готовый промпт: агент берёт версию из
  `evo version --json`, клонирует тег этой версии (не `main`), копирует
  `skills/evo-*` в свой каталог скиллов (у Claude Code — `~/.claude/skills/`),
  ставит встроенный скилл и MCP-сервер `evo agent setup --scope user --skill`
  после показа diff и проверяет `metadata.evo-version`; затем новая сессия и
  вопрос «какие evo-скиллы ты видишь?». У `npx skills add` — предупреждение
  отметить своего агента: не отмеченный агент скиллов не видит (Claude Code
  читает `~/.claude/skills/`, а не `~/.agents/skills/`). Чат Claude Desktop и
  claude.ai помечен как не проверенный.
