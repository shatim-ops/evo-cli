### Исправлено

- **`evo doctor` без ключа называет `evo login`:** проверка `project_id` — «не задан: evo login задаст его вместе с ключом», итог — «evo login (через браузер) или evo configure». В описании `doctor` и `docs/mcp.md` сказано, что `http_status` у IAM API и Compute API — ответ корня API: любой ответ, в том числе 404, значит «доступен». Коды выхода прежние.
- **Половина пары ключа в окружении называется прямо (`whoami` и любая команда с ключом):** `CLOUDRU_KEY_ID` без `CLOUDRU_SECRET` (или наоборот; так же пара AI Agents `EVOLUTION_SERVICE_ACCOUNT_KEY_ID` и `_SECRET`) — «окружение даёт ключ только парой, и тогда профиль не читается» вместо «не задан ключ доступа». Причина и код выхода прежние (`AUTH_REQUIRED`).
- **Профиль не найден — подсказка называет `evo login --profile <имя>`** рядом с `evo configure --profile <имя>`.
