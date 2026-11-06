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

  # agent-status (the Agents tab): claude reports what it's doing through
  # wtf's status hooks, added for this run only (see wtf.bash). Defined
  # before .zshrc, so a claude alias or function there wins.
  if [[ -n $WTF_AGENT_SETTINGS ]]; then
    claude() {
      local a
      if [[ -r $WTF_AGENT_SETTINGS ]]; then
        for a in "$@"; do
          case $a in
            --settings|--settings=*|--plugin-dir|--plugin-dir=*) command claude "$@"; return ;;
          esac
        done
        command claude --settings "$WTF_AGENT_SETTINGS" "$@"
      else
        command claude "$@"
      fi
    }
  fi

  # Semantic prompt marks (OSC 133), the command line (OSC 633;E) and the
  # cloud/cluster variables (OSC 633;P;WtfEnv); see wtf.bash for the protocol.
  # wtf's "clear history" sends Ctrl-X Ctrl-]: it removes the commands run
  # in this session from the history (↑ / Ctrl-R), without a visible
  # command. It switches to a fresh list loaded from the history file, so
  # saved history stays and the file isn't rewritten without it.
  __wtf_clear_history() {
    local file=$HISTFILE size=$HISTSIZE save=$SAVEHIST
    # The list being set aside is saved when zsh exits; detach it from the
    # file first so this session's commands don't get written after all.
    unset HISTFILE
    if [[ -n $file ]]; then
      fc -p "$file" "$size" "$save"
    else
      fc -p
    fi
  }

  # Bound at the first prompt, after .zshrc, so keymap changes there
  # (bindkey -v, frameworks) can't drop it.
  __wtf_bind_keys() {
    zle -N __wtf_clear_history
    local map
    for map in emacs viins vicmd; do
      bindkey -M $map '^X^]' __wtf_clear_history
    done
    # Report the environment after the user's own hooks (direnv and the
    # like) have changed it. zsh runs this prompt's hooks from a copy of the
    # list, so the new order holds from the next prompt on.
    add-zsh-hook -d precmd __wtf_report_env
    add-zsh-hook precmd __wtf_report_env
    add-zsh-hook -d precmd __wtf_bind_keys
  }

  typeset -ga __wtf_env_names=(
    PWD PATH KUBECONFIG AWS_PROFILE AWS_DEFAULT_PROFILE AWS_VAULT
    AWS_REGION AWS_DEFAULT_REGION AWS_CONFIG_FILE AWS_SHARED_CREDENTIALS_FILE CLOUDSDK_CONFIG
    CLOUDSDK_ACTIVE_CONFIG_NAME CLOUDSDK_CORE_PROJECT TF_WORKSPACE TF_DATA_DIR
    GIT_AUTHOR_NAME GIT_AUTHOR_EMAIL WTF_GIT_IDENTITY
    AWS_CREDENTIAL_EXPIRATION AWS_SESSION_EXPIRATION SSH_AUTH_SOCK
  )
  __wtf_report_env() {
    local out= name
    for name in $__wtf_env_names; do
      [[ -n ${(P)name} ]] && out+="$name=${(P)name}"$'\n'
    done
    # Whether AWS keys are exported, never the keys themselves.
    [[ -n $AWS_ACCESS_KEY_ID ]] && out+="WTF_AWS_ENV_KEYS=1"$'\n'
    out=${out//\\/\\\\}
    out=${out//;/\\x3b}
    out=${out//$'\n'/\\x0a}
    out=${out//$'\a'/}
    out=${out//$'\e'/}
    builtin printf '\e]633;P;WtfEnv=%s\a' "$out"
  }

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
  add-zsh-hook precmd __wtf_bind_keys
  add-zsh-hook precmd __wtf_report_env
  add-zsh-hook preexec __wtf_preexec
fi
