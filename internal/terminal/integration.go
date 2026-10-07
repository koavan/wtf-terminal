package terminal

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

// Shell integration scripts emit OSC 133 prompt marks so the frontend knows
// where commands start and end. They are written to disk at startup because
// the shell has to source them by path.
//
//go:embed all:shellintegration
var integrationFS embed.FS

// installIntegration copies the scripts into dir, replacing old versions.
func installIntegration(dir string) error {
	return fs.WalkDir(integrationFS, "shellintegration", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel("shellintegration", path)
		dst := filepath.Join(dir, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := integrationFS.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
}

// integrate returns argv and extra environment that load the integration
// for the given shell. Unsupported shells start normally, as login shells.
func integrate(dir, shellPath string) (args []string, env []string) {
	base := filepath.Base(shellPath)
	login := []string{"-" + base} // leading dash = login shell, like every macOS terminal
	if dir == "" {
		return login, nil
	}
	switch base {
	case "bash":
		// --init-file only applies to non-login shells; the script performs
		// the login startup itself.
		return []string{"bash", "--init-file", filepath.Join(dir, "wtf.bash")},
			[]string{"WTF_SHELL_INTEGRATION=1"}
	case "zsh":
		env = []string{"WTF_SHELL_INTEGRATION=1", "ZDOTDIR=" + filepath.Join(dir, "zsh")}
		if orig, ok := os.LookupEnv("ZDOTDIR"); ok {
			env = append(env, "WTF_ORIG_ZDOTDIR="+orig)
		}
		return login, env
	default:
		return login, nil
	}
}
