import { Clipboard } from '@wailsio/runtime';
import { Service as ConfigService } from '../../bindings/github.com/dayanraj/wtf/internal/config';
import { Service as SnippetsService } from '../../bindings/github.com/dayanraj/wtf/internal/snippets';
import { broadcast } from './broadcast.svelte';
import { openManual } from './help';
import { captureWindow } from './workspaces.svelte';
import { closeActivePane, closeTab, closeWindow } from './closing';
import { overlay } from './overlay.svelte';
import { isMac } from './platform';
import { search } from './search.svelte';
import { settings } from './settings.svelte';
import { sidebar } from './sidebar.svelte';
import { tabs } from './tabs.svelte';
import { activeTerminal } from './terminals';
import { focusedTextField, startRename } from './rename.svelte';
import { toast } from './toast.svelte';
import { closeThisWindow, moveTabToNewWindow, Windows } from './window';

export interface Command {
  /** Stable name; the Go menu emits these. */
  id: string;
  label: string;
  /** Shortcut shown in the palette, per platform. */
  mac?: string;
  other?: string;
  run: () => void;
  /** Hidden from the palette when false. */
  available?: () => boolean;
}

const hasSplits = () => tabs.active?.root.kind === 'split';
const integrated = () => !!tabs.activePane?.integrated;

function copyLastOutput() {
  const text = activeTerminal()?.lastOutput();
  if (text == null) {
    toast.show('No finished command yet');
    return;
  }
  Clipboard.SetText(text);
  const lines = text ? text.split('\n').length : 0;
  toast.show(lines === 0 ? 'Command had no output' : `Copied ${lines} ${lines === 1 ? 'line' : 'lines'} of output`);
}

function createGroupForActive() {
  const group = tabs.activeId === null ? undefined : tabs.createGroup(tabs.activeId);
  if (group) overlay.openGroupEditor(group.id);
}

