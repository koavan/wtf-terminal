// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render } from '@testing-library/svelte';

vi.mock('../lib/backend', () => ({ prettyPath: (p: string) => p, homeDir: () => '/home/me', Service: {}, endpoint: vi.fn() }));
vi.mock('../lib/window', () => ({ thisWindow: { name: 'main' } }));
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/usage', () => ({ Service: { Sample: vi.fn() } }));
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/config', () => ({ Service: { SetShowUsage: vi.fn(async () => {}) } }));
window.matchMedia ??= ((q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} })) as never;
Element.prototype.animate ??= function () {
  return { finished: Promise.resolve(), cancel() {}, onfinish: null } as unknown as Animation;
};

const { Service } = await import('../../bindings/github.com/dayanraj/wtf/internal/usage');
const { Service: ConfigService } = await import('../../bindings/github.com/dayanraj/wtf/internal/config');
const { overlay } = await import('../lib/overlay.svelte');
const { settings } = await import('../lib/settings.svelte');
const { tabs } = await import('../lib/tabs.svelte');
const { testConfig } = await import('../test/config');
const lib = await import('../lib/usage.svelte');
const Overlays = (await import('./Overlays.svelte')).default;
const AddressBar = (await import('./AddressBar.svelte')).default;

const MB = 1024 * 1024;
const SAMPLE = {
  app: { cpu: 1.5, memory: 120 * MB, procs: [{ pid: 10, name: 'wtf', cpu: 1.5, memory: 120 * MB }] },
  window: {
    cpu: 2.25, memory: 300 * MB,
    procs: [{ pid: 11, name: 'com.apple.WebKit.WebContent', cpu: 2, memory: 260 * MB }, { pid: 12, name: 'com.apple.WebKit.GPU', cpu: 0.25, memory: 40 * MB }],
  },
  programs: { cpu: 40, memory: 2048 * MB, procs: [{ pid: 20, name: 'node', cpu: 40, memory: 2048 * MB }] },
  interval: 2, cores: 10, memoryKind: 'footprint', note: '',
};

beforeEach(() => {
  settings.config = testConfig();
  tabs.list = [];
  tabs.activeId = null;
  tabs.open({ cwd: '/w' });
  overlay.current = null;
  lib.resourceUsage.current = null;
  vi.clearAllMocks();
  vi.mocked(Service.Sample).mockResolvedValue(structuredClone(SAMPLE) as never);
  vi.spyOn(document, 'hasFocus').mockReturnValue(true);
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe('formatting', () => {
  it('formats CPU and memory', () => {
    expect([lib.formatCPU(-1), lib.formatCPU(0.04), lib.formatCPU(9.96), lib.formatCPU(42.4)]).toEqual(['…', '0.0%', '10.0%', '42%']);
    expect([lib.formatBytes(512), lib.formatBytes(840 * 1024), lib.formatBytes(412 * MB), lib.formatBytes(1.25 * 1024 * MB)]).toEqual(['512 B', '840 KB', '412 MB', '1.3 GB']);
  });

  it('counts the app and its windows as wtf, not the programs run in it', () => {
    expect(lib.wtfTotal(SAMPLE as never)).toEqual({ cpu: 3.75, memory: 420 * MB });
    expect(lib.wtfTotal({ ...SAMPLE, interval: 0 } as never).cpu).toBe(-1);
  });
});

describe('sampling', () => {
  it('runs only while something watches, every 2 seconds', async () => {
    vi.useFakeTimers();
    const stop = lib.resourceUsage.watch();
    const again = lib.resourceUsage.watch();
    expect(Service.Sample).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(4000);
    expect(Service.Sample).toHaveBeenCalledTimes(3);
    stop();
    stop(); // twice is harmless
    await vi.advanceTimersByTimeAsync(2000);
    expect(Service.Sample).toHaveBeenCalledTimes(4); // the other watcher still wants it
    again();
    await vi.advanceTimersByTimeAsync(10_000);
    expect(Service.Sample).toHaveBeenCalledTimes(4);
  });
});

describe('the readout and the dialog', () => {
  it('shows in the address bar only when turned on, and nothing is sampled otherwise', async () => {
    const bar = render(AddressBar);
    expect(bar.queryByLabelText('Resource usage')).toBeNull();
    expect(Service.Sample).not.toHaveBeenCalled();
    cleanup();

    settings.config = testConfig({ showUsage: true });
    const shown = render(AddressBar);
    await shown.findByText('3.8%');
    shown.getByText('420 MB');
    await fireEvent.click(shown.getByLabelText('Resource usage'));
    expect(overlay.current?.kind).toBe('usage');
  });

  it('breaks it down by process, and turns the readout on', async () => {
    overlay.show({ kind: 'usage' });
    const view = render(Overlays);
    await view.findByText('wtf in all');
    view.getByText('com.apple.WebKit.WebContent');
    expect(view.getAllByText('2.0 GB')).toHaveLength(2); // the programs' total and node: listed, not in wtf's
    view.getAllByText('420 MB'); // wtf in all
    view.getByText(/10 cores = 1000%/);
    await fireEvent.click(view.getByLabelText('Show in the address bar'));
    expect(ConfigService.SetShowUsage).toHaveBeenCalledWith(true);
  });
});
