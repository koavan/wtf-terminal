import { Service, type Host } from '../../bindings/github.com/dayanraj/wtf/internal/sshhosts';
import { shellQuote } from './shell';
import { tabs } from './tabs.svelte';

export type { Host } from '../../bindings/github.com/dayanraj/wtf/internal/sshhosts';

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

export function matchesFilter(h: Host, query: string): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  const text = `${h.alias} ${h.hostName} ${h.user} ${h.proxyJump}`.toLowerCase();
  return q.split(/\s+/).every((word) => text.includes(word));
}

class SshHosts {
  hosts = $state.raw<Host[]>([]);
  problems = $state.raw<string[]>([]);
  loaded = $state(false);
  loading = $state(false);

  async load() {
    this.loading = true;
    try {
      const list = await Service.List();
      this.hosts = list.hosts ?? [];
      this.problems = list.problems ?? [];
    } catch (e) {
      this.problems = [String(e)];
    } finally {
      this.loading = false;
      this.loaded = true;
    }
  }
}

export const ssh = new SshHosts();

/** The name for a tab group of hosts: their shared config file, else "SSH". */
export function groupName(hosts: Host[]): string {
  const sources = new Set(hosts.map((h) => h.source));
  const only = sources.size === 1 ? [...sources][0] : '';
  const base = only.slice(only.lastIndexOf('/') + 1);
  return only && base !== 'config' ? base : 'SSH';
}

export type OpenWhere = 'tab' | 'group' | 'right' | 'down';

/** Connects to hosts: each in a new tab (optionally grouped), or one in a split. */
export function openHosts(hosts: Host[], where: OpenWhere = 'tab') {
  if (hosts.length === 0) return;
  if (where === 'right' || where === 'down') {
    tabs.split(where === 'right' ? 'row' : 'column', { command: sshCommand(hosts[0]) });
    return;
  }
  const ids = hosts.map((h) => tabs.open({ command: sshCommand(h) }));
  if (where === 'group' && ids.length > 1) {
    const group = tabs.createGroup(ids[0]);
    if (group) {
      group.name = groupName(hosts);
      for (const id of ids.slice(1)) tabs.addToGroup(id, group.id);
    }
    tabs.activate(ids[0]);
  }
}
