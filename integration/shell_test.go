//go:build shellintegration

package integration

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Exercise the shipped source in real shells: quoting, hook chaining, exit
// status and completion dispatch cannot be established by Go-only tests.
func TestShellSmoke(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			shellPath, err := exec.LookPath(shell)
			if err != nil {
				t.Fatalf("shell:smoke requires Bash, Zsh and Fish on PATH; install %s: %v", shell, err)
			}
			probe := exec.Command(shellPath, "--version")
			if out, err := probe.CombinedOutput(); err != nil {
				t.Fatalf("shell:smoke requires working %s on PATH (resolve any tool shim): %v\n%s", shell, err, out)
			}
			root := t.TempDir()
			project := filepath.Join(root, "project ' $(touch INJECTED)\n")
			for _, dir := range []string{filepath.Join(root, "home"), filepath.Join(project, "sub")} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(project, "noon.yaml"), []byte("jobs:\n  entry:\n    run: [touch, EXECUTED]\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			pathDir := filepath.Join(root, "completion paths = $(touch INJECTED) ;")
			promptPath := filepath.Join(pathDir, "prompt $(touch INJECTED) ;.md")
			configPath := filepath.Join(pathDir, "config $(touch INJECTED) ;.yaml")
			crontabPath := filepath.Join(pathDir, "crontab $(touch INJECTED) ;")
			cwdCompletionPath := filepath.Join(root, "cwd completion =.yaml")
			zleConfigPrefix := filepath.Join(root, "zle=paths", "config")
			zleConfigPath := zleConfigPrefix + ".yaml"
			if err := os.MkdirAll(pathDir, 0o700); err != nil {
				t.Fatal(err)
			}
			for path, data := range map[string]string{
				promptPath:        "prompt fixture\n",
				configPath:        "agents:\n  reviewer:\n    command: [agent]\n",
				crontabPath:       "0 2 * * * true\n",
				cwdCompletionPath: "cwd fixture\n",
				zleConfigPath:     "agents:\n  reviewer:\n    command: [agent]\n",
				filepath.Join(root, "config", "noon", "config.yaml"): "agents:\n  reviewer:\n    command: [agent]\n",
			} {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			alias := filepath.Join(root, "alias")
			if err := os.Symlink(project, alias); err != nil {
				t.Fatal(err)
			}
			env := []string{"PATH=" + filepath.Dir(noonBinary) + string(os.PathListSeparator) + os.Getenv("PATH"), "HOME=" + filepath.Join(root, "home"), "XDG_CONFIG_HOME=" + filepath.Join(root, "config"), "XDG_STATE_HOME=" + filepath.Join(root, "state"), "TERM=dumb", "NOON_SMOKE_PROJECT=" + project, "NOON_SMOKE_OUTSIDE=" + root, "NOON_SMOKE_ALIAS=" + alias, "NOON_SMOKE_PATH_PREFIX=" + filepath.Join(root, "completion paths"), "NOON_SMOKE_PROMPT_PREFIX=" + filepath.Join(pathDir, "prompt"), "NOON_SMOKE_CONFIG_PREFIX=" + filepath.Join(pathDir, "config"), "NOON_SMOKE_CONFIG_PATH=" + configPath, "NOON_SMOKE_CRONTAB_PREFIX=" + filepath.Join(pathDir, "crontab"), "NOON_SMOKE_CWD_FILE=" + filepath.Base(cwdCompletionPath), "NOON_SMOKE_ZLE_CONFIG_PREFIX=" + zleConfigPrefix, "NOON_SMOKE_ZLE_CONFIG_PATH=" + zleConfigPath}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			args := []string{"--noprofile", "--norc", "-c", bashSmoke}
			if shell == "zsh" {
				args = []string{"-f", "-c", zshSmoke}
			}
			if shell == "fish" {
				args = []string{"--no-config", "-c", fishSmoke}
			}
			cmd := exec.CommandContext(ctx, shellPath, args...)
			cmd.Env, cmd.Dir = env, root
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s integration: %v\n%s", shell, err, out)
			}
			var counts []int
			var candidates []string
			for _, line := range strings.Split(string(out), "\n") {
				if strings.HasPrefix(line, "COMP:") {
					candidates = append(candidates, strings.TrimPrefix(line, "COMP:"))
					continue
				}
				if line == "" {
					continue
				}
				var result struct {
					Runs []json.RawMessage `json:"runs"`
				}
				if err := json.Unmarshal([]byte(line), &result); err != nil {
					t.Fatalf("unexpected shell output: %s\nall: %s", line, out)
				}
				counts = append(counts, len(result.Runs))
			}
			if !slices.Equal(counts, []int{0, 0, 1, 1, 1, 1, 1, 2}) {
				t.Fatalf("entry counts: %v\n%s", counts, out)
			}
			for _, want := range []string{"run", "rm", "--detach", "entry", "bash", "zsh", "fish", "queued", "failed", "all", "job", "hook", "reviewer", promptPath, configPath, crontabPath} {
				if !slices.Contains(candidates, want) {
					t.Fatalf("missing completion %s: %v", want, candidates)
				}
			}
			if !slices.Contains(candidates, pathDir) && !slices.Contains(candidates, pathDir+string(os.PathSeparator)) {
				t.Fatalf("missing project directory completion %s: %v", pathDir, candidates)
			}
			for _, name := range []string{"INJECTED", "EXECUTED"} {
				for _, dir := range []string{root, project} {
					if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
						t.Fatalf("shell executed %s: %v", name, err)
					}
				}
			}
		})
	}
}

