# Bash without bash-completion still supplies Readline's words and cursor.
# Work backwards from the cursor so an earlier pipeline/identical command
# cannot shift our position. Adjacent Readline fields separated at = or :
# belong to the same argument; whitespace between arguments does not.
__evo_init_completion() {
    if declare -F _get_comp_words_by_ref >/dev/null; then
        _get_comp_words_by_ref "$@" cur prev words cword
        return
    fi
    # Readline in Bash 3 supplies a byte offset, even in a UTF-8 locale.
    if (( BASH_VERSINFO[0] == 3 )); then local LC_ALL=C; fi
    local left=${COMP_LINE:0:COMP_POINT} right=${COMP_LINE:COMP_POINT}
    local i=$COMP_CWORD token=${COMP_WORDS[COMP_CWORD]} n part suffix joined
    for ((n=${#token}; n>=0; n--)); do
        part=${token:0:n}
        suffix=${token:n}
        [[ $left == *"$part" && $right == "$suffix"* ]] && break
    done
    (( n >= 0 )) || return 1
    words=("$part")
    left=${left%"$part"}
    while (( i > 0 )); do
        ((i--))
        token=${COMP_WORDS[i]}
        joined=1
        while [[ $left == *[[:space:]] && $left != *"$token" ]]; do
            left=${left%?}
            joined=0
        done
        [[ $left == *"$token" ]] || return 1
        if (( joined )); then
            words[0]=$token${words[0]}
        else
            words=("$token" "${words[@]}")
        fi
        left=${left%"$token"}
    done
    cword=$((${#words[@]}-1))
    cur=${words[cword]}
    if (( cword > 0 )); then prev=${words[cword-1]}; else prev=; fi
}

# Convert shell quoting into one argument without evaluating expansions.
__evo_unquote_word() {
    local raw=$1 quote= ch next i
    value=
    for ((i=0; i<${#raw}; i++)); do
        ch=${raw:i:1}
        if [[ -n $quote && $ch == "$quote" ]]; then
            quote=
        elif [[ -z $quote && ( $ch == "'" || $ch == '"' ) ]]; then
            quote=$ch
        elif [[ $ch == '\' && $quote != "'" && $((i+1)) -lt ${#raw} ]]; then
            next=${raw:i+1:1}
            if [[ -z $quote || $next == [\`\$\\\"] ]]; then
                value+=$next
                ((i++))
            else
                value+=$ch
            fi
        else
            value+=$ch
        fi
    done
}

__evo_get_completion_results() {
    local args=() value i
    for ((i=1; i<cword; i++)); do
        __evo_unquote_word "${words[i]}"
        args+=("$value")
    done
    __evo_unquote_word "$cur"
    args+=("$value")
    cur=$value
    if [[ $cur == -*=* ]]; then cur=${cur#*=}; fi
    out=$(evo __EVO_COMPLETION_REQUEST__ "${args[@]}" 2>/dev/null) || { out=; directive=1; return; }
    directive=${out##*$'\n':}
    [[ $out == :* && $out != *$'\n'* ]] && directive=${out#:}
    case $directive in
        ''|*[!0-9]*) out=; directive=1 ;;
        *) out=${out%:"$directive"} ;;
    esac
}

# Bash 3 cannot change -o default for an individual request. Supply the
# filenames here only when Cobra permits them. This also covers future
# extension/directory directives in a shell without the package's _filedir.
__evo_process_completion_results() {
    local path ext accepted base=. candidate
    if (( directive & 24 )) && ! declare -F _filedir >/dev/null; then
        if (( directive & 16 )) && [[ -n $out ]]; then base=${out%%$'\n'*}; fi
        while IFS= read -r path; do
            accepted=
            if [[ -d $base/$path || ( $path == /* && -d $path ) ]]; then
                path=${path%/}/
                accepted=1
            elif (( directive & 8 )); then
                while IFS= read -r ext; do
                    [[ -n $ext && $path == *."$ext" ]] && accepted=1
                done <<< "$out"
            fi
            if [[ -n $accepted ]]; then
                printf -v candidate '%q' "$path"
                COMPREPLY+=("$candidate")
            fi
        done < <(cd -- "$base" && compgen -f -- "$cur")
        return 0
    fi
    __evo_cobra_results
    if [[ $(type -t compopt) != builtin && ${#COMPREPLY[@]} == 0 ]] && (( !(directive & 29) )); then
        __evo_default_files
    fi
}

__evo_default_files() {
    if declare -F _filedir >/dev/null; then _filedir; return; fi
    local path candidate
    while IFS= read -r path; do
        [[ -d $path ]] && path=${path%/}/
        printf -v candidate '%q' "$path"
        COMPREPLY+=("$candidate")
    done < <(compgen -f -- "$cur")
}
