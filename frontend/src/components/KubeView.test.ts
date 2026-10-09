// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';

vi.mock('../lib/backend', () => ({ prettyPath: (p: string) => p, homeDir: () => '/home/me', Service: {}, endpoint: vi.fn() }));
vi.mock('../lib/window', () => ({ thisWindow: { name: 'main' } }));
vi.mock('@wailsio/runtime', async (original) => ({
  ...(await original<object>()),
  Clipboard: { SetText: vi.fn(async () => {}) },
  Browser: { OpenURL: vi.fn() },
}));
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/kube', () => ({
  Service: { Target: vi.fn(), Deployments: vi.fn(), Pods: vi.fn(), Events: vi.fn(), Namespaces: vi.fn(), Table: vi.fn(), SecretValues: vi.fn() },
}));
vi.mock('../../bindings/github.com/dayanraj/wtf/internal/config', () => ({ Service: { SetSidebarAutoRefresh: vi.fn() } }));
window.matchMedia ??= ((q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} })) as never;
Element.prototype.animate ??= function () {
  return { finished: Promise.resolve(), cancel() {}, onfinish: null } as unknown as Animation;
};

const { Service } = await import('../../bindings/github.com/dayanraj/wtf/internal/kube');
const { tabs, focusedPane } = await import('../lib/tabs.svelte');
const { overlay } = await import('../lib/overlay.svelte');
const k = await import('../lib/kube.svelte');
const { settings } = await import('../lib/settings.svelte');
const { testConfig } = await import('../test/config');
const KubeView = (await import('./KubeView.svelte')).default;
const Overlays = (await import('./Overlays.svelte')).default;

const pod = (p: Record<string, unknown>) => ({
  name: '', status: 'Running', phase: 'Running', ready: 1, total: 1, restarts: 0, node: 'n1', ip: '10.0.0.1',
  created: new Date(Date.now() - 3 * 3600_000).toISOString(), lastRestart: '', owner: '', labels: {},
  containers: [{ name: 'app', image: 'web:1', ready: true, restarts: 0, reason: '', lastReason: '' }], ...p,
});
const SNAP = {
  context: 'prod', cluster: 'arn:aws:eks:us-east-1:123456789012:cluster/sales', namespace: 'shop', eventsError: '',
  deployments: [
    { name: 'web', desired: 3, ready: 2, upToDate: 3, available: 2, images: ['web:1'], selector: { app: 'web' }, created: '2026-10-01T00:00:00Z', problems: ['Available: Deployment does not have minimum availability.'] },
    { name: 'worker', desired: 1, ready: 1, upToDate: 1, available: 1, images: ['w:1'], selector: { app: 'worker' }, created: '2026-10-01T00:00:00Z', problems: [] },
  ],
  pods: [
    pod({ name: 'web-1', labels: { app: 'web' }, status: 'CrashLoopBackOff', ready: 1, total: 2, restarts: 7,
      containers: [
        { name: 'app', image: 'web:1', ready: false, restarts: 7, reason: 'CrashLoopBackOff', lastReason: 'OOMKilled' },
        { name: 'proxy', image: 'envoy', ready: true, restarts: 0, reason: '', lastReason: '' },
      ] }),
    pod({ name: 'web-2', labels: { app: 'web' } }),
    pod({ name: 'worker-1', labels: { app: 'worker' } }),
  ],
  events: [{ type: 'Warning', reason: 'BackOff', object: 'Pod/web-1', message: 'Back-off restarting failed container', count: 42, last: new Date().toISOString() }],
};
const TABLES: Record<string, { columns: string[]; rows: string[][] }> = {
  secrets: { columns: ['NAME', 'TYPE', 'DATA', 'AGE'], rows: [['db-password', 'Opaque', '2', '40d']] },
  cronjobs: {
    columns: ['NAME', 'SCHEDULE', 'TIMEZONE', 'SUSPEND', 'ACTIVE', 'LAST SCHEDULE', 'AGE'],
    rows: [['backup', '*/5 * * * *', '<none>', 'False', '0', '2m', '10d']],
  },
  services: {
    columns: ['NAME', 'TYPE', 'CLUSTER-IP', 'EXTERNAL-IP', 'PORT(S)', 'AGE'],
    rows: [['web', 'ClusterIP', '10.100.0.7', '<none>', '8080/TCP,9090/TCP', '30d']],
  },
  ingresses: { columns: ['NAME', 'CLASS', 'HOSTS', 'ADDRESS', 'PORTS', 'AGE'], rows: [['web', 'nginx', 'shop.example.com,*', '1.2.3.4', '80', '1d']] },
  nodes: { columns: ['NAME', 'STATUS', 'ROLES', 'AGE', 'VERSION'], rows: [['ip-10-0-1-5', 'NotReady', '<none>', '90d', 'v1.29.3']] },
  statefulsets: { columns: ['NAME', 'READY', 'AGE'], rows: [['db', '2/3', '50d']] },
};
const ENV = { PWD: '/work', PATH: '/opt/bin:/usr/bin', KUBECONFIG: '/work/kube config', AWS_PROFILE: 'devops' };
const T = { context: 'prod', namespace: 'shop', env: ENV };

