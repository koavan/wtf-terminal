import { describe, expect, it, vi } from 'vitest';
import { tokenMenu, type TokenActions } from './logActions';
import { scanLine, type LogToken } from './logs';
import type { MenuEntry } from './overlay.svelte';

const TARGET = { context: 'prod', namespace: 'shop', env: {} };

function actions(): TokenActions & Record<string, ReturnType<typeof vi.fn>> {
  return {
    copy: vi.fn(), copySecret: vi.fn(), openUrl: vi.fn(), runInTab: vi.fn(), runHere: vi.fn(), runInShell: vi.fn(), openInEditor: vi.fn(), openFile: vi.fn(), reveal: vi.fn(), showSecret: vi.fn(), inspectJwt: vi.fn(), inspectTls: vi.fn(),
  } as never;
}

const token = (line: string): LogToken => scanLine(line)[0];
const labels = (entries: MenuEntry[]) => entries.map((e) => (e === 'separator' ? '—' : 'heading' in e ? `# ${e.heading}` : e.label));
const click = (entries: MenuEntry[], label: string) => {
  const e = entries.find((x) => x !== 'separator' && 'label' in x && x.label === label);
  if (!e || e === 'separator' || !('action' in e)) throw new Error(`no ${label}`);
  e.action();
};

describe('tokenMenu', () => {
  it('runs kubectl for a pod against the shell’s context and namespace, and says which', () => {
    const a = actions();
    const m = tokenMenu(token('pod/web-1 created'), TARGET, a);
    expect(labels(m)).toEqual(['# pod/web-1', '# Context prod · namespace shop', 'Logs', 'Describe', 'View YAML', '—', 'Copy Name', 'Copy pod/web-1']);
    click(m, 'Logs');
    expect(a.runInTab).toHaveBeenCalledWith('kubectl --context prod -n shop logs -f --tail=200 web-1');
    click(m, 'Describe');
    expect(vi.mocked(a.runInShell).mock.calls[0][0](TARGET)).toBe('kubectl --context prod -n shop describe pod web-1');
  });

  it('shows a workload’s logs by its kind, and uses kubectl’s own context when the shell reported none', () => {
    const a = actions();
    const m = tokenMenu(token('deployment.apps/api restarted'), { context: '', namespace: '', env: {} }, a);
    expect(labels(m)[1]).toBe("# kubectl's current context (this shell reported none)");
    click(m, 'Logs');
    expect(a.runInTab).toHaveBeenCalledWith('kubectl logs -f --tail=200 --all-containers --prefix deployment/api');
  });

  it('never prints a secret’s values: the masked viewer instead of its YAML', () => {
    const a = actions();
    const m = tokenMenu(token('secret/db-creds configured'), TARGET, a);
    expect(labels(m)).not.toContain('View YAML');
    click(m, 'View Decoded Values…');
    expect(a.showSecret).toHaveBeenCalledWith('db-creds');
  });

  it('converts a timestamp and copies it in several forms', () => {
    const a = actions();
    const now = new Date('2026-10-09T12:05:00Z');
    const m = tokenMenu(token('2026-10-09T12:00:00Z start'), TARGET, a, now);
    expect(labels(m)[0]).toMatch(/your time · 5 min ago$/);
    expect(labels(m)).not.toContain('# No time zone in the text: read as your time');
    click(m, 'Copy as Unix Time');
    expect(a.copy).toHaveBeenCalledWith('1791547200', 'Unix time');
    click(m, 'Copy as UTC (ISO 8601)');
    expect(a.copy).toHaveBeenCalledWith('2026-10-09T12:00:00.000Z', 'time');
  });

  it('says when a timestamp had no time zone', () => {
    const m = tokenMenu(token('2026-10-09 12:00:00 start'), TARGET, actions());
    expect(labels(m)).toContain('# No time zone in the text: read as your time');
  });

  it('offers ssh and the browser for an address with a port, only copying for a range', () => {
    const a = actions();
    const m = tokenMenu(token('10.0.0.5:8080 up'), TARGET, a);
    click(m, 'Open http://10.0.0.5:8080');
    expect(a.openUrl).toHaveBeenCalledWith('http://10.0.0.5:8080');
    click(m, 'SSH to 10.0.0.5');
    expect(a.runHere).toHaveBeenCalledWith('ssh 10.0.0.5');
    expect(labels(tokenMenu(token('10.0.0.0/8'), TARGET, a))).toEqual(['# 10.0.0.0/8', 'Copy Address']);
  });

  it('opens a file at its line', () => {
    const a = actions();
    const m = tokenMenu({ ...(token('main.go:12') as Extract<LogToken, { kind: 'path' }>), path: '/src/main.go' }, TARGET, a);
    click(m, 'Open in Editor at Line 12');
    expect(a.openInEditor).toHaveBeenCalledWith('/src/main.go', 12);
  });

  it('has nothing for log levels and bare times', () => {
    expect(tokenMenu(token('ERROR'), TARGET, actions())).toEqual([]);
    expect(tokenMenu(token('at 10:00:00 ok'), TARGET, actions())).toEqual([]);
  });
});

describe('inspector entries', () => {
  it('offers TLS for https URLs only, and opens a JWT', () => {
    const a = actions();
    const https = tokenMenu(token('see https://api.example.com/v1'), TARGET, a);
    expect(labels(https)).toContain('Inspect TLS Certificate…');
    click(https, 'Inspect TLS Certificate…');
    expect(a.inspectTls).toHaveBeenCalledWith('https://api.example.com/v1');
    expect(labels(tokenMenu(token('see http://api.example.com/v1'), TARGET, a))).not.toContain('Inspect TLS Certificate…');

    const jwt = 'eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJhZGEifQ.sig';
    const m = tokenMenu(token(jwt), TARGET, a);
    expect(labels(m)).toEqual(['# JWT', 'Inspect Token…', 'Copy Token']);
    click(m, 'Inspect Token…');
    expect(a.inspectJwt).toHaveBeenCalledWith(jwt);
    click(m, 'Copy Token');
    expect(a.copySecret).toHaveBeenCalledWith(jwt, 'token');
    expect(a.copy).not.toHaveBeenCalled();
  });
});
