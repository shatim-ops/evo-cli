### Изменено

- Продукт называется **evo CMCP** (прежде — «Cloud.ru Evolution CLI CREW edition»), команда по-прежнему `evo`. Новое имя видно в `evo --help` (первая строка: «evo CMCP — CLI и MCP-сервер для Cloud.ru Evolution»), в `evo version` (`evo CMCP <версия>`; в `--json` — поле `product`), в MCP `serverInfo.title`, в User-Agent запросов к Cloud.ru (`evo/<версия> (evo CMCP; +…)`) и в названии релиза. Не меняются: команда `evo`, `serverInfo.name` (`evo`), имена инструментов, переменные окружения, пути настроек, коды выхода и `reason`.
