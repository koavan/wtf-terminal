import { describe, expect, it } from 'vitest';
import { fuzzyMatch } from './fuzzy';

const rank = (q: string, items: string[]) =>
  items
    .map((t) => ({ t, m: fuzzyMatch(q, t) }))
    .filter((r) => r.m)
    .sort((a, b) => b.m!.score - a.m!.score)
    .map((r) => r.t);

describe('fuzzyMatch', () => {
  it('matches subsequences case-insensitively', () => {
    expect(fuzzyMatch('nt', 'New Tab')?.indices).toEqual([0, 4]);
    expect(fuzzyMatch('xyz', 'New Tab')).toBeNull();
  });

  it('prefers word starts', () => {
    expect(rank('sr', ['Smaller Text', 'Split Right'])[0]).toBe('Split Right');
    expect(rank('ct', ['Clear Buffer', 'Close Tab'])[0]).toBe('Close Tab');
  });

  it('prefers consecutive runs', () => {
    expect(rank('split', ['Select Pane Left In Tab', 'Split Down'])[0]).toBe('Split Down');
  });

  it('ignores spaces in the query', () => {
    expect(fuzzyMatch('new tab', 'New Tab')).not.toBeNull();
  });
});
