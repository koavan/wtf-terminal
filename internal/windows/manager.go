// Package windows manages the app's windows: opening and restoring them,
// handing tabs from one to another, and asking before closing or quitting
// while programs are still running.
package windows

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/dayanraj/wtf/internal/state"
	"github.com/dayanraj/wtf/internal/terminal"
)

// Events. Wails delivers every event to every window, so each one names the
// window it is for and the others ignore it.
const (
	// EventCommand carries a menu command to the focused window.
	EventCommand = "app:command"
	// EventCloseRequested asks a window to confirm closing (its close button was clicked).
	EventCloseRequested = "window:closeRequested"
	// EventQuitRequested asks a window to confirm quitting while programs run.
	EventQuitRequested = "app:quitRequested"
	// EventAdopt hands a tab to another window.
	EventAdopt = "window:adopt"
)

type Command struct {
	Window string `json:"window"`
	Name   string `json:"name"`
}

type QuitRequest struct {
	Window  string             `json:"window"`
	Running []terminal.Running `json:"running"`
}

type Adoption struct {
	Window string `json:"window"`
	// Payload is a tab snapshot (see frontend lib/tabs.svelte.ts).
	Payload string `json:"payload"`
}

type Info struct {
	Name  string `json:"name"`
	Title string `json:"title"`
}

func init() {
	application.RegisterEvent[Command](EventCommand)
	application.RegisterEvent[string](EventCloseRequested)
	application.RegisterEvent[QuitRequest](EventQuitRequested)
	application.RegisterEvent[Adoption](EventAdopt)
}

// Options builds the options for a new window; the manager sets Name and position.
type Options func() application.WebviewWindowOptions

type Manager struct {
	app     *application.App
	term    *terminal.Service
	store   *state.Store
	options Options

	mu      sync.Mutex
	windows map[string]*entry
	next    int

	quitConfirmed atomic.Bool
	quitting      atomic.Bool
}

type entry struct {
	window *application.WebviewWindow
	// What the window starts with: a saved session or an adopted tab.
	bootstrap  string
	title      string
	order      int
	allowClose bool
}

func NewManager(term *terminal.Service, store *state.Store, options Options) *Manager {
	return &Manager{term: term, store: store, options: options, windows: map[string]*entry{}}
}

// Start opens the saved windows, or one fresh window. Call after
// application.New, before Run.
func (m *Manager) Start(app *application.App) {
	m.app = app
	saved := m.store.Windows()
	if len(saved) == 0 {
		m.open("main", "", nil)
		return
	}
	for _, w := range saved {
		m.open(w.Name, string(w.Session), nil)
	}
}

// ShouldQuit is the application's quit hook: it lets the quit through, or
// asks the focused window to confirm when programs are still running.
func (m *Manager) ShouldQuit() bool {
	if m.quitConfirmed.Load() {
		m.quitting.Store(true)
		return true
	}
	running := m.term.RunningProcesses()
	if len(running) == 0 {
		m.quitting.Store(true)
		return true
	}
	m.app.Event.Emit(EventQuitRequested, QuitRequest{Window: m.focused(), Running: running})
	return false
}

// SendCommand delivers a menu command to the focused window.
func (m *Manager) SendCommand(name string) {
	m.app.Event.Emit(EventCommand, Command{Window: m.focused(), Name: name})
}

func (m *Manager) focused() string {
	if w := m.app.Window.Current(); w != nil {
		return w.Name()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for name := range m.windows {
		return name
	}
	return ""
}

func (m *Manager) open(name, bootstrap string, at *[2]int) *application.WebviewWindow {
	opts := m.options()
	opts.Name = name
	if at != nil {
		opts.InitialPosition = application.WindowXY
		opts.X, opts.Y = at[0], at[1]
	}

	m.mu.Lock()
	m.next++
	e := &entry{bootstrap: bootstrap, order: m.next}
	m.windows[name] = e
	m.mu.Unlock()

	w := m.app.Window.NewWithOptions(opts)
	e.window = w

	// The close button asks the frontend first, which may confirm and then
	// call Close itself.
	w.RegisterHook(events.Common.WindowClosing, func(ev *application.WindowEvent) {
		m.mu.Lock()
		allowed := e.allowClose
		m.mu.Unlock()
		if !allowed && !m.quitting.Load() {
			ev.Cancel()
			m.app.Event.Emit(EventCloseRequested, name)
		}
	})
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		// When quitting, shutdown ends every shell quietly; hanging them up
		// here would report exits to windows that are still saving.
		if !m.quitting.Load() {
			m.term.CloseWindow(name)
		}
		m.mu.Lock()
		delete(m.windows, name)
		m.mu.Unlock()
	})
	return w
}

