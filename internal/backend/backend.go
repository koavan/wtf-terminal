// Package backend builds wtf's services once, for every UI: the Wails app
// (main.go binds each one), the native macOS app (cmd/libwtf dispatches
// calls to them by name) and the native Linux app (cmd/wtf-gtk calls them
// directly).
package backend

import (
	"context"
	"fmt"
	"log"
	"os"
	"slices"

	"github.com/dayanraj/wtf/internal/about"
	"github.com/dayanraj/wtf/internal/activity"
	"github.com/dayanraj/wtf/internal/apiclient"
	"github.com/dayanraj/wtf/internal/awsprofiles"
	"github.com/dayanraj/wtf/internal/bridge"
	"github.com/dayanraj/wtf/internal/certs"
	"github.com/dayanraj/wtf/internal/cloudsecrets"
	"github.com/dayanraj/wtf/internal/cmdhistory"
	"github.com/dayanraj/wtf/internal/config"
	"github.com/dayanraj/wtf/internal/files"
	"github.com/dayanraj/wtf/internal/findings"
	"github.com/dayanraj/wtf/internal/infra"
	"github.com/dayanraj/wtf/internal/kube"
	"github.com/dayanraj/wtf/internal/netconn"
	"github.com/dayanraj/wtf/internal/notify"
	"github.com/dayanraj/wtf/internal/procs"
	"github.com/dayanraj/wtf/internal/review"
	"github.com/dayanraj/wtf/internal/snippets"
	"github.com/dayanraj/wtf/internal/sshhosts"
	"github.com/dayanraj/wtf/internal/state"
	"github.com/dayanraj/wtf/internal/terminal"
	"github.com/dayanraj/wtf/internal/tools"
	"github.com/dayanraj/wtf/internal/usage"
)

// Backend holds every service. The window manager is UI-specific and not
// part of it.
type Backend struct {
	// Background polling pauses while no wtf window is in front.
	Active *activity.Gate
	Store  *state.Store

	Config      *config.Service
	Terminal    *terminal.Service
	State       *state.Service
	Notify      *notify.Service
	Files       *files.Service
	Infra       *infra.Service
	Certs       *certs.Service
	AWSProfiles *awsprofiles.Service
	Kube        *kube.Service
	Snippets    *snippets.Service
	About       *about.Service
	SSHHosts    *sshhosts.Service
	Tools       *tools.Service
	Findings    *findings.Service
	Usage       *usage.Service
	NetConn     *netconn.Service
	Procs       *procs.Service
	APIClient   *apiclient.Service
	Review      *review.Service
	Bridge      *bridge.Service
	History     *cmdhistory.Service
	Secrets     *cloudsecrets.Service

	started []any // by Start, for Shutdown
}

