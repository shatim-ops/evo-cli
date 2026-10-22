# Этап 7 — Релиз: goreleaser, ldflags, checksums, cosign, `version`

Контекст: `00-audit.md` разд. 1 («Релиз»). Зависит от решений D1 (лицензия),
D8 (подпись) и этапа 6 (CI-матрица). Делать **после** смены LICENSE —
первый публичный тег не должен нести «Внутренний код».

## Задачи

1. **Версия.** `cmd/cloudru-vm/main.go:24` `var version = "0.1.0"` →
   `version, commit, date = "dev", "none", "unknown"`, заполняются через
   `-ldflags "-X main.version=… -X main.commit=… -X main.date=…"`.
   `version` печатает все три + `runtime.Version()` + `GOOS/GOARCH`;
   `--json` — объектом. Та же строка версии уходит в MCP `initialize.serverInfo`.
2. **`.goreleaser.yaml`:** `CGO_ENABLED=0`; `linux/darwin/windows × amd64/arm64`;
   архивы `tar.gz` (zip для windows); `checksums.txt` (sha256); changelog из
   коммитов; `LICENSE`, `README.md` в архив.
3. **Подпись:** cosign keyless (`sigstore/cosign-installer`) для checksums и
   архивов; инструкция проверки в README (этап 9). Нотаризация macOS /
   Authenticode — только если есть сертификаты (D8); иначе BACKLOG + в README
   раздел «Gatekeeper/SmartScreen».
4. **Workflow `release.yml`:** триггер `push: tags: v*`; `permissions:
   contents: write, id-token: write`; перед сборкой — те же проверки, что в ci.
5. **Install-скрипт** `install.sh` (linux/macos: определяет ОС/arch, качает,
   проверяет sha256, кладёт в `~/.local/bin` или `/usr/local/bin`) и
   инструкция для Windows (zip + PATH). Homebrew tap / scoop — BACKLOG.
6. **Теги и SemVer.** Первый публичный — `v1.0.0-rc.1`. Политика: minor —
   новые команды, patch — исправления; ломающие изменения CLI только в major.
   Записать в `docs/RELEASING.md` (как выпускать релиз, 10 строк).

## Не делать

- Не публиковать Docker-образ здесь (этап 8).
- Не переименовывать бинарь (этап 10; goreleaser `binary:` потом меняется одной строкой).

## Критерии приёмки

```
goreleaser release --snapshot --clean      # 6 бинарей в dist/, checksums.txt
./dist/cloudru-vm_linux_amd64_v1/cloudru-vm version   # version/commit/date/go/os-arch
cosign verify-blob --certificate-identity-regexp … checksums.txt   # OK (в CI на тестовом теге)
```

Тестовый тег `v0.0.0-test` на форке/ветке → Release создаётся → тег удалить.
