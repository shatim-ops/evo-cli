### Безопасность

- **Отказы по форме значения в инструментах не повторяют его.** Правило: форма — без значения; ресурс, не найденный
  по имени, называется, как прежде.
  - Было: значение целиком, до согласия — «start "<значение>" is not a date YYYY-MM-DD», «since "…" is not a
    duration», «ports "…" is neither a port…», «invalid job_id "…"», «resolve_mode must be one of …, got "…"», имя
    ВМ, диска, образа, подсети, шлюза, копии, кластера Managed Kubernetes, бакета или приложения Container Apps,
    которое не проходит правило.
  - Стало: аргумент и правило («start: the value is not a date YYYY-MM-DD»); для имени — что с ним не так словами,
    без самого имени: «it starts or ends with a space», «it has a control character», «it is longer than N
    characters», «it has a character or a form the rule does not take».
  - `resolve_mode` у `k8s_add_addon` — закрытый список схемы (NONE, OVERWRITE).
  - `reason` (`USAGE`) и коды выхода не меняются.
