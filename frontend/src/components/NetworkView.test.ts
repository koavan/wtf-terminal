// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';

type Conn = {
  pid: number; process: string; protocol: string; local: string; remote: string; state: string; session: string;
  target: string; targetIP: string; targetResolved: boolean;
};
let conns: Conn[] = [];
const kill = vi.fn(async (_pid: number) => {});
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/netconn', () => ({
  Service: { List: vi.fn(async () => conns), Kill: (pid: number) => kill(pid) },
}));
const setAutoRefresh = vi.fn(async (_on: boolean) => {});
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/config', () => ({
  Service: { SetSidebarAutoRefresh: (on: boolean) => setAutoRefresh(on) },
}));
vi.mock('../lib/backend', () => ({ prettyPath: (p: string) => p, Service: {}, endpoint: vi.fn() }));
vi.mock('../lib/window', () => ({ thisWindow: { name: 'main' } }));
vi.mock('@wailsio/runtime', async (original) => ({
  ...(await original<object>()),
  Browser: { OpenURL: vi.fn() },
  Clipboard: { SetText: vi.fn(async () => {}) },
}));
window.matchMedia ??= ((q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} })) as never;
Element.prototype.animate ??= function () {
  return { finished: Promise.resolve(), cancel() {}, onfinish: null } as unknown as Animation;
};

const { tabs } = await import('../lib/tabs.svelte');
const { settings } = await import('../lib/settings.svelte');
const { testConfig } = await import('../test/config');
const { overlay } = await import('../lib/overlay.svelte');
const { network, splitAddress, isExposed, serviceName, mergeListeners } = await import('../lib/network.svelte');
const NetworkView = (await import('./NetworkView.svelte')).default;
const Overlays = (await import('./Overlays.svelte')).default;

const conn = (over: Partial<Conn>): Conn => ({
  pid: 1, process: 'x', protocol: 'TCP', local: '127.0.0.1:1', remote: '', state: 'LISTEN', session: '',
  target: '', targetIP: '', targetResolved: false, ...over,
});

beforeEach(() => {
  settings.config = testConfig();
  tabs.list = [];
  tabs.groups = [];
  tabs.open({ cwd: '/api' });
  tabs.activePane!.cwd = '/api';
  tabs.setSession(tabs.activePane!.id, 's1');
  tabs.open({ cwd: '/web' });
  tabs.activePane!.cwd = '/web';
  network.scope = 'terminals';
  network.filter = '';
  overlay.current = null;
  conns = [
    conn({ pid: 10, process: 'node', local: '127.0.0.1:3000', session: 's1' }),
    conn({ pid: 11, process: 'postgres', local: '*:5432' }),
    conn({ pid: 12, process: 'ssh', local: '10.0.0.2:50000', remote: '10.0.0.9:22', state: 'ESTABLISHED', session: 's1' }),
    conn({ pid: 13, process: 'vite', local: '[::1]:5173', session: 's-other-window' }),
  ];
  kill.mockClear();
});
afterEach(cleanup);

describe('address helpers', () => {
  it('splits IPv4 and IPv6 addresses', () => {
    expect(splitAddress('127.0.0.1:3000')).toEqual({ host: '127.0.0.1', port: '3000' });
    expect(splitAddress('[::1]:5432')).toEqual({ host: '::1', port: '5432' });
    expect(splitAddress('*:53')).toEqual({ host: '*', port: '53' });
  });
  it('knows which listeners other machines can reach', () => {
    expect(isExposed('127.0.0.1')).toBe(false);
    expect(isExposed('::1')).toBe(false);
    expect(isExposed('*')).toBe(true);
    expect(isExposed('0.0.0.0')).toBe(true);
  });
  it('merges one program listening on a port over IPv4 and IPv6', () => {
    const merged = mergeListeners([
      conn({ pid: 5, local: '127.0.0.1:32222' }),
      conn({ pid: 5, local: '[::1]:32222' }),
      conn({ pid: 5, local: '*:32222' }), // reachable from outside: a different listener
      conn({ pid: 6, local: '127.0.0.1:32222' }), // another program
    ]);
    expect(merged.map((c) => `${c.pid} ${c.local}`)).toEqual(['5 127.0.0.1:32222', '5 *:32222', '6 127.0.0.1:32222']);
  });

  it('names well-known ports', () => {
    expect(serviceName('443')).toBe('https');
    expect(serviceName('31337')).toBeUndefined();
  });
});