const bashSmoke = `
fail() { printf 'FAIL:%s\n' "$*"; exit 1; }
history_check() { command noon -C "$NOON_SMOKE_PROJECT" history --json; }
PROMPT_COMMAND='old_status=$?; old_hook=$(( ${old_hook:-0} + 1 ))'
eval "$(command noon shell init bash)"
[[ ! -e $XDG_STATE_HOME ]] || fail completion-created-state
COMP_WORDS=(noon ''); COMP_CWORD=1; _noon_complete
printf 'COMP:%s\n' "${COMPREPLY[@]}"
COMP_WORDS=(noon shell init ''); COMP_CWORD=3; _noon_complete
printf 'COMP:%s\n' "${COMPREPLY[@]}"
COMP_WORDS=(noon logs --status ''); COMP_CWORD=3; _noon_complete
printf 'COMP:%s\n' "${COMPREPLY[@]}"
COMP_WORDS=(noon logs --status = fa); COMP_CWORD=4; _noon_complete
printf 'COMP:%s\n' "${COMPREPLY[@]}"
COMP_WORDS=(noon logs --section ''); COMP_CWORD=3; _noon_complete
printf 'COMP:%s\n' "${COMPREPLY[@]}"
COMP_WORDS=(noon add --agent ''); COMP_CWORD=3; _noon_complete
printf 'COMP:%s\n' "${COMPREPLY[@]}"
COMP_WORDS=(noon --project "$NOON_SMOKE_PATH_PREFIX"); COMP_CWORD=2; _noon_complete
printf 'COMP:%s\n' "${COMPREPLY[@]}"
COMP_WORDS=(noon -C = "$NOON_SMOKE_PATH_PREFIX"); COMP_CWORD=3; _noon_complete
printf 'COMP:%s\n' "${COMPREPLY[@]}"
COMP_WORDS=(noon --config =); COMP_CWORD=2; _noon_complete
[[ " ${COMPREPLY[*]} " == *" $NOON_SMOKE_CWD_FILE "* ]] || fail config-equals-cwd-files
printf 'COMP:%s\n' "${COMPREPLY[@]}"
COMP_WORDS=(noon --config "$NOON_SMOKE_CONFIG_PREFIX"); COMP_CWORD=2; _noon_complete
printf 'COMP:%s\n' "${COMPREPLY[@]}"
COMP_WORDS=(noon add --prompt-file "$NOON_SMOKE_PROMPT_PREFIX"); COMP_CWORD=3; _noon_complete
printf 'COMP:%s\n' "${COMPREPLY[@]}"
COMP_WORDS=(noon import crontab "$NOON_SMOKE_CRONTAB_PREFIX"); COMP_CWORD=3; _noon_complete
printf 'COMP:%s\n' "${COMPREPLY[@]}"
command noon -C "$NOON_SMOKE_PROJECT" enable >/dev/null || fail enable
eval "$(command noon shell init bash --on-enter entry)"
history_check
eval "$(command noon shell init bash --on-enter entry)"
history_check
if (( BASH_VERSINFO[0] > 5 || (BASH_VERSINFO[0] == 5 && BASH_VERSINFO[1] >= 1) )); then
    [[ ${#PROMPT_COMMAND[@]} == 2 ]] || fail duplicate-hook
else
    [[ $PROMPT_COMMAND == $'_noon_prompt\nold_status='* ]] || fail scalar-hook
fi
cd "$NOON_SMOKE_PROJECT" || fail cd
prompt_script=$(printf '%s\n' "${PROMPT_COMMAND[@]}")
false
eval "$prompt_script"
[[ $old_status == 1 && $old_hook == 1 ]] || fail "status-or-existing-hook status=$old_status count=$old_hook hooks=${PROMPT_COMMAND[*]}"
history_check
cd sub || fail sub
false; _noon_prompt; [[ $? == 1 ]] || fail status
history_check
cd "$NOON_SMOKE_ALIAS" || fail alias
_noon_prompt
history_check
eval "$(command noon shell init bash --on-enter entry)"
history_check
COMP_WORDS=(noon run --d); COMP_CWORD=2; _noon_complete
printf 'COMP:%s\n' "${COMPREPLY[@]}"
printf -v quoted_project '%q' "$NOON_SMOKE_PROJECT"
COMP_WORDS=(noon -C "$quoted_project" run ''); COMP_CWORD=4; _noon_complete
printf 'COMP:%s\n' "${COMPREPLY[@]}"
history_check
command noon -C "$NOON_SMOKE_PROJECT" cancel entry >/dev/null || fail cancel
cd "$NOON_SMOKE_OUTSIDE"; _noon_prompt
cd "$NOON_SMOKE_PROJECT"; _noon_prompt
history_check
`

