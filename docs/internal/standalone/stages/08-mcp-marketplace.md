# Этап 8 — MCP для Marketplace: Streamable HTTP, Docker, protocolVersion, токен

Контекст: `00-audit.md` разд. 3 (Marketplace), разд. 4 п. 2. Зависит от
этапа 2 (кэш токена, креды из конфига). Поддерживаемый Cloud.ru путь —
MCP-сервер как Docker-образ из Artifact Registry
(https://cloud.ru/docs/ai-agents/ug/topics/guides__create-mcp-server).

## Задачи

1. **Транспорт Streamable HTTP** рядом со stdio: `cloudru-vm mcp --http :8080`.
   Один `POST /mcp` (JSON-RPC, ответ JSON или SSE), `GET /mcp` для SSE-потока,
   `Mcp-Session-Id`. Транспортный слой отделить от `internal/mcp/server.go`
   (сейчас читает stdin/stdout напрямую) так, чтобы stdio-тесты не менялись.
2. **protocolVersion.** Сейчас `2024-11-05`. Поддержать переговоры с
   `2025-03-26` и `2025-06-18` (клиент присылает свою — отвечать ею, если
   поддерживаем; иначе — последней нашей). Проверить, что ничего из новых
   обязательных полей не ломает существующие клиенты (Claude Code, внутренний MCP-клиент).
3. **Аутентификация HTTP-режима.** Два режима, выбираются флагом:
   - `--auth bearer`: статический токен из `MCP_BEARER_TOKEN`; без него сервер
     не стартует (никогда не слушать без auth);
   - `--auth none` только с `--http 127.0.0.1:…` (локальный запуск).
   Креды Cloud.ru сервер берёт из своего окружения/профиля (этап 2), **не от
   клиента** — клиент не должен передавать `CLOUDRU_SECRET` в аргументах.
   OAuth 2.1 по спецификации MCP — BACKLOG до требований платформы AI Agents.
4. **Dockerfile** (multi-stage, `distroless/static`, non-root, `HEALTHCHECK`
   на `GET /healthz`), образ `cloudru-vm:<version>`; публикация в Artifact
   Registry — в `release.yml` отдельным job (`ar_*` инструменты уже знают API).
   `kubectl`/`helm` в образ **не** класть — инструменты `k8s_kubectl/apply/helm`
   в контейнере отвечают понятной ошибкой «недоступно в этом режиме».
5. **Один Engine/TokenSource на процесс** (сейчас — на вызов), иначе под HTTP
   каждый инструмент будет ходить в IAM.
6. **Безопасность:** в HTTP-режиме не отдавать `k8s_kubeconfig` с embedded
   токеном и `cert_private_key` без явного флага `--allow-secret-export`;
   логировать имя инструмента и результат без аргументов (санитайзер
   `internal/log` наконец подключается здесь).

## Не делать

- Не менять набор инструментов и их схемы.
- Не реализовывать OAuth-сервер.

## Критерии приёмки

```
docker build -t cloudru-vm:dev .
docker run -e MCP_BEARER_TOKEN=t -e CLOUDRU_KEY_ID=… -e CLOUDRU_SECRET=… -p 8080:8080 cloudru-vm:dev mcp --http :8080
curl -s -H 'Authorization: Bearer t' -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}' \
  localhost:8080/mcp | jq .result.protocolVersion          # 2025-06-18
curl -s -o /dev/null -w '%{http_code}' localhost:8080/mcp   # 401 без токена
claude mcp add --transport http cloudru-vm http://localhost:8080/mcp --header 'Authorization: Bearer t'   # tools/list = 146
```

Тесты: `httptest` на initialize/tools/list/tools/call через HTTP; stdio-тесты
без изменений; отказ старта `--http :8080 --auth none`.
