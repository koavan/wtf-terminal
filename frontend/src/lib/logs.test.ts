import { describe, expect, it } from 'vitest';
import { findJson, k8sKind, parseTimestamp, relativeTime, scanLine, type LogToken } from './logs';

const kinds = (line: string) => scanLine(line).map((t) => [t.kind, t.text]);
const only = <K extends LogToken['kind']>(line: string, kind: K) =>
  scanLine(line).filter((t): t is Extract<LogToken, { kind: K }> => t.kind === kind);

describe('scanLine', () => {
  it('finds the parts of a typical log line', () => {
    expect(kinds('2026-10-09T12:34:56.789Z ERROR request from 10.0.3.7:52144 failed: see https://example.com/x.')).toEqual([
      ['timestamp', '2026-10-09T12:34:56.789Z'],
      ['level', 'ERROR'],
      ['ip', '10.0.3.7:52144'],
      ['url', 'https://example.com/x'],
    ]);
  });

  it('reads log levels in their usual spellings', () => {
    const levels = (line: string) => only(line, 'level').map((t) => t.level);
    expect(levels('[WARN] disk 91% full')).toEqual(['warn']);
    expect(levels('level=error msg="boom"')).toEqual(['error']);
    expect(levels('{"level":"info","msg":"ok"}')).toEqual(['info']);
    expect(levels('[debug] cache hit')).toEqual(['debug']);
    expect(levels('FATAL: out of memory')).toEqual(['error']);
    // Not words of their own, or ordinary lower-case prose.
    expect(levels('ERROR_CODES.md INFOS warning: an error happened')).toEqual([]);
  });

  it('finds timestamps in ISO, access-log and syslog form, and bare times', () => {
    expect(kinds('2026-10-09 12:34:56,123 +02:00 started')).toEqual([['timestamp', '2026-10-09 12:34:56,123 +02:00']]);
    expect(kinds('1.2.3.4 - - [09/Oct/2026:13:55:36 -0700] "GET /"')[1]).toEqual(['timestamp', '09/Oct/2026:13:55:36 -0700']);
    expect(kinds('Oct  9 12:34:56 host sshd[42]: Accepted')).toEqual([['timestamp', 'Oct  9 12:34:56']]);
    expect(kinds('took until 12:34:56.5 today')).toEqual([['time', '12:34:56.5']]);
  });

  it("doesn't mistake MAC addresses or versions inside words for times", () => {
    expect(only('ether aa:bb:12:34:56:78', 'time')).toEqual([]);
  });

  it('finds IPv4 with ports and CIDRs, and the IPv6 forms that are unambiguous', () => {
    const ips = (line: string) => only(line, 'ip').map((t) => [t.address, t.port, t.cidr]);
    expect(ips('peer 192.168.1.20:8443 in 10.0.0.0/16.')).toEqual([
      ['192.168.1.20', 8443, undefined],
      ['10.0.0.0', undefined, true],
    ]);
    expect(ips('listen [::1]:80 and fe80::1c2:3 and 2001:db8:0:0:0:0:2:1')).toEqual([
      ['::1', undefined, undefined],
      ['fe80::1c2:3', undefined, undefined],
      ['2001:db8:0:0:0:0:2:1', undefined, undefined],
    ]);
    expect(ips('version 999.1.1.1 and 1.2.3.4.5')).toEqual([]);
    expect(ips('std::vector Foo::Bar')).toEqual([]);
  });

  it('finds file:line references, including Python tracebacks', () => {
    const paths = (line: string) => only(line, 'path').map((t) => [t.path, t.line, t.col]);
    expect(paths('panic at /Users/me/app/main.go:42 +0x1d')).toEqual([['/Users/me/app/main.go', 42, undefined]]);
    expect(paths('src/lib/app.ts:3:14 - error TS2304')).toEqual([['src/lib/app.ts', 3, 14]]);
    expect(paths('  File "app/views.py", line 88, in index')).toEqual([['app/views.py', 88, undefined]]);
    expect(paths('./run.sh:7: command not found')).toEqual([['./run.sh', 7, undefined]]);
    expect(paths('at 12:30:45 and v1.2:3')).toEqual([]);
  });

  it('finds Kubernetes resources as kubectl prints them', () => {
    const res = (line: string) => only(line, 'k8s').map((t) => [t.resource, t.name]);
    expect(res('deployment.apps/api-server restarted')).toEqual([['deployment.apps', 'api-server']]);
    expect(res('pod/web-7d9f8-x2x9z created, service/web unchanged.')).toEqual([
      ['pod', 'web-7d9f8-x2x9z'],
      ['service', 'web'],
    ]);
    // Paths that merely contain a kind aren't resources.
    expect(res('src/pods/list.ts and ./secret/key and pod/UPPER')).toEqual([]);
  });

  it('keeps hosts in URLs from becoming IPs or paths', () => {
    expect(kinds('GET http://10.1.2.3:8080/health.json:12 ok')).toEqual([['url', 'http://10.1.2.3:8080/health.json:12']]);
  });

  it('keeps a closing bracket that belongs to the URL', () => {
    expect(only('(see https://en.wikipedia.org/wiki/Foo_(bar))', 'url')[0].text).toBe('https://en.wikipedia.org/wiki/Foo_(bar)');
  });

  it('gives positions that point at the text', () => {
    const line = 'x  WARN  pod/a-1';
    for (const t of scanLine(line)) expect(line.slice(t.start, t.end)).toBe(t.text);
  });

  it('skips very long lines', () => {
    expect(scanLine('ERROR '.repeat(1000))).toEqual([]);
  });
});

