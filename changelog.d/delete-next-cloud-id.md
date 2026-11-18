### Безопасность

- **Поле `next` у удалений не вставляет id операции облака необычной
  формы.** Было: девять удалений (`pg_delete`, `kafka_delete`,
  `redis_delete`, `tg_delete`, `lb_delete`, `vpc_delete`,
  `dns_zone_delete`, `dns_record_delete`, `ar_delete`), пока удаление идёт,
  писали в `next` команду с `operation_id` облака как есть — id с
  переводом строки давал в тексте evo чужую строку. Стало: id одним словом
  (буквы, цифры, `-`, `_`) пишется как раньше; любой другой остаётся только
  в `next_args`, который помечен `_untrusted`, а `next` говорит «с
  next_args».
