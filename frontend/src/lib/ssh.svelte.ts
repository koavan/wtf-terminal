import { Service, type Host, type List, type Option } from '../../bindings/github.com/dayanraj/wtf/internal/sshhosts';
import { prettyPath } from './backend';
import { overlay } from './overlay.svelte';
import { shellQuote } from './shell';
import { broadcast } from './broadcast.svelte';
import { tabs } from './tabs.svelte';
import { runInPane } from './terminals';
import { toast } from './toast.svelte';

export type { Host, Option } from '../../bindings/github.com/dayanraj/wtf/internal/sshhosts';

// The SSH side pane: hosts from ~/.ssh/config and known_hosts (see
// internal/sshhosts), opened as tabs, splits or a tab group.

/** The command that connects; config hosts go by alias so ssh applies their settings. */
export function sshCommand(h: Host): string {
  const port = h.known && h.port && h.port !== '22' ? `-p ${shellQuote(h.port)} ` : '';
  return `ssh ${port}${shellQuote(h.alias)}`;
}

/** Stable across reloads: a known host and a config alias may share a name. */
export function hostKey(h: Host): string {
  return `${h.known ? 'k' : 'c'}:${h.alias}:${h.port}`;
}

/** "ops@bastion.example.com:2222 via jump" — the parts that add something to the alias. */
export function hostDetail(h: Host): string {
  let target = h.hostName && h.hostName !== h.alias ? h.hostName : '';
  if (h.user) target = `${h.user}@${target || h.alias}`;
  if (h.port && h.port !== '22') target = `${target || h.alias}:${h.port}`;
  return [target, h.proxyJump && `via ${h.proxyJump}`].filter(Boolean).join(' ');
}

export interface HostFact {
  label: string;
  /** The setting as written. */
  title: string;
}

/**
 * Settings that change what a connection may do, as the config sets them
 * for this host (Host * included). Shown as they are, never judged.
 */
export function hostFacts(h: Host): HostFact[] {
  const out: HostFact[] = [];
  if (h.forwardAgent) {
    out.push({
      label: 'agent fwd',
      title: `ForwardAgent ${h.forwardAgent}: the server can use ${h.forwardAgent === 'yes' ? 'your ssh-agent' : 'that agent'} while you're connected`,
    });
  }
  const check = h.strictHostKeyChecking;
  if (check === 'no' || check === 'off' || check === 'accept-new') {
    out.push({
      label: `hostkey ${check}`,
      title:
        check === 'accept-new'
          ? 'StrictHostKeyChecking accept-new: a new host key is saved without asking; a changed one is refused'
          : `StrictHostKeyChecking ${check}: host keys are saved and connected to without asking`,
    });
  }
  return out;
}

export function matchesFilter(h: Host, query: string): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  const text = `${h.alias} ${h.hostName} ${h.user} ${h.proxyJump}`.toLowerCase();
  return q.split(/\s+/).every((word) => text.includes(word));
}

/** A host entry as the editor fills it in. */
export interface HostDraft {
  /** Host line patterns, space-separated: "web1 web2". */
  name: string;
  hostName: string;
  user: string;
  port: string;
  identityFile: string;
  proxyJump: string;
  /** Any other settings, one "Key value" per line. */
  more: string;
}

/** The settings with their own fields in the editor, in the order they're written. */
const FIELDS = [
  ['hostName', 'HostName'],
  ['user', 'User'],
  ['port', 'Port'],
  ['identityFile', 'IdentityFile'],
  ['proxyJump', 'ProxyJump'],
] as const;

const unquote = (v: string) => (v.length > 1 && v.startsWith('"') && v.endsWith('"') ? v.slice(1, -1) : v);

/** An entry's own settings, split into the editor's fields. */
export function draftOf(h: Host | null, prefill: Partial<HostDraft> = {}): HostDraft {
  const d: HostDraft = { name: '', hostName: '', user: '', port: '', identityFile: '', proxyJump: '', more: '', ...prefill };
  if (!h) return d;
  d.name = (h.patterns ?? [h.alias]).join(' ');
  const more: string[] = [];
  for (const o of h.options ?? []) {
    const field = FIELDS.find(([, key]) => key.toLowerCase() === o.key.toLowerCase());
    // The first of each goes to its field; repeats (a second IdentityFile) stay below.
    if (field && !d[field[0]]) d[field[0]] = unquote(o.value);
    else more.push(`${o.key} ${o.value}`);
  }
  d.more = more.join('\n');
  return d;
}

/**
 * The settings to write for a draft. Empty fields are left out (and so
 * removed); a line under "more" without a value is a mistake and throws.
 */
