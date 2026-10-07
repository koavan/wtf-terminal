const SHOW_MS = 1800;

/** A brief message at the bottom of the window, e.g. "Copied 12 lines". */
class Toast {
  message = $state<string | null>(null);
  #timer: ReturnType<typeof setTimeout> | undefined;

  show(message: string, ms = SHOW_MS) {
    this.message = message;
    clearTimeout(this.#timer);
    this.#timer = setTimeout(() => (this.message = null), ms);
  }
}

export const toast = new Toast();

/** 420 → "0.4s", 12_300 → "12s", 72_000 → "1m 12s", 3_780_000 → "1h 3m" */
export function formatDuration(ms: number): string {
  if (ms < 1000) return `${(ms / 1000).toFixed(1)}s`;
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${s % 60}s`;
  return `${Math.floor(m / 60)}h ${m % 60}m`;
}
