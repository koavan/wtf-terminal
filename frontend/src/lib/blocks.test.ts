import { describe, expect, it, vi } from 'vitest';
import { blockMenu, blockSummary, commandAndOutput, terminalMenu } from './blocks';

const actions = () => ({ copyCommand: vi.fn(), copyOutput: vi.fn(), copyBoth: vi.fn(), selectOutput: vi.fn(), pinOutput: vi.fn(), runAgain: vi.fn(), saveSnippet: vi.fn() });
const labels = (entries: ReturnType<typeof blockMenu>) => entries.map((e) => (typeof e === 'string' ? e : 'label' in e ? e.label : `# ${e.heading}`));

describe('command block menus', () => {
  it('sums a command up', () => {
    expect(blockSummary({ id: 1, text: 'kubectl get pods', running: false, exitCode: 1, durationMs: 3200, hasOutput: true })).toBe(
      'kubectl get pods · ✗ exit 1 · 3s',
    );
    expect(blockSummary({ id: 1, text: 'make', running: true, hasOutput: true })).toBe('make · running');
    expect(blockSummary({ id: 1, text: 'x'.repeat(60), running: false, exitCode: 0, hasOutput: false })).toMatch(/^x{47}… · ✓$/);
  });

  it('offers what applies to the command', () => {
    const done = blockMenu({ id: 1, text: 'ls', running: false, exitCode: 0, durationMs: 5, hasOutput: true }, actions());
    expect(labels(done)).toEqual(['# ls · ✓ · 0.0s', 'Copy Command', 'Copy Output', 'Copy Command and Output', 'Select Output', 'Pin Output', 'separator', 'Run Again', 'Save as Snippet…']);
    const running = blockMenu({ id: 1, text: 'make', running: true, hasOutput: true }, actions());
    expect(labels(running)).not.toContain('Run Again');
    expect(labels(running)).toContain('Pin Output (follows it)');
    const gone = blockMenu({ id: 1, text: 'ls', running: false, exitCode: 0, hasOutput: false }, actions());
    expect(labels(gone)).toEqual(['# ls · ✓', 'Copy Command', 'separator', 'Run Again', 'Save as Snippet…']);
  });

  it('has the general terminal actions, Copy only with a selection', () => {
    const a = { copy: vi.fn(), pinSelection: vi.fn(), paste: vi.fn(), selectAll: vi.fn(), clear: vi.fn(), find: vi.fn() };
    const has = (sel: boolean, label: string) => terminalMenu(sel, a).some((e) => typeof e !== 'string' && 'label' in e && e.label === label);
    expect(has(false, 'Copy')).toBe(false);
    expect(has(false, 'Pin Selection')).toBe(false);
    expect(terminalMenu(true, a)[0]).toMatchObject({ label: 'Copy' });
    expect(has(true, 'Pin Selection')).toBe(true);
  });

  it('copies a command with its output as it looked', () => {
    expect(commandAndOutput('ls', 'a\nb')).toBe('$ ls\na\nb');
    expect(commandAndOutput('true', '')).toBe('$ true');
  });
});
