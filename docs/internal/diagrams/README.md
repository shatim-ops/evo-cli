# Архитектурные схемы evo CLI + MCP

Схемы выведены из кода на `39b67e9` (v1.1.0): `auth/iam.go`,
`tools/registry.go`, `cli/tools.go`, `mcp/http.go`, клиенты сервисов,
реестр `evo tools list --json`. Схема 3 привязывает evo к модели Облачной
Платформы из скилла `arch-skill/skills/cloudru-platform-integration`
(разд. 5, 7, 8, 13).

| Файл | Что показывает | Чем нарисовано |
|---|---|---|
| `01-hld.svg` | HLD: потребители → поверхности CLI и MCP → реестр инструментов → шаги обработчика (аргументы, конфиг, токен IAM, чтения, согласие, изменение) → клиенты и прямые каналы → Cloud.ru | `hld.py` |
| `02-sequence.svg` | изменяющий вызов `pg_create` через MCP HTTP: два вызова, токен из кэша или обмен ключа в IAM, проверка прав на каждом запросе | `hld.py` |
| `03-platform.dot` | какие слои Service Controller продукта evo достигает (User Plane) и какие закрыты (Management, Admin, Internal) | Viz.js |
| `04-deploy.dot` | сценарий `evo deploy`: превью и согласие до создания ВМ, SSH, Docker, compose, verify | Viz.js |

## Рендер

```sh
# схемы 1–2: раскладка задана в коде, ширина текста — по Liberation Sans
python3 hld.py

# схемы 3–4: Graphviz в WebAssembly
npm install --prefix /tmp/viz @viz-js/viz@3.11.0
node render.mjs /tmp/viz/node_modules/@viz-js/viz/lib/viz-standalone.mjs *.dot
```

`hld.py` завершается с ошибкой, если подпись не помещается в блок. У Graphviz
в Viz.js нет метрик кириллицы, поэтому `render.mjs` раскладывает граф по копии
исходника, где не-ASCII символы заменены ASCII-символами близкой ширины, и
возвращает исходный текст в готовый SVG.

После изменения пакетов, групп инструментов, порядка проверок или эндпоинтов
поправьте схемы и перегенерируйте SVG: проверки на актуальность в CI нет.
