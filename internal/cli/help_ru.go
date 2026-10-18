// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// The help cobra prints around our texts (headings, the help and completion
// commands, the -h flag, parsing errors) is English. In Russian it is
// replaced here; in English nothing changes.

func init() {
	cobra.AddTemplateFunc("ruDefaults", ruDefaults)
	cobra.AddTemplateFunc("ruUseLine", ruUseLine)
}

// ruUseLine translates the "[flags]" cobra appends to a usage line.
func ruUseLine(s string) string {
	return strings.Replace(s, " [flags]", " [флаги]", 1)
}

// usageTemplateRU is cobra's default usage template with Russian headings.
const usageTemplateRU = `Использование:{{if .Runnable}}
  {{.UseLine | ruUseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [команда]{{end}}{{if gt (len .Aliases) 0}}

Псевдонимы:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Примеры:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Команды:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

Другие команды:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Флаги:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces | ruDefaults}}{{end}}{{if .HasAvailableInheritedFlags}}

Глобальные флаги:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces | ruDefaults}}{{end}}{{if .HasHelpSubCommands}}

Дополнительные разделы справки:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Справка по команде: "{{.CommandPath}} [команда] --help".{{end}}
`

// pflagDefaultRE is the "(default 100)" pflag appends to a flag usage line.
// Only at the end of a line: the English descriptions of the tree flags have
// their own "(default 2)." inside the text.
var pflagDefaultRE = regexp.MustCompile(`(?m) \(default ([^()\n]*)\)$`)

// ruDefaults translates the defaults pflag appends to flag usages.
func ruDefaults(s string) string {
	return pflagDefaultRE.ReplaceAllString(s, " (по умолчанию $1)")
}

