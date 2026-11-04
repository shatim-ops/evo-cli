### Добавлено

- **Превью удаления называет, создан ли ресурс этим evo.** Поле `provenance`
  у превью `destroy` (с целью и без), `release_floating_ip` и удалений
  `*_delete` через общий план (`pg_delete`, `redis_delete`, `kafka_delete`,
  `k8s_delete`, `vpc_delete`, `subnet_delete`, `snat_delete`, `lb_delete`,
  `tg_delete`, `ar_delete`): `{"created_by_evo": "yes" | "no" | "unknown",
  "note": "…"}` — по state папки и времени создания из облака; человек видит
  строку «создан evo: …». Чего нет в state папки (другая папка, консоль,
  ресурсы, которые evo пока не записывает), — «неизвестно». Поле — сведение,
  а не граница: удаление идёт как прежде, граница — права ключа в IAM.