export const commands: Command[] = [
  { id: 'palette', label: 'Command Palette', mac: '⌘⇧P', other: 'Ctrl+Shift+P', run: () => overlay.togglePalette() },
  { id: 'help.manual', label: 'Open Manual', mac: '⌘?', other: 'F1', run: () => openManual() },
  { id: 'snippets.run', label: 'Run Snippet…', mac: '⌘⇧R', other: 'Ctrl+Shift+Alt+R', run: () => overlay.togglePalette('snippets') },
  {
    id: 'snippets.open',
    label: 'Open Snippets File',
    run: () => SnippetsService.OpenUserFile().catch((e) => toast.show(`Couldn't open the snippets file: ${e}`)),
  },
  { id: 'workspace.open', label: 'Open Workspace…', run: () => overlay.togglePalette('workspaces') },
  { id: 'workspace.save', label: 'Save Window as Workspace…', run: () => overlay.show({ kind: 'workspaceEdit', workspace: null, draft: captureWindow() }) },
  { id: 'help.shortcuts', label: 'Show Keyboard Shortcuts', run: () => openManual('shortcuts') },
  { id: 'theme.pick', label: 'Change Theme…', run: () => overlay.togglePalette('themes') },
  { id: 'config.open', label: 'Open Settings File', mac: '⌘,', other: 'Ctrl+,', run: () => ConfigService.OpenConfigFile().catch((e) => toast.show(`Couldn't open settings: ${e}`)) },
  { id: 'themes.folder', label: 'Open Themes Folder', run: () => ConfigService.OpenThemesFolder().catch((e) => toast.show(`Couldn't open folder: ${e}`)) },

  { id: 'sidebar.toggle', label: 'Toggle Sidebar', mac: '⌘B', other: 'Ctrl+Shift+B', run: () => sidebar.toggle() },
  { id: 'sidebar.files', label: 'Show Files', mac: '⌘⇧E', other: 'Ctrl+Shift+Alt+E', run: () => sidebar.show('files') },
  { id: 'sidebar.network', label: 'Show Network Connections', mac: '⌘⇧O', other: 'Ctrl+Shift+Alt+O', run: () => sidebar.show('network') },
  {
    id: 'sidebar.autoRefresh',
    label: 'Toggle Side Pane Auto-Refresh',
    run: () =>
      ConfigService.SetSidebarAutoRefresh(!settings.config.sidebarAutoRefresh).then(() =>
        toast.show(settings.config.sidebarAutoRefresh ? 'Side pane auto-refresh off' : 'Side pane auto-refresh on'),
      ),
  },
  { id: 'sidebar.history', label: 'Show Command History', mac: '⌘⇧H', other: 'Ctrl+Shift+Alt+H', run: () => sidebar.show('history') },
  { id: 'sidebar.ssh', label: 'Show SSH Hosts', mac: '⌘⇧S', other: 'Ctrl+Shift+Alt+S', run: () => sidebar.show('ssh') },
  { id: 'sidebar.aws', label: 'Show AWS Profiles', mac: '⌘⇧U', other: 'Ctrl+Shift+Alt+U', run: () => sidebar.show('aws') },
  { id: 'sidebar.kube', label: 'Show Kubernetes', mac: '⌘⇧K', other: 'Ctrl+Shift+Alt+K', run: () => sidebar.show('kube') },
  { id: 'sidebar.snippets', label: 'Show Snippets', mac: '⌘⇧Y', other: 'Ctrl+Shift+Alt+Y', run: () => sidebar.show('snippets') },
  { id: 'sidebar.workspaces', label: 'Show Workspaces', mac: '⌘⇧L', other: 'Ctrl+Shift+Alt+L', run: () => sidebar.show('workspaces') },
  { id: 'sidebar.processes', label: 'Show Processes', mac: '⌘⇧A', other: 'Ctrl+Shift+Alt+A', run: () => sidebar.show('processes') },
  { id: 'tab.new', label: 'New Tab', mac: '⌘T', other: 'Ctrl+Shift+T', run: () => tabs.open() },
  { id: 'tab.duplicate', label: 'Duplicate Tab', run: () => tabs.duplicate() },
  { id: 'tab.close', label: 'Close Tab', mac: '⌥⌘W', run: () => tabs.activeId !== null && closeTab(tabs.activeId) },
  { id: 'tab.overview', label: 'Show All Tabs', mac: '⌘⇧\\', other: 'Ctrl+Shift+\\', run: () => overlay.toggleOverview() },
  { id: 'tab.toNewWindow', label: 'Move Tab to New Window', run: () => tabs.activeId !== null && moveTabToNewWindow(tabs.activeId) },
  { id: 'window.new', label: 'New Window', mac: '⌘N', other: 'Ctrl+Shift+N', run: () => Windows.NewWindow('', -1, -1) },
  { id: 'window.close', label: 'Close Window', mac: '⌘⇧W', run: () => closeWindow(closeThisWindow) },
  { id: 'tab.rename', label: 'Rename Tab…', run: () => startRename() },
  { id: 'tab.reopen', label: 'Reopen Closed Tab', mac: '⌘⇧T', run: () => tabs.reopen() },
  { id: 'tab.next', label: 'Next Tab', mac: '⌘⇧]', other: 'Ctrl+PgDn', run: () => tabs.cycle(1) },
  { id: 'tab.prev', label: 'Previous Tab', mac: '⌘⇧[', other: 'Ctrl+PgUp', run: () => tabs.cycle(-1) },
  { id: 'tab.moveRight', label: 'Move Tab Right', mac: '⌘⇧→', other: 'Ctrl+Shift+PgDn', run: () => tabs.moveActive(1) },
  { id: 'tab.moveLeft', label: 'Move Tab Left', mac: '⌘⇧←', other: 'Ctrl+Shift+PgUp', run: () => tabs.moveActive(-1) },
  {
    id: 'broadcast.toggle',
    label: 'Toggle Broadcast Input',
    mac: '⌥⌘B',
    other: 'Ctrl+Shift+Alt+B',
    run: () => {
      if (!broadcast.toggleActive()) toast.show('Broadcast needs a tab group or split panes');
    },
  },
  { id: 'tab.group', label: 'Add Tab to New Group', mac: '⌥⌘G', other: 'Ctrl+Shift+G', run: createGroupForActive },

  { id: 'pane.splitRight', label: 'Split Right', mac: '⌘D', other: 'Ctrl+Shift+E', run: () => tabs.split('row') },
  { id: 'pane.splitDown', label: 'Split Down', mac: '⌘⇧D', other: 'Ctrl+Shift+O', run: () => tabs.split('column') },
  { id: 'pane.close', label: 'Close Pane', mac: '⌘W', other: 'Ctrl+Shift+W', run: closeActivePane },
  { id: 'pane.zoom', label: 'Toggle Pane Zoom', mac: '⌘⇧↩', other: 'Ctrl+Shift+Enter', run: () => tabs.toggleZoom(), available: hasSplits },
  { id: 'pane.next', label: 'Next Pane', mac: '⌘]', run: () => tabs.cyclePane(1), available: hasSplits },
  { id: 'pane.prev', label: 'Previous Pane', mac: '⌘[', run: () => tabs.cyclePane(-1), available: hasSplits },
  { id: 'pane.left', label: 'Focus Pane Left', mac: '⌥⌘←', other: 'Ctrl+Alt+←', run: () => tabs.focusDirection('left'), available: hasSplits },
  { id: 'pane.right', label: 'Focus Pane Right', mac: '⌥⌘→', other: 'Ctrl+Alt+→', run: () => tabs.focusDirection('right'), available: hasSplits },
  { id: 'pane.up', label: 'Focus Pane Above', mac: '⌥⌘↑', other: 'Ctrl+Alt+↑', run: () => tabs.focusDirection('up'), available: hasSplits },
  { id: 'pane.down', label: 'Focus Pane Below', mac: '⌥⌘↓', other: 'Ctrl+Alt+↓', run: () => tabs.focusDirection('down'), available: hasSplits },

  { id: 'find', label: 'Find', mac: '⌘F', other: 'Ctrl+Shift+F', run: () => search.open() },
  { id: 'find.next', label: 'Find Next', mac: '⌘G', run: () => search.step(1) },
  { id: 'find.prev', label: 'Find Previous', mac: '⌘⇧G', run: () => search.step(-1) },
  { id: 'prompt.prev', label: 'Jump to Previous Prompt', mac: '⌘↑', other: 'Ctrl+Shift+↑', run: () => activeTerminal()?.jumpPrompt(-1), available: integrated },
  { id: 'prompt.next', label: 'Jump to Next Prompt', mac: '⌘↓', other: 'Ctrl+Shift+↓', run: () => activeTerminal()?.jumpPrompt(1), available: integrated },
  { id: 'term.copyOutput', label: 'Copy Last Command Output', mac: '⌥⌘C', other: 'Ctrl+Shift+Alt+C', run: copyLastOutput, available: integrated },
  { id: 'term.clear', label: 'Clear Buffer', mac: '⌘K', other: 'Ctrl+Shift+K', run: () => activeTerminal()?.clear() },
  {
    id: 'term.selectAll', label: 'Select All', mac: '⌘A', other: 'Ctrl+Shift+A',
    // ⌘A is a menu shortcut, so it lands here even while typing in a field.
    run: () => {
      const field = focusedTextField();
      if (field) field.select();
      else activeTerminal()?.selectAll();
    },
  },
  {
    id: 'term.copy', label: 'Copy', mac: '⌘C', other: 'Ctrl+Shift+C',
    run: () => {
      const text = activeTerminal()?.getSelection();
      if (text) Clipboard.SetText(text);
    },
  },
  {
    id: 'term.paste', label: 'Paste', mac: '⌘V', other: 'Ctrl+Shift+V',
    run: () => {
      Clipboard.Text().then((text) => text && activeTerminal()?.paste(text));
    },
  },

  { id: 'font.bigger', label: 'Bigger Text', mac: '⌘=', other: 'Ctrl+Shift+=', run: () => settings.bigger() },
  { id: 'font.smaller', label: 'Smaller Text', mac: '⌘-', other: 'Ctrl+Shift+-', run: () => settings.smaller() },
  { id: 'font.reset', label: 'Actual Size', mac: '⌘0', other: 'Ctrl+Shift+0', run: () => settings.resetFont() },
];

