# Подключить evo к AI-агенту одной вставкой

Вставьте своему агенту (Claude Code, Claude Desktop, Cursor, VS Code,
Gemini CLI, Codex) одну строку:

```text
Настрой evo по https://github.com/shatim-ops/evo-cli/blob/main/docs/agent-setup.md
```

Агент проверит evo, запустит вход в Cloud.ru и подключит evo к себе. От вас —
одно действие: на странице **Evolution CMCP** в браузере выбрать проект и
нажать «Выдать доступ». Ключ агенту выдаёте вы: агент не может нажать эту
кнопку за вас, а секрет ключа не проходит ни через чат, ни через браузер.
Что даёт ключ и как его отозвать — [auth.md](auth.md#вход-через-браузер-evo-login).

Вход через браузер работает, когда агент и браузер — на одной машине. По SSH
и в контейнере его нет: тогда ключ создаёте вы в личном кабинете Cloud.ru и
выполняете `evo configure` сами ([auth.md](auth.md#2-ключ-доступа)).

---

## Instructions for the agent

You are setting up evo, the CLI and MCP server for Cloud.ru Evolution, for
the person you work with. Follow the steps in order. Tell the person what
you do at each step, in their language.

### Rules

- **Only the person grants access.** The access key is issued when the
  person presses «Выдать доступ» on the Evolution CMCP page in their
  browser. Never open, fetch or submit that page or any `127.0.0.1` address
  yourself (no browser tool, no curl, no HTTP request). `evo login` has no
  flag that grants access; do not look for one. Do not paste the link
  `evo login` prints into the chat, and do not ask the person to send it to
  you.
- **Never handle the secret.** Do not ask the person for a key or secret in
  the chat, do not print or read evo's configuration or credentials files,
  do not pass a secret on a command line.
- **Ask before you install or write.** Show the command or the diff, and run
  it after the person agrees.
- **Stop when a step fails** and tell the person the error as evo printed
  it; do not work around it.

### 1. Check evo

Run `evo version`.

- It prints a version: go to step 2.
- `evo` is not found: ask the person to install it. On Linux and macOS the
  command is
  `curl -fsSL https://raw.githubusercontent.com/shatim-ops/evo-cli/main/install.sh | sh`
  (run it only after they agree). On Windows there is no installer: send
  them to
  https://github.com/shatim-ops/evo-cli/blob/main/docs/install.md#windows.
  Then run `evo version` again.

### 2. Sign in: the person grants access

Check where you run. If you run over SSH, in a container or on another
machine than the person's browser, the browser sign-in cannot work: tell the
person to create an access key in the Cloud.ru console and run
`evo configure --profile agent` themselves
(https://github.com/shatim-ops/evo-cli/blob/main/docs/auth.md), wait until
they say it is done, and go to step 3.

Otherwise run, in the person's terminal on this machine:

```sh
evo login --for agent
```

Tell the person before it starts: «Откроется страница Evolution CMCP. Выберите
проект и нажмите «Выдать доступ» — это делаете только вы». evo opens the
browser and waits up to 5 minutes. If the browser does not open, the person
opens the link evo printed, on this same machine. Wait for the command to
end; do not answer its questions for the person — if it asks in the
terminal, relay the question.

- Exit code 0: the key is in the profile `agent`. Go to step 3.
- Any other code: stop and show the person what evo printed. The page
  closed without «Выдать доступ» means the person did not grant access;
  ask whether to try again.

### 3. Connect evo to yourself

Pick your client:

| You are | `--client` |
|---|---|
| Claude Code | `claude-code` |
| Claude Desktop | `claude-desktop` |
| Cursor | `cursor` |
| VS Code (Copilot) | `vscode` |
| Gemini CLI | `gemini` |
| Codex | `codex` |

Show the change first:

```sh
evo agent setup --client <client> --profile agent --dry-run
```

Show the person the diff, and after they agree run the same command with
`--yes` instead of `--dry-run`. If your client is not in the table, run
`evo agent setup --client cursor --profile agent --print json` and tell the
person where their client keeps its MCP servers.

### 4. Hand over

Tell the person to restart the client (Claude Desktop: quit it fully and
start it again) or reload its MCP servers, then come back and work. After
the restart, check that the `evo` tools are there; `evo doctor --profile
agent` checks the key and the connection to Cloud.ru from the terminal.

The key has the role «Администратор проекта» on the chosen project for the
term the person chose. A change in the cloud comes with evo's preview, and
the approval of the tool call in the client is the person's guard — not
yours to give on their behalf. To close access early:
`evo logout --profile agent`.
