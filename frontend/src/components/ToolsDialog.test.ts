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
  Browser: { OpenURL: vi.fn() },
}));

const brew = {
  id: 'brew', label: 'Homebrew', command: 'brew install kubectl', script: '', sudo: false,
  dest: '/opt/homebrew/bin', verify: 'Homebrew checks each download’s SHA-256.', note: '',
};
const official = {
  id: 'official', label: 'Official release (dl.k8s.io)', command: '', script: '#!/bin/sh\nset -eu\ncurl -fL …/kubectl\n', sudo: false,
  dest: '~/.local/bin/kubectl', verify: 'Checked against the SHA-256 checksum.', note: '~/.local/bin isn’t on your shell’s PATH.',
};
const STATUS = [
  {
    id: 'kubectl', name: 'kubectl', about: 'The Kubernetes command-line tool', programs: ['kubectl'], found: false, path: '', version: '',
    missing: ['kubectl'], usedBy: ['Kubernetes tab'], homepage: 'https://kubernetes.io/docs/tasks/tools/', methods: [brew, official], builtIn: '',
  },
  {
    id: 'git', name: 'git', about: 'Version control', programs: ['git'], found: true, path: '/opt/homebrew/bin/git', version: 'git version 2.55.0',
    missing: null, usedBy: ['Git status in Files'], homepage: '', methods: null, builtIn: '',
  },
];

vi.mock('../../bindings/github.com/dayanraj/wtf/internal/tools', () => ({
  Service: {
    Check: vi.fn(async () => STATUS),
    Install: vi.fn(async (_id: string, method: string) => (method === 'brew' ? 'brew install kubectl' : "sh '/Users/me/Library/Caches/wtf/install/kubectl-official.sh'")),
    FirstRun: vi.fn(async () => []),
  },
}));

const { Service: Tools } = await import('../../bindings/github.com/dayanraj/wtf/internal/tools');
const { overlay } = await import('../lib/overlay.svelte');
const { runCommand } = await import('../lib/commands');
const { tabs, focusedPane } = await import('../lib/tabs.svelte');
const { missingTool } = await import('../lib/tools');
const Overlays = (await import('./Overlays.svelte')).default;

beforeEach(() => {
  overlay.current = null;
  vi.clearAllMocks();
  tabs.list.splice(0);
  tabs.open({ cwd: '/Users/me' });
  focusedPane(tabs.active!).env = { PATH: '/opt/homebrew/bin:/usr/bin' };
});
afterEach(cleanup);

describe('Command-Line Tools', () => {
  it('lists each tool: found with its version and path, or not installed', async () => {
    const view = render(Overlays);
    runCommand('tools.show');
    await view.findByText('git version 2.55.0');
    expect(Tools.Check).toHaveBeenCalledWith('/opt/homebrew/bin:/usr/bin');
    view.getByText('/opt/homebrew/bin/git');
    view.getByText('not installed');
    view.getByText('Used by: Kubernetes tab');
    // Only the missing one can be installed.
    expect(view.getAllByText('Install…')).toHaveLength(1);
  });

  it('shows the plan before running it in a new tab', async () => {
    const view = render(Overlays);
    runCommand('tools.show');
    await fireEvent.click(await view.findByText('Install…'));
    view.getByText('Install kubectl');
    view.getByText('brew install kubectl');
    view.getByText('/opt/homebrew/bin');

    // The official download: its whole script and the PATH note.
    await fireEvent.click(view.getByLabelText('Official release (dl.k8s.io)'));
    view.getByText(/curl -fL …\/kubectl/);
    view.getByText('~/.local/bin isn’t on your shell’s PATH.');
    expect(Tools.Install).not.toHaveBeenCalled();

    const before = tabs.list.length;
    await fireEvent.click(view.getByText('Run in New Tab'));
    await waitFor(() => expect(tabs.list.length).toBe(before + 1));
    expect(Tools.Install).toHaveBeenCalledWith('kubectl', 'official', '/opt/homebrew/bin:/usr/bin');
    expect(focusedPane(tabs.active!).initialCommand).toBe("sh '/Users/me/Library/Caches/wtf/install/kubectl-official.sh'");
    expect(overlay.current).toBeNull();
  });

  it('opens straight at a missing tool from an error', async () => {
    const view = render(Overlays);
    overlay.show({ kind: 'tools', focus: 'kubectl' });
    await view.findByText('Install kubectl');
    await fireEvent.click(view.getByText('Back'));
    await view.findByText('Command-Line Tools');
  });

  it('welcomes on the first run with what is missing', async () => {
    const view = render(Overlays);
    overlay.show({ kind: 'tools', welcome: ['kubectl', 'AWS CLI'] });
    await view.findByText(/Welcome to wtf\. kubectl, AWS CLI/);
  });
});

describe('missingTool', () => {
  it('knows the errors that mean a program is missing', () => {
    expect(missingTool("kubectl isn't installed (or isn't on the shell's PATH)")).toBe('kubectl');
    expect(missingTool("the aws CLI isn't installed")).toBe('aws');
    expect(missingTool('Unauthorized')).toBeNull();
    expect(missingTool('')).toBeNull();
  });
});
