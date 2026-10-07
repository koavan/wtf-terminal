# wtf shell integration for zsh.
#
# zsh is started with ZDOTDIR pointing here so this file runs first. Put the
# user's ZDOTDIR back right away, so zsh reads their .zprofile/.zshrc as usual.

if [[ -n "${WTF_ORIG_ZDOTDIR+X}" ]]; then
  ZDOTDIR="$WTF_ORIG_ZDOTDIR"
  unset WTF_ORIG_ZDOTDIR
else
  unset ZDOTDIR
fi

if [[ -f "${ZDOTDIR:-$HOME}/.zshenv" ]]; then
  source "${ZDOTDIR:-$HOME}/.zshenv"
fi

if [[ -o interactive && -n "$WTF_SHELL_INTEGRATION" && -z "$__wtf_loaded" ]]; then
  typeset -g __wtf_loaded=1
  typeset -gi __wtf_running=0

  # Semantic prompt marks (OSC 133) and the command line (OSC 633;E);
  # see wtf.bash for the protocol.
  __wtf_precmd() {
    local ret=$?
    if (( __wtf_running )); then
      builtin printf '\e]133;D;%s\a' "$ret"
    fi
    __wtf_running=0
    builtin printf '\e]133;A\a'
  }

  __wtf_preexec() {
    __wtf_running=1
    local cmd=${1[1,500]}
    cmd=${cmd//\\/\\\\}
    cmd=${cmd//;/\\x3b}
    cmd=${cmd//$'\n'/\\x0a}
    cmd=${cmd//$'\a'/}
    cmd=${cmd//$'\e'/}
    builtin printf '\e]633;E;%s\a\e]133;C\a' "$cmd"
  }

  autoload -Uz add-zsh-hook
  add-zsh-hook precmd __wtf_precmd
  add-zsh-hook preexec __wtf_preexec
fi
