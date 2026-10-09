import { SvelteMap, SvelteSet } from 'svelte/reactivity';
import {
  Service, type Container, type Deployment as RawDeployment, type Event, type Pod as RawPod, type Request, type Target as RawTarget,
} from '../../bindings/github.com/dayanraj/wtf/internal/kube';
import { shellQuote } from './shell';
import { tabs, type Pane } from './tabs.svelte';

export type { Container, Event } from '../../bindings/github.com/dayanraj/wtf/internal/kube';

// The backend's types with Go's nil slices and maps made empty.
export type Pod = Omit<RawPod, 'containers' | 'labels'> & { containers: Container[]; labels: Record<string, string> };
export type Deployment = Omit<RawDeployment, 'images' | 'problems' | 'selector'> & {
  images: string[];
  problems: string[];
  selector: Record<string, string>;
};

const pod = (p: RawPod): Pod => ({ ...p, containers: p.containers ?? [], labels: (p.labels ?? {}) as Record<string, string> });
const deployment = (d: RawDeployment): Deployment => ({
  ...d, images: d.images ?? [], problems: d.problems ?? [], selector: (d.selector ?? {}) as Record<string, string>,
});

// The Kubernetes tab: deployments, pods and events of the focused shell's
// context (see internal/kube). Everything loads on demand: opening the tab
// only resolves the context from the kubeconfig (no cluster request); each
// section is fetched when it's expanded, and only expanded sections are
// polled. Everything that touches the cluster otherwise is a real kubectl
// command run in a terminal tab — and the ones that change something are
// confirmed first.

/** A poll may run kubectl (and an auth plugin) per open section: not more often than this. */
export const MIN_POLL_MS = 10_000;

/** Variables a new tab's kubectl needs to reach the same cluster as the focused shell. */
const CARRIED = ['KUBECONFIG', 'AWS_PROFILE', 'AWS_REGION', 'AWS_DEFAULT_REGION'];

export interface Target {
  context: string;
  namespace: string;
  /** The focused shell's variables. */
  env: Record<string, string>;
}

/**
 * The start of every command: the shell's KUBECONFIG and AWS profile (a
 * new tab's shell may not have them), and the context and namespace named
 * outright, so the command does exactly what the view showed.
 */
export function kubectl(t: Target, ...args: string[]): string {
  const vars = CARRIED.filter((v) => t.env[v]).map((v) => `${v}=${shellQuote(t.env[v])}`);
  return [...vars, 'kubectl', '--context', shellQuote(t.context), '-n', shellQuote(t.namespace), ...args].join(' ');
}

const q = shellQuote;

export const commands = {
  logs: (t: Target, pod: string, container?: string, previous = false) =>
    kubectl(t, 'logs', previous ? '--previous' : '-f --tail=200', q(pod), ...(container ? ['-c', q(container)] : [])),
  exec: (t: Target, pod: string, container?: string) =>
    kubectl(t, 'exec -it', q(pod), ...(container ? ['-c', q(container)] : []), '--', 'sh -c', q('command -v bash >/dev/null && exec bash || exec sh')),
  /** `kind` is kubectl's short name: pod, deployment, statefulset, secret, node… */
  describe: (t: Target, kind: string, name: string) => kubectl(t, 'describe', kind, q(name)),
  yaml: (t: Target, kind: string, name: string) => kubectl(t, 'get', kind, q(name), '-o yaml'),
  workloadLogs: (t: Target, kind: string, name: string) => kubectl(t, 'logs -f --tail=200 --all-containers --prefix', q(`${kind}/${name}`)),
  rolloutStatus: (t: Target, kind: string, name: string) => kubectl(t, 'rollout status', q(`${kind}/${name}`)),
  rolloutHistory: (t: Target, kind: string, name: string) => kubectl(t, 'rollout history', q(`${kind}/${name}`)),
  restart: (t: Target, kind: string, name: string) => kubectl(t, 'rollout restart', q(`${kind}/${name}`)),
  scale: (t: Target, kind: string, name: string, replicas: number) => kubectl(t, 'scale', q(`${kind}/${name}`), `--replicas=${replicas}`),
  /** A one-off run of a CronJob, named so it's clearly manual. */
  triggerCronJob: (t: Target, name: string, now = Date.now()) =>
    kubectl(t, 'create job', `--from=${q(`cronjob/${name}`)}`, q(`${name.slice(0, 40)}-manual-${now.toString(36)}`)),
  deletePod: (t: Target, name: string) => kubectl(t, 'delete pod', q(name)),
  portForward: (t: Target, resource: string, local: number, remote: number) => kubectl(t, 'port-forward', q(resource), `${local}:${remote}`),
};