type MenuItem = { label?: string; heading?: string; action?: () => void } | string;
const menuItems = () => ((overlay.current as { entries: MenuItem[] } | null)?.entries ?? []);
const menuLabels = () => menuItems().map((e) => (typeof e === 'string' ? e : (e.label ?? e.heading)));
const pick = (label: string) => (menuItems().find((e) => typeof e !== 'string' && e.label === label) as { action: () => void }).action();
const commands = () => tabs.list.map((t) => focusedPane(t).initialCommand);

beforeEach(() => {
  settings.config = testConfig();
  tabs.list = [];
  tabs.groups = [];
  tabs.activeId = null;
  tabs.open({ cwd: '/work' });
  const pane = focusedPane(tabs.active!);
  pane.infra = { kubeContext: 'prod', kubeNamespace: 'shop' } as never;
  pane.env = ENV;
  overlay.current = null;
  k.kube.target = null;
  k.kube.targetError = '';
  k.kube.namespaces.clear();
  k.kube.namespaceList = null;
  k.kube.open.clear();
  k.kube.deployment = null;
  for (const sec of [k.kube.deployments, k.kube.pods, k.kube.events]) sec.clear();
  vi.clearAllMocks();
  vi.mocked(Service.Target).mockImplementation((async (r: { namespace: string }) => ({ context: 'prod', cluster: SNAP.cluster, namespace: r.namespace || 'shop' })) as never);
  vi.mocked(Service.Deployments).mockResolvedValue(structuredClone(SNAP.deployments) as never);
  vi.mocked(Service.Pods).mockImplementation((async (_r: unknown, sel: Record<string, string> | null) =>
    structuredClone(SNAP.pods).filter((p) => !sel || Object.entries(sel).every(([key, v]) => (p.labels as Record<string, string>)[key] === v))) as never);
  vi.mocked(Service.Events).mockResolvedValue(structuredClone(SNAP.events) as never);
  vi.mocked(Service.Namespaces).mockResolvedValue(['default', 'shop', 'staging']);
  vi.mocked(Service.Table).mockImplementation((async (_r: unknown, kind: string) => structuredClone(TABLES[kind] ?? { columns: [], rows: [] })) as never);
  for (const t of Object.values(k.kube.tables)) t.clear();
});
afterEach(cleanup);

/** Renders the tab and opens the given sections, as a user would. */
async function show(sections = ['Deployments', 'Pods', 'Events']) {
  const view = render(KubeView);
  render(Overlays);
  await view.findByText('sales');
  for (const s of sections) await fireEvent.click(view.getByTitle(`Show ${s}`));
  if (sections.includes('Pods')) await view.findByText('web-2');
  return view;
}

