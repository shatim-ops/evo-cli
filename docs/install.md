# Установка

Готовые бинари лежат в [GitHub Releases](https://github.com/shatim-ops/evo-cli/releases):
Linux, macOS и Windows, каждая для amd64 и arm64. К каждому релизу приложены
`checksums.txt` (sha256 всех архивов) и подписи cosign keyless
(`*.sigstore.json`). Бинарь статический и ничего, кроме себя, не требует —
кроме `kubectl`/`helm` для нескольких команд `k8s` (см. [ниже](#kubectl-и-helm)).

## Linux и macOS: install.sh

```sh
curl -fsSL https://raw.githubusercontent.com/shatim-ops/evo-cli/main/install.sh | sh
evo version
```

Если скрипт напишет, что `~/.local/bin` нет в `PATH`, в zsh (macOS) добавьте
его так: `echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc && source ~/.zshrc`.
В примерах документации строки с `#` — пояснения; чтобы zsh не ругался на них
при копировании, включите `setopt interactivecomments` (так же, через `~/.zshrc`).

Скрипт:

1. определяет ОС и архитектуру (`uname`);
2. скачивает архив и `checksums.txt` нужного релиза;
3. если в `PATH` есть [`cosign`](https://docs.sigstore.dev/cosign/system_config/installation/),
   проверяет подпись `checksums.txt` (identity — workflow `release.yml` этого
   репозитория на теге релиза); без cosign пишет, что подпись не проверена;
4. сверяет sha256 архива и кладёт бинарь в `~/.local/bin` (под root — в
   `/usr/local/bin`); если каталога нет в `PATH`, подскажет строку для профиля
   оболочки.
5. ставит автодополнение по Tab туда, где оболочка находит его сама, без
   правки её конфигов: fish — `~/.config/fish/completions/evo.fish`, bash —
   `~/.local/share/bash-completion/completions/evo` (нужен пакет
   bash-completion), zsh — каталог из `$fpath` в домашнем каталоге, если он
   там есть. Иначе для zsh, а также для PowerShell, печатает одну строку для
   `~/.zshrc` или профиля. Меню стрелками скрипт не включает: это настройка
   оболочки.

Переменные скрипта:

| Переменная | Что задаёт | По умолчанию |
|---|---|---|
| `CLOUDRU_VM_VERSION` | тег релиза, например `v1.4.0` | последний стабильный; pre-release (`-rc.N`) надо назвать явно |
| `INSTALL_DIR` | каталог для бинаря | `~/.local/bin`, под root `/usr/local/bin` |
| `CLOUDRU_VM_BASE_URL` | откуда качать вместо GitHub (зеркало); ожидается `<base>/<тег>/<файл>` | GitHub Releases |

```sh
# конкретная версия, в свой каталог
curl -fsSL https://raw.githubusercontent.com/shatim-ops/evo-cli/main/install.sh \
  | CLOUDRU_VM_VERSION=v1.4.0 INSTALL_DIR="$HOME/bin" sh
```

Без `CLOUDRU_VM_VERSION` скрипт ставит последний стабильный релиз; кандидаты
в релиз (`-rc.N`) — только с явной версией. Релизы до
переименования (`v1.0.0-rc.1`) выпущены под старым именем — архив
`cloudru-vm_<версия>_…` с одним бинарём `cloudru-vm`; скрипт узнаёт это по
`checksums.txt` релиза и ставит `cloudru-vm`. С `v1.0.0-rc.2` — `evo` и alias
`cloudru-vm`. Обновление — тот же запуск: бинарь и файлы автодополнения
заменяются, чужие файлы не трогаются. Удаление бинарей и файлов
автодополнения, которые поставил скрипт:

```sh
curl -fsSL https://raw.githubusercontent.com/shatim-ops/evo-cli/main/install.sh | sh -s -- --uninstall
```

Данные evo (профили, кэш) остаются, см.
[auth.md → «Что хранится на машине»](auth.md#что-хранится-на-машине).

## Linux и macOS вручную

```sh
# OS: linux | darwin; ARCH: amd64 | arm64
V=1.3.0; OS=linux; ARCH=amd64
base=https://github.com/shatim-ops/evo-cli/releases/download/v$V
curl -fLO "$base/evo_${V}_${OS}_${ARCH}.tar.gz"
curl -fLO "$base/checksums.txt"
curl -fLO "$base/checksums.txt.sigstore.json"
# проверка подписи и хэша — раздел ниже
tar -xzf "evo_${V}_${OS}_${ARCH}.tar.gz" evo
install -m 0755 evo ~/.local/bin/
```

В архиве есть и `cloudru-vm` — тот же бинарь под старым именем (alias до
v2.0, см. [rename.md](rename.md)). Нужен он,
только если скрипты или конфиг MCP ещё вызывают `cloudru-vm`; `install.sh`
ставит его рядом с `evo`.

## Windows

Скрипта-установщика для Windows пока нет.

1. Скачайте из релиза `evo_<версия>_windows_amd64.zip` (или `_arm64`) и
   `checksums.txt`.
2. Сверьте хэш в PowerShell — значение должно совпасть со строкой архива в
   `checksums.txt`:

   ```powershell
   (Get-FileHash .\evo_1.3.0_windows_amd64.zip -Algorithm SHA256).Hash.ToLower()
   Select-String -Path .\checksums.txt -Pattern windows_amd64
   ```

3. Распакуйте `evo.exe`, например, в `%LOCALAPPDATA%\Programs\evo`
   (`cloudru-vm.exe` в архиве — alias со старым именем до v2.0, нужен только
   старым скриптам).
4. Добавьте этот каталог в `Path` пользователя: «Параметры → Система → О
   программе → Дополнительные параметры системы → Переменные среды», или в
   PowerShell:

   ```powershell
   $dir = "$env:LOCALAPPDATA\Programs\evo"
   [Environment]::SetEnvironmentVariable("Path", [Environment]::GetEnvironmentVariable("Path", "User") + ";$dir", "User")
   ```

5. Откройте новый терминал: `evo version`.

На Windows вместо прав `0600` файлы с ключами получают собственный ACL без
наследования: доступ только у вас, SYSTEM и Администраторов (подробнее —
[docs/auth.md](auth.md)). Команды `k8s kubectl/apply/helm` на Windows —
без гарантий (см. [ниже](#kubectl-и-helm)).

## Проверка подписи

Релизы подписывает workflow `release.yml` через cosign keyless: сертификат
выдаётся на OIDC-токен GitHub Actions, запись ложится в публичный журнал
Rekor. Ключей у проекта нет — проверяется *кто* подписал (identity workflow).

```sh
V=v1.4.0
cosign verify-blob --bundle checksums.txt.sigstore.json \
  --certificate-identity "https://github.com/shatim-ops/evo-cli/.github/workflows/release.yml@refs/tags/$V" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
sha256sum --ignore-missing -c checksums.txt     # macOS: shasum -a 256 --ignore-missing -c checksums.txt
```

Подпись — в формате бандла sigstore v0.3 (её ставит cosign v3). Нужен cosign
v3 или v2.4+; у cosign v2 добавьте `--new-bundle-format`, иначе он ответит
«bundle does not contain cert for verification». `install.sh` пробует оба
варианта сам.

`v1.0.0-rc.1` и `v1.0.0-rc.2` подписаны до переименования репозитория: для
них в identity — `shatim-ops/cloudru-vm-cli` вместо `shatim-ops/evo-cli`
(`install.sh` это учитывает).

`Verified OK` от cosign и `OK` у вашего архива — бинарь собран из этого
репозитория в GitHub Actions на этом теге и не изменён. У каждого архива есть и
собственная подпись (`<архив>.sigstore.json`), проверяется так же.

## Gatekeeper (macOS) и SmartScreen (Windows)

Бинари подписаны cosign, но **не нотаризованы Apple и не подписаны
Authenticode**.

- **macOS.** После `install.sh` или `curl` бинарь запускается без вопросов:
  curl не ставит атрибут карантина. Если архив скачан браузером, macOS скажет
  «не удаётся проверить разработчика». После проверки sha256 снимите карантин:
  `xattr -d com.apple.quarantine ~/.local/bin/evo`.
- **Windows.** SmartScreen может показать «Windows защитила ваш компьютер» —
  «Подробнее» → «Выполнить в любом случае». Или до первого запуска, после
  проверки хэша: `Unblock-File .\evo.exe`.

## kubectl и helm

Почти все команды работают без внешних программ: API Cloud.ru и SSH
реализованы в самом бинаре. Исключение — три команды, которые управляют
содержимым кластера Kubernetes:

| Команда (инструмент MCP) | Нужна программа |
|---|---|
| `k8s kubectl` (`k8s_kubectl`) | `kubectl` |
| `k8s apply` (`k8s_apply`) | `kubectl` |
| `k8s helm` (`k8s_helm`) | `helm` |

Программы ищутся в `PATH`, затем в `~/.local/bin`, `/usr/local/bin`,
`/usr/bin`. Установка: [kubectl](https://kubernetes.io/docs/tasks/tools/),
[helm](https://helm.sh/docs/intro/install/). Проверить, видит ли их
evo, — `evo doctor` (отсутствие — предупреждение, не ошибка).

Ограничения:

- в **Docker-образе** MCP-сервера `kubectl` и `helm` нет — эти три
  инструмента отвечают ошибкой «not available in this mode»; используйте
  `k8s kubeconfig` и свой `kubectl` (см. [mcp.md](mcp.md#ограничения-http-режима));
- флаги evo (`--json`, `--yes`, `--dry-run`, `--profile`) у `k8s kubectl`
  ставятся до идентификатора кластера: `evo k8s kubectl --json <cluster-id>
  get pods`. Всё после него уходит в `kubectl` как есть, поэтому
  `--dry-run=server` после кластера — флаг kubectl; `--json` и `--yes` там
  отклоняются с подсказкой (кроме части после `--`, команды в контейнере);
- на **Windows** — без гарантий: поиск путей рассчитан на Unix, работает,
  только если `kubectl.exe`/`helm.exe` лежат в `PATH`.

## Сборка из исходников

```sh
git clone https://github.com/shatim-ops/evo-cli && cd evo-cli
go build -o bin/evo ./cmd/evo
```

Нужен Go версии из `go.mod` (CI собирает на Go 1.26.10). `evo version`
у такой сборки показывает версию модуля и коммит из данных VCS.
