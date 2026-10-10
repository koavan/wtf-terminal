// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';

vi.mock('../lib/backend', () => ({ prettyPath: (p: string) => p.replace('/home/me', '~'), homeDir: () => '/home/me', Service: {}, endpoint: vi.fn() }));
vi.mock('../lib/window', () => ({ thisWindow: { name: 'main' } }));
vi.mock('@wailsio/runtime', async (original) => ({
  ...(await original<object>()),
  Clipboard: { SetText: vi.fn(async () => {}) },
  Dialogs: { OpenFile: vi.fn(async () => '/home/me/Downloads/team.yaml') },
}));
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/files', () => ({ Service: { Reveal: vi.fn() } }));
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/awsprofiles', () => ({
  Service: {
    List: vi.fn(async () => ({
      profiles: [{ name: 'devops', kind: 'keys', region: 'us-west-2', settings: [], credentialsOther: [], locked: [], sourceFor: [] }],
      versions: {}, problems: [], credentialsFile: '', configFile: '',
    })),
  },
}));
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/kube', () => ({
  Service: {
    Contexts: vi.fn(), ShellConfig: vi.fn(), TestConnection: vi.fn(), PlanEdit: vi.fn(), ApplyEdit: vi.fn(),
    EKSClusters: vi.fn(), BackupConfigs: vi.fn(async () => {}),
  },
}));
window.matchMedia ??= ((q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} })) as never;
Element.prototype.animate ??= function () {
  return { finished: Promise.resolve(), cancel() {}, onfinish: null } as unknown as Animation;
};

const { Service } = await import('../../bindings/github.com/dayanraj/wtf/internal/kube');
const { tabs, focusedPane } = await import('../lib/tabs.svelte');
const { overlay } = await import('../lib/overlay.svelte');
const { registerTerminal } = await import('../lib/terminals');
const { kubeConfigs, authLabel, clusterLabel, eksCommand } = await import('../lib/kubeconfigs.svelte');
const KubeContexts = (await import('./KubeContexts.svelte')).default;
const Overlays = (await import('./Overlays.svelte')).default;

const ctx = (c: Record<string, unknown>) => ({
  name: '', cluster: '', user: '', namespace: '', file: '/home/me/.kube/config', server: '', auth: 'none', execCommand: '',
  execArgs: [], execEnv: [], pluginFound: false, eksCluster: '', eksRegion: '', awsProfile: '', current: false, active: true, problems: [], ...c,
});
const CONTEXTS = [
  ctx({
    name: 'dayan@production', cluster: 'production', user: 'dayan', namespace: 'shop', server: 'https://ABC.gr7.us-east-1.eks.amazonaws.com',
    auth: 'exec', execCommand: 'aws-iam-authenticator', execArgs: ['token', '-i', 'production'], execEnv: ['AWS_PROFILE=devops'],
    eksCluster: 'production', eksRegion: 'us-east-1', awsProfile: 'devops', current: true,
  }),
  ctx({ name: 'kind-local', cluster: 'kind', user: 'kind', server: 'https://127.0.0.1:6443', auth: 'client-cert' }),
  ctx({ name: 'qe', cluster: 'default', user: 'default', server: 'https://qe.example.com:6443', auth: 'token', file: '/home/me/.kube/qe.yaml', active: false }),
];
const VERSIONS = { '/home/me/.kube/config': 'v1' };
const LIST = { contexts: CONTEXTS, files: ['/home/me/.kube/config'], otherFiles: ['/home/me/.kube/qe.yaml'], problems: [], versions: VERSIONS };
const ENV = { PATH: '/usr/bin', KUBECONFIG: '/home/me/.kube/config' };

type MenuItem = { label?: string; action?: () => void } | string;
const menuItems = () => ((overlay.current as { entries: MenuItem[] } | null)?.entries ?? []);
const menuLabels = () => menuItems().map((e) => (typeof e === 'string' ? e : e.label));
const pick = (label: string) => (menuItems().find((e) => typeof e !== 'string' && e.label === label) as { action: () => void }).action();
const commands = () => tabs.list.map((t) => focusedPane(t).initialCommand);

