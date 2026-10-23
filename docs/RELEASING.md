# Выпуск релиза

Версии — SemVer, тег `vMAJOR.MINOR.PATCH[-rc.N]`. Первый публичный — `v1.0.0-rc.1`.
**minor** — новые команды и инструменты; **patch** — исправления;
ломающие изменения CLI (команды, флаги, коды возврата, формат `--json`) — только в **major**.
Совместимость со старым именем `cloudru-vm` (alias бинаря, `.cloudru-vm.yaml`,
каталоги `.cloudru-vm/`) сохраняется до v2.0 и удаляется вместе с ней — см.
[docs/rename.md](rename.md). (До v1.0 было объявлено удаление в v1.1; решение
владельца от 20.10.2026 — держать до v2.0.)

### Ломающее изменение в minor или patch: когда можно

По умолчанию ломающее изменение ждёт major. В minor или patch его можно
выпустить, только если выполнены **все** условия ниже:

1. **Причина — дефект, а не улучшение.** Нынешнее поведение — одно из
   двух. Первое: дыра в безопасности или в согласии, то есть деньги
   тратятся, данные удаляются или секрет раскрывается без явного согласия
   человека. Второе: ошибка, из-за которой результат неверен, а скрипт
   этого не видит (например, урезанный список с кодом 0 или чужой ресурс
   под тем же именем). Отказ в обслуживании сервера MCP — исчерпание его
   памяти или потоков одним клиентом — приравнен к дыре в безопасности:
   сервер с токеном у нескольких агентов падает для всех.
2. **Ждать major — значит оставить пользователей под риском.** Дефект
   задевает текущее использование, а не гипотетическое, и обходного пути
   без изменения evo нет.
3. **Изменение минимально.** Меняется ровно то, что закрывает дефект.
   Переименования, приведение форматов к единому виду, косметика и
   удобство ждут major, даже если идут в том же PR.
4. **Переход на новую версию — одна понятная правка.** Например, добавить
   `--yes` или сравнивать `reason` с другим значением. Если для перехода
   нужно переписывать логику скрипта, изменение ждёт major.
5. **Оформлено так:**
   - раздел «Миграция» первым в версии CHANGELOG, отдельный пункт на каждое
     изменение: было → стало → что поменять;
   - запись в `docs/internal/standalone/DECISIONS.md` с решением владельца;
   - упоминание в README («Что сейчас можно») и в анонсе релиза.

Если хотя бы одно условие не выполнено, изменение ждёт major. Старое
поведение можно сохранить до major за флагом или с предупреждением в
stderr, если это не оставляет дефект открытым.

Ломающие изменения, выпущенные по этому правилу:

| Версия | Что | Основание |
|---|---|---|
| 1.1.0 | коды выхода, `reason` и формат ответов: 404 на диск — `NOT_FOUND`, а не `VM_NOT_FOUND`; неполный список — ошибка, а не короткий список; `secret value` без подтверждения не печатает значение и др. | условия 1–5; раздел «Миграция» 1.1.0 в [CHANGELOG.md](../CHANGELOG.md) |
| 1.2.0 | повторный `evo deploy`, который обновляет существующую ВМ, без терминала и без `--yes` — код 3, а не 0 | дыра в согласии, расхождение с MCP `deploy`; условия 1–5; DECISIONS D13, `docs/internal/cli-mcp-parity/spec.md` §8 Q1 |
| 1.2.0 | MCP `provision` и `deploy` (и `evo tools call` с ними) отклоняют образ, который есть в каталоге, но недоступен проекту, уже на превью — `NOT_FOUND`, в `evo tools call` код 4, а не 3 | отказ приходил после согласия, расхождение с превью `evo vm create`; условия 1–5; DECISIONS D13, `docs/internal/cli-mcp-parity/spec.md` §8 Q13 |
| 1.2.1 | `evo vm create`, `evo tools call provision` и MCP `provision` без зоны, образа или проекта отказывают до превью: без зоны — `CONFIG_INVALID`, код 2 (было 3 без `--yes`, 1 с ним), без образа — `USAGE`, код 1 (было 3 без `--yes`), без проекта — `CONFIG_INVALID`, код 2 (было 3 без `--yes`); в MCP — ошибка вместо `confirmation_required` | согласие спрашивалось на вызов, который не мог пройти (догфуд 1.2.0); условия 1–5; DECISIONS D15 |
| 1.2.2 | MCP-сервер с `--allow-project`: `billing_balance` — `CONFIG_INVALID` вместо баланса договора, в `billing_finops` баланс `null` | раскрытие баланса всей организации серверу, ограниченному проектом (догфуд №5); условия 1–5; DECISIONS D18 |
| 1.2.2 | `evo s3 put`/`rm` и MCP `s3_put`/`s3_rm` с именем бакета, которое S3 не примет, — `USAGE`, код 1, до превью (было: код 3 без `--yes`, после согласия — ошибка клиента S3) | согласие спрашивалось на вызов, который не мог пройти (как D15); условия 1–5; DECISIONS D19 |
| 1.2.3 | `evo snat create` и MCP `snat_create` с занятым именем — `USAGE`, код 1, `existing_id`, до согласия (было: с `--yes` код 0 и второй платный шлюз, без `--yes` код 3); превью без ключа или проекта — код 2 | повтор создавал второй платный шлюз (матрица сбоев 6.1); условия 1–5; DECISIONS D20 |
| 1.2.3 | `evo k8s create`, `evo lb create` и MCP `k8s_create`, `lb_create` с занятым именем — `USAGE`, код 1, `existing_id`, до согласия (было: с `--yes` у `k8s_create` код 0 и второй платный кластер, у `lb_create` код 5 после согласия — 409 облака; без `--yes` код 3); превью `k8s_create` без ключа или проекта — код 2 | повтор `k8s_create` мог создать второй платный кластер; `lb_create` спрашивал согласие на вызов, который не пройдёт; условия 1–5; DECISIONS D21 |
| 1.3.0 | `evo vpc create` и MCP `vpc_create` с занятым именем — `USAGE`, код 1, `existing_id`, до согласия (было: с `--yes` код 0 и вторая VPC, без `--yes` код 3); превью без ключа или проекта — код 2 | две VPC с одним именем: поиск по имени после потерянного ответа (`lostCreate`) называет не ту (матрица сбоев 6.1); условия 1–5; DECISIONS D23 |
| 1.3.0 | MCP по HTTP: тело запроса больше 4 МБ — `413` (было 16 МБ, как по stdio); `s3_put` файла больше ~3 МБ по HTTP не проходит | отказ в обслуживании (условие 1): 32 вызова × 16 МБ — до 512 МБ одних тел; условия 1–5; DECISIONS D24, строка H12 в `docs/mcp.md` |
| 1.3.0 | создание с ответом без id — `API_ERROR`, код 5 (было: код 0 с пустым id); с потерянным ответом — код 0 с id ресурса, если он найден по имени (было: ошибка обрыва): `evo kafka create`, `evo redis create`, `evo pg create`, `evo ar create`, `evo snat create`, `evo ca deploy`, `evo disk create`, `evo dns zone-create`, `evo secret create`, `evo vpc create`, `evo k8s create`, `evo lb create`, `evo k8s add-nodepool`, `evo pg backup-create`, `evo tg create`, `evo vpc route-create`, `evo dns record-create`, `evo cert issue-le`, `evo cert import`, `evo image create` и MCP `kafka_create`, `redis_create`, `pg_create`, `ar_create`, `snat_create`, `ca_deploy`, `disk_create`, `dns_zone_create`, `secret_create`, `vpc_create`, `k8s_create`, `lb_create`, `k8s_add_nodepool`, `pg_backup_create`, `tg_create`, `vpc_route_create`, `dns_record_create`, `cert_issue_le`, `cert_import`, `image_create` | оплачиваемый ресурс оставался неназванным (матрица сбоев 6.1); условия 1–5; DECISIONS D25 |
| 1.3.0 | повторное создание с занятым именем — `USAGE`, код 1, `existing_id` (было: код 0 и второй объект): `evo k8s add-nodepool`, `evo pg backup-create`, `evo tg create`, `evo vpc route-create` — до согласия, превью без ключа или проекта — код 2; `evo dns record-create`, `evo cert issue-le`, `evo cert import` — после согласия; MCP `k8s_add_nodepool`, `pg_backup_create`, `tg_create`, `vpc_route_create`, `dns_record_create`, `cert_issue_le`, `cert_import` | повтор после потерянного ответа создавал второй объект, вторая группа узлов — платные узлы (матрица сбоев 6.1); условия 1–5; DECISIONS D28 |
| 1.3.0 | повторное удаление ресурса, которого нет, — код 0, `status: already_deleted`, `existed: false`, без вопроса о согласии (было: `NOT_FOUND`/`VM_NOT_FOUND`, код 4, у многих после согласия; `evo vpc fip-release --ip-address` с адресом, которого нет, — `USAGE`, код 1): `evo vm delete`, `evo disk delete`, `evo image delete`, `evo vpc fip-release`, `evo snat delete`, `evo vpc delete`, `evo vpc route-delete`, `evo k8s delete`, `evo pg delete`, `evo pg db-delete`, `evo pg user-delete`, `evo kafka delete`, `evo redis delete`, `evo lb delete`, `evo tg delete`, `evo dns zone-delete`, `evo dns record-delete`, `evo cert delete`, `evo ar delete`, `evo ca delete`, `evo secret delete`, `evo s3 rm`, `evo s3 bucket-delete` и MCP `destroy`, `disk_delete`, `image_delete`, `release_floating_ip`, `snat_delete`, `vpc_delete`, `vpc_route_delete`, `k8s_delete`, `pg_delete`, `pg_db_delete`, `pg_user_delete`, `kafka_delete`, `redis_delete`, `lb_delete`, `tg_delete`, `dns_zone_delete`, `dns_record_delete`, `cert_delete`, `ar_delete`, `ca_delete`, `secret_delete`, `s3_rm`, `s3_bucket_delete`; значение не той формы (не UUID там, где спецификация требует UUID) — `USAGE`, код 1; превью `evo disk delete`, `evo image delete`, `evo vpc fip-release`, `evo vpc route-delete`, `evo pg db-delete`, `evo pg user-delete`, `evo cert delete`, `evo ca delete`, `evo secret delete`, `evo s3 rm`, `evo s3 bucket-delete`, `evo dns zone-delete` и `evo dns record-delete` с `--type` читают облако — без ключа или проекта код 2 | повтор безопасен (`CLAUDE.md` §1): повтор после потерянного ответа и teardown после частичного удаления падали на уже удалённом; согласие спрашивалось на удаление того, чего нет; условия 1–5; DECISIONS D26 |
| 1.3.0 | `evo ar create` с занятым именем, `evo k8s create` с планом, который не разрешается (конфигурация, зона, подсеть, сервисный аккаунт), `evo k8s create` и `evo k8s add-nodepool` с явным flavor с GPU или не из каталога, `evo k8s add-nodepool`, которому не разрешаются зона кластера, подсеть узлов или группа безопасности, и MCP `ar_create`, `k8s_create`, `k8s_add_nodepool` — `USAGE`, код 1, или `CONFIG_INVALID`, код 2 (кластер, которого нет, — `NOT_FOUND`, код 4), до согласия (было: код 3 без `--yes`, отказ после «да»); превью `ar_create` без ключа или проекта — код 2 | согласие на вызов, который не пройдёт, и на кластер с невидимым flavor (FB123-4…7); условия 1–5; DECISIONS D29 |
| 1.3.0 | имя нового ресурса с пробелом по краям, пустое после обрезки, с управляющим символом или длиннее 255 символов — `USAGE`, код 1, до облака (было: код 0 и ресурс с таким именем): `evo ar create`, `evo cert import`, `evo cert issue-le`, `evo dns record-create`, `evo dns zone-create`, `evo k8s add-nodepool`, `evo kafka create`, `evo lb create`, `evo pg backup-create`, `evo pg db-create`, `evo pg user-create`, `evo redis create`, `evo tg create` и MCP `ar_create`, `cert_import`, `cert_issue_le`, `dns_record_create`, `dns_zone_create`, `k8s_add_nodepool`, `kafka_create`, `lb_create`, `pg_backup_create`, `pg_db_create`, `pg_user_create`, `redis_create`, `tg_create` | `"web "` проходило мимо занятого `"web"` и создавало второй платный ресурс (Cursor 1.2.3); условия 1–5; DECISIONS D30 |
| 1.3.0 | `evo deploy --dry-run` без ключа — превью, код 0 (было: `AUTH_REQUIRED`, код 2); сервер `evo mcp --allow-project`: `vm_get` с именем ВМ в `vm_id` находит её (было: `NOT_FOUND`), ошибка настройки сервера при проверке ресурса — своя (было: `NOT_FOUND`); `evo vpc fip-release` (MCP `release_floating_ip`) с адресом шлюза SNAT или занятым ВМ — `USAGE`, код 1, до согласия (было: код 3 без `--yes`, отказ после «да») | ошибка настройки пряталась под «нет ресурса» (FB121-2, FB121-5); dry run требовал ключ, чтобы посмотреть (REL12-7); согласие на вызов, который не пройдёт (A6); условия 1–5; DECISIONS D31 |
| 1.3.0 | MCP по HTTP на сервере без `kubectl`/`helm` (H1): `k8s_kubectl`, `k8s_apply`, `k8s_helm` с аргументом, которого инструмент не принимает, неверного типа или вне enum — `reason` `USAGE` (было: `CONFIG_INVALID`, «kubectl is not installed») | агент получал неверную причину отказа (G13-1); любой порядок, закрывающий G13-1, меняет `reason` хотя бы одного сочетания — выбран минимальный; условия 1–5; DECISIONS D33 |