// localizeHelp puts the Russian help around the command tree of root. Call
// it after every command is added.
func localizeHelp(root *cobra.Command) {
	if i18n.Lang() != i18n.RU {
		return
	}
	root.SetUsageTemplate(strings.TrimSuffix(usageTemplateRU, "\n") + exitCodesTail)

	// cobra adds these on Execute (and keeps what is already there).
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	for _, c := range root.Commands() {
		switch c.Name() {
		case "help":
			c.Short = "Справка по любой команде"
			c.Long = "Справка по любой команде: " + root.Name() + " help [команда]."
		case "completion":
			localizeCompletion(root.Name(), c)
		}
	}

	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		c.InitDefaultHelpFlag()
		if f := c.Flags().Lookup("help"); f != nil {
			f.Usage = "Справка по " + c.Name()
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
}

// localizeCompletion translates cobra's completion command.
func localizeCompletion(app string, c *cobra.Command) {
	c.Short = "Скрипт автодополнения для оболочки"
	c.Long = fmt.Sprintf("Печатает скрипт автодополнения %s для выбранной оболочки.\n"+
		"Как подключить скрипт — в справке подкоманды (bash, zsh, fish, powershell).\n", app)
	long := map[string]string{
		"bash": `Скрипт автодополнения для bash. Нужен пакет bash-completion (ставится
пакетным менеджером системы).

В текущей сессии:

	source <(%[1]s completion bash)

Для всех новых сессий выполните один раз:

#### Linux:

	%[1]s completion bash > /etc/bash_completion.d/%[1]s

#### macOS:

	%[1]s completion bash > $(brew --prefix)/etc/bash_completion.d/%[1]s

Настройка действует в новой оболочке.
`,
		"zsh": `Скрипт автодополнения для zsh. Если автодополнение в zsh ещё не включено,
выполните один раз:

	echo "autoload -U compinit; compinit" >> ~/.zshrc

В текущей сессии:

	source <(%[1]s completion zsh)

Для всех новых сессий выполните один раз:

#### Linux:

	%[1]s completion zsh > "${fpath[1]}/_%[1]s"

#### macOS:

	%[1]s completion zsh > $(brew --prefix)/share/zsh/site-functions/_%[1]s

Настройка действует в новой оболочке.
`,
		"fish": `Скрипт автодополнения для fish.

В текущей сессии:

	%[1]s completion fish | source

Для всех новых сессий выполните один раз:

	%[1]s completion fish > ~/.config/fish/completions/%[1]s.fish

Настройка действует в новой оболочке.
`,
		"powershell": `Скрипт автодополнения для PowerShell.

В текущей сессии:

	%[1]s completion powershell | Out-String | Invoke-Expression

Для всех новых сессий добавьте вывод этой команды в профиль PowerShell.
`,
	}
	for _, sub := range c.Commands() {
		sub.Short = "Скрипт автодополнения для " + sub.Name()
		if l, ok := long[sub.Name()]; ok {
			sub.Long = fmt.Sprintf(l, app)
		}
		if f := sub.Flags().Lookup("no-descriptions"); f != nil {
			f.Usage = "Без описаний команд в подсказках"
		}
	}
}

// Parsing errors of cobra and pflag, translated by usageMessage.
var usageErrorsRU = []struct {
	re *regexp.Regexp
	ru string
}{
	{regexp.MustCompile(`^unknown command (".*") for (".*")$`), "неизвестная команда $1 для $2"},
	{regexp.MustCompile(`^unknown flag: (\S+)$`), "неизвестный флаг: $1"},
	{regexp.MustCompile(`^unknown shorthand flag: '(.)' in (\S+)$`), "неизвестный флаг: -$1 в $2"},
	{regexp.MustCompile(`^flag needs an argument: '(.)' in (\S+)$`), "флагу -$1 нужно значение ($2)"},
	{regexp.MustCompile(`^flag needs an argument: (\S+)$`), "флагу $1 нужно значение"},
	{regexp.MustCompile(`^invalid argument (".*") for (".*") flag: (.*)$`), "недопустимое значение $1 флага $2: $3"},
	{regexp.MustCompile(`^accepts (\d+) arg\(s\), received (\d+)$`), "нужно аргументов: $1, передано: $2"},
	{regexp.MustCompile(`^accepts at most (\d+) arg\(s\), received (\d+)$`), "можно не больше аргументов: $1, передано: $2"},
	{regexp.MustCompile(`^requires at least (\d+) arg\(s\), only received (\d+)$`), "нужно хотя бы аргументов: $1, передано: $2"},
	{regexp.MustCompile(`^accepts between (\d+) and (\d+) arg\(s\), received (\d+)$`), "нужно аргументов: от $1 до $2, передано: $3"},
	{regexp.MustCompile(`^required flag\(s\) (.*) not set$`), "не заданы обязательные флаги: $1"},
}

// The strconv error pflag puts into an "invalid argument" message.
var (
	parseNumberRE = regexp.MustCompile(`strconv\.Parse(Int|Uint|Float): parsing ".*": (invalid syntax|value out of range)`)
	parseBoolRE   = regexp.MustCompile(`strconv\.ParseBool: parsing ".*": invalid syntax`)
)

// usageMessage translates a cobra parsing error. cobra's suggestion
// ("\n\nDid you mean this?\n\tdeploy\n") becomes "; может быть, deploy?".
func usageMessage(msg string) string {
	if i18n.Lang() != i18n.RU {
		return msg
	}
	head, suggest, _ := strings.Cut(msg, "\n\nDid you mean this?\n")
	for _, e := range usageErrorsRU {
		if e.re.MatchString(head) {
			head = e.re.ReplaceAllString(head, e.ru)
			break
		}
	}
	head = parseNumberRE.ReplaceAllString(head, "нужно число")
	head = parseBoolRE.ReplaceAllString(head, "нужно true или false")
	var names []string
	for _, l := range strings.Split(suggest, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			names = append(names, l)
		}
	}
	if len(names) > 0 {
		head += "; может быть, " + strings.Join(names, ", ") + "?"
	}
	return head
}
