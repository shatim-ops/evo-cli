### Исправлено

- `evo --yes stack destroy` (и `evo --yes tools call stack_destroy`) уже
  удалённого стека, у которого облако ещё удаляло ресурсы (`deleting`:
  балансировщик, целевая группа, кластеры), перепроверяет эти удаления, как
  MCP `stack_destroy` с `confirmed=true`; было — сразу `already_destroyed`
  без перепроверки, хотя подсказка её предлагала.