0. Стабильный релиз — до тега отдельным PR в `main`:
   - фрагменты `changelog.d/` — в «[Не выпущено]»: `python3
     scripts/changelog.py fold` (`changelog.d/README.md`);
   - `CHANGELOG.md`: «[Не выпущено]» → «[X.Y.Z] — ГГГГ-ММ-ДД» (дата тега)
     и новый пустой «[Не выпущено]» сверху; ломающие изменения (если minor
     — только как исправления, см. выше) — в разделе «Миграция» в начале версии;
   - `README.md`, раздел «Что сейчас можно (vX.Y.Z, …)»: версия, дата и
     главное новое этой версии;
   - убрать «до выхода `v1.0.0` стабильного релиза нет» и примеры с
     `CLOUDRU_VM_VERSION=v1.0.0-rc.N` из `README.md`, `docs/install.md`,
     `docs/guide.md`: `install.sh` без версии ставит последний стабильный;
   - cloud-e2e на этом коммите зелёный (ru.AZ-2), `cleanup_only` — без остатков.
     Его job ждут одобрения владельца в окружении `cloud-e2e`: Actions → прогон →
     **Review deployments** → Approve. Ключи Cloud.ru (`CLOUDRU_KEY_ID`,
     `CLOUDRU_SECRET`, `CLOUDRU_PROJECT_ID`, `CLOUDRU_S3_*`) лежат только в
     секретах окружений: `cloud-e2e` — ключ с правом изменений, с одобрением;
     `cloud-readonly` — ключ наблюдателя для `api-probe`, job `readonly` и
     `cloud-readonly-os` (те же чтения на Windows и macOS), без одобрения. В секретах репозитория их быть не должно — их видит любой
     workflow без `environment:`.