/** "45s", "12m", "3h", "5d" — like kubectl's AGE column. */
export function age(iso: string, now = Date.now()): string {
  const t = Date.parse(iso);
  if (!iso || Number.isNaN(t)) return '';
  const s = Math.max(0, Math.floor((now - t) / 1000));
  if (s < 120) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 120) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 48) return `${h}h`;
  return `${Math.floor(h / 24)}d`;
}

/** A pod belongs to a deployment when it carries every label of its selector. */
export function selects(d: Deployment, p: Pod): boolean {
  const sel = Object.entries(d.selector ?? {});
  return sel.length > 0 && sel.every(([k, v]) => p.labels?.[k] === v);
}

/** How a pod is doing, from its STATUS: for the dot's color only. */
export function podTone(p: Pod): 'ok' | 'wait' | 'bad' | 'done' {
  const s = p.status;
  if (s === 'Running') return p.ready === p.total ? 'ok' : 'wait';
  if (s === 'Completed' || s === 'Succeeded') return 'done';
  if (/Err|BackOff|Error|OOMKilled|Failed|Evicted|ExitCode|Unknown|NotReady/.test(s)) return 'bad';
  return 'wait'; // Pending, ContainerCreating, Init:0/1, Terminating…
}

export type TableKind =
  | 'statefulsets' | 'daemonsets' | 'jobs' | 'cronjobs' | 'services' | 'ingresses'
  | 'configmaps' | 'secrets' | 'pvcs' | 'hpas' | 'nodes';
export type KubeSection = 'deployments' | 'pods' | 'events' | TableKind;

/** A resource as `kubectl get` prints it: its name and its other columns. */
export interface Row {
  name: string;
  cells: Record<string, string>;
}

export interface TableKindInfo {
  id: TableKind;
  label: string;
  /** kubectl's short name, for commands: statefulset, secret, node… */
  kind: string;
  /** Columns shown under the name (AGE always comes last); the rest go in the tooltip. */
  summary: string[];
  /** Said in the section's tooltip. */
  note?: string;
}

export const TABLE_KINDS: Record<TableKind, TableKindInfo> = {
  statefulsets: { id: 'statefulsets', label: 'StatefulSets', kind: 'statefulset', summary: ['READY'] },
  daemonsets: { id: 'daemonsets', label: 'DaemonSets', kind: 'daemonset', summary: ['DESIRED', 'READY', 'NODE SELECTOR'] },
  jobs: { id: 'jobs', label: 'Jobs', kind: 'job', summary: ['STATUS', 'COMPLETIONS', 'DURATION'] },
  cronjobs: { id: 'cronjobs', label: 'CronJobs', kind: 'cronjob', summary: ['SCHEDULE', 'SUSPEND', 'LAST SCHEDULE'] },
  services: { id: 'services', label: 'Services', kind: 'service', summary: ['TYPE', 'CLUSTER-IP', 'EXTERNAL-IP', 'PORT(S)'] },
  ingresses: { id: 'ingresses', label: 'Ingresses', kind: 'ingress', summary: ['HOSTS', 'ADDRESS'] },
  configmaps: { id: 'configmaps', label: 'ConfigMaps', kind: 'configmap', summary: ['DATA'] },
  secrets: {
    id: 'secrets', label: 'Secrets', kind: 'secret', summary: ['TYPE', 'DATA'],
    note: 'Names, types and key counts only: secret values are never read into wtf',
  },
  pvcs: { id: 'pvcs', label: 'PersistentVolumeClaims', kind: 'pvc', summary: ['STATUS', 'CAPACITY', 'STORAGECLASS'] },
  hpas: { id: 'hpas', label: 'HorizontalPodAutoscalers', kind: 'hpa', summary: ['REFERENCE', 'TARGETS', 'REPLICAS'] },
  nodes: { id: 'nodes', label: 'Nodes', kind: 'node', summary: ['STATUS', 'ROLES', 'VERSION'], note: 'Cluster-wide, not in a namespace' },
};

/** The tab's sections, in groups. */
export const LAYOUT: { group: string; sections: KubeSection[] }[] = [
  { group: 'Workloads', sections: ['deployments', 'statefulsets', 'daemonsets', 'jobs', 'cronjobs', 'pods'] },
  { group: 'Network', sections: ['services', 'ingresses'] },
  { group: 'Config', sections: ['configmaps', 'secrets'] },
  { group: 'Storage', sections: ['pvcs'] },
  { group: 'Scaling', sections: ['hpas'] },
  { group: 'Cluster', sections: ['nodes', 'events'] },
];