export function optionsOf(d: HostDraft): Option[] {
  const out: Option[] = FIELDS.map(([field, key]) => ({ key, value: d[field].trim() })).filter((o) => o.value);
  for (const line of d.more.split('\n')) {
    const m = /^\s*([^\s=]+)\s*(?:=\s*|\s+)(.*)$/.exec(line);
    if (m && m[2].trim()) out.push({ key: m[1], value: m[2].trim() });
    else if (line.trim()) throw new Error(`${line.trim()} needs a value, like “ForwardAgent yes”`);
  }
  return out;
}

class SshHosts {
  hosts = $state.raw<Host[]>([]);
  problems = $state.raw<string[]>([]);
  loaded = $state(false);
  loading = $state(false);
  /** Private keys in ~/.ssh, for the IdentityFile field. */
  keys = $state.raw<string[]>([]);
  #versions: Record<string, string | undefined> = {};
  #mainConfig = '';

  /** Where new hosts go: ~/.ssh/config. */
  get mainConfig(): string {
    return this.#mainConfig;
  }

  #apply(list: List) {
    this.hosts = list.hosts ?? [];
    this.problems = list.problems ?? [];
    this.#versions = list.versions ?? {};
    this.#mainConfig = list.mainConfig;
  }

  async load() {
    this.loading = true;
    try {
      this.#apply(await Service.List());
      Service.Keys().then((k) => (this.keys = k ?? []));
    } catch (e) {
      this.problems = [String(e)];
    } finally {
      this.loading = false;
      this.loaded = true;
    }
  }

  /** Adds a host (`host` null) or changes its entry. Throws the backend's message. */
  async save(host: Host | null, draft: HostDraft) {
    const file = host?.source ?? this.#mainConfig;
    this.#apply(
      await Service.Save({
        file: host?.source ?? '',
        line: host?.line ?? 0,
        version: this.#versions[file] ?? '',
        patterns: draft.name.trim().split(/\s+/),
        options: optionsOf(draft),
      }),
    );
  }

  async remove(host: Host) {
    this.#apply(await Service.Delete(host.source, host.line, this.#versions[host.source] ?? ''));
  }
}

export const ssh = new SshHosts();

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

/** Asks, then removes a host's entry from its config file. */
export function confirmDeleteHost(h: Host) {
  const others = (h.patterns ?? []).filter((p) => p !== h.alias);
  overlay.confirm({
    title: `Delete ${h.alias}?`,
    message:
      `Removes its entry from ${prettyPath(h.source)} (line ${h.line}).` +
      (others.length ? ` The entry also names ${others.join(', ')}, which go with it.` : '') +
      ` A copy of the file is kept in wtf’s cache folder.`,
    items: [],
    confirmLabel: 'Delete Host',
    onConfirm: () =>
      ssh
        .remove(h)
        .then(() => toast.show(`Deleted ${h.alias}`))
        .catch((e) => toast.show(`Couldn’t delete ${h.alias}: ${message(e)}`, 6000)),
  });
}

/** The name for a tab group of hosts: their shared config file, else "SSH". */
export function groupName(hosts: Host[]): string {
  const sources = new Set(hosts.map((h) => h.source));
  const only = sources.size === 1 ? [...sources][0] : '';
  const base = only.slice(only.lastIndexOf('/') + 1);
  return only && base !== 'config' ? base : 'SSH';
}

/**
 * here: the focused shell, or a new tab if it's busy; group: a tab group;
 * broadcast: a tab group whose input is broadcast to every host.
 */
export type OpenWhere = 'here' | 'tab' | 'group' | 'broadcast' | 'right' | 'down';

/** Connects to hosts: one in the focused shell or a split, or each in a new tab (optionally grouped). */
export function openHosts(hosts: Host[], where: OpenWhere = 'tab') {
  if (hosts.length === 0) return;
  if (where === 'here') {
    runInPane(tabs.activePane, sshCommand(hosts[0]));
    return;
  }
  if (where === 'right' || where === 'down') {
    tabs.split(where === 'right' ? 'row' : 'column', { command: sshCommand(hosts[0]) });
    return;
  }
  const ids = hosts.map((h) => tabs.open({ command: sshCommand(h) }));
  if ((where === 'group' || where === 'broadcast') && ids.length > 1) {
    const group = tabs.createGroup(ids[0]);
    if (group) {
      group.name = groupName(hosts);
      for (const id of ids.slice(1)) tabs.addToGroup(id, group.id);
      if (where === 'broadcast') broadcast.groups.add(group.id);
    }
    tabs.activate(ids[0]);
  }
}
