# Подключить evo к AI-агенту одной вставкой

Вставьте своему агенту (Claude Code, Claude Desktop, Cursor, VS Code,
Gemini CLI, Codex) одну строку:

```text
Настрой evo по https://github.com/shatim-ops/evo-cli/blob/main/docs/agent-setup.md
```

Агент сам установит или обновит evo, запустит вход в Cloud.ru и подключит evo к себе. От вас —
одно действие: на странице **Evolution CMCP** в браузере выбрать проект и
нажать «Выдать доступ». Ключ агенту выдаёте вы: агент не может нажать эту
кнопку за вас, а секрет ключа не проходит ни через чат, ни через браузер.
Что даёт ключ и как его отозвать — [auth.md](auth.md#вход-через-браузер-evo-login).

Вход через браузер работает, когда агент и браузер — на одной машине. По SSH
и в контейнере его нет: тогда ключ создаёте вы в личном кабинете Cloud.ru и
выполняете `evo configure` сами ([auth.md](auth.md#2-ключ-доступа)).

---

## Инструкция для агента

Ты подключаешь evo — CLI и MCP-сервер для Cloud.ru Evolution — для человека,
с которым работаешь. Выполняй шаги по порядку. На каждом шаге говори
человеку, что делаешь, на его языке.

### Правила

- **Доступ выдаёт только человек.** Ключ доступа появляется, когда человек
  нажимает «Выдать доступ» на странице Evolution CMCP в своём браузере. Сам
  никогда не открывай, не запрашивай и не отправляй эту страницу и любой
  адрес `127.0.0.1` — ни браузерным инструментом, ни curl, ни другим
  HTTP-запросом. У `evo login` нет флага, который выдаёт доступ, — не ищи
  его. Ссылку, которую печатает `evo login`, не пересказывай, не отправляй и
  не открывай: ни в чат, ни в другой канал, ни браузерным инструментом. Не
  проси человека прислать её тебе — человек открывает её сам из терминала на
  этой же машине.
- **Не касайся секрета.** Не проси у человека ключ или секрет в чате, не
  печатай и не читай файлы настроек и ключей evo, не передавай секрет в
  командной строке.
- **Установку evo делай сам, запись в настройки клиента — после согласия.**
  Строка «Настрой evo по…» — согласие человека на установку evo (шаг 1):
  скажи, что ставишь, и ставь. Изменение настроек клиента (шаг 3) покажи
  diff'ом и выполняй после согласия человека.
- **На ошибке остановись** и покажи человеку ошибку так, как её напечатал
  evo; не обходи её.

### 1. Установи или обнови evo

Выполни `evo version`.

- Печатает версию 1.9.0 или новее — используй уже установленный evo и переходи к шагу 2. Для этого шага cosign не требуется.
- `evo` не найден или версия старше — установи его сам, сказав человеку,
  что ставишь. Для установки агентом нужен заранее установленный доверенный
  cosign v3. Если его нет, остановись и попроси человека
  установить cosign или самостоятельно установить evo по инструкции.
  Проверь подпись списка сумм выпуска и SHA-256 архива перед распаковкой.
  - Linux и macOS — выполни блок в локальной оболочке. Он ставит проверенные
    бинарники в `~/.local/bin`. Настройки оболочки и автодополнение человек
    может подключить отдельно по [инструкции установки](install.md).

    ```sh
    (
      set -eu
      command -v cosign >/dev/null || { echo 'Для установки нужен cosign v3' >&2; exit 1; }
      fetch() { curl --proto '=https' --tlsv1.2 -fsSL "$@"; }
      repo=https://github.com/shatim-ops/evo-cli
      release=$(fetch -o /dev/null -w '%{url_effective}' "$repo/releases/latest")
      tag=${release##*/}
      case "$tag" in v[0-9]*) ;; *) exit 1 ;; esac
      case "$(uname -s)" in Linux) os=linux ;; Darwin) os=darwin ;; *) exit 1 ;; esac
      case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; arm64|aarch64) arch=arm64 ;; *) exit 1 ;; esac
      archive="evo_${tag#v}_${os}_${arch}.tar.gz"
      base="$repo/releases/download/$tag"
      tmp=$(mktemp -d)
      trap 'rm -rf "$tmp"' EXIT
      cd "$tmp"
      for file in checksums.txt checksums.txt.sigstore.json "$archive"; do fetch -o "$file" "$base/$file"; done
      cosign verify-blob --bundle checksums.txt.sigstore.json \
        --certificate-identity "$repo/.github/workflows/release.yml@refs/tags/$tag" \
        --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
      want=$(awk -v file="$archive" '$2 == file {print $1}' checksums.txt)
      if command -v sha256sum >/dev/null; then sum=$(sha256sum "$archive"); else sum=$(shasum -a 256 "$archive"); fi
      [ "$want" = "${sum%% *}" ] || { echo 'sha256 mismatch' >&2; exit 1; }
      tar -xzf "$archive" evo cloudru-vm
      dest="$HOME/.local/bin"
      mkdir -p "$dest"
      for bin in evo cloudru-vm; do
        staged=$(mktemp "$dest/.$bin.XXXXXX")
        if install -m 0755 "$bin" "$staged" && mv -f "$staged" "$dest/$bin"; then :; else rm -f "$staged"; exit 1; fi
      done
    )
    ```
  - Windows (PowerShell; для ARM — `$arch = "arm64"`):

    ```powershell
    $ErrorActionPreference = "Stop"
    $cosign = (Get-Command cosign -CommandType Application -ErrorAction Stop).Source
    $repo = "https://github.com/shatim-ops/evo-cli"
    $tag = (Invoke-RestMethod https://api.github.com/repos/shatim-ops/evo-cli/releases/latest).tag_name
    $v = $tag.TrimStart("v")
    $arch = "amd64"
    $dir = "$env:LOCALAPPDATA\Programs\evo"
    $base = "$repo/releases/download/$tag"
    $zip = "evo_${v}_windows_$arch.zip"
    $tmp = New-Item -ItemType Directory (Join-Path $env:TEMP ([guid]::NewGuid().ToString()))
    try {
      foreach ($file in @($zip, "checksums.txt", "checksums.txt.sigstore.json")) {
        Invoke-WebRequest "$base/$file" -OutFile "$tmp\$file"
      }
      & $cosign verify-blob --bundle "$tmp\checksums.txt.sigstore.json" --certificate-identity "$repo/.github/workflows/release.yml@refs/tags/$tag" --certificate-oidc-issuer https://token.actions.githubusercontent.com "$tmp\checksums.txt"
      if ($LASTEXITCODE -ne 0) { throw "cosign verification failed" }
      $entries = @(Select-String -Path "$tmp\checksums.txt" -Pattern ("^([0-9a-f]{64})  " + [regex]::Escape($zip) + "$"))
      if ($entries.Count -ne 1) { throw "missing or duplicate archive checksum" }
      $want = $entries[0].Matches[0].Groups[1].Value
      if ($want -ne (Get-FileHash "$tmp\$zip" -Algorithm SHA256).Hash) { throw "sha256 mismatch" }
      Expand-Archive "$tmp\$zip" -DestinationPath $dir -Force
    } finally {
      Remove-Item -LiteralPath $tmp.FullName -Recurse -Force
    }
    if (($env:Path -split ';') -notcontains $dir) { [Environment]::SetEnvironmentVariable("Path", [Environment]::GetEnvironmentVariable("Path", "User") + ";$dir", "User"); $env:Path += ";$dir" }
    ```

    Скрипт кладёт `evo.exe` в `%LOCALAPPDATA%\Programs\evo` и, если этого
    каталога нет в `Path` пользователя, добавляет его туда навсегда — скажи
    об этом человеку до запуска.

  Затем снова выполни `evo version`. Если `evo` не находится, а установщик
  сказал, что каталога нет в `PATH`, — запускай `~/.local/bin/evo` (Windows —
  `$env:LOCALAPPDATA\Programs\evo\evo.exe`) полным путём до конца
  настройки и скажи человеку, какую строку добавить в профиль оболочки.
- Если `evo version` после установки по-прежнему печатает старую версию,
  evo стоит ещё где-то (Homebrew, другой каталог в `PATH`): не ставь
  повторно — назови человеку путь (`command -v evo`, на Windows
  `(Get-Command evo).Source`) и спроси, какую копию оставить.
- Установка упала (нет сети, хэш не сошёлся) — не обходи проверку и не
  качай бинарь другим способом: покажи человеку ошибку и ссылку
  https://github.com/shatim-ops/evo-cli/blob/main/docs/install.md.

### 2. Вход: доступ выдаёт человек

Проверь, где ты работаешь. Если ты работаешь по SSH, в контейнере или на
другой машине, чем браузер человека, вход через браузер не сработает: попроси
человека создать ключ доступа в личном кабинете Cloud.ru и самому выполнить
`evo configure --profile agent`
(https://github.com/shatim-ops/evo-cli/blob/main/docs/auth.md), дождись, когда
он скажет, что готово, и переходи к шагу 3.

Иначе выполни в терминале человека на этой машине:

```sh
evo login --for agent
```

До запуска скажи человеку: «Откроется страница Evolution CMCP. Выберите
проект и нажмите «Выдать доступ» — это делаете только вы». evo откроет
браузер и будет ждать до 5 минут. Если браузер не открылся, скажи
человеку: «Откройте ссылку, которую evo напечатал в терминале, в браузере на
этой же машине». Саму ссылку не пересказывай и не открывай. Если человек
не видит терминал, в котором ты запустил команду, остановись: прерви
`evo login` и попроси человека самому выполнить `evo login --for agent` в
своём терминале на этой машине и сказать, когда закончит. Дождись конца
команды; не отвечай на её вопросы за человека — если она спрашивает в
терминале, передай вопрос человеку.

- Код выхода 0: ключ записан в профиль `agent`. Переходи к шагу 3.
- Любой другой код: остановись и покажи человеку ошибку и подсказку, которые
  напечатал evo, — без ссылки входа. Если страницу закрыли без «Выдать
  доступ», человек доступ не выдал — спроси, попробовать ли ещё раз.

### 3. Подключи evo к себе

Выбери свой клиент:

| Ты работаешь в | `--client` |
|---|---|
| Claude Code | `claude-code` |
| Claude Desktop | `claude-desktop` |
| Cursor | `cursor` |
| VS Code (Copilot) | `vscode` |
| Gemini CLI | `gemini` |
| Codex | `codex` |

Сначала покажи изменение:

```sh
evo agent setup --client <клиент> --profile agent --dry-run
```

Покажи человеку diff и после его согласия выполни ту же команду с `--yes`
вместо `--dry-run`. Если твоего клиента нет в таблице, выполни
`evo agent setup --client cursor --profile agent --print json` и скажи
человеку, куда его клиент записывает MCP-серверы.

Без `--profile` клиент подключается к профилю по умолчанию, то есть к ключу
человека. Если в профиле `agent` есть ключ из `evo login --for agent`,
`evo agent setup` без `--profile` предупреждает об этом до diff и называет
команду с `--profile agent`.

### 4. Передай работу

Попроси человека перезапустить клиент (Claude Desktop — закрыть полностью и
открыть снова) или перечитать его MCP-серверы, потом вернуться и работать.
После перезапуска проверь, что инструменты `evo` на месте: вызови
инструмент `whoami` — он назовёт проект и роли ключа (из терминала то же
делает `evo doctor --profile agent`). Скажи человеку, что evo подключён, к
какому проекту, и спроси, с чего начать: например, показать ВМ проекта
(`vm_list`) или развернуть его `docker-compose`-проект (`deploy_plan`).

У ключа — роль «Администратор проекта» на выбранном проекте на срок, который
выбрал человек. Изменение в облаке приходит с превью evo, а одобрение вызова
инструмента в клиенте — защита человека: давать его за человека не тебе.
Закрыть доступ раньше срока — по тому, откуда ключ:

- Ключ из `evo login --for agent`: `evo logout --profile agent`. Команда
  удаляет этот ключ в облаке и профиль. Роль сервисного аккаунта человек
  снимает сам в личном кабинете Cloud.ru → «Сервисные аккаунты»; токены,
  выданные ключу раньше, действуют до времени, которое назовёт `evo logout`
  (не дольше часа).
- Ключ из `evo configure` или личного кабинета: `evo logout` его не удаляет,
  а удаляет только профиль. Ключ человек удаляет сам в личном кабинете
  Cloud.ru → «Сервисные аккаунты»; затем `evo logout --profile agent` убирает
  профиль с этой машины.

Подробно — [auth.md](auth.md#вход-через-браузер-evo-login).
