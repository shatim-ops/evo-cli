### Изменено

- **`evo disk create`, `evo disk attach`, `evo disk detach`, `evo disk
  resize`, `evo image create`, `evo ca deploy`, `evo ca logs`, `evo vpc
  create`, `evo vpc route-create`, `evo lb create`, `evo tg create`, `evo dns
  zone-create`, `evo dns record-create`: примечания и следующие шаги — по-русски.**
  «Диск создан и подключён…», «Диск увеличен…», «сборка образа в
  очереди», пустой журнал Container App, «асинхронно — опрашивайте …» и шаг
  диска у предела времени вызова выводились по-английски. Теперь в CLI
  они русские и называют команды evo (`evo lb operation`, `evo vpc get`,
  `evo ca logs --system`), а не имена инструментов. Состояние диска от
  облака в шаге — в кавычках. Ответы MCP, ключи и значения
  `status`/`reason` не меняются (D6; опись Астры #1835).
