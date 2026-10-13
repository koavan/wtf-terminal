import type { Pane } from './layout';
import { tabs } from './tabs.svelte';
import type { TranscriptBlock } from './transcript';

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
  /** A command's output so far, or null once it has left the scrollback. */
  blockOutput(id: number): string | null;
  /** The whole scrollback as plain text (soft-wrapped lines joined). */
  plainText(): string;
  /** Every command with its output, for a Markdown transcript; null without shell integration. */
  transcript(): TranscriptBlock[] | null;
  /** Pins the last finished command's output to the side pane; false if there's none. */
  pinLastOutput(): boolean;
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

const encoder = new TextEncoder();

/**
 * Types a command into a pane's shell when it's at its prompt — its own
 * context, credentials and history apply; Ctrl-U clears anything
 * half-typed first. Otherwise (a program is running) opens a new tab that
 * runs `tabCommand`, which can carry what that tab's shell won't have.
 * Returns where it went.
 */
export function runInPane(pane: Pane | null | undefined, command: string, tabCommand = command): 'here' | 'tab' {
  const term = pane && handles.get(pane.id);
  if (pane && term && pane.isShell && !pane.running) {
    term.sendInput(encoder.encode(`\x15${command}\r`));
    term.focus();
    return 'here';
  }
  tabs.open({ command: tabCommand, cwd: pane?.cwd || undefined });
  return 'tab';
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
