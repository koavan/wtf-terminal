import { describe, expect, it, vi } from 'vitest';

vi.mock('../../bindings/github.com/dayanraj/wtf/internal/infra', () => ({ Service: { Resolve: vi.fn() } }));

import { Service } from '../../bindings/github.com/dayanraj/wtf/internal/infra';
import { badges, parseEnv, resolveContext, shortClusterName, type InfraContext } from './infra';

const ctx = (c: Partial<InfraContext>): InfraContext => ({
  kubeContext: '', kubeCluster: '', kubeNamespace: '', awsProfile: '', awsAccount: '', awsAccountNote: '', awsRegion: '',
  gcpProject: '', tfWorkspace: '', ...c,
});

describe('parseEnv', () => {
  it('splits lines at the first =', () => {
    expect(parseEnv('PWD=/a=b\nKUBECONFIG=/x:/y\n\nbogus\n')).toEqual({ PWD: '/a=b', KUBECONFIG: '/x:/y' });
  });
});

describe('shortClusterName', () => {
  it('shortens EKS and GKE names to the cluster', () => {
    expect(shortClusterName('arn:aws:eks:us-east-1:123456789012:cluster/payments')).toBe('payments');
    expect(shortClusterName('gke_my-proj_europe-west1-b_shop')).toBe('shop');
    expect(shortClusterName('minikube')).toBe('minikube');
  });
});

describe('badges', () => {
  it('shows the cluster, account, project and workspace', () => {
    const b = badges(
      ctx({
        kubeContext: 'work',
        kubeCluster: 'arn:aws:eks:us-east-1:123456789012:cluster/payments',
        kubeNamespace: 'api',
        awsProfile: 'admin',
        awsAccount: '210987654321',
        awsRegion: 'us-east-1',
        gcpProject: 'shop',
        tfWorkspace: 'staging',
      }),
    );
    expect(b.map((x) => [x.kind, x.label])).toEqual([
      ['kube', 'payments · api'],
      ['aws', '210987654321 · admin · us-east-1'],
      ['gcp', 'shop'],
      ['tf', 'staging'],
    ]);
    expect(b[0].title).toContain('Context: work');
    expect(b[0].title).toContain('Cluster: arn:aws:eks:us-east-1:123456789012:cluster/payments');
  });

  it('falls back to the context name and leaves out what is unknown', () => {
    const b = badges(ctx({ kubeContext: 'minikube', awsProfile: 'static', awsAccountNote: 'asking AWS…' }));
    expect(b.map((x) => x.label)).toEqual(['minikube', 'static']);
    expect(b[1].title).toContain('Account: asking AWS…');
  });

  it('is empty without a context', () => {
    expect(badges(null)).toEqual([]);
    expect(badges(ctx({}))).toEqual([]);
  });
});

describe('resolveContext', () => {
  it('drops a slow answer when a newer one was asked for', async () => {
    let finishFirst!: (c: InfraContext) => void;
    vi.mocked(Service.Resolve)
      .mockReturnValueOnce(new Promise((r) => (finishFirst = r)) as never)
      .mockResolvedValueOnce(ctx({ kubeContext: 'new' }) as never);
    const apply = vi.fn();
    const first = resolveContext(1, {}, apply);
    await resolveContext(1, {}, apply);
    finishFirst(ctx({ kubeContext: 'old' }));
    await first;
    expect(apply).toHaveBeenCalledTimes(1);
    expect(apply).toHaveBeenCalledWith(expect.objectContaining({ kubeContext: 'new' }));
  });
});
