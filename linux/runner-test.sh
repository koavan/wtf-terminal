#!/bin/sh
# Runs the agent runner's live test (#41, internal/runner TestLive) against
# a throwaway container: sshd, tmux and Claude Code, on 127.0.0.1:2222
# (WTF_RUNNER_TEST_NAME and WTF_RUNNER_TEST_PORT change them). It
# never touches ~/.ssh: the key, ssh config and known_hosts live in
# $WTF_RUNNER_TEST_HOME (default ~/.cache/wtf-runner-test).
#
#   linux/runner-test.sh          set up (once), install, check, connect
#   linux/runner-test.sh login    sign in to Claude Code there (once)
#   linux/runner-test.sh agents   also run agents: approvals, a daemon
#                                 restart, peek, reply (needs the sign-in;
#                                 uses your Claude account)
#   linux/runner-test.sh clean    remove the container and its files
set -eu
cd "$(dirname "$0")/.."
home=${WTF_RUNNER_TEST_HOME:-$HOME/.cache/wtf-runner-test}
name=${WTF_RUNNER_TEST_NAME:-wtf-runner-test}
port=${WTF_RUNNER_TEST_PORT:-2222}
cfg=$home/ssh_config

case ${1:-} in
clean)
	docker rm -f "$name" >/dev/null 2>&1 || true
	docker rmi -f "$name" >/dev/null 2>&1 || true
	rm -rf "$home"
	exit 0
	;;
esac

mkdir -p "$home"
chmod 700 "$home"
if [ ! -f "$home/key" ]; then
	ssh-keygen -q -t ed25519 -N '' -f "$home/key" -C wtf-runner-test
fi
cat >"$cfg" <<CFG
Host runner
  HostName 127.0.0.1
  Port $port
  User dev
  IdentityFile $home/key
  IdentitiesOnly yes
  UserKnownHostsFile $home/known_hosts
  StrictHostKeyChecking accept-new
CFG
if ! docker inspect "$name" >/dev/null 2>&1; then
	cp "$home/key.pub" testdata/runner/key.pub
	docker build -q -t "$name" testdata/runner >/dev/null
	rm -f testdata/runner/key.pub
	docker run -d --name "$name" -p "127.0.0.1:$port:22" "$name" >/dev/null
	rm -f "$home/known_hosts"
fi
docker start "$name" >/dev/null
for _ in 1 2 3 4 5 6 7 8 9 10; do
	ssh -F "$cfg" -o BatchMode=yes runner true 2>/dev/null && break
	sleep 1
done

if [ "${1:-}" = login ]; then
	exec ssh -t -F "$cfg" runner '~/.local/bin/claude auth login --claudeai'
fi

linux/runner-payload.sh "$home/payload" test >/dev/null
export WTF_RUNNER_TEST_SSH_CONFIG=$cfg WTF_RUNNER_TEST_PAYLOAD=$home/payload
if [ "${1:-}" = agents ]; then
	export WTF_RUNNER_TEST_DIR=/home/dev/project
fi
exec go test -count=1 -run TestLive -v ./internal/runner
