// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

// localCommand is a command of the CLI that calls no registry tool, and why.
type localCommand struct {
	Path   string // as cobra's CommandPath: "evo configure list"
	Reason string // in Russian: it goes to docs/commands.md (REQ-10)
}

// localCommands is the closed list of the commands that call no tool
// (docs/internal/cli-mcp-parity/spec.md, REQ-2; owner's decision Q2): every
// other command is built from a tool's CLISpec. TestParity_CommandsAndTools
// fails on a command that is neither, and on an entry that is no command.
var localCommands = []localCommand{
	{"evo configure", "пишет ключ доступа в профиль на диске той машины, где стоит evo; MCP-клиент ключей не передаёт по контракту (docs/mcp.md)"},
	{"evo configure list", "читает профили с диска"},
	{"evo configure delete", "удаляет профиль с диска"},
	{"evo project", "группа: project use — локальная, project list — команда org_projects"},
	{"evo project use", "пишет проект в профиль на диске"},
	{"evo init", "интерактивно выбирает зону и образ и пишет .evo.yaml в текущий каталог"},
	{"evo verify", "TCP/HTTP-проверки портов развёрнутого приложения с машины оператора: с хоста сервера MCP результат был бы другим"},
	{"evo version", "версия бинаря; MCP отдаёт её в initialize.serverInfo"},
	{"evo completion", "скрипты автодополнения оболочки; к облаку не обращается"},
	{"evo completion bash", "скрипт автодополнения bash"},
	{"evo completion zsh", "скрипт автодополнения zsh"},
	{"evo completion fish", "скрипт автодополнения fish"},
	{"evo completion powershell", "скрипт автодополнения PowerShell"},
	{"evo mcp", "сам сервер MCP"},
	{"evo mcp healthcheck", "проверка живости сервера MCP (HEALTHCHECK Docker-образа)"},
	{"evo tools", "доступ к реестру инструментов"},
	{"evo tools list", "список инструментов реестра"},
	{"evo tools describe", "описание и схема инструмента"},
	{"evo tools call", "универсальный вызов любого инструмента"},
	{"evo crew", "титры"},
	// Until PR 9 (spec.md REQ-11, owner's decision Q2): they become the
	// tools whoami and doctor.
	{"evo whoami", "профиль, маскированный Key ID, проект и срок токена с машины оператора; до PR 9 (REQ-11) — станет инструментом whoami"},
	{"evo doctor", "проверки файлов настроек, прав, PATH и связи с машины оператора; до PR 9 (REQ-11) — станет инструментом doctor"},
	{"evo help", "справка cobra"},
}
