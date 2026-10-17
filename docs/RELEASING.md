# Выпуск релиза

Версии — SemVer, тег `vMAJOR.MINOR.PATCH[-rc.N]`. Первый публичный — `v1.0.0-rc.1`.
**minor** — новые команды и инструменты; **patch** — исправления;
ломающие изменения CLI (команды, флаги, коды возврата, формат `--json`) — только в **major**.
Исключение, объявленное до v1.0: в v1.1 удаляется совместимость со старым именем
`cloudru-vm` (alias бинаря, `.cloudru-vm.yaml`, каталоги `.cloudru-vm/`) — см. README, «Переименование».

0. Стабильный релиз — до тега отдельным PR в `main`:
   - `CHANGELOG.md`: «[Не выпущено]» → «[X.Y.Z] — ГГГГ-ММ-ДД» (дата тега)
     и новый пустой «[Не выпущено]» сверху;
   - убрать «до выхода `v1.0.0` стабильного релиза нет» и примеры с
     `CLOUDRU_VM_VERSION=v1.0.0-rc.N` из `README.md`, `docs/install.md`,
     `docs/guide.md`: `install.sh` без версии ставит последний стабильный;
   - cloud-e2e на этом коммите зелёный (ru.AZ-2), `cleanup_only` — без остатков.
1. `main` зелёный в CI (в том числе `TestCommandsDoc`: `docs/commands.md` совпадает с бинарём); локально `goreleaser check` и, по желанию,
   `goreleaser release --snapshot --clean --skip=sign` — в `dist/` 6 архивов (в каждом `evo` и alias `cloudru-vm` до v1.1) и `checksums.txt`.
2. `git tag -a v1.2.3 -m v1.2.3 && git push origin v1.2.3` — тег ставится на коммит в `main`.
   Если тег не запушить (облачная сессия пушит только свои ветки): Actions →
   **tag** → Run workflow с `tag` и полным `sha` коммита в `main`
   (`gh workflow run tag.yml -f tag=v1.2.3 -f sha=…`). Он ставит тег и сам
   запускает `release.yml` на нём; identity подписи та же (`release.yml@refs/tags/…`).
3. `release.yml` гоняет весь `ci.yml`, затем goreleaser: архивы, `checksums.txt`,
   подписи cosign keyless (`*.sigstore.json`), GitHub Release. `-rc.N` публикуется как pre-release.
4. Проверить подпись скачанного `checksums.txt` (пример для `v1.2.3`):

   ```
   cosign verify-blob --bundle checksums.txt.sigstore.json \
     --certificate-identity-regexp '^https://github.com/shatim-ops/evo-cli/\.github/workflows/release\.yml@refs/tags/v' \
     --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
   sha256sum --ignore-missing -c checksums.txt
   ```

   С cosign v2 (v2.4+) добавьте `--new-bundle-format`: бандл подписи — sigstore
   v0.3, его пишет cosign v3 из `release.yml`.

   `v1.0.0-rc.1` и `rc.2` выпущены до переименования репозитория: их identity —
   `shatim-ops/cloudru-vm-cli`.

5. Неудачный релиз: удалить Release и тег (`git push --delete origin v1.2.3`), исправить, выпустить следующий patch — опубликованный номер не переиспользовать.

Версия goreleaser зафиксирована в `ci.yml` и `release.yml` (`v2.18.3`); поднимать в обоих сразу.

Цепочка поставок: сторонние экшены закреплены по SHA коммита (версия — в
комментарии), базовые образы `Dockerfile` и `hadolint` — по digest; их
поднимает Dependabot (`.github/dependabot.yml`) отдельными PR. Тег можно
переписать в чужом репозитории, SHA и digest — нет. Job `goreleaser`
собирает без кэша Actions (`cache: false`): подписанный бинарь не берёт
объектные файлы из кэша, который мог записать другой запуск. У `GITHUB_TOKEN`
по умолчанию только `contents: read`, `checkout` не оставляет токен в
`.git/config`.

## Docker-образ MCP-сервера

Job `image` в `release.yml` после `ci` собирает образ, гоняет smoke-тест
(`initialize` по HTTP, версия сервера = тег) и публикует его в Artifact
Registry Cloud.ru: `<реестр>/evo:<версия>`, для стабильных тегов ещё и
`:latest` (`-rc.N` его не двигает). Нужны секреты репозитория
(Settings → Secrets and variables → Actions):

| Секрет | Что это |
|---|---|
| `CLOUDRU_AR_REGISTRY` | имя реестра (`<имя>` → `<имя>.cr.cloud.ru`) или полный хост |
| `CLOUDRU_AR_KEY_ID`, `CLOUDRU_AR_SECRET` | ключ доступа с правом push в этот реестр |

Без них job пишет notice «Образ не опубликован» и не падает: бинари
выпускаются всё равно. Образ только `linux/amd64`, не подписан cosign.

## Документация

Справка по командам `docs/commands.md` генерируется из дерева команд;
после изменения команд, флагов или описаний инструментов:

```
go test ./internal/cli -run TestCommandsDoc -update-docs
```

`TestAuthDoc_ListsEveryEnvVar` падает, если код читает переменную окружения,
не упомянутую в `docs/auth.md`.