describe('k8sKind', () => {
  it('turns short, plural and grouped names into the kind', () => {
    expect(k8sKind('deploy')).toBe('deployment');
    expect(k8sKind('deployments.apps')).toBe('deployment');
    expect(k8sKind('pods')).toBe('pod');
    expect(k8sKind('ingresses')).toBe('ingress');
    expect(k8sKind('networkpolicies')).toBe('networkpolicy');
    expect(k8sKind('storageclasses')).toBe('storageclass');
    expect(k8sKind('endpoints')).toBe('endpoints');
    expect(k8sKind('job.batch')).toBe('job');
  });
});

describe('parseTimestamp', () => {
  it('reads zoned ISO timestamps exactly', () => {
    expect(parseTimestamp('2026-10-09T12:34:56.789Z')).toEqual({ date: new Date('2026-10-09T12:34:56.789Z'), zoned: true });
    expect(parseTimestamp('2026-10-09 14:34:56,5 +02:00')?.date.toISOString()).toBe('2026-10-09T12:34:56.500Z');
    expect(parseTimestamp('09/Oct/2026:13:55:36 -0700')?.date.toISOString()).toBe('2026-10-09T20:55:36.000Z');
  });

  it('reads timestamps without a zone as local time, and says so', () => {
    const t = parseTimestamp('2026-10-09 12:34');
    expect(t?.zoned).toBe(false);
    expect(t?.date.getHours()).toBe(12);
  });

  it("gives syslog timestamps this year, or last year's December in January", () => {
    expect(parseTimestamp('Oct  9 12:34:56', new Date(2026, 9, 10))?.date.getFullYear()).toBe(2026);
    expect(parseTimestamp('Dec 31 23:00:00', new Date(2027, 0, 2))?.date.getFullYear()).toBe(2026);
  });

  it('rejects impossible dates and other text', () => {
    expect(parseTimestamp('2026-13-40T10:00:00Z')).toBeNull();
    expect(parseTimestamp('12:34:56')).toBeNull();
  });
});

describe('relativeTime', () => {
  const now = new Date('2026-10-09T12:00:00Z');
  it('says how long ago, or how far ahead', () => {
    expect(relativeTime(new Date('2026-10-09T11:59:40Z'), now)).toBe('just now');
    expect(relativeTime(new Date('2026-10-09T11:57:00Z'), now)).toBe('3 min ago');
    expect(relativeTime(new Date('2026-10-09T09:00:00Z'), now)).toBe('3 h ago');
    expect(relativeTime(new Date('2026-10-12T12:00:00Z'), now)).toBe('in 3 d');
  });
});

describe('findJson', () => {
  it('pretty-prints the JSON after a log prefix', () => {
    expect(findJson('2026-10-09 INFO {"a":1,"b":[true]}')).toBe('{\n  "a": 1,\n  "b": [\n    true\n  ]\n}');
  });
  it('skips brackets that start no JSON', () => {
    expect(findJson('[INFO] payload=["x"]')).toBe('[\n  "x"\n]');
  });
  it('is null without JSON', () => {
    expect(findJson('[INFO] nothing {here}')).toBeNull();
    expect(findJson('42')).toBeNull();
  });
});