let sent: string[] = [];
beforeEach(() => {
  tabs.list = [];
  tabs.groups = [];
  tabs.activeId = null;
  tabs.open({ cwd: '/home/me' });
  const pane = focusedPane(tabs.active!);
  pane.env = ENV;
  pane.infra = { kubeContext: 'kind-local' } as never;
  overlay.current = null;
  kubeConfigs.checks.clear();
  vi.clearAllMocks();
  vi.mocked(Service.Contexts).mockResolvedValue(structuredClone(LIST) as never);
  vi.mocked(Service.ShellConfig).mockResolvedValue(`export KUBECONFIG='/state/kube-shells/ab.yaml:/home/me/.kube/config'`);
  sent = [];
  registerTerminal(pane.id, {
    focus() {}, clear() {}, selectAll() {}, getSelection: () => '', paste() {}, type() {},
    sendInput: (b) => sent.push(new TextDecoder().decode(b)),
    jumpPrompt() {}, revealCommand: () => true, lastOutput: () => null, blockOutput: () => null, pinLastOutput: () => false, serialize: () => '', snapshot: () => [],
  });
});
afterEach(cleanup);

async function show() {
  const view = render(KubeContexts);
  render(Overlays);
  await view.findByText('kind-local');
  return view;
}

describe('helpers', () => {
  it('describe a context briefly', () => {
    expect(authLabel(CONTEXTS[0] as never)).toBe('aws-iam-authenticator');
    expect(authLabel(ctx({ auth: 'exec', execCommand: '/usr/local/bin/aws', execArgs: ['eks', 'get-token'] }) as never)).toBe('aws eks get-token');
    expect(clusterLabel(CONTEXTS[0] as never)).toBe('production (EKS us-east-1)');
    expect(clusterLabel(CONTEXTS[1] as never)).toBe('127.0.0.1:6443');
    expect(eksCommand('sales', 'us-east-1', 'devops', 'sales', '/home/me/k.yaml')).toBe(
      'aws eks update-kubeconfig --name sales --region us-east-1 --profile devops --alias sales --kubeconfig /home/me/k.yaml',
    );
  });
});

