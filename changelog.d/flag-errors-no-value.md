### Безопасность

- **Ошибки разбора флагов не повторяют введённый текст.** Было: `-p<секрет>`, `--json=<секрет>`, `--<секрет>`
  и `---<секрет>` отказывали текстом, где токен стоял целиком («unknown shorthand flag: 'p' in -p…»,
  «invalid argument "…" for "--json" flag», «unknown flag: --…», «bad flag syntax: …»), до вопроса о
  согласии, с `--json` и без. Стало: отказ называет флаг evo и место аргумента, но не значение:
  `flag --json: the value is not true or false (argument 3)`, `unknown shorthand flag -p (argument 1)`,
  `unknown flag (argument 2)` — имя неизвестного длинного флага не повторяется, называется место.
  `reason` (`USAGE`) и код выхода 1 не меняются.