export const SECTION_LABEL: Record<KubeSection, string> = {
  deployments: 'Deployments',
  pods: 'Pods',
  events: 'Events',
  ...Object.fromEntries(Object.values(TABLE_KINDS).map((k) => [k.id, k.label])),
} as Record<KubeSection, string>;

export const isTableKind = (s: KubeSection): s is TableKind => s in TABLE_KINDS;

function toRows(columns: string[] | null, rows: (string[] | null)[] | null): Row[] {
  const cols = columns ?? [];
  return (rows ?? []).map((r) => ({
    name: r?.[0] ?? '',
    cells: Object.fromEntries(cols.map((c, i) => [c, r?.[i] ?? ''])),
  }));
}

/** "ClusterIP · 10.0.0.1 · 80/TCP · 5d": the kind's summary columns that say something, then the age. */
export function rowSummary(kind: TableKind, r: Row): string {
  const parts = TABLE_KINDS[kind].summary.map((c) => r.cells[c]).filter((v) => v && v !== '<none>');
  if (r.cells.AGE) parts.push(r.cells.AGE);
  return parts.join(' · ');
}

/** "2/3" → [2, 3] */
const fraction = (v = '') => {
  const m = /^(\d+)\/(\d+)$/.exec(v);
  return m ? [+m[1], +m[2]] : null;
};

/** A dot color from the row's own status columns; null for kinds without one. */
export function rowTone(kind: TableKind, r: Row): 'ok' | 'wait' | 'bad' | null {
  const c = r.cells;
  const ratio = (f: number[] | null) => (!f ? null : f[0] === f[1] ? 'ok' : f[0] === 0 && f[1] > 0 ? 'bad' : 'wait');
  switch (kind) {
    case 'statefulsets':
      return ratio(fraction(c.READY));
    case 'daemonsets':
      return c.DESIRED && c.READY ? ratio([+c.READY, +c.DESIRED]) : null;
    case 'jobs':
      if (c.STATUS) return c.STATUS === 'Complete' ? 'ok' : c.STATUS === 'Failed' ? 'bad' : 'wait';
      return ratio(fraction(c.COMPLETIONS));
    case 'pvcs':
      return c.STATUS === 'Bound' ? 'ok' : c.STATUS === 'Lost' ? 'bad' : 'wait';
    case 'nodes':
      return c.STATUS === 'Ready' ? 'ok' : c.STATUS?.startsWith('NotReady') || c.STATUS === 'Unknown' ? 'bad' : 'wait';
  }
  return null;
}

/** The first port of a Service's PORT(S) ("80/TCP,443/TCP", "80:30080/TCP"). */
export function firstPort(ports = ''): string {
  return /^(\d+)/.exec(ports)?.[1] ?? '';
}

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

/** One section's data, fetched on its own. */
class Section<T> {
  items = $state.raw<T[] | null>(null);
  error = $state('');
  loading = $state(false);
  updatedAt = $state<number | null>(null);
  /** Bumped to drop answers that arrive for an old target or selection. */
  seq = 0;

  clear() {
    this.seq++;
    this.items = null;
    this.error = '';
    this.loading = false;
    this.updatedAt = null;
  }

  async load(fetch: () => Promise<T[]>) {
    if (this.loading) return; // a slow cluster mustn't pile up requests
    const seq = ++this.seq;
    this.loading = true;
    try {
      const items = await fetch();
      if (seq !== this.seq) return;
      this.items = items;
      this.error = '';
      this.updatedAt = Date.now();
    } catch (e) {
      if (seq !== this.seq) return;
      this.error = message(e);
    } finally {
      if (seq === this.seq) this.loading = false;
    }
  }
}

class Kube {
  /** Where the tab looks; resolved from the kubeconfig without asking the cluster. */
  target = $state.raw<RawTarget | null>(null);
  targetError = $state('');
  #targetSeq = 0;

  deployments = new Section<Deployment>();
  pods = new Section<Pod>();
  events = new Section<Event>();
  tables = Object.fromEntries(Object.keys(TABLE_KINDS).map((k) => [k, new Section<Row>()])) as Record<TableKind, Section<Row>>;
  /** Expanded sections. Everything starts collapsed: nothing is fetched until asked for. */
  open = new SvelteSet<KubeSection>();

