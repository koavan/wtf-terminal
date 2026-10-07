// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render } from '@testing-library/svelte';

// The Wails bridge isn't available outside the app.
vi.mock('../lib/backend', () => ({ prettyPath: (p: string) => p, Service: {}, endpoint: vi.fn() }));
vi.mock('../lib/window', () => ({ thisWindow: { name: 'main' } }));

// jsdom has no Web Animations; Svelte transitions only need a stub.
Element.prototype.animate ??= function () {
  return { finished: Promise.resolve(), cancel() {}, onfinish: null } as unknown as Animation;
};

window.matchMedia ??= ((query: string) => ({
  matches: false, media: query, addEventListener() {}, removeEventListener() {},
})) as unknown as typeof window.matchMedia;

const { tabs } = await import('../lib/tabs.svelte');
const { overlay } = await import('../lib/overlay.svelte');
const { leaves } = await import('../lib/layout');
const Overlays = (await import('./Overlays.svelte')).default;

const ids = () => tabs.list.map((t) => t.id);
const anchor = { left: 10, bottom: 40 } as DOMRect;

/** Three tabs; the last two in a group. */
function setup() {
  for (const cwd of ['/a', '/b', '/c']) tabs.open({ cwd });
  const [a, b, c] = ids();
  const g = tabs.createGroup(b)!;
  tabs.addToGroup(c, g.id);
  return { a, b, c, g };
}

beforeEach(() => {
  tabs.list = [];
  tabs.groups = [];
  tabs.activeId = null;
  tabs.onEmpty = vi.fn();
  overlay.current = null;
});
afterEach(cleanup);

describe('group editor buttons', () => {
  it('Close group closes the tabs in the group', async () => {
    const { a, g } = setup();
    overlay.current = { kind: 'group', groupId: g.id, anchor };
    const screen = render(Overlays);
    await fireEvent.click(screen.getByText('Close group'));
    expect(ids()).toEqual([a]);
    expect(tabs.groups).toHaveLength(0);
    expect(overlay.current).toBeNull();
  });

  it('Ungroup keeps the tabs', async () => {
    const { g } = setup();
    overlay.current = { kind: 'group', groupId: g.id, anchor };
    const screen = render(Overlays);
    await fireEvent.click(screen.getByText('Ungroup'));
    expect(tabs.list).toHaveLength(3);
    expect(tabs.groups).toHaveLength(0);
  });

  it('New tab in group adds a tab to it', async () => {
    const { g } = setup();
    overlay.current = { kind: 'group', groupId: g.id, anchor };
    const screen = render(Overlays);
    await fireEvent.click(screen.getByText('New tab in group'));
    expect(tabs.tabsIn(g.id)).toHaveLength(3);
  });

  it('Close group asks first when a program is running, then closes', async () => {
    const { a, c, g } = setup();
    const pane = leaves(tabs.get(c)!.root)[0];
    pane.process = 'vim';
    pane.isShell = false;
    overlay.current = { kind: 'group', groupId: g.id, anchor };
    const screen = render(Overlays);

    await fireEvent.click(screen.getByText('Close group'));
    expect(tabs.list).toHaveLength(3); // not yet
    expect(overlay.current?.kind).toBe('confirm');

    await fireEvent.click(await screen.findByText('Close Tabs'));
    expect(ids()).toEqual([a]);
    expect(overlay.current).toBeNull();
  });

  it('Cancel in the confirmation keeps everything', async () => {
    const { c, g } = setup();
    const pane = leaves(tabs.get(c)!.root)[0];
    pane.running = true;
    pane.command = 'npm run dev';
    overlay.current = { kind: 'group', groupId: g.id, anchor };
    const screen = render(Overlays);
    await fireEvent.click(screen.getByText('Close group'));
    expect(screen.getByText(/npm run dev/)).toBeTruthy();
    await fireEvent.click(await screen.findByText('Cancel'));
    expect(tabs.list).toHaveLength(3);
  });
});

describe('context menu', () => {
  it('runs the clicked entry and closes', async () => {
    const action = vi.fn();
    overlay.openMenu(10, 10, [{ label: 'Do it', action }, 'separator', { label: 'Other', action: vi.fn() }]);
    const screen = render(Overlays);
    await fireEvent.click(screen.getByText('Do it'));
    expect(action).toHaveBeenCalledOnce();
    expect(overlay.current).toBeNull();
  });
});