1. `main` зелёный в CI (в том числе `TestCommandsDoc` и `TestToolsDoc`: `docs/commands.md` и `docs/tools.md` совпадают с бинарём); локально `goreleaser check` и, по желанию,
   `goreleaser release --snapshot --clean --skip=sign,sbom` — в `dist/` 6 архивов (в каждом `evo` и alias `cloudru-vm` до v2.0) и `checksums.txt`; с `syft` в `PATH` и без `sbom` в `--skip` — ещё SBOM каждого архива.
2. `git tag -a v1.2.3 -m v1.2.3 && git push origin v1.2.3` — тег ставится на коммит в `main`.
   Если тег не запушить (облачная сессия пушит только свои ветки): Actions →
   **tag** → Run workflow с `tag` и полным `sha` коммита в `main`
   (`gh workflow run tag.yml -f tag=v1.2.3 -f sha=…`). Он ставит тег и сам
   запускает `release.yml` на нём; identity подписи та же (`release.yml@refs/tags/…`).
   Тег может поставить и агент (через `tag.yml`), но выпуск ждёт владельца:
   первый job `release.yml` — в окружении `release` (одобряет только владелец,
   только для тегов `v*`). Actions → прогон release → **Review deployments** →
   Approve. Отклонённый прогон оставляет тег без релиза; номер не
   переиспользуется, следующий — X.Y.(Z+1).
