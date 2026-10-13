// The command-line programs side-pane features run (internal/tools): which
// one an error is about, so the place that shows the error can offer to
// install it.

import { overlay } from './overlay.svelte';
import { isMac } from './platform';
import { tabs } from './tabs.svelte';

/** The messages the Go side uses when a program isn't there, by tool id. */
const MISSING: [RegExp, string][] = [
  [/\bkubectl isn't installed\b/, 'kubectl'],
  [/\baws CLI isn't installed\b/, 'aws'],
  // Linux lists connections with ss, falling back to lsof.
  [/\blsof isn't installed\b/, isMac ? 'lsof' : 'ss'],
];

/** The tool an error says is missing, or null. */
export function missingTool(error: string | null | undefined): string | null {
  if (!error) return null;
  for (const [re, id] of MISSING) if (re.test(error)) return id;
  return null;
}

/** The focused shell's PATH, which the features look on first. */
export function shellPATH(): string {
  return tabs.activePane?.env?.PATH ?? '';
}

/** Opens the Tools dialog, at one tool when given. */
export function showTools(focus?: string, welcome?: string[]) {
  overlay.show({ kind: 'tools', focus, welcome });
}
