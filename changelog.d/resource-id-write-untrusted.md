### Безопасность

- **id ресурса в ответе записи и ответ `cert_private_key` помечены
  `_untrusted`.** Было: `registry_id`, `vpc_id`, `zone_id`, `record_id`,
  `route_id`, `backup_id` у создания и `vm_id` у `vm_start`, `vm_stop`,
  `vm_reboot` и `vm_resize` приходили из облака без метки, а
  `cert_private_key` не помечал ничего. Стало: эти поля — в центральном
  списке полей, которые `_untrusted` называет у ответа любого инструмента,
  как `operation_id`. У `cert_private_key` помечен весь ответ сервиса,
  кроме `key_format`: его пишет evo.
