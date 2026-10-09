import type { MenuEntry } from './overlay.svelte';
import { isMac } from './platform';
import type { BlockInfo } from './prompts';
import { formatDuration } from './toast.svelte';

// The terminal's menus: a command block's actions (from its ✓/✗ mark, or a
// right-click in its output) and the general ones (copy, paste, …).

export interface BlockActions {
  copyCommand(): void;
  copyOutput(): void;
  copyBoth(): void;
  selectOutput(): void;
  runAgain(): void;
  saveSnippet(): void;
}

/** "kubectl get pods · ✗ exit 1 · 3.2s": what the command was and how it went. */
export function blockSummary(b: BlockInfo): string {
  const text = b.text.length > 48 ? b.text.slice(0, 47) + '…' : b.text || 'command';
  if (b.running) return `${text} · running`;
  const status = b.exitCode === 0 ? '✓' : `✗ exit ${b.exitCode}`;
  return [text, status, b.durationMs !== undefined ? formatDuration(b.durationMs) : ''].filter(Boolean).join(' · ');
}

export function blockMenu(b: BlockInfo, a: BlockActions): MenuEntry[] {
  const entries: MenuEntry[] = [{ heading: blockSummary(b) }];
  if (b.text) entries.push({ label: 'Copy Command', action: a.copyCommand });
  if (b.hasOutput) {
    entries.push({ label: 'Copy Output', action: a.copyOutput });
    if (b.text) entries.push({ label: 'Copy Command and Output', action: a.copyBoth });
    entries.push({ label: 'Select Output', action: a.selectOutput });
  }
  if (b.text && !b.running) entries.push('separator', { label: 'Run Again', action: a.runAgain });
  if (b.text) entries.push({ label: 'Save as Snippet…', action: a.saveSnippet });
  return entries;
}

export interface TerminalActions {
  copy(): void;
  paste(): void;
  selectAll(): void;
  clear(): void;
  find(): void;
}

export function terminalMenu(hasSelection: boolean, a: TerminalActions): MenuEntry[] {
  const key = (mac: string, other: string) => (isMac ? mac : other);
  const entries: MenuEntry[] = [];
  if (hasSelection) entries.push({ label: 'Copy', hint: key('⌘C', 'Ctrl+Shift+C'), action: a.copy });
  entries.push(
    { label: 'Paste', hint: key('⌘V', 'Ctrl+Shift+V'), action: a.paste },
    { label: 'Select All', hint: key('⌘A', 'Ctrl+Shift+A'), action: a.selectAll },
    'separator',
    { label: 'Find…', hint: key('⌘F', 'Ctrl+Shift+F'), action: a.find },
    { label: 'Clear', hint: key('⌘K', 'Ctrl+Shift+K'), action: a.clear },
  );
  return entries;
}

/** A command and its output as one text, like it looked at the prompt. */
export function commandAndOutput(command: string, output: string): string {
  return output ? `$ ${command}\n${output}` : `$ ${command}`;
}
