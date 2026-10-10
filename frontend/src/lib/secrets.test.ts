import { describe, expect, it } from 'vitest';
import { findSecrets, redact, REDACTED } from './secrets';

const found = (text: string) => findSecrets(text).map((s) => [s.kind, text.slice(s.start, s.end)]);

// Made-up values in the shapes the real ones have.
const AKID = 'AKIAABCDEFGHIJKLMNOP';
const SECRET = 'wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY';
const JWT = 'eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U';

describe('findSecrets', () => {
  it('finds AWS keys, alone and in the credentials file', () => {
    expect(found(`aws_access_key_id = ${AKID}`)).toEqual([['AWS access key', AKID]]);
    expect(found(`aws_secret_access_key = ${SECRET}`)).toEqual([['AWS secret key', SECRET]]);
    expect(found(`export AWS_SECRET_ACCESS_KEY="${SECRET}"`)).toEqual([['AWS secret key', SECRET]]);
    expect(found(`"SecretAccessKey": "${SECRET}",`)).toEqual([['AWS secret key', SECRET]]);
  });

  it('finds tokens by their prefixes', () => {
    const tokens = [
      'ghp_abcdefghijklmnopqrstuvwxyz0123456789',
      'github_pat_11ABCDEFG0123456789_abcdefghijklmnop',
      'glpat-abcdefghij0123456789',
      'xoxb-1234567890-abcdefghij',
      'sk_live_abcdefghij0123456789ab',
      'AIzaSyA1234567890abcdefghijklmnopqrstuv',
      'sk-ant-api03-abcdefghij0123456789',
      'npm_abcdefghijklmnopqrstuvwxyz0123456789',
    ];
    for (const t of tokens) expect(found(`use ${t} here`).map((f) => f[1]), t).toEqual([t]);
    expect(found(`token ${JWT}`)[0]).toEqual(['JWT', JWT]);
  });

  it('finds passwords in URLs and bearer tokens', () => {
    expect(found('DATABASE_URL=postgres://app:s3cr3t-pw@db:5432/app')).toEqual([['password in URL', 's3cr3t-pw']]);
    expect(found('Authorization: Bearer abcdefghijklmnopqrstuvwxyz012345')).toEqual([['bearer token', 'abcdefghijklmnopqrstuvwxyz012345']]);
  });

  it('finds the values of settings named like secrets', () => {
    expect(found('password: hunter2222')).toEqual([['secret setting', 'hunter2222']]);
    expect(found('export GITHUB_TOKEN=abc123def456')).toEqual([['secret setting', 'abc123def456']]);
    expect(found('"client_secret": "q8~Zz.Y1"')).toEqual([['secret setting', 'q8~Zz.Y1']]);
    expect(found('db.password=correcthorse')).toEqual([['secret setting', 'correcthorse']]);
  });

  it('leaves settings without a real value alone', () => {
    expect(found('password: null')).toEqual([]);
    expect(found('token: 1220 bytes')).toEqual([]);
    expect(found('api_key: ${API_KEY}')).toEqual([]);
    expect(found('password: ********')).toEqual([]);
    expect(found('the password is set')).toEqual([]);
  });

  it("finds a secret's base64 data in YAML, but not hashes or words", () => {
    const yaml = 'data:\n  username: aHVudGVyMjIyMg==\n  sha: 3f786850e387550fdab836ed7e6dc881de23001b\n  note: averylongwordwithoutdigitsorcaps\n  kind: ConfigMapListItems';
    expect(found(yaml)).toEqual([['encoded value', 'aHVudGVyMjIyMg==']]);
  });

  it('finds private keys, to their end or the end of the text', () => {
    const key = '-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\n-----END OPENSSH PRIVATE KEY-----';
    expect(found(`before\n${key}\nafter`)).toEqual([['private key', key]]);
    expect(found('-----BEGIN RSA PRIVATE KEY-----\nMIIEow')[0][1]).toBe('-----BEGIN RSA PRIVATE KEY-----\nMIIEow');
    expect(found('-----BEGIN PUBLIC KEY-----')).toEqual([]);
  });

  it('leaves ordinary output alone', () => {
    expect(found('NAME READY STATUS RESTARTS AGE\nweb-1 1/1 Running 0 3d')).toEqual([]);
    expect(found('commit 3f786850e387550fdab836ed7e6dc881de23001b')).toEqual([]);
  });
});

describe('redact', () => {
  it('replaces each secret and keeps the rest', () => {
    expect(redact(`id=${AKID} secret=${SECRET}`)).toBe(`id=${REDACTED} secret=${REDACTED}`);
    expect(redact('nothing here')).toBe('nothing here');
  });
});