describe('NetworkView', () => {
  it('shows only connections from terminals, labeled with their tab', async () => {
    const screen = render(NetworkView);
    await screen.findByText(':3000');
    expect(screen.queryByText(':5432')).toBeNull(); // postgres isn't from a terminal
    expect(screen.getByText('10.0.0.9')).toBeTruthy();
    expect(screen.getAllByText('api')).toHaveLength(2); // node + ssh, both from the /api tab
    expect(screen.getByText('Another window')).toBeTruthy();
  });

  it('All shows every connection, with exposure badges', async () => {
    const screen = render(NetworkView);
    await screen.findByText(':3000');
    await fireEvent.click(screen.getByText('All'));
    expect(await screen.findByText(':5432')).toBeTruthy();
    expect(screen.getAllByText('network')).toHaveLength(1); // *:5432
    expect(screen.getAllByText('local').length).toBeGreaterThanOrEqual(2);
  });

  it('clicking the tab label jumps to that tab', async () => {
    expect(tabs.active!.groupId).toBeNull();
    const screen = render(NetworkView);
    const [chip] = await screen.findAllByText('api');
    await fireEvent.click(chip);
    expect(tabs.activePane!.sessionId).toBe('s1');
  });

  it('filters by port', async () => {
    const screen = render(NetworkView);
    await screen.findByText(':3000');
    await fireEvent.input(screen.getByPlaceholderText(/Filter/), { target: { value: '5173' } });
    await waitFor(() => expect(screen.queryByText(':3000')).toBeNull());
    expect(screen.getByText(':5173')).toBeTruthy();
  });

  it('ending a process asks first, then sends the kill', async () => {
    render(Overlays);
    const screen = render(NetworkView);
    const row = (await screen.findByText(':3000')).closest('.row')!;
    await fireEvent.contextMenu(row);
    await fireEvent.click(await screen.findByText('End node…'));
    expect(kill).not.toHaveBeenCalled();
    expect(await screen.findByText(/freeing port 3000/)).toBeTruthy();
    await fireEvent.click(screen.getByText('End Process'));
    expect(kill).toHaveBeenCalledWith(10);
  });

  it('shows where ping is pinging, as a connection', async () => {
    conns = [
      conn({ pid: 20, process: 'ping', protocol: 'ICMP', local: '*:*', state: '', session: 's1', target: 'google.com', targetIP: '142.251.215.78', targetResolved: true }),
      conn({ pid: 21, process: 'ping', protocol: 'ICMP', local: '*:*', state: '', session: 's1', target: '1.1.1.1', targetIP: '1.1.1.1' }),
    ];
    const screen = render(NetworkView);
    expect(await screen.findByText('142.251.215.78')).toBeTruthy();
    expect(screen.getByText('google.com')).toBeTruthy();
    expect(screen.getByText('1.1.1.1')).toBeTruthy();
    expect(screen.queryByText(/Other sockets/)).toBeNull();
    // Only the looked-up address is marked approximate.
    expect(screen.getAllByLabelText('resolved address')).toHaveLength(1);
  });

  it('pid: filters to exactly one process', async () => {
    conns = [conn({ pid: 123, process: 'a', local: '127.0.0.1:1230', session: 's1' }), conn({ pid: 1234, process: 'b', local: '127.0.0.1:9', session: 's1' })];
    network.filter = 'pid:123';
    const screen = render(NetworkView);
    expect(await screen.findByText(':1230')).toBeTruthy();
    expect(screen.queryByText(':9')).toBeNull();
  });

  it('with auto-refresh off it loads once, says so, and only refreshes on request', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const { Service } = await import('../../bindings/github.com/dayanraj/wtf/internal/netconn');
      const list = vi.mocked(Service.List);
      list.mockClear();
      settings.config = testConfig({ sidebarAutoRefresh: false });
      const screen = render(NetworkView);
      await screen.findByText(':3000');
      expect(screen.getByRole('status').textContent).toMatch(/Paused · updated/);
      await vi.advanceTimersByTimeAsync(30_000);
      expect(list).toHaveBeenCalledTimes(1);

      await fireEvent.click(screen.getByLabelText('Refresh now'));
      expect(list).toHaveBeenCalledTimes(2);

      await fireEvent.click(screen.getByLabelText('Resume auto-refresh'));
      expect(setAutoRefresh).toHaveBeenCalledWith(true);
    } finally {
      vi.useRealTimers();
    }
  });

  it('with auto-refresh on it polls at the configured interval', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.spyOn(document, 'hasFocus').mockReturnValue(true);
    try {
      const { Service } = await import('../../bindings/github.com/dayanraj/wtf/internal/netconn');
      const list = vi.mocked(Service.List);
      list.mockClear();
      settings.config = testConfig({ sidebarRefreshInterval: 5 });
      const screen = render(NetworkView);
      await screen.findByText(':3000');
      expect(screen.queryByRole('status')).toBeNull();
      await vi.advanceTimersByTimeAsync(10_500);
      expect(list).toHaveBeenCalledTimes(3); // on open, then at 5s and 10s
    } finally {
      vi.restoreAllMocks();
      vi.useRealTimers();
    }
  });

  it('explains an empty terminals view and offers All', async () => {
    conns = [conn({ pid: 11, process: 'postgres', local: '*:5432' })];
    const screen = render(NetworkView);
    await screen.findByText('Nothing from your terminals');
    await fireEvent.click(screen.getByText('Show all connections'));
    expect(await screen.findByText(':5432')).toBeTruthy();
  });
});
