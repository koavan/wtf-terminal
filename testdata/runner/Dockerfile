# A throwaway agent runner (#41) for linux/runner-test.sh: Debian with
# sshd, tmux and Claude Code, a user "dev" reached with a key the script
# makes. Never your own machines.
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends openssh-server tmux curl ca-certificates git procps && rm -rf /var/lib/apt/lists/*
RUN useradd -m -s /bin/bash dev && mkdir -p /run/sshd /home/dev/.ssh /home/dev/project
COPY key.pub /home/dev/.ssh/authorized_keys
RUN chown -R dev:dev /home/dev && chmod 700 /home/dev/.ssh && chmod 600 /home/dev/.ssh/authorized_keys
RUN su dev -c 'curl -fsSL https://claude.ai/install.sh | bash'
EXPOSE 22
CMD ["/usr/sbin/sshd", "-D", "-e"]
