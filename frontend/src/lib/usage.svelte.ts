import { Service, type Group, type Usage } from '../../bindings/github.com/dayanraj/wtf/internal/usage';
import { poll } from './poller';

export type { Group, Proc, Usage } from '../../bindings/github.com/dayanraj/wtf/internal/usage';

// wtf's own CPU and memory (internal/usage), for the address bar's readout
// and the Resource Usage dialog. Sampled every 2 seconds only while one of
// them is on screen and the window is in front; otherwise nothing runs.

export const SAMPLE_MS = 2000;

class ResourceUsage {
  current = $state.raw<Usage | null>(null);
  error = $state('');
  #watchers = 0;
  #stop: (() => void) | null = null;

  /** Samples while anything watches; call the returned function to stop watching. */
  watch(): () => void {
    if (++this.#watchers === 1) this.#stop = poll(() => this.sample(), SAMPLE_MS);
    let done = false;
    return () => {
      if (done) return;
      done = true;
      if (--this.#watchers === 0) {
        this.#stop?.();
        this.#stop = null;
      }
    };
  }

  async sample() {
    try {
      const u = await Service.Sample();
      const fix = (g: Group): Group => ({ ...g, procs: g.procs ?? [] });
      this.current = { ...u, app: fix(u.app), window: fix(u.window), programs: fix(u.programs) };
      this.error = '';
    } catch (e) {
      this.error = e instanceof Error ? e.message : String(e);
    }
  }
}

export const resourceUsage = new ResourceUsage();

/** wtf's own share: the app and its windows, not the programs run in it. CPU is -1 until there's an interval to measure over. */
export function wtfTotal(u: Usage): { cpu: number; memory: number } {
  return { cpu: u.interval > 0 ? u.app.cpu + u.window.cpu : -1, memory: u.app.memory + u.window.memory };
}

/** "0.4%", "12%"; "…" before there's an interval to measure over. */
export function formatCPU(cpu: number): string {
  if (cpu < 0) return '…';
  return cpu < 10 ? `${cpu.toFixed(1)}%` : `${Math.round(cpu)}%`;
}

/** "840 KB", "412 MB", "1.2 GB" (powers of 1024, as Activity Monitor and top count). */
export function formatBytes(n: number): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  return `${i >= 3 ? n.toFixed(1) : Math.round(n)} ${units[i]}`;
}
