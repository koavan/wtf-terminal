// Package explain describes a shell as facts for the wtf plugin's tools
// (internal/bridge, #32): its folder, system, Kubernetes, AWS, Google Cloud,
// Terraform and git context (read from files, never asked of a cloud), and
// the well-known tools on its PATH.
package explain

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/dayanraj/wtf/internal/infra"
	"github.com/dayanraj/wtf/internal/tools"
)

// ShellFacts are the lines that say where a shell is: its folder (home as
// ~), system and shell, and the Kubernetes, AWS, Google Cloud, Terraform
// and git context its variables point at. They're read from files only,
// never asked of a cloud.
func ShellFacts(in *infra.Service, env map[string]string, cwd string) string {
	var b strings.Builder
	line := func(label, value string) {
		if value != "" {
			fmt.Fprintf(&b, "%s: %s\n", label, value)
		}
	}
	line("Folder", tilde(cwd))
	line("System", systemLine(env))
	if in != nil && env != nil {
		if env["PWD"] == "" && cwd != "" {
			env = copyEnv(env)
			env["PWD"] = cwd
		}
		c := in.Resolve(env)
		line("Kubernetes", kube(c))
		line("AWS", aws(c))
		line("Google Cloud project", c.GCPProject)
		line("Terraform workspace", c.TFWorkspace)
		if g := c.Git; g != nil {
			line("Git", gitLine(g))
		}
	}
	return b.String()
}

// ToolsOnPath are the well-known tools found on a shell's PATH, sorted.
// Nothing is run.
func ToolsOnPath(env map[string]string) []string {
	path := env["PATH"]
	if path == "" {
		path = os.Getenv("PATH")
	}
	var found []string
	for _, name := range wellKnown {
		if tools.Find(name, path) != "" {
			found = append(found, name)
		}
	}
	slices.Sort(found)
	return found
}

// tilde shortens the home folder to ~ (it names the user).
func tilde(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || p == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(p, home+"/"); ok {
		return "~/" + rest
	}
	return p
}

// systemLine is "macOS, zsh".
func systemLine(env map[string]string) string {
	osName := map[string]string{"darwin": "macOS", "linux": "Linux"}[runtime.GOOS]
	if osName == "" {
		osName = runtime.GOOS
	}
	if sh := filepath.Base(env["SHELL"]); env["SHELL"] != "" {
		return osName + ", " + sh
	}
	return osName
}

func kube(c infra.Context) string {
	if c.KubeContext == "" {
		return ""
	}
	parts := []string{"context " + c.KubeContext}
	if c.KubeCluster != "" && c.KubeCluster != c.KubeContext {
		parts = append(parts, "cluster "+c.KubeCluster)
	}
	if c.KubeNamespace != "" {
		parts = append(parts, "namespace "+c.KubeNamespace)
	}
	return strings.Join(parts, ", ")
}

func aws(c infra.Context) string {
	if c.AWSProfile == "" {
		return ""
	}
	parts := []string{"profile " + c.AWSProfile}
	if c.AWSRegion != "" {
		parts = append(parts, "region "+c.AWSRegion)
	}
	if c.AWSAccount != "" {
		parts = append(parts, "account "+c.AWSAccount)
	}
	return strings.Join(parts, ", ")
}

func gitLine(g *infra.Git) string {
	s := "branch " + g.Branch
	if g.Detached {
		s = "detached at " + g.Branch
	}
	if g.Upstream != "" {
		s += ", upstream " + g.Upstream
		if g.UpstreamGone {
			s += " (gone)"
		}
	}
	return s
}

func copyEnv(env map[string]string) map[string]string {
	c := make(map[string]string, len(env)+1)
	for k, v := range env {
		c[k] = v
	}
	return c
}

// wellKnown are the tools ToolsOnPath looks for.
var wellKnown = []string{
	"kubectl", "helm", "kustomize", "aws", "eksctl", "gcloud", "terraform", "tofu", "terragrunt", "packer",
	"pulumi", "ansible", "ansible-playbook", "vault", "docker", "podman", "kind", "minikube", "argocd",
	"istioctl", "skaffold", "git", "gh", "ssh", "scp", "curl", "jq", "node", "npm", "npx", "yarn", "pnpm",
	"deno", "bun", "python", "python3", "pip", "pip3", "go", "cargo", "rustc", "java", "mvn", "ruby", "bundle",
	"make", "psql", "mysql", "redis-cli",
}
