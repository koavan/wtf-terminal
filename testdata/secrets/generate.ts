// Writes cases.json: what frontend/src/lib/secrets.ts finds in each input.
// The TypeScript (frontend) and Swift (macos) tests both check against it,
// so the two implementations can't drift apart. After changing the rules
// or the inputs, run:  node testdata/secrets/generate.ts
import { writeFileSync } from 'node:fs';
import { findSecrets, redact } from '../../frontend/src/lib/secrets.ts';

// Made-up values in the shapes the real ones have.
const AKID = 'AKIAABCDEFGHIJKLMNOP';
const SECRET = 'wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY';
const JWT = 'eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U';
const KEY = '-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\n-----END OPENSSH PRIVATE KEY-----';

const inputs = [
  `aws_access_key_id = ${AKID}`,
  `aws_secret_access_key = ${SECRET}`,
  `export AWS_SECRET_ACCESS_KEY="${SECRET}"`,
  `"SecretAccessKey": "${SECRET}",`,
  `aws_session_token = ${'IQoJb3JpZ2luX2VjEJr'.repeat(6)}`,
  'use ghp_abcdefghijklmnopqrstuvwxyz0123456789 here',
  'use github_pat_11ABCDEFG0123456789_abcdefghijklmnop here',
  'use glpat-abcdefghij0123456789 here',
  'use xoxb-1234567890-abcdefghij here',
  'curl https://hooks.slack.com/services/T000/B000/XXXXXXXX -d @msg',
  'use sk_live_abcdefghij0123456789ab here',
  'use AIzaSyA1234567890abcdefghijklmnopqrstuv here',
  'use sk-ant-api03-abcdefghij0123456789 here',
  'use npm_abcdefghijklmnopqrstuvwxyz0123456789 here',
  `token ${JWT}`,
  `Authorization: Bearer ${JWT}`,
  'DATABASE_URL=postgres://app:s3cr3t-pw@db:5432/app',
  'Authorization: Bearer abcdefghijklmnopqrstuvwxyz012345',
  'password: hunter2222',
  'export GITHUB_TOKEN=abc123def456',
  '"client_secret": "q8~Zz.Y1"',
  'db.password=correcthorse',
  "PASSWORD='quoted-value-1'",
  'password: null',
  'token: 1220 bytes',
  'api_key: ${API_KEY}',
  'password: ********',
  'password=<redacted>',
  'password: 12345678',
  'the password is set',
  'data:\n  username: aHVudGVyMjIyMg==\n  sha: 3f786850e387550fdab836ed7e6dc881de23001b\n  note: averylongwordwithoutdigitsorcaps\n  kind: ConfigMapListItems',
  `before\n${KEY}\nafter`,
  '-----BEGIN RSA PRIVATE KEY-----\nMIIEow',
  '-----BEGIN PUBLIC KEY-----',
  'NAME READY STATUS RESTARTS AGE\nweb-1 1/1 Running 0 3d',
  'commit 3f786850e387550fdab836ed7e6dc881de23001b',
  `id=${AKID} secret=${SECRET}`,
  `two keys: ${AKID} and AKIAZZZZZZZZZZZZZZZZ`,
  'unicode café ✓ password: pässwörd99 done',
  '',
  'nothing here',
];

const cases = inputs.map((text) => ({
  text,
  found: findSecrets(text).map((s) => ({ kind: s.kind, start: s.start, end: s.end })),
  redacted: redact(text),
}));
writeFileSync(new URL('./cases.json', import.meta.url), JSON.stringify(cases, null, 2) + '\n');
console.log(`wrote ${cases.length} cases`);
