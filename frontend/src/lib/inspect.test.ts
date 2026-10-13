import { describe, expect, it, vi } from 'vitest';
import { base64Bytes, decodeBase64, decodeJwt, hexDump, isCertFile, looksBase64, selectionEntries, tlsTarget, yamlBase64At } from './inspect';
import { scanLine } from './logs';

const b64url = (s: string) => btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
const jwt = (header: object, claims: object, sig = 'c2lnbmF0dXJl') => `${b64url(JSON.stringify(header))}.${b64url(JSON.stringify(claims))}.${sig}`;
const TOKEN = jwt({ alg: 'RS256', typ: 'JWT' }, { sub: 'ada', iss: 'https://idp', exp: 1791829800, iat: 1791826200, aud: ['api'] });
const PEM = '-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n';

describe('decodeJwt', () => {
  it('decodes header, claims and the time claims', () => {
    const j = decodeJwt(TOKEN)!;
    expect(j.alg).toBe('RS256');
    expect(j.signed).toBe(true);
    expect(j.claims).toMatchObject({ sub: 'ada', aud: ['api'] });
    expect(j.times.map((t) => [t.claim, t.date.getTime()])).toEqual([['exp', 1791829800000], ['iat', 1791826200000]]);
  });

  it('notices an unsigned token and rejects what isn’t a JWT', () => {
    expect(decodeJwt(jwt({ alg: 'none' }, { a: 1 }, ''))!.signed).toBe(false);
    for (const bad of ['', 'a.b', 'a.b.c', `${b64url('[1]')}.${b64url('{}')}.x`, `${b64url('{}')}.${b64url('nope')}.x`]) {
      expect(decodeJwt(bad)).toBeNull();
    }
  });

  it('is found in output as its own token', () => {
    const line = `Authorization: Bearer ${TOKEN} sent`;
    expect(scanLine(line).find((t) => t.kind === 'jwt')?.text).toBe(TOKEN);
  });
});

describe('decodeBase64', () => {
  it('reads standard and url-safe base64, with or without padding', () => {
    expect(Array.from(base64Bytes('+/8=')!)).toEqual([251, 255]);
    expect(Array.from(base64Bytes('-_8')!)).toEqual([251, 255]);
    expect(base64Bytes('abcde')).toBeNull(); // 5 chars: can't be base64
    expect(base64Bytes('not base64!')).toBeNull();
  });

  it('tells text, JSON, certificates, JWTs and binary data apart', () => {
    expect(decodeBase64(btoa('hunter2-password'))).toEqual({ kind: 'text', size: 16, text: 'hunter2-password' });
    expect(decodeBase64(btoa('{"a":1}'))).toEqual({ kind: 'json', size: 7, text: '{\n  "a": 1\n}' });
    expect(decodeBase64(btoa(PEM))?.kind).toBe('certificate');
    expect(decodeBase64(btoa(TOKEN + '\n'))).toMatchObject({ kind: 'jwt', text: TOKEN });
    const bin = decodeBase64(btoa('\x00\x01\x02hello'))!;
    expect(bin.kind).toBe('binary');
    expect(bin.text).toBe(`00000000  ${'00 01 02 68 65 6c 6c 6f'.padEnd(47)}  |...hello|`);
  });

  it('dumps 16 bytes a row', () => {
    expect(hexDump(new Uint8Array(17)).split('\n')).toHaveLength(2);
  });
});

describe('what a selection can be inspected as', () => {
  it('only offers base64 for data, not words', () => {
    expect(looksBase64('cGFzc3dvcmQxMjM=')).toBe(true);
    expect(looksBase64('U2VjcmV0VmFsdWU')).toBe(true);
    expect(looksBase64('deployment')).toBe(false);
    expect(looksBase64('12345678')).toBe(false);
    expect(looksBase64('short=')).toBe(false);
  });

  it('finds the base64 value under the mouse in YAML', () => {
    const line = '  password: cGFzc3dvcmQxMjM=';
    expect(yamlBase64At(line, 14)).toBe('cGFzc3dvcmQxMjM=');
    expect(yamlBase64At(line, 3)).toBeNull();
    expect(yamlBase64At('  name: payments', 10)).toBeNull();
  });

  it('reads TLS servers from URLs, host:port and dotted host names', () => {
    expect(tlsTarget('https://api.example.com/v1/x?y=1')).toBe('https://api.example.com');
    expect(tlsTarget('db.internal:5432')).toBe('db.internal:5432');
    expect(tlsTarget('example.com')).toBe('example.com');
    expect(tlsTarget('10.0.0.5:8443')).toBe('10.0.0.5:8443');
    for (const no of ['main.go', 'config.yaml', 'hello', 'two words.com', 'http://plain.example.com', '']) expect(tlsTarget(no)).toBeNull();
  });

  const actions = () => ({ inspectJwt: vi.fn(), decodeBase64: vi.fn(), inspectCertText: vi.fn(), inspectTls: vi.fn() });
  const labels = (sel: string, secrets = true) =>
    selectionEntries(sel, actions(), secrets).map((e) => (typeof e === 'object' && 'label' in e ? e.label : e));

  it('builds the menu, and leaves secrets alone while Hide Secrets is on', () => {
    expect(labels(PEM)).toEqual(['Inspect Certificate…']);
    expect(labels(`token=${TOKEN}`)).toEqual(['Inspect JWT…']);
    expect(labels('cGFzc3dvcmQxMjM=')).toEqual(['Decode Base64…']);
    expect(labels('https://example.com:8443/')).toEqual(['Inspect TLS Certificate of example.com:8443…']);
    expect(labels(TOKEN, false)).toEqual([]);
    expect(labels('cGFzc3dvcmQxMjM=', false)).toEqual([]);
    expect(labels(PEM, false)).toEqual(['Inspect Certificate…']);
    expect(labels('just words')).toEqual([]);
  });

  it('passes what was selected to the action', () => {
    const a = actions();
    const [entry] = selectionEntries(` ${TOKEN} `, a);
    if (typeof entry === 'object' && 'action' in entry) entry.action();
    expect(a.inspectJwt).toHaveBeenCalledWith(TOKEN);
  });
});

it('knows certificate files', () => {
  expect(['a.pem', 'ca.crt', 'x.CER', 'b.der'].every(isCertFile)).toBe(true);
  expect(['id_rsa', 'a.key', 'a.pub'].some(isCertFile)).toBe(false);
});
