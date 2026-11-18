package runner

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
)

// probeScript runs on the runner with `sh -s`. It prints key=value lines
// and changes nothing. claude is looked up the way the user's login shell
// finds it, then in the native installer's ~/.local/bin. ANTHROPIC_API_KEY
// is only tested for being set; its value never leaves the runner.
const probeScript = `
echo "os=$(uname -s)"
echo "arch=$(uname -m)"
echo "home=$HOME"
echo "tmux=$(tmux -V 2>/dev/null)"
for pm in apt-get dnf yum pacman zypper apk; do
	if command -v "$pm" >/dev/null 2>&1; then echo "pm=$pm"; break; fi
done
c=$("${SHELL:-sh}" -lc 'command -v claude' </dev/null 2>/dev/null | tail -n 1)
[ -n "$c" ] && [ -x "$c" ] || c="$HOME/.local/bin/claude"
[ -x "$c" ] || c=""
echo "claude=$c"
if [ -n "$c" ]; then
	echo "claudeVersion=$("$c" --version </dev/null 2>/dev/null | head -n 1)"
	echo "auth=$("$c" auth status --json </dev/null 2>/dev/null | tr -d '\n')"
fi
grep -q '"hasCompletedOnboarding": *true' "$HOME/.claude.json" 2>/dev/null && echo "onboarded=1"
if "${SHELL:-sh}" -lc '[ -n "${ANTHROPIC_API_KEY:-}" ]' </dev/null >/dev/null 2>&1; then echo "apiKey=1"; fi
r="$HOME/.local/share/wtf-runner/wtf-runner"
[ -x "$r" ] && echo "runner=$("$r" --version </dev/null 2>/dev/null | head -n 1)"
echo "end=1"
`

// Facts is what a check found on a runner. Only facts: wtf never guesses.
type Facts struct {
	// The check reached the runner and the probe finished; Error says why
	// not.
	Reachable bool   `json:"reachable"`
	Error     string `json:"error,omitempty"`
	// uname -s and -m, and the Go arch wtf's binaries are built for ("" if
	// there are none for it).
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	GoArch string `json:"goarch"`
	Home   string `json:"home"`
	// tmux -V ("" without tmux) and the package manager to install it with.
	Tmux           string `json:"tmux"`
	PackageManager string `json:"packageManager"`
	// Claude Code: where, which version, and whether it's signed in
	// (claude auth status).
	Claude        string `json:"claude"`
	ClaudeVersion string `json:"claudeVersion"`
	SignedIn      bool   `json:"signedIn"`
	AuthMethod    string `json:"authMethod"`
	Account       string `json:"account"`
	Org           string `json:"org"`
	Plan          string `json:"plan"`
	LoginExpires  string `json:"loginExpires"`
	// Claude Code's first-run screens (theme, notes) are done; until they
	// are, an agent started there waits on them before any hook fires.
	Onboarded bool `json:"onboarded"`
	// ANTHROPIC_API_KEY is set in the user's login shell there; wtf
	// removes it from the agents it starts.
	APIKeySet bool `json:"apiKeySet"`
	// wtf-runner's version and protocol ("" if it isn't installed).
	RunnerVersion  string `json:"runnerVersion"`
	RunnerProtocol string `json:"runnerProtocol"`
}

// goArch maps uname -m to the Go arch of the binaries wtf ships.
func goArch(m string) string {
	switch m {
	case "x86_64", "amd64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	}
	return ""
}

// parseProbe reads probeScript's output.
func parseProbe(out []byte) Facts {
	var f Facts
	end := false
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch k {
		case "os":
			f.OS = v
		case "arch":
			f.Arch, f.GoArch = v, goArch(v)
		case "home":
			f.Home = v
		case "tmux":
			f.Tmux = v
		case "pm":
			f.PackageManager = v
		case "claude":
			f.Claude = v
		case "claudeVersion":
			f.ClaudeVersion = strings.TrimSuffix(v, " (Claude Code)")
		case "auth":
			var a struct {
				LoggedIn     bool   `json:"loggedIn"`
				AuthMethod   string `json:"authMethod"`
				Email        string `json:"email"`
				OrgName      string `json:"orgName"`
				Subscription string `json:"subscriptionType"`
				Expires      string `json:"loginExpiresAt"`
			}
			if json.Unmarshal([]byte(v), &a) == nil {
				f.SignedIn, f.AuthMethod, f.Account, f.Org, f.Plan, f.LoginExpires =
					a.LoggedIn, a.AuthMethod, a.Email, a.OrgName, a.Subscription, a.Expires
			}
		case "onboarded":
			f.Onboarded = v == "1"
		case "apiKey":
			f.APIKeySet = v == "1"
		case "runner":
			ver, proto, _ := strings.Cut(v, " protocol=")
			f.RunnerVersion, f.RunnerProtocol = ver, proto
		case "end":
			end = true
		}
	}
	f.Reachable = end
	return f
}

// Problems lists what stops agents running there, in the order to fix
// them. Empty means ready.
func (f Facts) Problems() []string {
	if !f.Reachable {
		return []string{"can't reach it over SSH"}
	}
	var p []string
	if f.OS != "Linux" {
		p = append(p, "it isn't Linux ("+f.OS+")")
	} else if f.GoArch == "" {
		p = append(p, "wtf has no runner for "+f.Arch)
	}
	if f.Tmux == "" {
		p = append(p, "tmux isn't installed")
	}
	if f.Claude == "" {
		p = append(p, "Claude Code isn't installed")
	} else if !f.SignedIn {
		p = append(p, "Claude Code isn't signed in")
	} else if f.AuthMethod != "claude.ai" {
		// Agents here use your Claude account, never API billing.
		p = append(p, "Claude Code is signed in with "+f.AuthMethod+", not a Claude account")
	} else if !f.Onboarded {
		p = append(p, "Claude Code's first-run setup isn't done")
	}
	if f.RunnerVersion == "" {
		p = append(p, "wtf-runner isn't installed")
	}
	return p
}
