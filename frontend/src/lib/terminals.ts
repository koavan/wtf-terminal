import { tabs } from './tabs.svelte';

/** What app-level commands can do to a mounted terminal. */
export interface TerminalHandle {
  focus(): void;
  clear(): void;
  selectAll(): void;
  getSelection(): string;
  paste(text: string): void;
  /** Scroll to the previous (-1) or next (1) prompt. Needs shell integration. */
  jumpPrompt(direction: -1 | 1): void;
  /** Output of the last finished command, or null. Needs shell integration. */
  lastOutput(): string | null;
}

const handles = new Map<number, TerminalHandle>();

export function registerTerminal(paneId: number, handle: TerminalHandle): () => void {
  handles.set(paneId, handle);
  return () => handles.delete(paneId);
}

/** The terminal with keyboard focus: the focused pane of the active tab. */
export function activeTerminal(): TerminalHandle | undefined {
  const pane = tabs.activePane;
  return pane && handles.get(pane.id);
}
