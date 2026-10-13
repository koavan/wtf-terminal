import { Service as InfraService, type Context, type Expiry, type Git } from '../../bindings/github.com/dayanraj/wtf/internal/infra';
import { relativeTime } from './logs';

export type { Context as InfraContext } from '../../bindings/github.com/dayanraj/wtf/internal/infra';

// What each shell is pointed at — Kubernetes, AWS, gcloud, Terraform, the
// git repository — for the badges in the address bar.

export type InfraKind = 'kube' | 'aws' | 'gcp' | 'tf' | 'git' | 'ssh';

export interface Badge {
  kind: InfraKind;
  /** Short text for the badge, e.g. "payments · default". */
  label: string;
  /** Everything known, for the tooltip. */
  title: string;
  /** When the badge's soonest credential expires, if that is within a day or past. */
  clock?: Clock;
}

export interface Clock {
  /** "42m", "3h 10m", "expired". */
  label: string;
  /** Under ten minutes left, or none. */
  state: 'ok' | 'soon' | 'expired';
}

const MIN = 60_000;
const HOUR = 60 * MIN;
const DAY = 24 * HOUR;

/** Time left until `at`, coarse: "<1m", "42m", "3h 10m", "5d". */
export function timeLeft(at: number, now: number): string {
  const ms = at - now;
  if (ms < MIN) return '<1m';
  if (ms < HOUR) return `${Math.floor(ms / MIN)}m`;
  if (ms < DAY) {
    const m = Math.floor((ms % HOUR) / MIN);
    return `${Math.floor(ms / HOUR)}h${m ? ` ${m}m` : ''}`;
  }
  return `${Math.floor(ms / DAY)}d`;
}

/** The credentials' clock for a badge: shown only within a day of the soonest expiry. */
export function clockFor(expiries: Expiry[], now: number): Clock | undefined {
  if (!expiries.length) return undefined;
  const at = Math.min(...expiries.map((e) => e.expiresAt));
  if (at <= now) return { label: 'expired', state: 'expired' };
  if (at - now >= DAY) return undefined;
  return { label: timeLeft(at, now), state: at - now < 10 * MIN ? 'soon' : 'ok' };
}

/** "AWS SSO session acme expires Oct 12, 18:30 (in 42m)", for tooltips. */
export function describeExpiry(e: Expiry, now: number): string {
  const at = new Date(e.expiresAt).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });
  return e.expiresAt <= now
    ? `${e.what} expired ${at} (${relativeTime(new Date(e.expiresAt), new Date(now))})`
    : `${e.what} expires ${at} (in ${timeLeft(e.expiresAt, now)})`;
}

/** Adds a kind's expiries to its badge: the clock, and a tooltip line each. */
function withExpiries(b: Badge, expiries: Expiry[], now: number): Badge {
  if (!expiries.length) return b;
  return {
    ...b,
    title: [b.title, ...expiries.map((e) => describeExpiry(e, now))].join('\n'),
    clock: clockFor(expiries, now),
  };
}

/** The variables the shell scripts report (see internal/infra). */
export function parseEnv(text: string): Record<string, string> {
  const env: Record<string, string> = {};
  for (const line of text.split('\n')) {
    const i = line.indexOf('=');
    if (i > 0) env[line.slice(0, i)] = line.slice(i + 1);
  }
  return env;
}

/**
 * The cluster's own name, out of the long names EKS (an ARN) and GKE
 * (gke_project_zone_name) give it. The tooltip keeps the full one.
 */
export function shortClusterName(cluster: string): string {
  const eks = /^arn:aws[\w-]*:eks:[^:]*:\d+:cluster\/(.+)$/.exec(cluster);
  if (eks) return eks[1];
  const gke = /^gke_[^_]+_[^_]+_(.+)$/.exec(cluster);
  if (gke) return gke[1];
  return cluster;
}

const join = (...parts: string[]) => parts.filter(Boolean).join(' · ');

