import { SvelteMap, SvelteSet } from 'svelte/reactivity';
import { Service as Files, type Entry } from '../../bindings/github.com/dayanraj/wtf/internal/files';
import { GitIndex } from './gitstatus';
import { network } from './network.svelte';
import { processes, type ProcessSort } from './processes.svelte';

export type { Entry } from '../../bindings/github.com/dayanraj/wtf/internal/files';

export const MIN_WIDTH = 220;
export const MAX_WIDTH = 520;
export const DEFAULT_WIDTH = 300;

export const SIDEBAR_TABS = ['files', 'ssh', 'aws', 'kube', 'snippets', 'workspaces', 'network', 'processes', 'history', 'pins', 'share'] as const;
export type SidebarTab = (typeof SIDEBAR_TABS)[number];

export const SSH_SECTIONS = ['hosts', 'keys', 'tunnels'] as const;
export const KUBE_VIEWS = ['cluster', 'contexts'] as const;
export type KubeView = (typeof KUBE_VIEWS)[number];
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
  kubeView?: KubeView;
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
  /** Which part of the Kubernetes tab shows. */
  kubeView = $state<KubeView>('cluster');

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
      kubeView: this.kubeView,
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
    if (KUBE_VIEWS.includes(v.kubeView as KubeView)) this.kubeView = v.kubeView as KubeView;
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
/** Folders a tree went through, per pane (or for the pinned root), for Back. */
const MAX_HISTORY = 50;

class FileTree {
  /** Visited roots by key, the current one last. */
  history = new SvelteMap<string, string[]>();
  folders = new SvelteMap<string, Folder>();
  expanded = new SvelteSet<string>();
  /** The row with keyboard selection. */
  selected = $state<string | null>(null);
  /** Git status of the repository the root is in; null outside one (or not loaded yet). */
  git = $state.raw<GitIndex | null>(null);
  /** Show the list of changed files instead of the tree. */
  changesOnly = $state(false);
  #pending = new Map<string, Promise<void>>();
  #gitSeq = 0;
  #gitFor = '';

  /** Asks git about the repository `dir` is in; the newest answer wins. */
  async loadGit(dir: string) {
    const seq = ++this.#gitSeq;
    if (this.#gitFor !== dir) {
      this.#gitFor = dir;
      // A folder in another repository (or none): don't show the old one's status meanwhile.
      if (this.git && !(dir === this.git.status.root || dir.startsWith(this.git.status.root + '/'))) this.git = null;
    }
    try {
      const st = await Files.GitStatus(dir);
      if (seq === this.#gitSeq) this.git = st.root ? new GitIndex(st) : null;
    } catch {
      if (seq === this.#gitSeq) this.git = null;
    }
  }

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
    return Promise.all([...paths].map((p) => this.load(p)).concat(this.loadGit(root)));
  }

  collapseAll() {
    this.expanded.clear();
  }

  /** Records that the tree now shows `root` (however it got there). */
  visit(key: string, root: string) {
    const h = this.history.get(key) ?? [];
    if (h.at(-1) === root) return;
    this.history.set(key, [...h, root].slice(-MAX_HISTORY));
  }

  /** The folder Back goes to, if any. */
  previous(key: string): string | null {
    const h = this.history.get(key) ?? [];
    return h.length > 1 ? h[h.length - 2] : null;
  }

  /**
   * Steps back: forgets the current root and returns the one before. The
   * caller then shows it, and visit() finds it already last.
   */
  back(key: string): string | null {
    const prev = this.previous(key);
    if (prev) this.history.set(key, (this.history.get(key) ?? []).slice(0, -1));
    return prev;
  }
}

export const fileTree = new FileTree();
