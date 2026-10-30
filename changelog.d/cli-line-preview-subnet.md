### Изменено

- **Превью `evo pg create`, `evo kafka create`, `evo redis create` и MCP тех же инструментов называют подсеть «имя (id)», превью `evo pg create` — и зону кластера.** Ответ несёт `subnet_name` рядом с `subnet_id` в `will_create` и в `preview.params` — имя подсети evo уже прочитал, выбирая её. Строки превью с парой `<x>_id`/`<x>_name` (так уже были `vpc_id` у `subnet_create` и кластер у `k8s_add_addon`) теперь одна строка «имя (id)», а не две.