const zshSmoke = `
fail() { print -r -- "FAIL:$*"; exit 1; }
history_check() { command noon -C "$NOON_SMOKE_PROJECT" history --json; }
autoload -Uz add-zsh-hook
old_chpwd() { (( old_hook += 1 )); return 0; }
add-zsh-hook chpwd old_chpwd
eval "$(command noon shell init zsh)"
[[ ! -e $XDG_STATE_HOME ]] || fail completion-created-state
[[ $_comps[noon] == _noon_complete ]] || fail completion-registration
# Capture callback candidates and exercise path globbing; zpty below also
# validates native ZLE and Bash Readline insertion.
compadd() { shift; printf 'COMP:%s\n' "$@"; }
compset() {
    [[ $1 == -P && $2 == '[^=]#=' ]] && PREFIX=${PREFIX#*=}
}
_files() {
    local -a matches
    if [[ $1 == -/ ]]; then
        matches=( "${PREFIX}"*(N/) )
    else
        matches=( "${PREFIX}"*(N) )
    fi
    compadd -- "${matches[@]}"
}
words=(noon ''); CURRENT=2; PREFIX=''; _noon_complete
words=(noon shell init ''); CURRENT=4; PREFIX=''; _noon_complete
words=(noon logs --status ''); CURRENT=4; PREFIX=''; _noon_complete
words=(noon logs --status=fa); CURRENT=3; PREFIX='--status=fa'; _noon_complete
words=(noon logs --section ''); CURRENT=4; PREFIX=''; _noon_complete
words=(noon add --agent ''); CURRENT=4; PREFIX=''; _noon_complete
words=(noon --project "$NOON_SMOKE_PATH_PREFIX"); CURRENT=3; PREFIX=$NOON_SMOKE_PATH_PREFIX; _noon_complete
words=(noon --config "$NOON_SMOKE_CONFIG_PREFIX"); CURRENT=3; PREFIX=$NOON_SMOKE_CONFIG_PREFIX; _noon_complete
words=(noon "--config=$NOON_SMOKE_CONFIG_PREFIX"); CURRENT=2; PREFIX="--config=$NOON_SMOKE_CONFIG_PREFIX"; _noon_complete
words=(noon add --prompt-file "$NOON_SMOKE_PROMPT_PREFIX"); CURRENT=4; PREFIX=$NOON_SMOKE_PROMPT_PREFIX; _noon_complete
words=(noon import crontab "$NOON_SMOKE_CRONTAB_PREFIX"); CURRENT=4; PREFIX=$NOON_SMOKE_CRONTAB_PREFIX; _noon_complete
zmodload zsh/zpty || fail zpty-unavailable
zpty -b noon_completion_zle zsh -f -i || fail zle-start
zpty -w noon_completion_zle 'eval "$(command noon shell init zsh)"'
zpty -w noon_completion_zle 'noon_show_buffer() { print -r -- "__NOON_ZLE_BUFFER_BEGIN__${BUFFER}__NOON_ZLE_BUFFER_END__"; BUFFER=; }; zle -N noon_show_buffer; bindkey "^I" complete-word; bindkey "^X" noon_show_buffer; print -r -- "__NOON_ZLE_READY__"'
zpty -r -m noon_completion_zle zle_output '*__NOON_ZLE_READY__*' || fail zle-ready
zpty -w -n noon_completion_zle "noon --config=$NOON_SMOKE_ZLE_CONFIG_PREFIX"$'\t'
zpty -w -n noon_completion_zle $'\x18'
zpty -r -m noon_completion_zle zle_output '*__NOON_ZLE_BUFFER_END__*' || fail zle-path-result
[[ $zle_output == *"__NOON_ZLE_BUFFER_BEGIN__noon --config=${NOON_SMOKE_ZLE_CONFIG_PATH}"* ]] || fail "zle-path-insertion:$zle_output"
zpty -w -n noon_completion_zle 'noon logs --status=fa'$'\t'
zpty -w -n noon_completion_zle $'\x18'
zpty -r -m noon_completion_zle zle_output '*__NOON_ZLE_BUFFER_END__*' || fail zle-value-result
[[ $zle_output == *__NOON_ZLE_BUFFER_BEGIN__noon\ logs\ --status=failed*__NOON_ZLE_BUFFER_END__* ]] || fail "zle-value-insertion:$zle_output"
zpty -w -n noon_completion_zle $'\x03'
zpty -d noon_completion_zle
zpty -b noon_completion_bash bash --noprofile --norc -i || fail bash-start
zpty -w noon_completion_bash 'eval "$(command noon shell init bash)"'
zpty -w noon_completion_bash $'noon_show_buffer() { printf "%s" "__NOON_BASH_BUFFER_BEGIN__${READLINE_LINE}__NOON_BASH_BUFFER_END__"; READLINE_LINE=; }; bind -x \'"\\C-x": noon_show_buffer\'; printf "%s" "__NOON_BASH_READY__"'
zpty -r -m noon_completion_bash bash_output '*__NOON_BASH_READY__*' || fail bash-ready
zpty -w -n noon_completion_bash "noon --config=\"$NOON_SMOKE_CONFIG_PREFIX"$'\t'
zpty -w -n noon_completion_bash $'\x18'
zpty -r -m noon_completion_bash bash_output '*__NOON_BASH_BUFFER_END__*' || fail bash-path-result
[[ $bash_output == *"__NOON_BASH_BUFFER_BEGIN__noon --config=\"${NOON_SMOKE_CONFIG_PATH}"* ]] || fail "bash-quoted-path-insertion:$bash_output"
zpty -w -n noon_completion_bash $'\x03'
zpty -d noon_completion_bash
command noon -C "$NOON_SMOKE_PROJECT" enable >/dev/null || fail enable
eval "$(command noon shell init zsh --on-enter entry)"
history_check
eval "$(command noon shell init zsh --on-enter entry)"
history_check
[[ ${#chpwd_functions} == 2 ]] || fail duplicate-hook
cd "$NOON_SMOKE_PROJECT" || fail cd
[[ $? == 0 && $old_hook == 1 ]] || fail status-or-existing-hook
false; _noon_chpwd; [[ $? == 1 ]] || fail status
history_check
cd sub || fail sub
history_check
cd "$NOON_SMOKE_ALIAS" || fail alias
history_check
eval "$(command noon shell init zsh --on-enter entry)"
history_check
words=(noon run --d); CURRENT=3; _noon_complete
words=(noon -C "${(q)NOON_SMOKE_PROJECT}" run ''); CURRENT=5; _noon_complete
history_check
command noon -C "$NOON_SMOKE_PROJECT" cancel entry >/dev/null || fail cancel
cd "$NOON_SMOKE_OUTSIDE"
cd "$NOON_SMOKE_PROJECT"
history_check
`