3. `release.yml` после одобрения гоняет весь `ci.yml`, затем goreleaser: архивы, SBOM архивов
   (`*.sbom.json`, SPDX), `checksums.txt`, подписи cosign keyless (`*.sigstore.json`), GitHub
   Release; затем attestation происхождения каждого файла из `checksums.txt`. Образ MCP-сервера
   подписывается cosign по digest. `-rc.N` публикуется как pre-release.
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

   С 1.3.0 (решение владельца В8) ещё три проверки:

   ```
   # SBOM архива — в checksums.txt, sha256sum -c выше проверил и его
   jq '.packages | length' evo_1.3.0_linux_amd64.tar.gz.sbom.json
   # происхождение: собран release.yml этого репозитория из тега
   gh attestation verify evo_1.3.0_linux_amd64.tar.gz --repo shatim-ops/evo-cli \
     --signer-workflow shatim-ops/evo-cli/.github/workflows/release.yml
   # образ MCP-сервера — только когда он публикуется (D32): подпись того же
   # workflow; полное имя образа — в строке «Образ опубликован» лога job image
   cosign verify <реестр>.cr.cloud.ru/evo:1.3.0 \
     --certificate-identity-regexp '^https://github.com/shatim-ops/evo-cli/\.github/workflows/release\.yml@refs/tags/v' \
     --certificate-oidc-issuer https://token.actions.githubusercontent.com
   ```

   Впервые — на `1.3.0-rc.1`: все шесть архивов с SBOM в `checksums.txt`,
   `gh attestation verify` проходит (проверено 23.10). Образ пока не
   публикуется (D32: общий MCP-сервер не делаем, только локальная установка),
   поэтому `cosign verify` образа — с первого релиза, в котором образ выйдет.
   Если тогда реестр не примет подпись (тег `sha256-….sig`), job `image`
   красная, а образ уже опубликован без подписи: это находка rc, до
   стабильного тега.

5. Неудачный релиз. Опубликованные тег и Release **не удаляются**: архивы
   уже могли скачать, а подписи лежат в журнале прозрачности Rekor.
   - Release помечается **pre-release**, к заголовку — «Отозван: ставьте
     X.Y.(Z+1)», в описании — что сломано. `install.sh` без версии берёт
     `/releases/latest`, куда pre-release не попадает, — установка по
     умолчанию сразу возвращается к предыдущему стабильному.
   - Кто уже поставил: `CLOUDRU_VM_VERSION=<предыдущий тег>` в команде
     установки — откат одной командой (`docs/install.md`).
   - Docker-образ: `:latest` сдвинулся на сломанную версию; до patch —
     вернуть `:latest` на предыдущий тег в реестре вручную.
   - Исправление — следующий patch; опубликованный номер не
     переиспользуется.
   - Тег удаляется (`git push --delete origin v1.2.3`) только если
     `release.yml` не дошёл до публикации: одобрение отклонено или сбой до
     goreleaser. Номер и тогда не переиспользуется.

6. Передача дел — после тега и до слияния следующего PR в `main`: релизная сессия пишет `docs/internal/handoff/<версия>.md`
   по шаблону `docs/internal/handoff/README.md` (выпущено, открыто,
   следующая версия, сессии) и сливает его вместе с отметкой версии в
   `docs/internal/release-plan.md`. Без передачи релиз не закрыт: новая
   сессия берёт контекст только из файлов (`CLAUDE.md`, «Старт сессии»).

### Release candidate

Minor, который меняет поверхность для агентов (наборы инструментов, режимы
сервера MCP, новые проверки согласия), выходит сначала `-rc.1` на волну
догфуда; стабильный тег — через 2–3 дня без находок. Patch выходит без rc.
Перед тегом rc фрагменты `changelog.d/` вливаются в «[Не выпущено]» тем же
`python3 scripts/changelog.py fold` отдельным PR: `CHANGELOG.md` в архиве rc
называет всю версию, раздел версии не создаётся.

### Ветка релиза и hotfix

Пока `main` можно выпустить, patch идёт с `main`. Когда в `main` ложится
первая ломающая правка 2.0, от последнего тега 1.x создаётся ветка
`release-1.x`: patch 1.x — PR в неё и тег с неё. В тот же момент отдельным
PR `tag.yml` (сейчас ставит тег только на коммит из `main`) и окружение
`release` расширяются на `release-1.x`.

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
выпускаются всё равно. Сейчас секретов нет намеренно: образ не публикуется,
пока общий MCP-сервер не в плане (D32); собрать его можно из `Dockerfile`. Образ только `linux/amd64`; с 1.3.0 подписан cosign по digest (проверка — шаг 4).

## Документация

Справка по командам `docs/commands.md` генерируется из дерева команд;
после изменения команд, флагов или описаний инструментов:

```
go test ./internal/cli -run 'TestCommandsDoc|TestToolsDoc' -update-docs
```

`docs/tools.md` (каталог инструментов MCP) генерируется так же, из реестра
инструментов; оба теста идут в CI.

`TestAuthDoc_ListsEveryEnvVar` падает, если код читает переменную окружения,
не упомянутую в `docs/auth.md`.
