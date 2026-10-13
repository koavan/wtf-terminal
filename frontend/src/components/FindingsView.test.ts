// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';

vi.mock('../lib/backend', () => ({ prettyPath: (p: string) => p.replace('/home/me', '~'), homeDir: () => '/home/me', Service: {}, endpoint: vi.fn() }));
vi.mock('../lib/window', () => ({ thisWindow: { name: 'main' } }));
vi.mock('@wailsio/runtime', async (original) => ({
  ...(await original<object>()),
  Clipboard: { SetText: vi.fn(async () => {}) },
  Browser: { OpenURL: vi.fn() },
}));
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/findings', () => ({
  Service: { Load: vi.fn(), Scanners: vi.fn(), ScanCommand: vi.fn(), Forget: vi.fn() },
}));
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/files', () => ({ Service: { Reveal: vi.fn(async () => {}) } }));
window.matchMedia ??= ((q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} })) as never;
Element.prototype.animate ??= function () {
  return { finished: Promise.resolve(), cancel() {}, onfinish: null } as unknown as Animation;
};

const { Service } = await import('../../bindings/github.com/dayanraj/wtf/internal/findings');
const { tabs, focusedPane } = await import('../lib/tabs.svelte');
const { overlay } = await import('../lib/overlay.svelte');
const { sidebar } = await import('../lib/sidebar.svelte');
const { commandFinished, registerTerminal } = await import('../lib/terminals');
const lib = await import('../lib/findings.svelte');
const FindingsView = (await import('./FindingsView.svelte')).default;
const Overlays = (await import('./Overlays.svelte')).default;

type Finding = import('../lib/findings.svelte').Finding;
const finding = (f: Partial<Finding>): Finding => ({
  source: 0, tool: 'Trivy', ruleId: '', rule: '', description: '', helpUri: '', severity: 'warning', score: 0,
  message: '', path: '', line: 0, column: 0, ...f,
});

const ROOT = '/home/me/shop';
const FINDINGS = [
  finding({ ruleId: 'DS002', rule: 'Image user should not be root', severity: 'medium', score: 4, message: 'Specify a USER', path: `${ROOT}/Dockerfile`, line: 1 }),
  finding({ ruleId: 'CVE-2024-0001', rule: 'openssl: buffer overflow', severity: 'critical', score: 9.8, message: 'Package: openssl', path: `${ROOT}/go.mod`, line: 12, helpUri: 'https://avd.example/cve' }),
  finding({ ruleId: 'CVE-2024-0001', rule: 'openssl: buffer overflow', severity: 'critical', score: 9.8, message: 'Package: openssl', path: `${ROOT}/svc/go.mod`, line: 3 }),
  finding({ source: 1, tool: 'Semgrep OSS', ruleId: 'go.sqli', rule: 'SQL built from user input', severity: 'error', message: 'Use a parameterized query', path: `${ROOT}/db.go`, line: 40 }),
];
const REPORT = {
  root: ROOT,
  sources: [
    { path: '/cache/abc/trivy.sarif', tool: 'Trivy', version: '0.56.2', modified: new Date().toISOString(), count: 3, fromScan: true, error: '' },
    { path: `${ROOT}/ci.sarif`, tool: 'Semgrep OSS', version: '1.90.0', modified: new Date().toISOString(), count: 1, fromScan: false, error: '' },
  ],
  findings: FINDINGS,
  truncated: 0,
};

beforeEach(() => {
  tabs.list = [];
  tabs.groups = [];
  tabs.activeId = null;
  tabs.open({ cwd: `${ROOT}/svc` });
  focusedPane(tabs.active!).cwd = `${ROOT}/svc`;
  overlay.current = null;
  lib.findings.report = null;
  lib.findings.error = '';
  vi.clearAllMocks();
  vi.mocked(Service.Load).mockResolvedValue(structuredClone(REPORT) as never);
});
afterEach(cleanup);

describe('grouping', () => {
  it('orders by severity, then by how often a rule was found', () => {
    const g = lib.group(FINDINGS);
    expect(g.map((s) => [s.severity, s.count])).toEqual([['critical', 2], ['error', 1], ['medium', 1]]);
    expect(g[0].rules[0].items.map((f) => f.path)).toEqual([`${ROOT}/go.mod`, `${ROOT}/svc/go.mod`]);
  });

  it('filters on every word, and shows paths relative to the root', () => {
    expect(lib.group(FINDINGS, 'openssl svc').flatMap((s) => s.rules.flatMap((r) => r.items)).length).toBe(1);
    expect(lib.group(FINDINGS, 'semgrep')[0].severity).toBe('error');
    expect(lib.where(FINDINGS[1], ROOT)).toBe('go.mod:12');
    expect(lib.where(finding({ path: 'pkg:golang/x@1' }), ROOT)).toBe('pkg:golang/x@1');
  });
});