describe('KubeContexts', () => {
  it('lists contexts with facts, reading the shell’s files only', async () => {
    const view = await show();
    expect(Service.Contexts).toHaveBeenCalledWith(ENV);
    expect(Service.TestConnection).not.toHaveBeenCalled();
    view.getByText('production (EKS us-east-1) · ns shop · aws-iam-authenticator · profile devops');
    view.getByText('aws-iam-authenticator isn’t on this shell’s PATH');
    expect(view.getByText('in use').closest('.row')?.textContent).toContain('kind-local');
    expect(view.getByText('default').closest('.row')?.textContent).toContain('dayan@production');
  });

  it('switches only the focused shell', async () => {
    const view = await show();
    await fireEvent.dblClick(view.getByText('dayan@production'));
    await waitFor(() => expect(sent).toEqual([`export KUBECONFIG='/state/kube-shells/ab.yaml:/home/me/.kube/config'\r`]));
    expect(Service.ShellConfig).toHaveBeenCalledWith(ENV, 'dayan@production', '/home/me/.kube/config', '');
    expect(Service.ApplyEdit).not.toHaveBeenCalled(); // nothing global changed

    focusedPane(tabs.active!).running = true;
    await fireEvent.dblClick(view.getByText('kind-local'));
    await waitFor(() => expect(commands().at(-1)).toMatch(/^export KUBECONFIG=/));
    expect(sent.length).toBe(1);
  });

  it('lists the other kubeconfigs in ~/.kube, and switches to one of them', async () => {
    const view = await show();
    const titles = [...view.container.querySelectorAll('.section')].map((e) => e.textContent);
    expect(titles).toEqual(['Used by this shell · ~/.kube/config', '~/.kube/qe.yaml']);
    await fireEvent.dblClick(view.getByText('qe'));
    await waitFor(() => expect(Service.ShellConfig).toHaveBeenCalledWith(ENV, 'qe', '/home/me/.kube/qe.yaml', ''));
    // A context named like the shell's, but from a file it doesn't read, isn't "in use".
    focusedPane(tabs.active!).infra = { kubeContext: 'qe' } as never;
    await waitFor(() => expect(view.queryByText('in use')).toBeNull());
  });

  it('tests a connection only when asked', async () => {
    vi.mocked(Service.TestConnection).mockResolvedValue({ version: 'v1.29.3-eks', username: 'arn:aws:iam::1:user/dayan', groups: [] });
    const view = await show();
    await fireEvent.contextMenu(view.getByText('kind-local'));
    pick('Test Connection');
    await view.findByText('Connected · v1.29.3-eks · as arn:aws:iam::1:user/dayan');
    expect(Service.TestConnection).toHaveBeenCalledWith(ENV, 'kind-local', '/home/me/.kube/config');
  });

  it('deletes only after showing the plan', async () => {
    vi.mocked(Service.PlanEdit).mockResolvedValue(['kubectl config delete-context kind-local', 'kubectl config delete-cluster kind']);
    vi.mocked(Service.ApplyEdit).mockResolvedValue({ ...LIST, contexts: [CONTEXTS[0], CONTEXTS[2]] } as never);
    const view = await show();
    await fireEvent.contextMenu(view.getByText('kind-local'));
    pick('Delete…');
    await view.findByText('kubectl config delete-cluster kind');
    expect(Service.ApplyEdit).not.toHaveBeenCalled();
    await fireEvent.click(view.getByText('Delete Context'));
    expect(Service.ApplyEdit).toHaveBeenCalledWith(ENV, { kind: 'delete', name: 'kind-local', value: '', file: '/home/me/.kube/config' }, VERSIONS);
    await waitFor(() => expect(view.queryByText('kind-local')).toBeNull());
  });

  it('renames from a prompt that shows the command', async () => {
    vi.mocked(Service.ApplyEdit).mockResolvedValue(LIST as never);
    const view = await show();
    await fireEvent.contextMenu(view.getByText('kind-local'));
    pick('Rename…');
    const input = (await view.findByDisplayValue('kind-local')) as HTMLInputElement;
    await fireEvent.input(input, { target: { value: 'local' } });
    view.getByText('kubectl config rename-context kind-local local');
    await fireEvent.click(view.getByText('Rename'));
    await waitFor(() =>
      expect(Service.ApplyEdit).toHaveBeenCalledWith(ENV, { kind: 'rename', name: 'kind-local', value: 'local', file: '/home/me/.kube/config' }, VERSIONS),
    );
  });

  it('offers the EKS auth fix only for aws-iam-authenticator', async () => {
    vi.mocked(Service.PlanEdit).mockResolvedValue(['~/.kube/config: user dayan logs in with `aws --region us-east-1 eks get-token …`']);
    const view = await show();
    await fireEvent.contextMenu(view.getByText('kind-local'));
    expect(menuLabels()).not.toContain('Log In with aws eks get-token…');
    await fireEvent.contextMenu(view.getByText('dayan@production'));
    pick('Log In with aws eks get-token…');
    await view.findByText(/eks get-token/);
    expect(Service.PlanEdit).toHaveBeenCalledWith(ENV, { kind: 'fixAuth', name: 'dayan@production', value: '', file: '/home/me/.kube/config' });
    expect(Service.ApplyEdit).not.toHaveBeenCalled();
  });

  it('merges a chosen file after showing what it adds', async () => {
    vi.mocked(Service.PlanEdit).mockResolvedValue(['~/.kube/config: add context team', 'Skip cluster kind: already defined (the existing one stays)']);
    vi.mocked(Service.ApplyEdit).mockResolvedValue(LIST as never);
    const view = await show();
    await fireEvent.click(view.getByLabelText('Add a cluster'));
    pick('Merge a Kubeconfig File…');
    await view.findByText('~/.kube/config: add context team');
    await fireEvent.click(view.getByText('Merge'));
    expect(Service.ApplyEdit).toHaveBeenCalledWith(ENV, { kind: 'merge', name: '', value: '/home/me/Downloads/team.yaml', file: '' }, VERSIONS);
  });

  it('adds an EKS cluster: lists clusters only when asked, backs up, runs in a tab', async () => {
    vi.mocked(Service.EKSClusters).mockResolvedValue(['production', 'sales']);
    const view = await show();
    await fireEvent.click(view.getByLabelText('Add a cluster'));
    pick('Add EKS Cluster…');
    await view.findByText('Add EKS Cluster');
    expect(Service.EKSClusters).not.toHaveBeenCalled();
    const profile = (await view.findByText('devops')).closest('select')!;
    await fireEvent.change(profile, { target: { value: 'devops' } });
    await waitFor(() => expect((view.getByDisplayValue('us-west-2') as HTMLInputElement).value).toBe('us-west-2'));
    await fireEvent.click(view.getByText('List Clusters'));
    await view.findByText('2 in us-west-2: production, sales');
    expect(Service.EKSClusters).toHaveBeenCalledWith(ENV, 'devops', 'us-west-2');
    await fireEvent.input(view.getByPlaceholderText('cluster name'), { target: { value: 'sales' } });
    await fireEvent.click(view.getByText('Add in Terminal'));
    await waitFor(() =>
      expect(commands().at(-1)).toBe('aws eks update-kubeconfig --name sales --region us-west-2 --profile devops --kubeconfig /home/me/.kube/config'),
    );
    expect(Service.BackupConfigs).toHaveBeenCalledWith(ENV);
  });
});
