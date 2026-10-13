import { describe, expect, it, vi } from 'vitest';

vi.mock('../../bindings/github.com/dayanraj/wtf/internal/infra', () => ({ Service: { Resolve: vi.fn() } }));

import { Service } from '../../bindings/github.com/dayanraj/wtf/internal/infra';
import { badges, clockFor, gitBadge, parseEnv, resolveContext, shortClusterName, timeLeft, type InfraContext } from './infra';

const ctx = (c: Partial<InfraContext>): InfraContext => ({
  kubeContext: '', kubeCluster: '', kubeNamespace: '', awsProfile: '', awsAccount: '', awsAccountNote: '', awsRegion: '',
  gcpProject: '', tfWorkspace: '', git: null, expiries: null, ...c,
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

describe('credential expiry clocks', () => {
  const now = Date.UTC(2026, 9, 12, 12, 0);
  const min = 60_000;

  it('formats time left coarsely', () => {
    expect(timeLeft(now + 30_000, now)).toBe('<1m');
    expect(timeLeft(now + 42 * min, now)).toBe('42m');
    expect(timeLeft(now + 190 * min, now)).toBe('3h 10m');
    expect(timeLeft(now + 120 * min, now)).toBe('2h');
    expect(timeLeft(now + 5 * 24 * 60 * min, now)).toBe('5d');
  });

  it('shows a clock only within a day, flagging under ten minutes and expired', () => {
    const e = (m: number) => [{ kind: 'aws', what: 'x', expiresAt: now + m * min }];
    expect(clockFor([], now)).toBeUndefined();
    expect(clockFor(e(25 * 60), now)).toBeUndefined();
    expect(clockFor(e(42), now)).toEqual({ label: '42m', state: 'ok' });
    expect(clockFor(e(9), now)).toEqual({ label: '9m', state: 'soon' });
    expect(clockFor(e(-1), now)).toEqual({ label: 'expired', state: 'expired' });
  });

  it('puts each kind on its badge, soonest first, with every expiry in the tooltip', () => {
    const b = badges(ctx({
      kubeContext: 'dev', awsProfile: 'sso', awsAccount: '111111111111',
      expiries: [
        { kind: 'kube', what: 'Kubernetes token of user dev', expiresAt: now + 300 * min },
        { kind: 'kube', what: 'Kubernetes client certificate of user dev', expiresAt: now + 30 * min },
        { kind: 'aws', what: 'AWS SSO session acme', expiresAt: now - 5 * min },
        { kind: 'ssh', what: 'SSH certificate ada@corp (ada)', expiresAt: now + 9 * 24 * 60 * min },
      ],
    }), now);
    const [kube, aws, ssh] = b;
    expect(kube.clock).toEqual({ label: '30m', state: 'ok' });
    expect(kube.title).toMatch(/client certificate of user dev expires .* \(in 30m\)\n.*token of user dev expires .* \(in 5h\)$/);
    expect(aws.clock).toEqual({ label: 'expired', state: 'expired' });
    expect(aws.title).toMatch(/AWS SSO session acme expired .* \(5 min ago\)$/);
    expect(ssh).toMatchObject({ kind: 'ssh', label: 'certificate', clock: undefined });
    expect(ssh.title).toMatch(/ada@corp \(ada\) expires .* \(in 9d\)$/);
  });

  it('adds nothing without expiries', () => {
    expect(badges(ctx({ awsProfile: 'p' }), now)[0].clock).toBeUndefined();
  });
});
