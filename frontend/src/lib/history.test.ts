import { beforeEach, describe, expect, it } from 'vitest';
import { history, MAX_ENTRIES } from './history.svelte';

beforeEach(() => {
  for (const id of [1, 2]) history.clear(id);
});

describe('history', () => {
  it('records a command and its completion', () => {
    history.started(1, 7, 'npm test', '/api', 1000);
    expect(history.of(1)[0]).toMatchObject({ id: 7, command: 'npm test', cwd: '/api', finishedAt: null });
    history.finished(1, 1, 2500, 3500);
    expect(history.of(1)[0]).toMatchObject({ finishedAt: 3500, durationMs: 2500, exitCode: 1 });
  });

  it('ignores blank commands and stray completions', () => {
    history.started(1, 1, '   ', '/');
    history.finished(1, 0, 5);
    history.finished(2, 0, 5);
    expect(history.of(1)).toEqual([]);
    expect(history.of(2)).toEqual([]);
  });

  it('a second completion does not overwrite a finished command', () => {
    history.started(1, 1, 'ls', '/', 0);
    history.finished(1, 0, 10, 10);
    history.finished(1, 2, 99, 99);
    expect(history.of(1)[0]).toMatchObject({ exitCode: 0, finishedAt: 10 });
  });

  it('keeps the newest entries', () => {
    for (let i = 0; i < MAX_ENTRIES + 5; i++) history.started(1, i, `c${i}`, '/', i);
    const list = history.of(1);
    expect(list).toHaveLength(MAX_ENTRIES);
    expect(list[0].command).toBe('c5');
  });

  it('merges panes into one timeline', () => {
    history.started(1, 1, 'a', '/', 10);
    history.started(2, 1, 'b', '/', 5);
    history.started(1, 2, 'c', '/', 20);
    expect(history.merged([1, 2]).map((e) => e.command)).toEqual(['b', 'a', 'c']);
  });
});
