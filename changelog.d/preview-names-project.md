### Изменено

- **Превью каждого create называет проект, в который пишет** (план 1.6, «Превью create называет проект»).
  - Раньше 20 из 28 create не называли проект в превью: `ar_create`, `ca_deploy`, `cert_import`, `cert_issue_le`, `dns_record_create`, `dns_zone_create`, `image_create`, `kafka_create`, `lb_create`, `pg_backup_create`, `pg_db_create`, `pg_user_create`, `redis_create`, `s3_bucket_create`, `s3_put`, `secret_create`, `snat_create`, `tg_create`, `vpc_create`, `vpc_route_create`. Проект берётся из профиля, `.evo.yaml` или окружения, а не из вызова, поэтому создать ресурс не в том проекте было легко.
  - Теперь их ответ `confirmation_required` содержит `project_id`: в объекте `preview`, если он есть, иначе рядом с `message`. `evo … --dry-run` и вопрос о согласии показывают строку «проект: …». Это новое поле; `reason` и коды выхода не меняются.
