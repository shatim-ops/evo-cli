### Миграция

- **`disk_attach`, `disk_detach`, `disk_resize` (`evo disk attach`, `detach`, `resize`): `disk_id` — только UUID** (DECISIONS D85). Было: имя диска уходило в облако и возвращалось сырым `API_ERROR` 422 `uuid_parsing`, код 5, даже при `--dry-run`. Стало: `USAGE`, код 1, до облака и вопроса. Что поменять: передавайте UUID диска из `disk_list` (`evo disk list`). ВМ (`vm`, `--vm`) по-прежнему можно назвать по имени.
