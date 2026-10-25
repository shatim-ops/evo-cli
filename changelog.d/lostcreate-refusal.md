### Миграция

- **Создание, которое API отклонил (статус 4xx), — ошибка с `reason` по
  статусу, а не «создано»: `evo secret create` и остальные 21 команда
  создания (`evo kafka create`, `evo redis create`, `evo pg create`, `evo ar
  create`, `evo snat create`, `evo ca deploy`, `evo disk create`, `evo dns
  zone-create`, `evo vpc create`, `evo k8s create`, `evo lb create`, `evo k8s
  add-nodepool`, `evo pg backup-create`, `evo tg create`, `evo vpc
  route-create`, `evo dns record-create`, `evo cert issue-le`, `evo cert
  import`, `evo image create`) и те же инструменты MCP** (DECISIONS D42;
  исправление: живой прогон Grok на 1.3.0, G130-15). Было: отказ API на
  запрос создания читался как потерянный ответ — evo искал ресурс по имени в
  списке и, найдя одноимённый (например, секрет, который был по этому пути
  до вызова), отвечал `status: creating` с его id, «exists and is billed»,
  код 0, `isError: false`. Стало: статус 4xx — запрос прочитан и отклонён,
  ничего не создано: ошибка с `reason` по статусу (`API_ERROR`, код 5; 404
  — `NOT_FOUND`, код 4; 429 — `API_RATE_LIMITED`, код 5), без поиска по
  имени. Потерянный ответ (обрыв, тайм-аут, 5xx, успех без id) читается как
  раньше: ресурс ищется по имени и называется, если есть. Что поменять:
  скрипт, который на дубль ждал `status: creating` и брал id из ответа,
  теперь получает ошибку — найдите существующий ресурс по имени (`evo secret
  get`, `evo … list`) или возьмите другое имя.
