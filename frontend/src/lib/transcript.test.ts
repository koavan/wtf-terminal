import { describe, expect, it } from 'vitest';
import { exportName, fence, markdownTranscript } from './transcript';

describe('markdownTranscript', () => {
  const at = new Date(2026, 9, 9, 23, 50, 0);
  it('lists each command with how it went and its output', () => {
    const md = markdownTranscript(
      [
        { text: 'kubectl get pods', output: 'NAME READY\nweb-1 1/1', startedAt: new Date(2026, 9, 9, 23, 45, 1), exitCode: 0, durationMs: 1200 },
        { text: 'false', output: '', exitCode: 1, durationMs: 5 },
        { text: 'tail -f log', output: 'x', exitCode: undefined },
        { text: 'old', output: null, exitCode: 0 },
      ],
      { title: 'Incident 42', cwd: '~/ops', exportedAt: at },
    );
    expect(md).toContain('# Incident 42');
    expect(md).toContain('~/ops · 4 commands');
    expect(md).toContain('## 1. `kubectl get pods`\n\n✓ succeeded · started 23:45:01 · took 1s\n\n```text\nNAME READY\nweb-1 1/1\n```');
    expect(md).toContain('## 2. `false`\n\n✗ exit 1 · took 0.0s\n\n_No output._');
    expect(md).toContain('## 3. `tail -f log`\n\nstill running');
    expect(md).toContain('_Output no longer in the scrollback._');
  });

  it('keeps backticks in commands and output from breaking the Markdown', () => {
    const md = markdownTranscript([{ text: 'echo `date`', output: 'a ``` b', exitCode: 0 }], { title: 't', cwd: '', exportedAt: at });
    expect(md).toContain('## 1. `` echo `date` ``');
    expect(md).toContain('````text\na ``` b\n````');
  });
});

describe('fence', () => {
  it('is longer than any backticks inside', () => {
    expect(fence('plain')).toBe('```');
    expect(fence('````')).toBe('`````');
  });
});

describe('exportName', () => {
  it('makes a file name from the title and time', () => {
    expect(exportName('kubectl get pods -n shop', new Date(2026, 9, 9, 23, 5))).toBe('wtf-kubectl-get-pods-n-shop-2026-10-09-2305');
    expect(exportName('~/', new Date(2026, 0, 1, 0, 0))).toBe('wtf-terminal-2026-01-01-0000');
  });
});