const byId = new Map(commands.map((c) => [c.id, c]));

export function shortcutLabel(c: Command): string | undefined {
  return isMac ? c.mac : c.other;
}

/** Runs a command by id. */
export function runCommand(id: string) {
  if (id.startsWith('tab.select.')) {
    const which = id.slice('tab.select.'.length);
    if (which === 'last') tabs.selectLast();
    else tabs.selectNumber(Number(which));
    return;
  }
  byId.get(id)?.run();
}

// On macOS the native menu owns ⌘-shortcuts. Elsewhere there is no menu bar,
// so we bind browser-style Ctrl+Shift shortcuts (plain Ctrl+letter belongs to
// the shell). Ctrl+Tab works on every platform, like in a browser.
function shortcutFor(e: KeyboardEvent): string | null {
  if (e.key === 'F1' && !e.ctrlKey && !e.metaKey && !e.altKey && !isMac) return 'help.manual';
  if (e.ctrlKey && !e.metaKey && !e.altKey && e.key === 'Tab') {
    return e.shiftKey ? 'tab.prev' : 'tab.next';
  }
  if (isMac) return null;

  if (e.ctrlKey && !e.shiftKey && !e.altKey) {
    if (e.key === 'PageDown') return 'tab.next';
    if (e.key === 'PageUp') return 'tab.prev';
  }
  if (e.ctrlKey && e.altKey && !e.shiftKey) {
    switch (e.key) {
      case 'ArrowLeft': return 'pane.left';
      case 'ArrowRight': return 'pane.right';
      case 'ArrowUp': return 'pane.up';
      case 'ArrowDown': return 'pane.down';
    }
  }
  if (e.ctrlKey && e.shiftKey && e.altKey && e.code === 'KeyC') return 'term.copyOutput';
  if (e.ctrlKey && e.shiftKey && e.altKey && e.code === 'KeyE') return 'sidebar.files';
  if (e.ctrlKey && e.shiftKey && e.altKey && e.code === 'KeyO') return 'sidebar.network';
  if (e.ctrlKey && e.shiftKey && e.altKey && e.code === 'KeyA') return 'sidebar.processes';
  if (e.ctrlKey && e.shiftKey && e.altKey && e.code === 'KeyH') return 'sidebar.history';
  if (e.ctrlKey && e.shiftKey && e.altKey && e.code === 'KeyS') return 'sidebar.ssh';
  if (e.ctrlKey && e.shiftKey && e.altKey && e.code === 'KeyU') return 'sidebar.aws';
  if (e.ctrlKey && e.shiftKey && e.altKey && e.code === 'KeyK') return 'sidebar.kube';
  if (e.ctrlKey && e.shiftKey && e.altKey && e.code === 'KeyR') return 'snippets.run';
  if (e.ctrlKey && e.shiftKey && e.altKey && e.code === 'KeyY') return 'sidebar.snippets';
  if (e.ctrlKey && e.shiftKey && e.altKey && e.code === 'KeyL') return 'sidebar.workspaces';
  if (e.ctrlKey && e.shiftKey && e.altKey && e.code === 'KeyB') return 'broadcast.toggle';
  if (e.ctrlKey && !e.shiftKey && !e.altKey && e.code === 'Comma') return 'config.open';
  if (e.ctrlKey && e.shiftKey && !e.altKey) {
    switch (e.code) {
      case 'ArrowUp': return 'prompt.prev';
      case 'ArrowDown': return 'prompt.next';
      case 'KeyP': return 'palette';
      case 'KeyT': return 'tab.new';
      case 'KeyB': return 'sidebar.toggle';
      case 'KeyN': return 'window.new';
      case 'Backslash': return 'tab.overview';
      case 'KeyW': return 'pane.close';
      case 'KeyG': return 'tab.group';
      case 'KeyE': return 'pane.splitRight';
      case 'KeyO': return 'pane.splitDown';
      case 'Enter': return 'pane.zoom';
      case 'KeyF': return 'find';
      case 'KeyC': return 'term.copy';
      case 'KeyV': return 'term.paste';
      case 'KeyA': return 'term.selectAll';
      case 'KeyK': return 'term.clear';
      case 'Equal': return 'font.bigger';
      case 'Minus': return 'font.smaller';
      case 'Digit0': return 'font.reset';
      case 'PageDown': return 'tab.moveRight';
      case 'PageUp': return 'tab.moveLeft';
    }
  }
  if (e.altKey && !e.ctrlKey && !e.shiftKey && /^Digit[1-9]$/.test(e.code)) {
    const n = e.code.slice(5);
    return n === '9' ? 'tab.select.last' : 'tab.select.' + n;
  }
  return null;
}

/** Installs the keyboard shortcuts. Runs before xterm.js sees the key. */
export function installShortcuts(): () => void {
  const onKeyDown = (e: KeyboardEvent) => {
    const command = shortcutFor(e);
    if (!command) return;
    e.preventDefault();
    e.stopPropagation();
    runCommand(command);
  };
  window.addEventListener('keydown', onKeyDown, { capture: true });
  return () => window.removeEventListener('keydown', onKeyDown, { capture: true });
}
