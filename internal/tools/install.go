package tools

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Method is one way to install a tool. It runs in a terminal tab, so the
// user sees every step and answers sudo's password prompt themselves.
type Method struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// What the tab runs: the command itself, or `sh <script>` for Script.
	Command string `json:"command"`
	// A script wtf writes before running it; shown in full first.
	Script string `json:"script"`
	// It asks for the user's password (sudo).
	Sudo bool `json:"sudo"`
	// Where the program ends up, and how the download is checked.
	Dest   string `json:"dest"`
	Verify string `json:"verify"`
	// Something to do afterwards, e.g. put ~/.local/bin on PATH.
	Note string `json:"note"`
}

type packageManager struct {
	id      string
	program string
	install string // %s: the package
}

// packageManagers are tried in this order; the first one found is used.
var packageManagers = []packageManager{
	{"apt", "apt-get", "sudo apt-get update && sudo apt-get install %s"},
	{"dnf", "dnf", "sudo dnf install %s"},
	{"yum", "yum", "sudo yum install %s"},
	{"pacman", "pacman", "sudo pacman -S --needed %s"},
	{"zypper", "zypper", "sudo zypper install %s"},
	{"apk", "apk", "sudo apk add %s"},
}

// platform is what the install methods depend on, gathered once per check.
type platform struct {
	goos, goarch string
	home         string
	// The Homebrew program, "" without it.
	brew string
	// The Linux package manager, nil without one.
	pm *packageManager
	// unzip is there (the AWS CLI's Linux installer is a zip).
	unzip bool
	// The shell's PATH and the shell program, for the PATH note.
	shellPATH, shell string
}

func detectPlatform(goos, goarch, home, shellPATH, shell string) platform {
	p := platform{goos: goos, goarch: goarch, home: home, shellPATH: shellPATH, shell: shell}
	switch goos {
	case "darwin":
		p.brew = Find("brew", shellPATH)
	case "linux":
		for i := range packageManagers {
			if Find(packageManagers[i].program, shellPATH) != "" {
				p.pm = &packageManagers[i]
				break
			}
		}
		p.unzip = Find("unzip", shellPATH) != ""
	}
	return p
}

// methods lists the ways to install t here, the recommended one first.
func (p platform) methods(t tool) []Method {
	var out []Method
	if p.goos == "darwin" && p.brew != "" && t.brew != "" {
		out = append(out, p.brewMethod(t))
	}
	switch t.id {
	case "kubectl":
		out = append(out, p.kubectlOfficial())
	case "aws":
		if m, ok := p.awsOfficial(); ok {
			out = append(out, m)
		}
	case "git":
		if p.goos == "darwin" {
			out = append(out, Method{
				ID: "xcode", Label: "Apple Command Line Tools",
				Command: "xcode-select --install",
				Dest:    "/Library/Developer/CommandLineTools (git, make, clang…)",
				Verify:  "Installed by macOS's Software Update, signed by Apple.",
				Note:    "macOS opens a window to confirm and download.",
			})
		}
	}
	if p.goos == "linux" && p.pm != nil && t.pkgs[p.pm.id] != "" {
		out = append(out, p.packageMethod(t))
	}
	return out
}

func (p platform) brewMethod(t tool) Method {
	// Its full path when the shell wouldn't find it by name.
	brew := "brew"
	if !onPATH(filepath.Dir(p.brew), p.shellPATH) {
		brew = shellQuote(p.brew)
	}
	return Method{
		ID: "brew", Label: "Homebrew",
		Command: fmt.Sprintf("%s install %s", brew, t.brew),
		Dest:    filepath.Dir(p.brew),
		Verify:  "Homebrew checks each download's SHA-256.",
	}
}

func (p platform) packageMethod(t tool) Method {
	pkg := t.pkgs[p.pm.id]
	return Method{
		ID: "package", Label: fmt.Sprintf("%s package %s", p.pm.program, pkg),
		Command: fmt.Sprintf(p.pm.install, pkg),
		Sudo:    true,
		Dest:    "/usr/bin",
		Verify:  "The package manager checks the distribution's signatures.",
	}
}

// localBin is where wtf's own installs go: the user's, so no sudo.
func (p platform) localBin() string { return filepath.Join(p.home, ".local", "bin") }

// pathNote says how to put ~/.local/bin on the shell's PATH when it isn't.
// wtf finds programs there either way.
func (p platform) pathNote(program string) string {
	if onPATH(p.localBin(), p.shellPATH) {
		return ""
	}
	line := `export PATH="$HOME/.local/bin:$PATH"`
	rc := "~/.profile"
	switch filepath.Base(p.shell) {
	case "zsh":
		rc = "~/.zshrc"
	case "bash":
		rc = "~/.bashrc"
		if p.goos == "darwin" {
			rc = "~/.bash_profile"
		}
	case "fish":
		line, rc = "fish_add_path ~/.local/bin", "once in fish (it remembers)"
	}
	return fmt.Sprintf("~/.local/bin isn't on your shell's PATH. wtf finds %s there anyway; to type it in the terminal, add this to %s: %s",
		program, rc, line)
}

