### Изменено

- **`evo vm create`, `evo vm delete`, `evo deploy`, `evo deploy plan`,
  `evo deploy apply`, `evo vpc fip-release`: тексты ответов — по-русски.**
  Сообщение о готовой ВМ, предупреждение о пароле, ход удаления ВМ и
  освобождения floating IP, примечания о проверке SSH и публичном IP, шаги
  «покажите план» и «ВМ ещё создаётся» выводились по-английски. Теперь в
  CLI они русские и называют команды evo (`evo vm delete`, `evo vpc
  fip-release`, `evo deploy apply`), а не имена инструментов. Ответы MCP,
  ключи и значения `status`/`reason` не меняются (D6; опись Астры #1835).
