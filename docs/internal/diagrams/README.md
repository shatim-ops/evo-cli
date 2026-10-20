# Архитектурные схемы evo CLI + MCP

Схемы выведены из кода на `39b67e9` (v1.1.0): граф импортов `go list`,
эндпоинты в `internal/*/client.go`, реестр `evo tools list --json`.
Схема 3 привязывает evo к модели Облачной Платформы из скилла
`arch-skill/skills/cloudru-platform-integration` (разд. 5, 7, 8, 13).

| Файл | Что показывает |
|---|---|
| `01-context.dot` | кто вызывает evo, локальное хранилище, хосты Cloud.ru и третьи стороны |
| `02-components.dot` | пакеты `internal/*` по слоям: входы → CLI/MCP → реестр и согласие → оркестрация → клиенты API |
| `03-platform.dot` | какие слои Service Controller продукта evo достигает (User Plane) и какие закрыты (Management, Admin, Internal) |
| `04-deploy.dot` | сценарий `evo deploy`: превью и согласие до создания ВМ, SSH, Docker, compose, verify |

## Рендер

SVG собираются движком [Viz.js](https://github.com/mdaines/viz-js)
(Graphviz в WebAssembly):

```sh
npm install --prefix /tmp/viz @viz-js/viz@3.11.0
node render.mjs /tmp/viz/node_modules/@viz-js/viz/lib/viz-standalone.mjs *.dot
```

У Graphviz в Viz.js нет метрик кириллицы: ширина такого символа считается
как у пробела, и подписи не помещаются в блоки. `render.mjs` раскладывает
граф по копии исходника с заменой каждого не-ASCII символа ASCII-символом
близкой ширины и возвращает исходный текст в готовый SVG.

После изменения пакетов, групп инструментов или эндпоинтов поправьте `.dot` и
перегенерируйте SVG: проверки на актуальность схем в CI нет.
