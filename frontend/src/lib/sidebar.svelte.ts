import { SvelteMap, SvelteSet } from 'svelte/reactivity';
import { Service as Files, type Entry } from '../../bindings/github.com/dayanraj/wtf/internal/files';
import { network } from './network.svelte';
import { processes, type ProcessSort } from './processes.svelte';

export type { Entry } from '../../bindings/github.com/dayanraj/wtf/internal/files';

export const MIN_WIDTH = 180;
export const MAX_WIDTH = 520;
const DEFAULT_WIDTH = 260;

export const SIDEBAR_TABS = ['files', 'ssh', 'aws', 'kube', 'network', 'processes', 'history'] as const;
export type SidebarTab = (typeof SIDEBAR_TABS)[number];

export const SSH_SECTIONS = ['hosts', 'keys', 'tunnels'] as const;
export type SshSection = (typeof SSH_SECTIONS)[number];

export interface SavedSidebar {
  open: boolean;
  width: number;
  pinned: string | null;
  tab?: SidebarTab;
  networkScope?: 'terminals' | 'all';
  processScope?: 'terminals' | 'all';
  processSort?: ProcessSort;
  historyScope?: 'pane' | 'all';
  sshSection?: SshSection;
}

/** The window's side pane: whether it shows, how wide, which tab. */
class Sidebar {
  open = $state(false);
  width = $state(DEFAULT_WIDTH);
  tab = $state<SidebarTab>('files');
  /** File tree root locked by the user; null follows the focused pane. */
  pinned = $state<string | null>(null);
  /** History of the focused pane, or of every pane in the window. */
  historyScope = $state<'pane' | 'all'>('pane');
  /** Which part of the SSH tab shows. */
  sshSection = $state<SshSection>('hosts');

  toggle() {
    this.open = !this.open;
  }

  /** Opens the pane on a tab; on that tab already, closes it (like VS Code). */
  show(tab: SidebarTab) {
    if (this.open && this.tab === tab) this.open = false;
    else {
      this.tab = tab;
      this.open = true;
    }
  }

  setWidth(w: number) {
    this.width = Math.round(Math.max(MIN_WIDTH, Math.min(MAX_WIDTH, w)));
  }

  snapshot(): SavedSidebar {
    return {
      open: this.open,
      width: this.width,
      pinned: this.pinned,
      tab: this.tab,
      networkScope: network.scope,
      processScope: processes.scope,
      processSort: processes.sort,
      historyScope: this.historyScope,
      sshSection: this.sshSection,
    };
  }

  restore(s: unknown) {
    if (typeof s !== 'object' || s === null) return;
    const v = s as Record<string, unknown>;
    if (typeof v.open === 'boolean') this.open = v.open;
    if (typeof v.width === 'number' && Number.isFinite(v.width)) this.setWidth(v.width);
    if (typeof v.pinned === 'string' && v.pinned.startsWith('/')) this.pinned = v.pinned;
    if (SIDEBAR_TABS.includes(v.tab as SidebarTab)) this.tab = v.tab as SidebarTab;
    if (v.networkScope === 'terminals' || v.networkScope === 'all') network.scope = v.networkScope;
    if (v.processScope === 'terminals' || v.processScope === 'all') processes.scope = v.processScope;
    if (v.historyScope === 'pane' || v.historyScope === 'all') this.historyScope = v.historyScope;
    if (SSH_SECTIONS.includes(v.sshSection as SshSection)) this.sshSection = v.sshSection as SshSection;
    if (v.processSort === 'cpu' || v.processSort === 'memory' || v.processSort === 'name') processes.sort = v.processSort;
  }
}

export const sidebar = new Sidebar();

export interface Folder {
  entries: Entry[] | null;
  error: string | null;
  truncated: boolean;
}

/** Folder listings and which folders are expanded. Folders load on demand. */
class FileTree {
  folders = new SvelteMap<string, Folder>();
  expanded = new SvelteSet<string>();
  /** The row with keyboard selection. */
  selected = $state<string | null>(null);
  #pending = new Map<string, Promise<void>>();

  async load(path: string) {
    const running = this.#pending.get(path);
    if (running) return running;
    const job = (async () => {
      try {
        const listing = await Files.List(path);
        this.folders.set(path, { entries: listing.entries ?? [], error: null, truncated: listing.truncated });
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        this.folders.set(path, { entries: null, error: message, truncated: false });
      } finally {
        this.#pending.delete(path);
      }
    })();
    this.#pending.set(path, job);
    return job;
  }

  toggle(path: string) {
    if (this.expanded.has(path)) {
      this.expanded.delete(path);
    } else {
      this.expanded.add(path);
      if (!this.folders.has(path)) this.load(path);
    }
  }

  /** Re-lists the root and every expanded folder, e.g. after a command ran. */
  refresh(root: string) {
    const paths = new Set([root, ...[...this.expanded].filter((p) => p.startsWith(root === '/' ? '/' : root + '/'))]);
    return Promise.all([...paths].map((p) => this.load(p)));
  }

  collapseAll() {
    this.expanded.clear();
  }
}

export const fileTree = new FileTree();
