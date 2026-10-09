// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';

window.matchMedia ??= ((q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} })) as never;
Element.prototype.animate ??= function () {
  return { finished: Promise.resolve(), cancel() {}, onfinish: null } as unknown as Animation;
};
Element.prototype.scrollTo ??= function () {};
vi.mock('../lib/backend', () => ({ prettyPath: (p: string) => p.replace('/Users/me', '~'), homeDir: () => '/Users/me', Service: {}, endpoint: vi.fn() }));
vi.mock('../lib/window', () => ({ thisWindow: { name: 'main' }, Windows: {}, closeThisWindow: vi.fn(), moveTabToNewWindow: vi.fn() }));
vi.mock('@wailsio/runtime', async (original) => ({
  ...(await original<object>()),
  Clipboard: { SetText: vi.fn(async () => {}) },
  Browser: { OpenURL: vi.fn() },
}));
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/files', () => ({ Service: { Reveal: vi.fn(async () => {}) } }));
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/about', () => ({
  Service: {
    Info: vi.fn(async () => ({
      version: '0.1.0', commit: '85e7f34-dirty', buildDate: '2026-10-09', goVersion: 'go1.25.0', wailsVersion: 'v3.0.0-beta.28',
      os: 'macOS 26.0 (25A123)', arch: 'arm64', shell: '/bin/bash', shellIntegration: true,
      configFile: '/Users/me/.config/wtf/config.toml', snippetsFile: '/Users/me/.config/wtf/snippets.toml',
      stateDir: '/Users/me/Library/Application Support/wtf', cacheDir: '/Users/me/Library/Caches/wtf',
      repository: 'https://github.com/dayanraj-rently/wtf-terminal',
    })),
  },
}));

const { Clipboard, Browser } = await import('@wailsio/runtime');
const { Service: Files } = await import('../../bindings/github.com/dayanraj/wtf/internal/files');
const { overlay } = await import('../lib/overlay.svelte');
const { runCommand } = await import('../lib/commands');
const Overlays = (await import('./Overlays.svelte')).default;

beforeEach(() => {
  overlay.current = null;
  vi.clearAllMocks();
});
afterEach(cleanup);

describe('About wtf', () => {
  it('shows the tagline, the build and this setup', async () => {
    const view = render(Overlays);
    runCommand('app.about');
    await view.findByText('A terminal for all the What-The-Fuck moments');
    await view.findByText('Version 0.1.0 · 85e7f34-dirty · built 2026-10-09');
    view.getByText('macOS 26.0 (25A123) · arm64');
    view.getByText('Go 1.25.0 · Wails v3.0.0-beta.28');
    view.getByText(/shell integration on/);
    view.getByText('~/.config/wtf/config.toml');
    expect(document.querySelector('.dialog')?.getAttribute('aria-label')).toBe('About wtf');
  });

  it('copies diagnostics without any paths', async () => {
    const view = render(Overlays);
    runCommand('app.about');
    await view.findByText(/Version 0.1.0/);
    await fireEvent.click(view.getByText('Copy Diagnostics'));
    const text = vi.mocked(Clipboard.SetText).mock.calls[0][0];
    expect(text).toBe(
      'wtf 0.1.0 (85e7f34-dirty, built 2026-10-09)\nmacOS 26.0 (25A123) · arm64\nGo go1.25.0 · Wails v3.0.0-beta.28\nShell: bash, shell integration on',
    );
    expect(text).not.toContain('/Users/');
  });

  it('links to the manual, the repository and the files it names', async () => {
    const view = render(Overlays);
    runCommand('app.about');
    await view.findByText(/Version 0.1.0/);
    await fireEvent.click(view.getByText('~/.config/wtf/snippets.toml'));
    expect(Files.Reveal).toHaveBeenCalledWith('/Users/me/.config/wtf/snippets.toml');
    await fireEvent.click(view.getByText('GitHub'));
    expect(Browser.OpenURL).toHaveBeenCalledWith('https://github.com/dayanraj-rently/wtf-terminal');
    await fireEvent.click(view.getByText('Kubernetes'));
    await waitFor(() => expect(overlay.current).toMatchObject({ kind: 'manual', topic: 'kubernetes' }));
  });
});
