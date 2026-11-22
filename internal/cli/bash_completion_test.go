// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Run the emitted script as an ordinary Bash completion. The executable
// returns the real registry's offline answers for commands and records the
// exact arguments for cases whose values would require cloud credentials.
func TestBashCompletion_Readline(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("Bash unavailable")
	}
	dir := t.TempDir()
	generate := func(file string, args ...string) {
		t.Helper()
		out, msg, code := execOffline(t, args...)
		if code != 0 {
			t.Fatalf("%v: %d %s", args, code, msg)
		}
		if err := os.WriteFile(filepath.Join(dir, file), []byte(out), 0600); err != nil {
			t.Fatal(err)
		}
	}
	generate("script", "completion", "bash", "--no-descriptions")
	generate("descriptions", "completion", "bash")
	generate("root", "__completeNoDesc", "v")
	generate("vm", "__completeNoDesc", "vm", "")
	test := `
# Some minimal Bash builds omit programmable completion entirely.
[[ $(type -t complete) == builtin && $(type -t compgen) == builtin ]] || exit 77
touch_marker() { touch "$dir/executed"; }
if [[ -n $2 ]]; then
    source "$2" || exit 1
    declare -F _init_completion >/dev/null || declare -F _get_comp_words_by_ref >/dev/null || exit 77
fi
source "$1/script" && source "$1/script" || exit 2
complete -p evo | grep -q __start_evo || exit 3
dir=$1
evo() {
    [[ $1 == __completeNoDesc ]] || return 1
    shift
    printf '%s\n' "$@" >"$dir/argv"
    case $fixture in
        root|vm) cat "$dir/$fixture" ;;
        list) printf 'list\n:4\n' ;;
        *) printf ':4\n' ;;
    esac
}
run() {
    COMP_CWORD=$1; COMP_LINE=$2; COMP_POINT=$3; shift 3
    COMP_WORDS=("$@")
    __start_evo "${COMP_WORDS[0]}" "${COMP_WORDS[COMP_CWORD]}" "${COMP_WORDS[COMP_CWORD-1]}"
}
has() {
    local item
    for item in "${COMPREPLY[@]}"; do [[ $item == "$1" ]] && return 0; done
    return 1
}
fixture=root
run 1 'evo v' 5 evo v
for word in verify version vm vpc; do has "$word" || exit 4; done
fixture=vm
run 2 'evo vm ' 7 evo vm ''
has create && has list || exit 5
fixture=list
run 2 'evo vm list --json' 9 evo vm list --json
has list && [[ $(cat "$dir/argv") == $'vm\nli' ]] || exit 6
fixture=list
line='evo --name Москва list --json'
prefix='evo --name Москва li'
point=${#prefix}
if (( BASH_VERSINFO[0] == 3 )); then point=$(LC_ALL=C printf %s "$prefix" | wc -c); fi
run 3 "$line" "$point" evo --name Москва list --json
has list && [[ $(cat "$dir/argv") == $'--name\nМосква\nli' ]] || exit 10
fixture=args
line='evo --name "two words" --value=a:b'
# Readline represents the final nonempty word as the current field.
run 7 "$line" "${#line}" evo --name '"two words"' --value = a : b
[[ $(cat "$dir/argv") == $'--name\ntwo words\n--value=a:b' ]] || exit 7
line='evo --name $(touch_marker) a\ b'
run 3 "$line" "${#line}" evo --name '$(touch_marker)' 'a\ b'
[[ $(cat "$dir/argv") == $'--name\n$(touch_marker)\na b' ]] || exit 8
[[ ! -e "$dir/executed" ]] || exit 9
mkdir -p "$dir/path with space" || exit 11
printf '%s\n' '#!/bin/sh' 'printf "via_path\n:4\n"' > "$dir/path with space/evo"
chmod +x "$dir/path with space/evo"
program='"'"$dir/path with space/evo"'"'
line="$program v"
run 1 "$line" "${#line}" "$program" v
has via_path || exit 12
# A prefix with whitespace must match with and without candidate descriptions.
for form in script descriptions; do
    source "$dir/$form"
    evo() {
        case $1 in
            __complete) printf 'two words\tnote\n:4\n' ;;
            __completeNoDesc) printf 'two words\n:4\n' ;;
            *) return 1 ;;
        esac
    }
    for input in 'two\ w' '"two w'; do
        line="evo $input"
        run 1 "$line" "${#line}" evo "$input"
        case $input in
            '"two w') has 'two words' || exit 13 ;;
            *) has 'two\ words' || exit 13 ;;
        esac
    done
done
`
	locale := "C"
	if locales, err := exec.Command("locale", "-a").Output(); err == nil {
		for _, name := range strings.Fields(string(locales)) {
			switch strings.ToLower(name) {
			case "c.utf-8", "c.utf8", "en_us.utf-8", "en_us.utf8":
				locale = name
			}
		}
	}
	loaders := []string{""}
	for _, candidate := range []string{"/usr/share/bash-completion/bash_completion", "/etc/bash_completion", "/opt/homebrew/etc/profile.d/bash_completion.sh", "/usr/local/etc/profile.d/bash_completion.sh"} {
		if _, err := os.Stat(candidate); err == nil {
			loaders = append(loaders, candidate)
			break
		}
	}
	for _, loader := range loaders {
		name := "standalone"
		if loader != "" {
			name = "bash-completion"
		}
		t.Run(name, func(t *testing.T) {
			// Model a fresh Bash session. Exported functions, shell options and
			// the user's completion configuration must not supply our adapter.
			cmd := exec.Command(bash, "--noprofile", "--norc", "-c", test, "bash", dir, loader)
			cmd.Env = []string{"HOME=" + dir, "PATH=" + os.Getenv("PATH"), "LC_ALL=" + locale, "COMP_TYPE=9", "COMP_KEY=9"}
			out, err := cmd.CombinedOutput()
			if status, ok := err.(*exec.ExitError); ok && status.ExitCode() == 77 {
				t.Skipf("programmable completion unavailable in %s with loader %q", bash, loader)
			}
			if err != nil {
				t.Fatalf("%s loader %q: %v\n%s", bash, loader, err, strings.TrimSpace(string(out)))
			}
		})
	}
}
