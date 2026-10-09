import { Service as InfraService, type Context } from '../../bindings/github.com/dayanraj/wtf/internal/infra';

export type { Context as InfraContext } from '../../bindings/github.com/dayanraj/wtf/internal/infra';

// What each shell is pointed at — Kubernetes, AWS, gcloud, Terraform — for
// the badges in the address bar.

export type InfraKind = 'kube' | 'aws' | 'gcp' | 'tf';

export interface Badge {
  kind: InfraKind;
  /** Short text for the badge, e.g. "payments · default". */
  label: string;
  /** Everything known, for the tooltip. */
  title: string;
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

export function badges(ctx: Context | null | undefined): Badge[] {
  if (!ctx) return [];
  const out: Badge[] = [];

  if (ctx.kubeContext) {
    const cluster = ctx.kubeCluster || ctx.kubeContext;
    out.push({
      kind: 'kube',
      label: join(shortClusterName(cluster), ctx.kubeNamespace),
      title: [
        ctx.kubeCluster && `Cluster: ${ctx.kubeCluster}`,
        `Context: ${ctx.kubeContext}`,
        ctx.kubeNamespace && `Namespace: ${ctx.kubeNamespace}`,
      ].filter(Boolean).join('\n'),
    });
  }
  if (ctx.awsProfile) {
    out.push({
      kind: 'aws',
      label: join(ctx.awsAccount, ctx.awsProfile, ctx.awsRegion),
      title: [
        `Account: ${ctx.awsAccount || ctx.awsAccountNote || 'unknown'}`,
        `Profile: ${ctx.awsProfile}`,
        ctx.awsRegion && `Region: ${ctx.awsRegion}`,
      ].filter(Boolean).join('\n'),
    });
  }
  if (ctx.gcpProject) out.push({ kind: 'gcp', label: ctx.gcpProject, title: `Google Cloud project: ${ctx.gcpProject}` });
  if (ctx.tfWorkspace) out.push({ kind: 'tf', label: ctx.tfWorkspace, title: `Terraform workspace: ${ctx.tfWorkspace}` });
  return out;
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
