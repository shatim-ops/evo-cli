### Изменено

- **Превью `evo k8s add-nodepool` и MCP `k8s_add_nodepool` называют подсеть узлов и группу безопасности «имя (id)», ответ несёт имя flavor (`flavor`); `evo k8s plan` показывает flavor «имя (id)».** Правило вывода «имя (id)» берёт имя из `<x>_name`, а если его нет — из строкового `<x>` рядом с `<x>_id` (так k8s уже отдавал `flavor` и `flavor_id`). Ответ (`--json`, MCP) несёт новые `nodes_subnet_name`, `security_group_name` в превью и `flavor` в ответе создания; значения прежних полей не меняются.
