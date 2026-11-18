### Безопасность

- **Значения облака в `vpc_list`, `destroy`, `image_create` и `snat_create` помечены `_untrusted`.** Было: непустой
  `vpc_list` отдавал имена VPC без метки (агентный прогон rc.1, #2524); превью `destroy` называло ВМ (`name`,
  `target`) без метки и с именем и зоной без кавычек в `message`; ответ `image_create` (`image`, `will_create`) и
  `snat_create` (`nat_gateway`) — без метки. Стало: эти поля названы в `_untrusted`, имя и зона ВМ в тексте превью
  `destroy` — в кавычках, на одной строке. Коды выхода и `reason` не меняются.
