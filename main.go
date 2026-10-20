package main

import (
	"embed"
	"log"
	"runtime"
	"strconv"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/dayanraj/wtf/internal/backend"
	"github.com/dayanraj/wtf/internal/events"
	wtfwindows "github.com/dayanraj/wtf/internal/windows"
)

const version = "1.1.0"

// Set by the build (see build/*/Taskfile.yml): the git commit and the date.
var (
	commit    string
	buildDate string
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	b, err := backend.New(version, commit, buildDate, "")
	if err != nil {
		log.Fatal(err)
	}
	windows := wtfwindows.NewManager(b.Terminal, b.Store, windowOptions, b.Active)

	app := application.New(application.Options{
		Name:        "wtf",
		Description: "A browser-like terminal",
		// Listed one by one: the bindings generator needs each concrete type.
		Services: []application.Service{
			application.NewService(b.Config),
			application.NewService(b.Terminal),
			application.NewService(b.State),
			application.NewService(b.Notify),
			application.NewService(b.Files),
			application.NewService(b.Infra),
			application.NewService(b.Certs),
			application.NewService(b.AWSProfiles),
			application.NewService(b.Kube),
			application.NewService(b.Snippets),
			application.NewService(b.About),
			application.NewService(b.SSHHosts),
			application.NewService(b.Tools),
			application.NewService(b.Findings),
			application.NewService(b.Usage),
			application.NewService(b.NetConn),
			application.NewService(b.Procs),
			application.NewService(windows.Service()),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		ShouldQuit: windows.ShouldQuit,
	})

	events.SetEmitter(func(name string, data any) { app.Event.Emit(name, data) })

	if runtime.GOOS == "darwin" {
		app.Menu.SetApplicationMenu(buildMenu(windows))
	}
	windows.Start(app)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func windowOptions() application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Title:     "wtf",
		Width:     1100,
		Height:    700,
		MinWidth:  480,
		MinHeight: 300,
		Mac: application.MacWindow{
			// The tab strip is our title bar: traffic lights sit inside it.
			TitleBar:                application.MacTitleBarHiddenInsetUnified,
			Backdrop:                application.MacBackdropTranslucent,
			InvisibleTitleBarHeight: 0,
		},
		Linux: application.LinuxWindow{
			// xterm.js renders through WebGL; WebKitGTK disables the GPU by default.
			WebviewGpuPolicy: application.WebviewGpuPolicyAlways,
		},
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		URL:              "/",
	}
}