func (p platform) kubectlOfficial() Method {
	arch := p.goarch // amd64 or arm64, as dl.k8s.io names them
	check := "sha256sum --check"
	if p.goos == "darwin" {
		check = "shasum -a 256 --check"
	}
	script := scriptHeader("kubectl") + fmt.Sprintf(`version=$(curl -fsSL https://dl.k8s.io/release/stable.txt)
url="https://dl.k8s.io/release/$version/bin/%[1]s/%[2]s/kubectl"
echo "==> Downloading kubectl $version for %[1]s/%[2]s"
curl -fL --progress-bar -o kubectl "$url"
curl -fsSL -o kubectl.sha256 "$url.sha256"
echo "==> Checking its SHA-256 checksum"
echo "$(cat kubectl.sha256)  kubectl" | %[3]s
mkdir -p "$HOME/.local/bin"
install -m 0755 kubectl "$HOME/.local/bin/kubectl"
echo "==> Installed $HOME/.local/bin/kubectl"
"$HOME/.local/bin/kubectl" version --client
`, p.goos, arch, check)
	return Method{
		ID: "official", Label: "Official release (dl.k8s.io)",
		Script: script,
		Dest:   "~/.local/bin/kubectl",
		Verify: "Checked against the SHA-256 checksum published next to the download.",
		Note:   p.pathNote("kubectl"),
	}
}

func (p platform) awsOfficial() (Method, bool) {
	switch p.goos {
	case "darwin":
		script := scriptHeader("the AWS CLI") + `echo "==> Downloading the AWS CLI v2 installer (works on Apple silicon and Intel)"
curl -fL --progress-bar -o AWSCLIV2.pkg https://awscli.amazonaws.com/AWSCLIV2.pkg
echo "==> Checking the package's signature"
pkgutil --check-signature AWSCLIV2.pkg
echo "==> Installing (sudo asks for your password)"
sudo installer -pkg AWSCLIV2.pkg -target /
/usr/local/bin/aws --version
`
		return Method{
			ID: "official", Label: "Official installer (awscli.amazonaws.com)",
			Script: script, Sudo: true,
			Dest:   "/usr/local/aws-cli, linked from /usr/local/bin/aws",
			Verify: "The package's Apple Developer ID signature is checked before installing.",
		}, true
	case "linux":
		arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[p.goarch]
		if arch == "" {
			return Method{}, false
		}
		var needUnzip string
		if !p.unzip {
			install := "your package manager"
			if p.pm != nil {
				install = fmt.Sprintf(p.pm.install, "unzip")
			}
			needUnzip = fmt.Sprintf(`if ! command -v unzip >/dev/null 2>&1; then
  echo "==> unzip is needed to unpack the installer"
  %s
fi
`, install)
			if p.pm == nil {
				needUnzip = `command -v unzip >/dev/null 2>&1 || { echo "unzip is needed: install it with your package manager"; exit 1; }
`
			}
		}
		script := scriptHeader("the AWS CLI") + needUnzip + fmt.Sprintf(`echo "==> Downloading the AWS CLI v2 for linux/%[1]s"
curl -fL --progress-bar -o awscliv2.zip https://awscli.amazonaws.com/awscli-exe-linux-%[1]s.zip
unzip -q awscliv2.zip
./aws/install --install-dir "$HOME/.local/aws-cli" --bin-dir "$HOME/.local/bin" --update
echo "==> Installed $HOME/.local/bin/aws"
"$HOME/.local/bin/aws" --version
`, arch)
		return Method{
			ID: "official", Label: "Official installer (awscli.amazonaws.com)",
			Script: script, Sudo: !p.unzip && p.pm != nil,
			Dest:   "~/.local/aws-cli, linked from ~/.local/bin/aws",
			Verify: "Downloaded over HTTPS from AWS. AWS signs it with PGP but publishes no checksum, so wtf doesn't check it further.",
			Note:   p.pathNote("aws"),
		}, true
	}
	return Method{}, false
}

// scriptHeader stops at the first failure and works in a temporary folder
// that is removed afterwards.
func scriptHeader(what string) string {
	return fmt.Sprintf(`#!/bin/sh
# Written by wtf to install %s. Safe to delete.
set -eu
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
cd "$tmp"
`, what)
}

// shellQuote quotes s for sh, bash, zsh and fish alike.
func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r == '/' || r == '.' || r == '-' || r == '_' || r == '+' || r == ':' ||
			r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
