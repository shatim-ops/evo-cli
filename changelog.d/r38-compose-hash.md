### Добавлено

- **`deploy` принимает `compose_hash` (`evo deploy --compose-hash`) — хеш compose-файла, на который дано согласие.** Его несёт шаг продолжения `deploy` (`next_args` ответа `deploying` и `DEPLOY_IN_PROGRESS`, D82). Если файл с тех пор изменился, `deploy` не продолжает по старому согласию: MCP отвечает превью с `compose_changed_since_consent` и `warning`, CLI с `--yes` — `CONFIRMATION_REQUIRED`, код 3. Без `compose_hash` всё как прежде. Так порт, добавленный в compose после согласия, не откроется без вопроса («Риск · 1.8», R-38).
