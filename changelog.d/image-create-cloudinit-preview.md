### Исправлено

- **Превью `image_create` (`evo image create`) говорит, есть ли у образа шаблон cloud-init.** Было: ответ называл
  `will_create.has_user_data_template`, а человеческое превью его не показывало — человек не видел, что ВМ из
  образа будет запускать шаблон cloud-init. Стало: строка «шаблон cloud-init: да / нет» в превью, в CLI и в
  `preview.details` MCP. `reason` и коды не меняются.
