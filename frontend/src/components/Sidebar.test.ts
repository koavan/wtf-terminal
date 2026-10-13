// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render } from '@testing-library/svelte';

vi.mock('../../bindings/github.com/dayanraj/wtf/internal/files', () => ({ Service: { List: vi.fn(async (path: string) => ({ path, entries: [], truncated: false })) } }));
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/netconn', () => ({ Service: { List: vi.fn(async () => []) } }));
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/procs', () => ({ Service: { List: vi.fn(async () => []) } }));
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/config', () => ({ Service: { SetSidebarAutoRefresh: vi.fn() } }));
vi.mock('../lib/backend', () => ({ prettyPath: (p: string) => p, Service: {}, endpoint: vi.fn() }));
vi.mock('../lib/window', () => ({ thisWindow: { name: 'main' } }));
window.matchMedia ??= ((q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} })) as never;

const { settings } = await import('../lib/settings.svelte');
const { testConfig } = await import('../test/config');
const { sidebar } = await import('../lib/sidebar.svelte');
const { tabs } = await import('../lib/tabs.svelte');
const Sidebar = (await import('./Sidebar.svelte')).default;

afterEach(cleanup);

describe('Sidebar tabs', () => {
  it('are icons only, each named for tooltips and screen readers', async () => {
    settings.config = testConfig();
    tabs.open({ cwd: '/w' });
    sidebar.tab = 'files';
    const screen = render(Sidebar);
    const buttons = screen.getAllByRole('tab');
    expect(buttons.map((b) => b.getAttribute('aria-label'))).toEqual(['Files', 'SSH Hosts', 'AWS Profiles', 'Kubernetes', 'Snippets', 'Workspaces', 'Network', 'Processes', 'History', 'Pinned Output', 'Share & Record', 'Generate Keys & Passwords']);
    for (const b of buttons) {
      expect(b.textContent?.trim()).toBe(''); // no visible text to truncate
      expect(b.getAttribute('title')).toMatch(/\(.+\)$/); // name and shortcut on hover
    }
    expect(screen.getByRole('tab', { name: 'Files' }).getAttribute('aria-selected')).toBe('true');

    await fireEvent.click(screen.getByRole('tab', { name: 'History' }));
    expect(sidebar.tab).toBe('history');
    expect(screen.getByRole('tab', { name: 'History' }).getAttribute('aria-selected')).toBe('true');
  });

  it('has a button for the command-line tools', async () => {
    settings.config = testConfig();
    tabs.open({ cwd: '/w' });
    const { overlay } = await import('../lib/overlay.svelte');
    const screen = render(Sidebar);
    await fireEvent.click(screen.getByRole('button', { name: 'Command-Line Tools' }));
    expect(overlay.current).toEqual({ kind: 'tools', focus: undefined, welcome: undefined });
    overlay.current = null;
  });
});