func (m *Manager) newName() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := 2; ; i++ {
		name := fmt.Sprintf("w%d", i)
		if _, taken := m.windows[name]; !taken && !m.saved(name) {
			return name
		}
	}
}

func (m *Manager) saved(name string) bool {
	for _, w := range m.store.Windows() {
		if w.Name == name {
			return true
		}
	}
	return false
}

// ── Called by the frontend, through Service ──────────────────────────

// NewWindow opens a window. payload is a tab snapshot to adopt ("" for a
// fresh tab); x/y place it on screen when both are ≥ 0.
func (m *Manager) newWindow(payload string, x, y int) string {
	name := m.newName()
	var at *[2]int
	if x >= 0 && y >= 0 {
		at = &[2]int{x, y}
	}
	m.open(name, payload, at)
	return name
}

// Bootstrap returns what a window should start with, once.
func (m *Manager) bootstrap(name string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.windows[name]
	if e == nil {
		return ""
	}
	b := e.bootstrap
	e.bootstrap = ""
	return b
}

// Close closes a window after the frontend confirmed it. forget drops its
// saved session (the user closed it, as opposed to quitting the app).
func (m *Manager) close(name string, forget bool) {
	m.mu.Lock()
	e := m.windows[name]
	if e != nil {
		e.allowClose = true
	}
	m.mu.Unlock()
	if forget {
		_ = m.store.Forget(name)
	}
	if e != nil {
		e.window.Close()
	}
}

// Quit quits after the user confirmed it.
func (m *Manager) quit() {
	m.quitConfirmed.Store(true)
	m.app.Quit()
}

// MoveTab hands a tab snapshot to another window and brings it forward.
func (m *Manager) moveTab(target, payload string) error {
	m.mu.Lock()
	e := m.windows[target]
	m.mu.Unlock()
	if e == nil {
		return fmt.Errorf("no window %q", target)
	}
	m.app.Event.Emit(EventAdopt, Adoption{Window: target, Payload: payload})
	e.window.Focus()
	return nil
}

// SetTitle sets a window's title (shown in the Window menu and Mission Control).
func (m *Manager) setTitle(name, title string) {
	m.mu.Lock()
	e := m.windows[name]
	if e != nil {
		e.title = title
	}
	m.mu.Unlock()
	if e != nil {
		e.window.SetTitle(title)
	}
}

// Windows lists open windows in the order they were opened.
func (m *Manager) list() []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	type ordered struct {
		Info
		order int
	}
	var all []ordered
	for name, e := range m.windows {
		all = append(all, ordered{Info{Name: name, Title: e.title}, e.order})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].order < all[j].order })
	out := make([]Info, len(all))
	for i, o := range all {
		out[i] = o.Info
	}
	return out
}

// RunningProcesses lists programs still running, across all windows.
func (m *Manager) runningProcesses() []terminal.Running {
	return m.term.RunningProcesses()
}

// Service is the part of the manager the frontend may call.
type Service struct{ m *Manager }

func (m *Manager) Service() *Service { return &Service{m} }

// NewWindow opens a window, optionally adopting a tab snapshot, at x/y when both are ≥ 0.
func (s *Service) NewWindow(payload string, x, y int) string { return s.m.newWindow(payload, x, y) }

// Bootstrap returns what a window should start with, once.
func (s *Service) Bootstrap(name string) string { return s.m.bootstrap(name) }

// Close closes a window the user confirmed; forget drops its saved session.
func (s *Service) Close(name string, forget bool) { s.m.close(name, forget) }

// Quit quits after the user confirmed it.
func (s *Service) Quit() { s.m.quit() }

// MoveTab hands a tab snapshot to another window.
func (s *Service) MoveTab(target, payload string) error { return s.m.moveTab(target, payload) }

// SetTitle sets a window's title.
func (s *Service) SetTitle(name, title string) { s.m.setTitle(name, title) }

// Windows lists open windows in the order they were opened.
func (s *Service) Windows() []Info { return s.m.list() }

// RunningProcesses lists programs still running, across all windows.
func (s *Service) RunningProcesses() []terminal.Running { return s.m.runningProcesses() }
