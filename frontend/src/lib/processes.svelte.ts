import { Service as Procs, type Proc } from '../../bindings/github.com/dayanraj/wtf/internal/procs';

export type { Proc } from '../../bindings/github.com/dayanraj/wtf/internal/procs';

export type ProcessSort = 'cpu' | 'memory' | 'name';

/** 1536 → "2 KB", 12_582_912 → "12 MB", 1_610_612_736 → "1.5 GB" */
export function formatBytes(n: number): string {
  if (n < 1024 * 1024) return `${Math.max(1, Math.round(n / 1024))} KB`;
  if (n < 1024 ** 3) return `${Math.round(n / 1024 ** 2)} MB`;
  return `${(n / 1024 ** 3).toFixed(1)} GB`;
}

/** Seconds → "45s", "12m", "3h 5m", "2d 4h". */
export function formatElapsed(s: number): string {
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`;
  return `${Math.floor(s / 86400)}d ${Math.floor((s % 86400) / 3600)}h`;
}

export function sortProcs(list: Proc[], by: ProcessSort): Proc[] {
  const out = [...list];
  if (by === 'cpu') out.sort((a, b) => b.cpu - a.cpu || b.memory - a.memory);
  else if (by === 'memory') out.sort((a, b) => b.memory - a.memory || b.cpu - a.cpu);
  else out.sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }) || a.pid - b.pid);
  return out;
}

/**
 * A session's processes as a tree under its shell, flattened depth-first
 * (children in start order, i.e. by PID).
 */
export function processTree(list: Proc[]): { proc: Proc; depth: number }[] {
  const children = new Map<number, Proc[]>();
  for (const p of list) {
    const siblings = children.get(p.ppid) ?? [];
    siblings.push(p);
    children.set(p.ppid, siblings);
  }
  for (const c of children.values()) c.sort((a, b) => a.pid - b.pid);

  const out: { proc: Proc; depth: number }[] = [];
  const seen = new Set<number>();
  const walk = (p: Proc, depth: number) => {
    if (seen.has(p.pid)) return;
    seen.add(p.pid);
    out.push({ proc: p, depth });
    for (const c of children.get(p.pid) ?? []) walk(c, depth + 1);
  };
  const pids = new Set(list.map((p) => p.pid));
  // Roots: the shell, plus anything whose parent isn't in this session.
  for (const p of [...list].sort((a, b) => Number(b.shell) - Number(a.shell) || a.pid - b.pid)) {
    if (p.shell || !pids.has(p.ppid)) walk(p, 0);
  }
  return out;
}

/** The user's processes; the Processes tab polls refresh() (see lib/poller.ts). */
class Processes {
  procs = $state.raw<Proc[]>([]);
  error = $state<string | null>(null);
  loaded = $state(false);
  scope = $state<'terminals' | 'all'>('terminals');
  sort = $state<ProcessSort>('cpu');
  /** When the list was last fetched (ms since epoch). */
  updatedAt = $state<number | null>(null);

  #inFlight = false;

  async refresh() {
    if (this.#inFlight) return;
    this.#inFlight = true;
    try {
      this.procs = (await Procs.List()) ?? [];
      this.error = null;
      this.updatedAt = Date.now();
    } catch (err) {
      this.error = err instanceof Error ? err.message : String(err);
    } finally {
      this.loaded = true;
      this.#inFlight = false;
    }
  }

  kill(pid: number, force: boolean) {
    return Procs.Kill(pid, force);
  }
}

export const processes = new Processes();
