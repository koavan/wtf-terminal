import { tabs } from './tabs.svelte';

/** What app-level commands can do to a mounted terminal. */
export interface TerminalHandle {
  focus(): void;
  clear(): void;
  selectAll(): void;
  getSelection(): string;
  paste(text: string): void;
  /** Sends keystrokes to the shell, as if typed. */
  type(text: string): void;
  /** Sends bytes straight to the shell, bypassing xterm (broadcast input). */
  sendInput(bytes: Uint8Array): void;
  /** Scroll to the previous (-1) or next (1) prompt. Needs shell integration. */
  jumpPrompt(direction: -1 | 1): void;
  /** Scrolls to a command from the history; false if it left the scrollback. */
  revealCommand(id: number): boolean;
  /** Output of the last finished command, or null. Needs shell integration. */
  lastOutput(): string | null;
  /** Scrollback with colors, to carry a tab to another window. */
  serialize(): string;
  /** The last `rows` lines on screen, as plain text (tab overview). */
  snapshot(rows: number): string[];
}

const handles = new Map<number, TerminalHandle>();

export function registerTerminal(paneId: number, handle: TerminalHandle): () => void {
  handles.set(paneId, handle);
  return () => handles.delete(paneId);
}

export function terminalFor(paneId: number): TerminalHandle | undefined {
  return handles.get(paneId);
}

/** The terminal with keyboard focus: the focused pane of the active tab. */
export function activeTerminal(): TerminalHandle | undefined {
  const pane = tabs.activePane;
  return pane && handles.get(pane.id);
}

type FinishedListener = (command: string, exitCode: number) => void;
const finishedListeners = new Set<FinishedListener>();

/** Calls `fn` whenever a command finishes in any pane (needs shell integration). */
export function onCommandFinished(fn: FinishedListener): () => void {
  finishedListeners.add(fn);
  return () => finishedListeners.delete(fn);
}

export function commandFinished(command: string, exitCode: number) {
  for (const fn of finishedListeners) fn(command, exitCode);
}