func buildMenu(windows *wtfwindows.Manager) *application.Menu {
	cmd := func(m *application.Menu, label, accel, command string) {
		item := m.Add(label).OnClick(func(*application.Context) {
			windows.SendCommand(command)
		})
		if accel != "" {
			item.SetAccelerator(accel)
		}
	}

	menu := application.NewMenu()
	appMenu := menu.AddSubmenu("wtf")
	cmd(appMenu, "About wtf", "", "app.about")
	appMenu.AddSeparator()
	cmd(appMenu, "Settings…", "CmdOrCtrl+,", "config.open")
	cmd(appMenu, "Change Theme…", "", "theme.pick")
	cmd(appMenu, "Command-Line Tools…", "", "tools.show")
	appMenu.AddSeparator()
	appMenu.AddRole(application.ServicesMenu)
	appMenu.AddSeparator()
	appMenu.AddRole(application.Hide)
	appMenu.AddRole(application.HideOthers)
	appMenu.AddRole(application.ShowAll)
	appMenu.AddSeparator()
	appMenu.AddRole(application.Quit)

	shell := menu.AddSubmenu("Shell")
	cmd(shell, "New Tab", "CmdOrCtrl+t", "tab.new")
	cmd(shell, "New Window", "CmdOrCtrl+n", "window.new")
	cmd(shell, "Split Right", "CmdOrCtrl+d", "pane.splitRight")
	cmd(shell, "Split Down", "CmdOrCtrl+shift+d", "pane.splitDown")
	cmd(shell, "Duplicate Tab", "", "tab.duplicate")
	cmd(shell, "Run Snippet…", "CmdOrCtrl+shift+r", "snippets.run")
	cmd(shell, "Open Workspace…", "", "workspace.open")
	cmd(shell, "Save Window as Workspace…", "", "workspace.save")
	shell.AddSeparator()
	cmd(shell, "Start or Stop Recording…", "CmdOrCtrl+OptionOrAlt+s", "record.toggle")
	cmd(shell, "Save Output as Text…", "", "export.text")
	cmd(shell, "Save Commands as Markdown…", "", "export.markdown")
	shell.AddSeparator()
	cmd(shell, "Broadcast Input", "CmdOrCtrl+OptionOrAlt+b", "broadcast.toggle")
	cmd(shell, "Move Tab to New Window", "", "tab.toNewWindow")
	shell.AddSeparator()
	cmd(shell, "Close", "CmdOrCtrl+w", "pane.close")
	cmd(shell, "Close Tab", "CmdOrCtrl+OptionOrAlt+w", "tab.close")
	cmd(shell, "Reopen Closed Tab", "CmdOrCtrl+shift+t", "tab.reopen")
	shell.AddSeparator()
	cmd(shell, "Close Window", "CmdOrCtrl+shift+w", "window.close")

	edit := menu.AddSubmenu("Edit")
	edit.AddRole(application.Copy)
	edit.AddRole(application.Paste)
	cmd(edit, "Select All", "CmdOrCtrl+a", "term.selectAll")
	edit.AddSeparator()
	cmd(edit, "Find…", "CmdOrCtrl+f", "find")
	cmd(edit, "Find Next", "CmdOrCtrl+g", "find.next")
	cmd(edit, "Find Previous", "CmdOrCtrl+shift+g", "find.prev")
	edit.AddSeparator()
	cmd(edit, "Copy Last Command Output", "CmdOrCtrl+OptionOrAlt+c", "term.copyOutput")
	cmd(edit, "Pin Last Command Output", "", "term.pinOutput")
	cmd(edit, "Clear Buffer", "CmdOrCtrl+k", "term.clear")

	view := menu.AddSubmenu("View")
	cmd(view, "Command Palette…", "CmdOrCtrl+shift+p", "palette")
	cmd(view, "Toggle Sidebar", "CmdOrCtrl+b", "sidebar.toggle")
	cmd(view, "Show Files", "CmdOrCtrl+shift+e", "sidebar.files")
	cmd(view, "Show Network Connections", "CmdOrCtrl+shift+o", "sidebar.network")
	cmd(view, "Show Processes", "CmdOrCtrl+shift+a", "sidebar.processes")
	cmd(view, "Show Command History", "CmdOrCtrl+shift+h", "sidebar.history")
	cmd(view, "Show Pinned Output", "CmdOrCtrl+shift+i", "sidebar.pins")
	cmd(view, "Show Share & Record", "CmdOrCtrl+shift+j", "sidebar.share")
	cmd(view, "Show Generate", "CmdOrCtrl+shift+n", "sidebar.generate")
	cmd(view, "Show SSH Hosts", "CmdOrCtrl+shift+s", "sidebar.ssh")
	cmd(view, "Show AWS Profiles", "CmdOrCtrl+shift+u", "sidebar.aws")
	cmd(view, "Show Kubernetes", "CmdOrCtrl+shift+k", "sidebar.kube")
	cmd(view, "Show Snippets", "CmdOrCtrl+shift+y", "sidebar.snippets")
	cmd(view, "Show Workspaces", "CmdOrCtrl+shift+l", "sidebar.workspaces")
	cmd(view, "Show Findings", "CmdOrCtrl+shift+f", "sidebar.findings")
	cmd(view, "Hide Secrets", "CmdOrCtrl+OptionOrAlt+r", "secrets.toggle")
	cmd(view, "Resource Usage…", "", "usage.show")
	view.AddSeparator()
	cmd(view, "Previous Prompt", "CmdOrCtrl+up", "prompt.prev")
	cmd(view, "Next Prompt", "CmdOrCtrl+down", "prompt.next")
	view.AddSeparator()
	cmd(view, "Bigger Text", "CmdOrCtrl+=", "font.bigger")
	cmd(view, "Smaller Text", "CmdOrCtrl+-", "font.smaller")
	cmd(view, "Actual Size", "CmdOrCtrl+0", "font.reset")
	view.AddSeparator()
	view.AddRole(application.ToggleFullscreen)

	tabs := menu.AddSubmenu("Tab")
	cmd(tabs, "Show All Tabs", "CmdOrCtrl+shift+\\", "tab.overview")
	tabs.AddSeparator()
	cmd(tabs, "Show Next Tab", "CmdOrCtrl+shift+]", "tab.next")
	cmd(tabs, "Show Previous Tab", "CmdOrCtrl+shift+[", "tab.prev")
	cmd(tabs, "Move Tab Right", "CmdOrCtrl+shift+right", "tab.moveRight")
	cmd(tabs, "Move Tab Left", "CmdOrCtrl+shift+left", "tab.moveLeft")
	tabs.AddSeparator()
	cmd(tabs, "Add Tab to New Group", "CmdOrCtrl+OptionOrAlt+g", "tab.group")
	tabs.AddSeparator()
	for i := 1; i <= 8; i++ {
		cmd(tabs, "Select Tab "+strconv.Itoa(i), "CmdOrCtrl+"+strconv.Itoa(i), "tab.select."+strconv.Itoa(i))
	}
	cmd(tabs, "Select Last Tab", "CmdOrCtrl+9", "tab.select.last")

	panes := menu.AddSubmenu("Pane")
	cmd(panes, "Toggle Zoom", "CmdOrCtrl+shift+return", "pane.zoom")
	panes.AddSeparator()
	cmd(panes, "Select Next Pane", "CmdOrCtrl+]", "pane.next")
	cmd(panes, "Select Previous Pane", "CmdOrCtrl+[", "pane.prev")
	cmd(panes, "Select Pane Left", "CmdOrCtrl+OptionOrAlt+left", "pane.left")
	cmd(panes, "Select Pane Right", "CmdOrCtrl+OptionOrAlt+right", "pane.right")
	cmd(panes, "Select Pane Above", "CmdOrCtrl+OptionOrAlt+up", "pane.up")
	cmd(panes, "Select Pane Below", "CmdOrCtrl+OptionOrAlt+down", "pane.down")

	menu.AddRole(application.WindowMenu)

	help := menu.AddSubmenu("Help")
	cmd(help, "wtf Manual", "CmdOrCtrl+?", "help.manual")
	cmd(help, "Keyboard Shortcuts", "", "help.shortcuts")
	return menu
}