describe('commands', () => {
  it('carry the shell’s kubeconfig and profile, and name context and namespace', () => {
    expect(k.commands.logs(T, 'web-1', 'app')).toBe(`KUBECONFIG='/work/kube config' AWS_PROFILE=devops kubectl --context prod -n shop logs -f --tail=200 web-1 -c app`);
    expect(k.commands.logs({ ...T, env: {} }, 'web-1', undefined, true)).toBe('kubectl --context prod -n shop logs --previous web-1');
    expect(k.commands.exec({ ...T, env: {} }, 'web-1')).toBe(`kubectl --context prod -n shop exec -it web-1 -- sh -c 'command -v bash >/dev/null && exec bash || exec sh'`);
    expect(k.commands.scale({ ...T, env: {} }, 'deployment', 'web', 5)).toBe('kubectl --context prod -n shop scale deployment/web --replicas=5');
    expect(k.commands.restart({ ...T, context: 'arn:aws:eks:x:1:cluster/a b', env: {} }, 'statefulset', 'db')).toBe(`kubectl --context 'arn:aws:eks:x:1:cluster/a b' -n shop rollout restart statefulset/db`);
    expect(k.commands.triggerCronJob({ ...T, env: {} }, 'backup', 1_700_000_000_000)).toBe('kubectl --context prod -n shop create job --from=cronjob/backup backup-manual-loyw3v28');
    expect(k.commands.yaml({ ...T, env: {} }, 'configmap', 'app')).toBe('kubectl --context prod -n shop get configmap app -o yaml');
  });

  it('formats ages and matches pods to deployments', () => {
    const now = Date.parse('2026-10-09T12:00:00Z');
    expect(k.age('2026-10-09T11:59:30Z', now)).toBe('30s');
    expect(k.age('2026-10-09T11:00:00Z', now)).toBe('60m');
    expect(k.age('2026-10-09T02:00:00Z', now)).toBe('10h');
    expect(k.age('2026-10-01T12:00:00Z', now)).toBe('8d');
    const d = SNAP.deployments[0] as never;
    expect(k.selects(d, SNAP.pods[1] as never)).toBe(true);
    expect(k.selects(d, SNAP.pods[2] as never)).toBe(false);
    expect(k.podTone(SNAP.pods[0] as never)).toBe('bad');
    expect(k.podTone(SNAP.pods[1] as never)).toBe('ok');
    expect(k.podTone(pod({ status: 'Init:0/1' }) as never)).toBe('wait');
  });
});