// New builds the services. Nothing runs until the UI starts them. An empty
// stateDir means the default one.
func New(version, commit, buildDate, stateDir string) (*Backend, error) {
	if stateDir == "" {
		var err error
		if stateDir, err = state.DefaultDir(); err != nil {
			return nil, err
		}
	}
	configDir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	b := &Backend{Active: activity.New(), Store: state.NewStore(stateDir)}
	b.Config = config.NewService(configDir, b.Active)
	shellSettings := func() (string, bool) {
		c := b.Config.Current().Config
		return c.Shell, c.ShellIntegration
	}
	b.Terminal = terminal.NewService(version, shellSettings, b.Active)
	b.State = state.NewService(b.Store)
	b.Notify = notify.NewService()
	b.Files = files.NewService()
	b.Infra = infra.NewService()
	b.Certs = certs.NewService()
	b.AWSProfiles = awsprofiles.NewService()
	b.Kube = kube.NewService(stateDir)
	b.Snippets = snippets.NewService(configDir)
	b.About = about.NewService(version, commit, buildDate, configDir, stateDir, b.Terminal.ShellInfo)
	b.SSHHosts = sshhosts.NewService(stateDir)
	b.Tools = tools.NewService(stateDir, b.Terminal.ShellInfo)
	b.Findings = findings.NewService()
	b.Usage = usage.NewService(b.Terminal.ShellPIDs)
	b.NetConn = netconn.NewService(b.Terminal.ShellPIDs)
	b.Procs = procs.NewService(b.Terminal.ShellPIDs)
	b.APIClient = apiclient.NewService()
	b.Review = review.NewService(b.Infra)
	b.Secrets = cloudsecrets.NewService(b.Infra)
	// Self-checks don't add to (or prune) the user's saved history; the
	// history check saves to a throwaway folder.
	switch debug := os.Getenv("WTF_NATIVE_DEBUG"); debug {
	case "history":
		dir, err := os.MkdirTemp("", "wtf-history-check-")
		if err != nil {
			return nil, err
		}
		b.History = cmdhistory.NewService(dir, b.Infra)
	default:
		b.History = cmdhistory.NewService(stateDir, b.Infra)
		b.History.ReadOnly = debug != ""
	}
	b.History.Days = func() int { return b.Config.Current().Config.HistoryDays }
	b.Bridge = bridge.NewService(b.Infra)
	b.Bridge.History = b.History
	b.Bridge.AutoMode = func() bool { return b.Config.Current().Config.ClaudeAutoMode }
	// The Claude pane self-checks (WTF_NATIVE_DEBUG=claude) run a stand-in,
	// never the user's real claude.
	b.Bridge.Program = func() string {
		if os.Getenv("WTF_NATIVE_DEBUG") == "claude" {
			if p := os.Getenv("WTF_CLAUDE_STANDIN"); p != "" {
				return p
			}
			return "/nonexistent/wtf-claude-standin"
		}
		return ""
	}
	b.Bridge.AgentStatus = func() bool { return b.Config.Current().Config.AgentStatus }
	b.Terminal.ExtraEnv = bridge.ShellEnv(b.Bridge)
	b.Terminal.OnExit = b.Bridge.ShellClosed
	b.Terminal.OnAtShell = b.Bridge.Forget
	return b, nil
}

// Named is a service with the name the native UI calls it by: the last
// element of its Go package path, as in the generated Wails bindings.
type Named struct {
	Name    string
	Service any
}

// Services lists every service in startup order (the order main.go binds
// them in).
func (b *Backend) Services() []Named {
	return []Named{
		{"config", b.Config},
		{"terminal", b.Terminal},
		{"state", b.State},
		{"notify", b.Notify},
		{"files", b.Files},
		{"infra", b.Infra},
		{"certs", b.Certs},
		{"awsprofiles", b.AWSProfiles},
		{"kube", b.Kube},
		{"snippets", b.Snippets},
		{"about", b.About},
		{"sshhosts", b.SSHHosts},
		{"tools", b.Tools},
		{"findings", b.Findings},
		{"usage", b.Usage},
		{"netconn", b.NetConn},
		{"procs", b.Procs},
		{"apiclient", b.APIClient},
		{"review", b.Review},
		{"bridge", b.Bridge},
		{"cmdhistory", b.History},
		{"cloudsecrets", b.Secrets},
	}
}

// Lifecycle hooks, from each package's native.go.
type starter interface{ ServiceStartup(context.Context) error }
type stopper interface{ ServiceShutdown() error }

// Start runs each service's startup hook in order, for the native UIs (Wails
// runs them itself). If one fails, the ones already started are stopped.
func (b *Backend) Start(ctx context.Context) error {
	for _, n := range b.Services() {
		if s, ok := n.Service.(starter); ok {
			if err := s.ServiceStartup(ctx); err != nil {
				b.stop()
				return fmt.Errorf("starting %s: %w", n.Name, err)
			}
		}
		b.started = append(b.started, n.Service)
	}
	return nil
}

// Shutdown stops the services Start started, last started first.
func (b *Backend) Shutdown() { b.stop() }

func (b *Backend) stop() {
	for _, svc := range slices.Backward(b.started) {
		if s, ok := svc.(stopper); ok {
			if err := s.ServiceShutdown(); err != nil {
				log.Printf("shutdown: %v", err)
			}
		}
	}
	b.started = nil
}