  /** The namespace picked in the view, per context; absent follows the shell. */
  namespaces = new SvelteMap<string, string>();
  /** Namespaces of the current context, loaded when the picker is used; null if not (yet) known. */
  namespaceList = $state.raw<string[] | null>(null);
  #namespacesFor = '';
  /** Pods shown only for this deployment (fetched with its selector). */
  deployment = $state<Deployment | null>(null);

  /** What the focused shell is pointed at. */
  shell(pane: Pane | undefined = tabs.activePane): { context: string; namespace: string; env: Record<string, string> } {
    return {
      context: pane?.infra?.kubeContext ?? '',
      namespace: pane?.infra?.kubeNamespace ?? '',
      env: pane?.env ?? {},
    };
  }

  /** The target of commands: what the view shows. */
  get commandTarget(): Target | null {
    return this.target ? { context: this.target.context, namespace: this.target.namespace, env: this.shell().env } : null;
  }

  #request(): Request {
    const t = this.target;
    return { env: this.shell().env, context: t?.context ?? '', namespace: t?.namespace ?? '' };
  }

  /** Works out the context and namespace (kubeconfig only), then reloads what's open. */
  async resolve() {
    const s = this.shell();
    const seq = ++this.#targetSeq;
    try {
      const t = await Service.Target({ env: s.env, context: s.context, namespace: this.namespaces.get(s.context) ?? '' });
      if (seq !== this.#targetSeq) return;
      const changed = t.context !== this.target?.context || t.namespace !== this.target?.namespace;
      this.target = t;
      this.targetError = '';
      if (changed) {
        this.#clearData();
        if (this.#namespacesFor !== t.context) this.namespaceList = null;
      }
      this.refreshOpen();
    } catch (e) {
      if (seq !== this.#targetSeq) return;
      this.target = null;
      this.targetError = message(e);
      this.#clearData();
    }
  }

  /** A section's data. */
  section(s: KubeSection): Section<unknown> {
    return isTableKind(s) ? this.tables[s] : (this[s] as Section<unknown>);
  }

  #clearData() {
    this.deployments.clear();
    this.pods.clear();
    this.events.clear();
    for (const t of Object.values(this.tables)) t.clear();
    this.deployment = null;
  }

  /** Fetches one section, if there's a target. */
  refresh(section: KubeSection) {
    if (!this.target) return;
    const req = this.#request();
    if (isTableKind(section)) {
      this.tables[section].load(async () => {
        const t = await Service.Table(req, section);
        return toRows(t.columns, t.rows);
      });
    } else if (section === 'deployments') this.deployments.load(async () => ((await Service.Deployments(req)) ?? []).map(deployment));
    else if (section === 'pods') {
      const selector = this.deployment?.selector ?? null;
      this.pods.load(async () => ((await Service.Pods(req, selector)) ?? []).map(pod));
    } else this.events.load(async () => (await Service.Events(req)) ?? []);
  }

  /** Fetches the expanded sections: what the poller calls. */
  refreshOpen() {
    for (const s of this.open) this.refresh(s);
  }

  /** Expands (and fetches) or collapses a section. */
  toggle(section: KubeSection) {
    if (this.open.has(section)) {
      this.open.delete(section);
      this.section(section).seq++; // an answer on its way is no longer wanted
      this.section(section).loading = false;
    } else {
      this.open.add(section);
      this.refresh(section);
    }
  }

  /** Shows only a deployment's pods (or all again): pods are fetched with its selector. */
  selectDeployment(d: Deployment | null) {
    this.deployment = d && this.deployment?.name !== d.name ? d : null;
    this.pods.clear();
    if (!this.open.has('pods')) this.open.add('pods');
    this.refresh('pods');
  }

  /** Shows another namespace in the view (the shell keeps its own). */
  pickNamespace(ns: string) {
    const s = this.shell();
    const ctx = this.target?.context ?? s.context;
    if (ns === s.namespace && ctx === s.context) this.namespaces.delete(ctx);
    else this.namespaces.set(ctx, ns);
    this.resolve();
  }

  /** Lists the context's namespaces, once, when the picker is first used. */
  async loadNamespaces() {
    const ctx = this.target?.context;
    if (!ctx || this.#namespacesFor === ctx) return;
    this.#namespacesFor = ctx;
    try {
      this.namespaceList = (await Service.Namespaces({ ...this.#request(), context: ctx })) ?? [];
    } catch {
      this.namespaceList = null; // not allowed to list them: the picker takes a typed name
    }
  }
}

export const kube = new Kube();