describe('KubeView', () => {
  it('reads nothing from the cluster until a section is opened', async () => {
    const view = render(KubeView);
    await view.findByText('sales');
    expect(Service.Target).toHaveBeenCalledWith({ env: ENV, context: 'prod', namespace: '' });
    expect(Service.Deployments).not.toHaveBeenCalled();
    expect(Service.Pods).not.toHaveBeenCalled();
    expect(Service.Events).not.toHaveBeenCalled();
    expect(Service.Namespaces).not.toHaveBeenCalled();
    view.getByText(/Open a section to load it/);

    await fireEvent.click(view.getByTitle('Show Deployments'));
    await view.findByText('worker');
    expect(Service.Deployments).toHaveBeenCalledWith({ env: ENV, context: 'prod', namespace: 'shop' });
    expect(Service.Pods).not.toHaveBeenCalled();
    expect(Service.Events).not.toHaveBeenCalled();
  });

  it('refreshes only open sections, and stops when one is closed', async () => {
    const view = await show(['Deployments', 'Events']);
    await view.findByText('BackOff', { exact: false });
    vi.clearAllMocks();
    k.kube.refreshOpen();
    await waitFor(() => expect(Service.Events).toHaveBeenCalledTimes(1));
    expect(Service.Deployments).toHaveBeenCalledTimes(1);
    expect(Service.Pods).not.toHaveBeenCalled();

    await fireEvent.click(view.getByTitle('Collapse Events'));
    vi.clearAllMocks();
    k.kube.refreshOpen();
    await waitFor(() => expect(Service.Deployments).toHaveBeenCalledTimes(1));
    expect(Service.Events).not.toHaveBeenCalled();
  });

  it('shows what the cluster says', async () => {
    const view = await show();
    view.getByText(/^2\/3 ready · \d+d$/);
    view.getByText('Available: Deployment does not have minimum availability.');
    view.getByText(/CrashLoopBackOff · 1\/2 ready · 7 restarts \(OOMKilled\)/);
    expect(view.container.querySelector('.event.warn')?.textContent).toMatch(/BackOff Pod\/web-1 ×42 · 0s\s*Back-off restarting failed container/);
  });

  it('fetches only a deployment’s pods when it’s clicked, opening Pods', async () => {
    const view = await show(['Deployments']);
    await view.findByText('worker');
    await fireEvent.click(view.getByText('worker'));
    await view.findByText('worker-1');
    expect(Service.Pods).toHaveBeenLastCalledWith({ env: ENV, context: 'prod', namespace: 'shop' }, { app: 'worker' });
    expect(view.queryByText('web-1')).toBeNull();
    await fireEvent.click(view.getByText('worker ×'));
    await view.findByText('web-1');
    expect(Service.Pods).toHaveBeenLastCalledWith(expect.anything(), null);
  });

  it('opens logs on double-click, and a shell per container from the menu', async () => {
    const view = await show(['Pods']);
    await fireEvent.dblClick(view.getByText('web-2'));
    expect(commands().at(-1)).toBe(`KUBECONFIG='/work/kube config' AWS_PROFILE=devops kubectl --context prod -n shop logs -f --tail=200 web-2`);
    await fireEvent.contextMenu(view.getByText('web-1'));
    expect(menuLabels()).toContain('Logs Before Last Restart: app');
    expect(menuLabels().filter((l) => l === 'proxy').length).toBe(2); // logs + shell
  });

  it('deletes a pod only after showing the command', async () => {
    const view = await show(['Pods']);
    const before = tabs.list.length;
    await fireEvent.contextMenu(view.getByText('web-1'));
    pick('Delete Pod…');
    await view.findByText(/Context prod \(cluster sales\), namespace shop/);
    view.getByText(`KUBECONFIG='/work/kube config' AWS_PROFILE=devops kubectl --context prod -n shop delete pod web-1`);
    expect(tabs.list.length).toBe(before);
    await fireEvent.click(view.getByText('Delete Pod'));
    expect(commands().at(-1)).toMatch(/delete pod web-1$/);
  });

  it('scales from a prompt that previews the command', async () => {
    const view = await show(['Deployments']);
    await fireEvent.contextMenu(await view.findByText('web'));
    pick('Scale…');
    const input = (await view.findByDisplayValue('3')) as HTMLInputElement;
    view.getByText('It already has 3');
    await fireEvent.input(input, { target: { value: 'x' } });
    view.getByText(/Enter a number/);
    await fireEvent.input(input, { target: { value: '5' } });
    view.getByText(/scale deployment\/web --replicas=5$/);
    await fireEvent.click(view.getByText('Scale'));
    expect(commands().at(-1)).toMatch(/kubectl --context prod -n shop scale deployment\/web --replicas=5$/);
  });

  it('lists namespaces only when the picker is used, and views another without touching the shell', async () => {
    const view = await show(['Deployments']);
    const input = view.getByLabelText('Namespace') as HTMLInputElement;
    expect(Service.Namespaces).not.toHaveBeenCalled();
    await fireEvent.focus(input);
    await waitFor(() => expect(view.container.querySelectorAll('#kube-namespaces option').length).toBe(3));
    await fireEvent.change(input, { target: { value: 'staging' } });
    await waitFor(() => expect(Service.Deployments).toHaveBeenLastCalledWith({ env: ENV, context: 'prod', namespace: 'staging' }));
    expect(Service.Target).toHaveBeenLastCalledWith({ env: ENV, context: 'prod', namespace: 'staging' });
  });

  it('keeps sections open across remounts', async () => {
    await show(['Pods']);
    cleanup();
    const again = render(KubeView);
    await again.findByText('web-2');
    again.getByTitle('Collapse Pods');
    again.getByTitle('Show Deployments');
  });

  it('says what went wrong, per section', async () => {
    vi.mocked(Service.Pods).mockRejectedValue(new Error('getting credentials: exec: executable aws-iam-authenticator not found'));
    const view = await show(['Deployments']);
    await fireEvent.click(view.getByTitle('Show Pods'));
    await view.findByText(/aws-iam-authenticator not found/);
    view.getByText('worker'); // deployments still show
  });

  it('lists other kinds only when their section is opened', async () => {
    const view = await show([]);
    expect(Service.Table).not.toHaveBeenCalled();
    for (const g of ['Workloads', 'Network', 'Config', 'Storage', 'Scaling', 'Cluster']) view.getByText(g);
    await fireEvent.click(view.getByTitle(/^Show Secrets/));
    await view.findByText('db-password');
    expect(Service.Table).toHaveBeenCalledTimes(1);
    expect(Service.Table).toHaveBeenCalledWith({ env: ENV, context: 'prod', namespace: 'shop' }, 'secrets');
    view.getByText('Opaque · 2 · 40d');
  });

  it('reads a secret’s values only when asked, masked until revealed', async () => {
    vi.mocked(Service.SecretValues).mockResolvedValue([
      { key: 'password', value: 'hunter2', binary: false, size: 7 },
      { key: 'keystore', value: 'AAECA/8=', binary: true, size: 5 },
    ]);
    const { Clipboard } = await import('@wailsio/runtime');
    const view = await show([]);
    await fireEvent.click(view.getByTitle(/^Show Secrets/));
    expect(view.getByTitle(/^Collapse Secrets/).title).toMatch(/values are read only when you open that secret/);
    await fireEvent.contextMenu(await view.findByText('db-password'));
    expect(Service.SecretValues).not.toHaveBeenCalled();
    expect(menuLabels()).not.toContain('View YAML');
    pick('View Decoded Values…');

    await view.findByText('password');
    expect(Service.SecretValues).toHaveBeenCalledWith({ env: ENV, context: 'prod', namespace: 'shop' }, 'db-password');
    expect(view.queryByText('hunter2')).toBeNull();
    view.getByText('binary · 5 B');

    await fireEvent.click(view.getByLabelText('Reveal password'));
    view.getByText('hunter2');
    await fireEvent.click(view.getByLabelText('Copy keystore'));
    expect(Clipboard.SetText).toHaveBeenCalledWith('AAECA/8=');

    // Losing focus (switching to a screen share) masks everything again.
    window.dispatchEvent(new Event('blur'));
    await waitFor(() => expect(view.queryByText('hunter2')).toBeNull());
    await fireEvent.click(view.getByText('Reveal All'));
    view.getByText('hunter2');
    view.getByText('AAECA/8=');
  });

  it('decodes a secret in a terminal with kubectl’s own template', async () => {
    const view = await show([]);
    await fireEvent.click(view.getByTitle(/^Show Secrets/));
    await fireEvent.contextMenu(await view.findByText('db-password'));
    pick('Decode in Terminal');
    expect(commands().at(-1)).toBe(
      `KUBECONFIG='/work/kube config' AWS_PROFILE=devops kubectl --context prod -n shop get secret db-password -o 'go-template={{range $k, $v := .data}}{{$k}}={{$v | base64decode}}{{"\\n"}}{{end}}'`,
    );
    expect(Service.SecretValues).not.toHaveBeenCalled();
  });

  it('runs a CronJob only after confirming', async () => {
    const view = await show([]);
    await fireEvent.click(view.getByTitle('Show CronJobs'));
    await view.findByText('*/5 * * * * · False · 2m · 10d');
    await fireEvent.contextMenu(view.getByText('backup'));
    pick('Run Now…');
    const before = tabs.list.length;
    await view.findByText(/create job --from=cronjob\/backup backup-manual-/);
    expect(tabs.list.length).toBe(before);
    await fireEvent.click(view.getByText('Create Job'));
    expect(commands().at(-1)).toMatch(/create job --from=cronjob\/backup backup-manual-\w+$/);
  });

  it('offers kind-specific actions', async () => {
    const view = await show([]);
    await fireEvent.click(view.getByTitle('Show Services'));
    await fireEvent.contextMenu(await view.findByText('ClusterIP · 10.100.0.7 · 8080/TCP,9090/TCP · 30d'));
    pick('Port Forward…');
    // The port-forward editor, filled in for this service: its first port, both sides.
    await view.findByText('New Port Forward');
    expect(view.getAllByDisplayValue('8080')).toHaveLength(2);
    view.getByDisplayValue('service/web');
    view.getByText(/kubectl --context prod --namespace shop port-forward service\/web 8080/);

    overlay.current = null;
    await fireEvent.click(view.getByTitle('Show Ingresses'));
    await fireEvent.contextMenu((await view.findAllByText('web')).at(-1)!);
    expect(menuLabels()).toContain('Open https://shop.example.com');
    expect(menuLabels().some((l) => l?.includes('https://*'))).toBe(false);

    await fireEvent.click(view.getByTitle(/^Show Nodes/));
    const node = await view.findByText('ip-10-0-1-5');
    expect(node.closest('.row')!.querySelector('.dot')!.classList.contains('bad')).toBe(true);

    await fireEvent.click(view.getByTitle('Show StatefulSets'));
    await fireEvent.contextMenu(await view.findByText('db'));
    expect(menuLabels()).toEqual(expect.arrayContaining(['Logs (all pods)', 'Rollout Status', 'Restart…', 'Scale…', 'View YAML']));
    pick('Scale…');
    await view.findByDisplayValue('3');
  });

  it('says when there’s no context', async () => {
    vi.mocked(Service.Target).mockRejectedValue(new Error('no Kubernetes context is set (kubectl config current-context)'));
    const view = render(KubeView);
    await view.findByText('Can’t find a Kubernetes context');
  });
});
