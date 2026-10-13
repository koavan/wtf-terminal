package tools

// tool is a command-line program some feature runs. wtf never needs one to
// start; each feature says when its program is missing.
type tool struct {
	id    string
	name  string
	about string
	// The programs it provides. Found when the first is; with any, when one of them is.
	programs []string
	any      bool
	// Arguments that print the version; nil: don't run it to ask.
	version []string
	usedBy  []string
	// Only on these systems (runtime.GOOS); empty: everywhere.
	only     string
	homepage string
	// What it is called by Homebrew and by each Linux package manager;
	// missing: not installed that way.
	brew string
	pkgs map[string]string
	// It comes with the system there ("macOS"), so there's nothing to offer.
	builtIn map[string]string
}

var catalog = []tool{
	{
		id: "kubectl", name: "kubectl", about: "The Kubernetes command-line tool",
		programs: []string{"kubectl"},
		version:  []string{"version", "--client"},
		usedBy:   []string{"Kubernetes tab", "Kubernetes contexts: Test Connection", "kubectl port forwards"},
		homepage: "https://kubernetes.io/docs/tasks/tools/",
		brew:     "kubectl",
	},
	{
		id: "aws", name: "AWS CLI", about: "Amazon Web Services command-line interface, version 2",
		programs: []string{"aws"},
		version:  []string{"--version"},
		usedBy:   []string{"AWS tab: Verify and access keys", "AWS account in the address bar", "Kubernetes: Add EKS Cluster"},
		homepage: "https://aws.amazon.com/cli/",
		brew:     "awscli",
	},
	{
		id: "git", name: "git", about: "Version control",
		programs: []string{"git"},
		version:  []string{"--version"},
		usedBy:   []string{"Branch and sync in the address bar", "Git status and ignored files in Files"},
		homepage: "https://git-scm.com/downloads",
		brew:     "git",
		pkgs:     samePkg("git"),
	},
	{
		id: "openssh", name: "OpenSSH client", about: "ssh, ssh-keygen, ssh-add and ssh-keyscan",
		programs: []string{"ssh", "ssh-keygen", "ssh-add", "ssh-keyscan"},
		version:  []string{"-V"},
		usedBy:   []string{"SSH tab: connect, test, keys, host keys, tunnels, file copies"},
		homepage: "https://www.openssh.com/",
		pkgs: map[string]string{
			"apt": "openssh-client", "dnf": "openssh-clients", "yum": "openssh-clients",
			"pacman": "openssh", "zypper": "openssh-clients", "apk": "openssh-client",
		},
		builtIn: map[string]string{"darwin": "macOS"},
	},
	{
		id: "lsof", name: "lsof", about: "Lists open files and network connections",
		programs: []string{"lsof"},
		usedBy:   []string{"Network tab"},
		only:     "darwin",
		builtIn:  map[string]string{"darwin": "macOS"},
	},
	{
		id: "ss", name: "ss or lsof", about: "Lists network connections (ss is part of iproute2)",
		programs: []string{"ss", "lsof"}, any: true,
		usedBy: []string{"Network tab"},
		only:   "linux",
		pkgs: map[string]string{
			"apt": "iproute2", "dnf": "iproute", "yum": "iproute",
			"pacman": "iproute2", "zypper": "iproute2", "apk": "iproute2",
		},
	},
	{
		id: "notify-send", name: "notify-send", about: "Desktop notifications (libnotify)",
		programs: []string{"notify-send"},
		version:  []string{"--version"},
		usedBy:   []string{"Notifications when a long command finishes"},
		only:     "linux",
		pkgs: map[string]string{
			"apt": "libnotify-bin", "dnf": "libnotify", "yum": "libnotify",
			"pacman": "libnotify", "zypper": "libnotify-tools", "apk": "libnotify",
		},
	},
}

func samePkg(name string) map[string]string {
	m := map[string]string{}
	for _, pm := range packageManagers {
		m[pm.id] = name
	}
	return m
}

func lookup(id string) (tool, bool) {
	for _, t := range catalog {
		if t.id == id {
			return t, true
		}
	}
	return tool{}, false
}
