### Миграция

- **Имя подсети или группы безопасности, которое носят два объекта проекта, — отказ до согласия: `--subnet-id` в `evo vm create`, `evo pg create`, `evo kafka create`, `evo redis create`, `evo k8s create`, `evo k8s plan`, `evo lb create`; `--nodes-subnet-id` и `--security-group-id` в `evo k8s add-nodepool` и те же аргументы MCP** (DECISIONS D57).
  Было: имя не разрешалось — отказ инструмента (`CONFIG_INVALID` или `NOT_FOUND`), у группы `k8s_add_nodepool` — вопрос о согласии и отказ платформы после «да».
  Стало: `USAGE`, код 1, до вопроса; ошибка называет id объектов с этим именем.
  Что поменять: передать id из ошибки.

### Добавлено

- **Подсеть и группу безопасности можно указать по имени** (DECISIONS D57):
  `--subnet-id` в `evo vm create`, `evo pg create`, `evo kafka create`,
  `evo redis create`, `evo k8s create`, `evo k8s plan`, `evo lb create`;
  `--nodes-subnet-id` и `--security-group-id` в `evo k8s add-nodepool` и те же
  аргументы MCP. Имя без учёта регистра разрешается в id по свежему списку
  проекта до превью и вопроса; UUID работает как раньше. Tab предлагает имена
  вместе с id.
