import { commands as kube, type Target } from './kube.svelte';
import { parseTimestamp, relativeTime, k8sKind, type LogToken } from './logs';
import type { MenuEntry } from './overlay.svelte';
import { isMac } from './platform';

// What can be done with a token in the output (see lib/logs): the menu a
// ⌘-click on it, or a right-click, opens. Everything that reaches a cluster
// is a kubectl command shown in a new tab, run against the context and
// namespace the shell reported — the menu says which.

export interface TokenActions {
  copy(text: string, what: string): void;
  openUrl(url: string): void;
  /** Runs a command in a new tab. */
  runInTab(command: string): void;
  openInEditor(path: string, line: number): void;
  openFile(path: string): void;
  reveal(path: string): void;
  /** The masked secret viewer (the Kubernetes tab's). */
  showSecret(name: string): void;
}

/** Kinds whose pods' logs kubectl can show by the kind's name. */
const HAS_LOGS = new Set(['deployment', 'statefulset', 'daemonset', 'replicaset', 'job']);

/** The time in a menu heading: "Thu, Oct 9, 2026, 12:34:56". */
const longTime = (d: Date, utc = false) =>
  d.toLocaleString(undefined, {
    weekday: 'short', year: 'numeric', month: 'short', day: 'numeric',
    hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false,
    ...(utc ? { timeZone: 'UTC' } : {}),
  });

export function tokenMenu(token: LogToken, target: Target, a: TokenActions, now = new Date()): MenuEntry[] {
  switch (token.kind) {
    case 'url':
      return [
        { heading: token.text.length > 60 ? token.text.slice(0, 59) + '…' : token.text },
        { label: 'Open in Browser', hint: isMac ? '⌘-click' : 'Ctrl-click', action: () => a.openUrl(token.text) },
        { label: 'Copy URL', action: () => a.copy(token.text, 'URL') },
      ];

    case 'timestamp': {
      const t = parseTimestamp(token.text, now);
      if (!t) return [];
      const iso = t.date.toISOString();
      return [
        { heading: `${longTime(t.date)} your time · ${relativeTime(t.date, now)}` },
        { heading: `${longTime(t.date, true)} UTC` },
        ...(t.zoned ? [] : [{ heading: 'No time zone in the text: read as your time' }]),
        { label: 'Copy as UTC (ISO 8601)', action: () => a.copy(iso, 'time') },
        { label: 'Copy as Your Time', action: () => a.copy(longTime(t.date), 'time') },
        { label: 'Copy as Unix Time', action: () => a.copy(String(Math.floor(t.date.getTime() / 1000)), 'Unix time') },
      ];
    }

    case 'ip': {
      const entries: MenuEntry[] = [{ heading: token.text }, { label: 'Copy Address', action: () => a.copy(token.address, 'address') }];
      if (token.port) entries.push({ label: `Copy ${token.text}`, action: () => a.copy(token.text, 'address and port') });
      if (!token.cidr) {
        const host = token.address.includes(':') ? `[${token.address}]` : token.address;
        if (token.port) entries.push({ label: `Open http://${host}:${token.port}`, action: () => a.openUrl(`http://${host}:${token.port}`) });
        entries.push({ label: `SSH to ${token.address}`, action: () => a.runInTab(`ssh ${token.address}`) });
      }
      return entries;
    }

    case 'path':
      return [
        { heading: `${token.path}:${token.line}` },
        { label: `Open in Editor at Line ${token.line}`, action: () => a.openInEditor(token.path, token.line) },
        { label: 'Open with Default App', action: () => a.openFile(token.path) },
        { label: isMac ? 'Reveal in Finder' : 'Open Containing Folder', action: () => a.reveal(token.path) },
        'separator',
        { label: 'Copy Path', action: () => a.copy(token.path, 'path') },
        { label: `Copy Path:${token.line}`, action: () => a.copy(`${token.path}:${token.line}`, 'path and line') },
      ];

    case 'k8s': {
      const kind = k8sKind(token.resource);
      const where = target.context
        ? `Context ${target.context}${target.namespace ? ` · namespace ${target.namespace}` : ''}`
        : "kubectl's current context (this shell reported none)";
      const entries: MenuEntry[] = [{ heading: `${kind}/${token.name}` }, { heading: where }];
      if (kind === 'pod') entries.push({ label: 'Logs', action: () => a.runInTab(kube.logs(target, token.name)) });
      else if (HAS_LOGS.has(kind)) entries.push({ label: 'Logs', action: () => a.runInTab(kube.workloadLogs(target, kind, token.name)) });
      entries.push(
        { label: 'Describe', action: () => a.runInTab(kube.describe(target, kind, token.name)) },
        // A secret's YAML would print its values in the clear.
        kind === 'secret'
          ? { label: 'View Decoded Values…', action: () => a.showSecret(token.name) }
          : { label: 'View YAML', action: () => a.runInTab(kube.yaml(target, kind, token.name)) },
        'separator',
        { label: 'Copy Name', action: () => a.copy(token.name, 'name') },
        { label: `Copy ${kind}/${token.name}`, action: () => a.copy(`${kind}/${token.name}`, 'resource') },
      );
      return entries;
    }

    default:
      return [];
  }
}
