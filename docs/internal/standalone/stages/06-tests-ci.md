# Этап 6 — Тесты без реального времени и сети; CI-матрица ОС

Контекст: `00-audit.md` разд. 1 («Сборка/тесты»). Независим от других этапов —
можно делать первым.

## Почему 91 с и 10 с

- `internal/engine`: `vmPollInterval` 5 с, 12 итераций ожидания интерфейса
  (мок без `State:"created"`, `engine_test.go:174-176`), `time.Sleep(2s)`
  (`deploy.go:188`), реальный HTTP на `203.0.113.42:8080/5432` с таймаутом 10 с
  на порт (`engine_test.go:187`).
- `internal/provision`: `initialBackoff = 1s` ×2 в `TestRetryOn429`, `TestServerError`.
- `internal/verify`: `sleep 2s` в `verify_test.go:78`.

## Задачи

1. Внедрить зависимости времени: `Clock`/`Sleeper` интерфейс или поля
   `PollInterval`, `Backoff`, `SettleDelay` в `Engine`/`Adapter` с дефолтами
   как сейчас; тесты ставят миллисекунды.
2. Убрать реальную сеть из тестов: `httptest.Server` для HTTP-проверок,
   мок с `State:"created"` для интерфейса.
3. Цель: `go test -race ./...` **< 10 с** суммарно; ни один тест не ходит
   за пределы localhost.
4. CI (`.github/workflows/ci.yml`):
   - матрица `ubuntu-latest`, `macos-latest`, `windows-latest`; govulncheck —
     только на ubuntu;
   - зафиксировать версию govulncheck (`@vX.Y.Z`, не `@latest`);
   - `go.mod`: `go 1.25.0` при CI 1.26.5 — поднять в go.mod до версии, которой
     реально требуют уязвимости (см. комментарий в ci.yml), чтобы `go build`
     локально не отличался от CI.
5. Тесты, которые завязаны на Unix (права файлов 0600), помечать
   `t.Skip` на Windows с причиной.

## Не делать

- Не менять продуктовое поведение таймингов (дефолты те же).
- Не добавлять тесты для 16 пакетов без тестов (отдельный P2).

## Критерии приёмки

```
time go test -race -count=1 ./...      # real < 10s
grep -rn '203.0.113' internal/          # пусто
```

CI зелёный на трёх ОС в PR этого этапа.
