import { Service as Net, type Conn } from '../../bindings/github.com/dayanraj/wtf/internal/netconn';

export type { Conn } from '../../bindings/github.com/dayanraj/wtf/internal/netconn';

/** "127.0.0.1:3000" → { host: "127.0.0.1", port: "3000" }; "[::1]:5432" → { host: "::1", … } */
export function splitAddress(addr: string): { host: string; port: string } {
  const i = addr.lastIndexOf(':');
  if (i < 0) return { host: addr, port: '' };
  let host = addr.slice(0, i);
  if (host.startsWith('[') && host.endsWith(']')) host = host.slice(1, -1);
  return { host, port: addr.slice(i + 1) };
}

/** Whether a listening address accepts connections from other machines. */
export function isExposed(host: string): boolean {
  return !(host === 'localhost' || host.startsWith('127.') || host === '::1');
}

/**
 * One row per listener: a program bound to the same port over IPv4 and IPv6
 * (127.0.0.1 and ::1, or * and [::]) is listening once, as far as you're
 * concerned.
 */
export function mergeListeners(conns: Conn[]): Conn[] {
  const seen = new Set<string>();
  return conns.filter((c) => {
    const { host, port } = splitAddress(c.local);
    const key = `${c.pid}|${c.protocol}|${port}|${isExposed(host)}`;
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

const SERVICES: Record<string, string> = {
  '22': 'ssh', '53': 'dns', '80': 'http', '443': 'https', '993': 'imaps', '587': 'smtp',
  '3306': 'mysql', '5432': 'postgres', '6379': 'redis', '27017': 'mongodb', '9200': 'elasticsearch',
  '5672': 'amqp', '9092': 'kafka', '11211': 'memcached', '8080': 'http-alt', '5353': 'mdns',
};

export function serviceName(port: string): string | undefined {
  return SERVICES[port];
}

/** Network connections; the Network tab polls refresh() (see lib/poller.ts). */
class Network {
  conns = $state.raw<Conn[]>([]);
  error = $state<string | null>(null);
  loaded = $state(false);
  /** Show only connections from programs in wtf terminals, or everything. */
  scope = $state<'terminals' | 'all'>('terminals');
  /** Text filter; the Processes tab sets it to a PID. */
  filter = $state('');

  /** When the list was last fetched (ms since epoch). */
  updatedAt = $state<number | null>(null);
  #inFlight = false;

  async refresh() {
    if (this.#inFlight) return;
    this.#inFlight = true;
    try {
      this.conns = (await Net.List()) ?? [];
      this.error = null;
      this.updatedAt = Date.now();
    } catch (err) {
      this.error = err instanceof Error ? err.message : String(err);
    } finally {
      this.loaded = true;
      this.#inFlight = false;
    }
  }

  kill(pid: number) {
    return Net.Kill(pid);
  }
}

export const network = new Network();
