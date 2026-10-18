# Этап 5 — Сценарий deploy на дефолтах: Docker, FIP, `logs -f`, хардкод

Контекст: `00-audit.md` разд. 2 п. 1, 2, 5, 9. Зависит от этапа 1.

## Задачи

1. **Docker на ВМ.** `internal/bootstrap` (cloud-init, get.docker.com) нигде не
   подключён, а образ из `init` — ubuntu-22.04 без Docker. Подключить
   bootstrap в путь создания ВМ (`internal/engine/deploy.go`) с ожиданием
   готовности Docker (`docker compose version` с retry до N минут), либо
   выбирать образ с Docker — решить по тому, что даёт compute API
   (`list-images`). Предпочтительно cloud-init: не зависит от каталога образов.
2. **Floating IP при `destroy`.** `engine/destroy.go` удаляет ВМ, но не FIP;
   MCP-инструмент `release_floating_ip` освобождает. Сделать как в MCP; FIP
   фиксировать в `state.json` при создании.
3. **`logs --follow`.** `deploy/ssh.go:121-135` собирает `CombinedOutput` с
   таймаутом 120 с. Добавить `ExecStream(ctx, cmd, stdout, stderr)` через
   `session.Stdout/Stderr` и использовать для `-f`; завершение по `ctx`.
   Имя сервиса экранировать (сейчас подставляется в shell как есть, `engine/logs.go:64`).
4. **Хардкод.** `DiskTypeID` (`deploy.go:340`), UUID зон/образов в `init`
   и в `--help` (`main.go:438-551`), ssh-user `user1`:
   - `init` запрашивает зоны и образы через API и предлагает выбор
     (неинтерактивно — первая зона, первый подходящий образ, с печатью выбора);
   - тип диска — из `list_disk_types` по имени (`ssd` по умолчанию);
   - справка без UUID.
5. **`verify`:** нездоровый сервис → код 1 (сейчас 0).
6. **SSH-ключ.** Сейчас создаётся в `./.cloudru-vm/` даже для `list-zones`.
   Генерировать лениво — только командам, которые ходят на ВМ. Расположение
   не менять (ключ привязан к проекту; это документируется на этапе 9).
7. **Таймаут ожидания FIP** (`deploy.go:435-479`, 2 мин вслепую): печатать
   прогресс, уважать `ctx`.

## Не делать

- Не добавлять `.env`/`build:`/несколько compose-файлов (BACKLOG).
- Не менять формат `state.json` кроме добавления `floating_ip_id`.

## Критерии приёмки

На реальном проекте (нужны креды; выполняет человек или агент с доступом):

```
rm -rf .cloudru-vm .cloudru-vm.yaml
cloudru-vm init --non-interactive
cloudru-vm deploy --yes -f docker-compose.yml     # ВМ с Docker, сервис отвечает
cloudru-vm logs -f & sleep 5; kill %1             # поток, без ошибки 120 с
cloudru-vm destroy --yes                          # ВМ и FIP удалены
cloudru-vm tools call list_floating_ips           # нашего FIP нет
```

Юнит-тесты: `ExecStream` на фейковой сессии; `destroy` вызывает release FIP
когда `floating_ip_id` есть в state; `verify` возвращает ошибку на unhealthy.
