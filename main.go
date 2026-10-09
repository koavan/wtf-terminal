package main

import (
	"embed"
	"log"
	"runtime"
	"strconv"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/dayanraj/wtf/internal/config"
	"github.com/dayanraj/wtf/internal/files"
	"github.com/dayanraj/wtf/internal/infra"
	"github.com/dayanraj/wtf/internal/netconn"
	"github.com/dayanraj/wtf/internal/notify"
	"github.com/dayanraj/wtf/internal/procs"
	"github.com/dayanraj/wtf/internal/sshhosts"
	"github.com/dayanraj/wtf/internal/state"
	"github.com/dayanraj/wtf/internal/terminal"
	wtfwindows "github.com/dayanraj/wtf/internal/windows"
)

const version = "0.1.0"

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	stateDir, err := state.DefaultDir()
	if err != nil {
		log.Fatal(err)
	}
	configDir, err := config.Dir()
	if err != nil {
		log.Fatal(err)
	}
	cfg := config.NewService(configDir)
	shellSettings := func() (string, bool) {
		c := cfg.Current().Config
		return c.Shell, c.ShellIntegration
	}
	term := terminal.NewService(version, shellSettings)
	store := state.NewStore(stateDir)
	windows := wtfwindows.NewManager(term, store, windowOptions)

	app := application.New(application.Options{
		Name:        "wtf",
		Description: "A browser-like terminal",
		Services: []application.Service{
			application.NewService(cfg),
			application.NewService(term),
			application.NewService(state.NewService(store)),
			application.NewService(notify.NewService()),
			application.NewService(files.NewService()),
			application.NewService(infra.NewService()),
			application.NewService(sshhosts.NewService(stateDir)),
			application.NewService(netconn.NewService(term.ShellPIDs)),
			application.NewService(procs.NewService(term.ShellPIDs)),
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
	appMenu.AddRole(application.About)
	appMenu.AddSeparator()
	cmd(appMenu, "Settings…", "CmdOrCtrl+,", "config.open")
	cmd(appMenu, "Change Theme…", "", "theme.pick")
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
	cmd(edit, "Clear Buffer", "CmdOrCtrl+k", "term.clear")

	view := menu.AddSubmenu("View")
	cmd(view, "Command Palette…", "CmdOrCtrl+shift+p", "palette")
	cmd(view, "Toggle Sidebar", "CmdOrCtrl+b", "sidebar.toggle")
	cmd(view, "Show Files", "CmdOrCtrl+shift+e", "sidebar.files")
	cmd(view, "Show Network Connections", "CmdOrCtrl+shift+o", "sidebar.network")
	cmd(view, "Show Processes", "CmdOrCtrl+shift+a", "sidebar.processes")
	cmd(view, "Show Command History", "CmdOrCtrl+shift+h", "sidebar.history")
	cmd(view, "Show SSH Hosts", "CmdOrCtrl+shift+s", "sidebar.ssh")
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
	return menu
}