describe('FindingsView', () => {
  it('reads the results of the focused shell’s folder, grouped by severity', async () => {
    const view = render(FindingsView);
    await view.findByText('openssl: buffer overflow');
    expect(Service.Load).toHaveBeenCalledWith(`${ROOT}/svc`);
    view.getByText('shop'); // the repository, not the subfolder
    view.getByText('Trivy 0.56.2');
    view.getByText(/3 findings/);
    const severities = [...view.container.querySelectorAll('.severity')].map((e) => e.textContent?.replace(/\s+/g, ' ').trim());
    expect(severities).toEqual(['Critical 2', 'Error 1', 'Medium 1']);
  });

  it('shows a rule’s locations on click, and opens one in the editor at its line', async () => {
    const view = render(FindingsView);
    await fireEvent.click(await view.findByText('openssl: buffer overflow'));
    await fireEvent.click(view.getByText('svc/go.mod:3'));
    const editor = tabs.active!.root;
    expect(editor.kind === 'split' && editor.b.kind === 'pane' && editor.b.pane.initialCommand).toBe(`\${EDITOR:-vi} +3 ${ROOT}/svc/go.mod`);
  });

  it('reads again when a scan finishes, not for other commands', async () => {
    render(FindingsView);
    await waitFor(() => expect(Service.Load).toHaveBeenCalledTimes(1));
    commandFinished('ls -la', 0);
    expect(Service.Load).toHaveBeenCalledTimes(1);
    commandFinished(`(cd ${ROOT} && trivy fs --format sarif --output /c/trivy.sarif .)`, 0);
    expect(Service.Load).toHaveBeenCalledTimes(2);
  });

  it('offers a scan when there are no results', async () => {
    vi.mocked(Service.Load).mockResolvedValue({ root: ROOT, sources: null, findings: null, truncated: 0 } as never);
    const view = render(FindingsView);
    await fireEvent.click(await view.findByText('Run Scan…'));
    expect(overlay.current?.kind).toBe('scan');
  });
});

describe('Run Scan', () => {
  const SCANNERS = [
    { id: 'trivy', name: 'Trivy', about: 'Vulnerable dependencies', tool: 'trivy', note: 'Downloads its database.', found: true },
    { id: 'gitleaks', name: 'Gitleaks', about: 'Secrets in git history', tool: 'gitleaks', note: '', found: false },
  ];
  beforeEach(() => {
    vi.mocked(Service.Scanners).mockResolvedValue(SCANNERS as never);
    vi.mocked(Service.ScanCommand).mockImplementation((async (_folder: string, id: string) => ({
      root: ROOT, output: `/cache/abc/${id}.sarif`, command: `(cd ${ROOT} && ${id} scan --out /cache/abc/${id}.sarif .)`,
    })) as never);
  });

  it('shows the command, and types it into the focused shell', async () => {
    const pane = focusedPane(tabs.active!);
    const typed: string[] = [];
    const unregister = registerTerminal(pane.id, { sendInput: (b: Uint8Array) => typed.push(new TextDecoder().decode(b)), focus: vi.fn() } as never);
    try {
      overlay.show({ kind: 'scan' });
      const view = render(Overlays);
      await view.findByText(`(cd ${ROOT} && trivy scan --out /cache/abc/trivy.sarif .)`);
      view.getByText('Downloads its database.');
      expect(Service.ScanCommand).toHaveBeenCalledWith(`${ROOT}/svc`, 'trivy');
      await fireEvent.click(view.getByText('Run Scan'));
      expect(typed).toEqual([`\x15(cd ${ROOT} && trivy scan --out /cache/abc/trivy.sarif .)\r`]);
      expect(sidebar.tab).toBe('findings');
    } finally {
      unregister();
    }
  });

  it('offers to install a scanner that isn’t there, and won’t run it', async () => {
    overlay.show({ kind: 'scan' });
    const view = render(Overlays);
    await fireEvent.click(await view.findByLabelText(/Gitleaks/));
    view.getByText(/not installed/);
    expect((view.getByText('Run Scan') as HTMLButtonElement).disabled).toBe(true);
    await fireEvent.click(view.getByText('Install Gitleaks…'));
    expect(overlay.current).toMatchObject({ kind: 'tools', focus: 'gitleaks' });
  });
});
