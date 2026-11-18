### Безопасность

- **Отказы о лишних позиционных аргументах не повторяют их текст.** Было: `evo secret create app/db <значение>`,
  `evo secret add-version …` и любая другая команда при лишнем аргументе печатали его в отказе
  («extra arguments: "…"», «takes no arguments, got "…"», «unknown command "…"»), в том числе с `--json` и
  `--dry-run`. Значение секрета, переданное не тем аргументом, попадало в вывод и логи до вопроса о согласии.
  Стало: лишние аргументы названы числом, а неизвестная подкоманда — местом:
  `evo vm create: want <name>, got 3 argument(s); extra arguments: 2 — a flag left out?`,
  `evo doctor takes no arguments, got 1`, `unknown command (argument 1) for "evo vm"; did you mean list?`.
  Подсказка с похожими командами осталась. `reason` (`USAGE`) и код выхода 1 не меняются.
