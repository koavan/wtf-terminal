import { describe, expect, it, vi } from 'vitest';

vi.mock('../../bindings/github.com/dayanraj/wtf/internal/infra', () => ({ Service: { Resolve: vi.fn() } }));

import { Service } from '../../bindings/github.com/dayanraj/wtf/internal/infra';
import { badges, gitBadge, parseEnv, resolveContext, shortClusterName, type InfraContext } from './infra';

const ctx = (c: Partial<InfraContext>): InfraContext => ({
  kubeContext: '', kubeCluster: '', kubeNamespace: '', awsProfile: '', awsAccount: '', awsAccountNote: '', awsRegion: '',
  gcpProject: '', tfWorkspace: '', git: null, ...c,
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

describe('gitBadge', () => {
  const git = (g: Partial<NonNullable<InfraContext['git']>>) => ({
    root: '/src/engine', branch: 'main', detached: false, upstream: 'origin/main', upstreamGone: false,
    ahead: 0, behind: 0, remote: 'origin', remoteURL: 'git@github.com:ada/engine.git', fetchedAt: 0,
    authorName: 'Ada', authorEmail: 'ada@example.com', identity: '', ...g,
  });
  const now = new Date(10 * 3600_000);

  it('shows the author, branch, remote and commits apart', () => {
    const b = gitBadge(git({ ahead: 2, behind: 1, fetchedAt: now.getTime() - 3 * 3600_000 }), now);
    expect(b.label).toBe('Ada · main → origin ↑2 ↓1');
    expect(b.title).toContain('Remote: origin git@github.com:ada/engine.git');
    expect(b.title).toContain('2 commits to push, 1 commit to pull');
    expect(b.title).toContain('As of the last fetch (3 h ago)');
    expect(b.title).toContain('Author: Ada <ada@example.com>');
  });

  it('names the upstream in full when its branch differs', () => {
    expect(gitBadge(git({ upstream: 'origin/release/2.0' })).label).toBe('Ada · main → origin/release/2.0');
    expect(gitBadge(git({ upstream: 'upstream/main' })).label).toBe('Ada · main → upstream');
  });

  it('says when there is nothing to compare with', () => {
    expect(gitBadge(git({ upstream: '' })).label).toBe('Ada · main');
    expect(gitBadge(git({ upstream: '' })).title).toContain('No upstream branch');
    expect(gitBadge(git({ upstreamGone: true })).label).toBe('Ada · main → origin gone');
    expect(gitBadge(git({ upstream: '', detached: true, branch: 'abc1234' })).title).toContain('Commit: abc1234');
    expect(gitBadge(git({ authorName: '', authorEmail: '' })).title).toContain('Author: not set');
  });

  it('names the git identity the tab uses', () => {
    const b = gitBadge(git({ identity: 'work' }));
    expect(b.label).toBe('work · main → origin');
    expect(b.title).toContain('Git identity: work');
    expect(b.title).toContain('Author: Ada <ada@example.com>');
  });

  it('is one of the badges', () => {
    expect(badges(ctx({ git: git({}) })).map((x) => x.kind)).toEqual(['git']);
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
