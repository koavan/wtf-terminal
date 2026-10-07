# wtf shell integration for bash (works with bash 3.2+).
#
# bash is started as `bash --init-file <this file>`, which replaces the
# normal startup files, so first do what a login shell would have done.

[ -r /etc/profile ] && . /etc/profile
if [ -r "$HOME/.bash_profile" ]; then
  . "$HOME/.bash_profile"
elif [ -r "$HOME/.bash_login" ]; then
  . "$HOME/.bash_login"
elif [ -r "$HOME/.profile" ]; then
  . "$HOME/.profile"
fi

# Semantic prompt marks (OSC 133), as understood by iTerm2, VS Code, Ghostty:
#   A           a prompt is about to be drawn
#   C           the command line was accepted; output follows
#   D;<status>  the command finished
# plus OSC 633;E;<command line>, escaped like VS Code does.

if [ -n "$WTF_SHELL_INTEGRATION" ] && [ -z "$__wtf_loaded" ]; then
  __wtf_loaded=1
  __wtf_running=0
  __wtf_in_prompt=0

  __wtf_prompt_start() {
    local ret=$?
    __wtf_in_prompt=1
    if [ "$__wtf_running" = 1 ]; then
      builtin printf '\033]133;D;%s\007' "$ret"
    fi
    __wtf_running=0
    return $ret # keep $? for the user's PROMPT_COMMAND
  }

  __wtf_prompt_end() {
    builtin printf '\033]133;A\007'
    __wtf_in_prompt=0
  }

  # Runs before every simple command; only the first one of a command line
  # that the user typed counts.
  __wtf_preexec() {
    [ "$__wtf_in_prompt" = 1 ] && return
    [ "$__wtf_running" = 1 ] && return
    [ -n "$COMP_LINE" ] && return
    case "$BASH_COMMAND" in __wtf_*) return ;; esac
    __wtf_running=1
    local cmd=${BASH_COMMAND:0:500}
    cmd=${cmd//\\/\\\\}
    cmd=${cmd//;/\\x3b}
    cmd=${cmd//$'\n'/\\x0a}
    cmd=${cmd//$'\a'/}
    cmd=${cmd//$'\033'/}
    builtin printf '\033]633;E;%s\007\033]133;C\007' "$cmd"
  }

  PROMPT_COMMAND="__wtf_prompt_start${PROMPT_COMMAND:+; $PROMPT_COMMAND}; __wtf_prompt_end"
  trap '__wtf_preexec' DEBUG
fi