export function badges(ctx: Context | null | undefined, now = Date.now()): Badge[] {
  if (!ctx) return [];
  const out: Badge[] = [];
  const expiries = (kind: string) =>
    (ctx.expiries ?? []).filter((e) => e.kind === kind).sort((a, b) => a.expiresAt - b.expiresAt);

  if (ctx.kubeContext) {
    const cluster = ctx.kubeCluster || ctx.kubeContext;
    out.push(withExpiries({
      kind: 'kube',
      label: join(shortClusterName(cluster), ctx.kubeNamespace),
      title: [
        ctx.kubeCluster && `Cluster: ${ctx.kubeCluster}`,
        `Context: ${ctx.kubeContext}`,
        ctx.kubeNamespace && `Namespace: ${ctx.kubeNamespace}`,
      ].filter(Boolean).join('\n'),
    }, expiries('kube'), now));
  }
  if (ctx.awsProfile) {
    out.push(withExpiries({
      kind: 'aws',
      label: join(ctx.awsAccount, ctx.awsProfile, ctx.awsRegion),
      title: [
        `Account: ${ctx.awsAccount || ctx.awsAccountNote || 'unknown'}`,
        `Profile: ${ctx.awsProfile}`,
        ctx.awsRegion && `Region: ${ctx.awsRegion}`,
      ].filter(Boolean).join('\n'),
    }, expiries('aws'), now));
  }
  if (ctx.gcpProject) out.push({ kind: 'gcp', label: ctx.gcpProject, title: `Google Cloud project: ${ctx.gcpProject}` });
  if (ctx.tfWorkspace) out.push({ kind: 'tf', label: ctx.tfWorkspace, title: `Terraform workspace: ${ctx.tfWorkspace}` });
  if (ctx.git) out.push(gitBadge(ctx.git));
  const certs = expiries('ssh');
  if (certs.length) {
    out.push(withExpiries({
      kind: 'ssh',
      label: certs.length === 1 ? 'certificate' : `${certs.length} certificates`,
      title: "Certificates in the shell's ssh-agent",
    }, certs, now));
  }
  return out;
}

/**
 * "Ada · main → origin ↑2 ↓1": who commits (the tab's git identity, if it
 * uses one), the branch, its upstream (just
 * the remote when the branch names match) and the commits between them.
 */
export function gitBadge(g: Git, now = new Date()): Badge {
  const branch = g.branch || '(no commits)';
  let upstream = '';
  if (g.upstream) {
    const [remote, ...rest] = g.upstream.split('/');
    upstream = g.upstream.includes('/') && rest.join('/') === g.branch ? remote : g.upstream;
  }
  const diff = g.upstreamGone ? 'gone' : [g.ahead && `↑${g.ahead}`, g.behind && `↓${g.behind}`].filter(Boolean).join(' ');
  // A wtf git identity names who the tab works as; else the author does.
  const author = g.identity || g.authorName || g.authorEmail;

  let sync: string;
  if (g.detached) sync = 'Detached HEAD: not on a branch';
  else if (!g.upstream) sync = 'No upstream branch: nothing to compare with';
  else if (g.upstreamGone) sync = `${g.upstream} is gone (deleted on the remote, or not fetched yet)`;
  else if (!g.ahead && !g.behind) sync = `Up to date with ${g.upstream}`;
  else {
    const n = (k: number) => `${k} commit${k === 1 ? '' : 's'}`;
    sync = [g.ahead && `${n(g.ahead)} to push`, g.behind && `${n(g.behind)} to pull`].filter(Boolean).join(', ');
  }
  const fetched = g.upstream && !g.upstreamGone
    ? `As of the last fetch${g.fetchedAt ? ` (${relativeTime(new Date(g.fetchedAt), now)})` : ''}`
    : '';

  return {
    kind: 'git',
    label: join(author, [branch, upstream && `→ ${upstream}`, diff].filter(Boolean).join(' ')),
    title: [
      `Repository: ${g.root}`,
      `${g.detached ? 'Commit' : 'Branch'}: ${branch}`,
      g.upstream && `Upstream: ${g.upstream}`,
      g.remoteURL && `Remote: ${g.remote} ${g.remoteURL}`,
      sync,
      fetched,
      g.identity && `Git identity: ${g.identity} (its SSH key only)`,
      `Author: ${[g.authorName, g.authorEmail && `<${g.authorEmail}>`].filter(Boolean).join(' ') || 'not set (git config user.name)'}`,
    ].filter(Boolean).join('\n'),
  };
}

/**
 * Resolves a shell's context; only the newest call per pane delivers its
 * answer, so a slow reply can't overwrite a newer one.
 */
const latest = new Map<number, number>();
let seq = 0;

export async function resolveContext(
  paneId: number,
  env: Record<string, string>,
  apply: (ctx: Context) => void,
): Promise<void> {
  const mine = ++seq;
  latest.set(paneId, mine);
  try {
    const ctx = await InfraService.Resolve(env);
    if (latest.get(paneId) === mine) apply(ctx);
  } catch {
    // Leave the last known context; the next prompt tries again.
  } finally {
    if (latest.get(paneId) === mine) latest.delete(paneId);
  }
}