const fishSmoke = `
function fail
    echo "FAIL:$argv"
    exit 1
end
function history_check
    command noon -C "$NOON_SMOKE_PROJECT" history --json
end
function old_chpwd --on-variable PWD
    set -g old_hook (math "$old_hook + 1")
end
set -g old_hook 0
command noon shell init fish | source
test ! -e "$XDG_STATE_HOME"; or fail completion-created-state
for item in (complete -C 'noon ')
    printf 'COMP:%s\n' (string split -f1 \t -- $item)
end
for query in 'noon shell init ' 'noon logs --status ' 'noon logs --status=fa' 'noon logs --section ' 'noon add --agent '
    for item in (complete -C "$query")
        printf 'COMP:%s\n' (string split -f1 \t -- $item)
    end
end
set project_prefix (string escape -- "$NOON_SMOKE_PATH_PREFIX")
set config_prefix (string escape -- "$NOON_SMOKE_CONFIG_PREFIX")
set prompt_prefix (string escape -- "$NOON_SMOKE_PROMPT_PREFIX")
set crontab_prefix (string escape -- "$NOON_SMOKE_CRONTAB_PREFIX")
for query in "noon --project $project_prefix" "noon --config $config_prefix" "noon --config=$config_prefix" "noon add --prompt-file $prompt_prefix" "noon import crontab $crontab_prefix"
    for item in (complete -C "$query")
        printf 'COMP:%s\n' (string split -f1 \t -- $item)
    end
end
command noon -C "$NOON_SMOKE_PROJECT" enable >/dev/null; or fail enable
command noon shell init fish --on-enter entry | source
history_check
command noon shell init fish --on-enter entry | source
history_check
cd "$NOON_SMOKE_PROJECT"; or fail cd
test $status = 0; or fail cd-status
test $old_hook = 1; or fail existing-hook
false; _noon_chpwd; test $status = 1; or fail status
history_check
cd sub; or fail sub
history_check
cd "$NOON_SMOKE_ALIAS"; or fail alias
history_check
command noon shell init fish --on-enter entry | source
history_check
set query "noon -C "(string escape -- "$NOON_SMOKE_PROJECT")" run "
contains -- entry (complete -C "$query"); or fail explicit-project-completion
for item in (complete -C 'noon run --d') (complete -C 'noon run ')
    printf 'COMP:%s\n' (string split -f1 \t -- $item)
end
history_check
command noon -C "$NOON_SMOKE_PROJECT" cancel entry >/dev/null; or fail cancel
cd "$NOON_SMOKE_OUTSIDE"
cd "$NOON_SMOKE_PROJECT"
history_check
`
