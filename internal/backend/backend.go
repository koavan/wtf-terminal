// Package backend builds wtf's services once, for either UI: the Wails app
// (main.go binds each one) and the native macOS app (cmd/libwtf dispatches
// calls to them by name).
package backend

import (
	"github.com/dayanraj/wtf/internal/about"
	"github.com/dayanraj/wtf/internal/activity"
	"github.com/dayanraj/wtf/internal/awsprofiles"
	"github.com/dayanraj/wtf/internal/certs"
	"github.com/dayanraj/wtf/internal/config"
	"github.com/dayanraj/wtf/internal/files"
	"github.com/dayanraj/wtf/internal/findings"
	"github.com/dayanraj/wtf/internal/infra"
	"github.com/dayanraj/wtf/internal/kube"
	"github.com/dayanraj/wtf/internal/netconn"
	"github.com/dayanraj/wtf/internal/notify"
	"github.com/dayanraj/wtf/internal/procs"
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
	}
}
