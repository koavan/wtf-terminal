import { describe, expect, it, vi } from 'vitest';
import { Terminal as Headless } from '@xterm/headless';
import type { Terminal } from '@xterm/xterm';
import { PromptTracker, unescapeCommand } from './prompts';
import { formatDuration } from './toast.svelte';

const A = '\x1b]133;A\x07';
const C = '\x1b]133;C\x07';
const D = (code: number) => `\x1b]133;D;${code}\x07`;
const E = (cmd: string) => `\x1b]633;E;${cmd}\x07`;

function setup(rows = 5) {
  const term = new Headless({ cols: 40, rows, allowProposedApi: true, scrollback: 1000 });
  const events = { onPrompt: vi.fn(), onCommandStart: vi.fn(), onCommandEnd: vi.fn() };
  const tracker = new PromptTracker(term as unknown as Terminal, events);
  const write = (s: string) => new Promise<void>((resolve) => term.write(s, resolve));
  return { term, events, tracker, write };
}

/** What a shell with the integration prints for one command. */
const run = (cmd: string, output: string, code = 0) => `${A}$ ${cmd}\r\n${E(cmd)}${C}${output}${D(code)}`;

describe('PromptTracker', () => {
  it('reports prompts, command text and exit codes', async () => {
    const { events, write } = setup();
    await write(run('false', '', 1) + A + '$ ');
    expect(events.onPrompt).toHaveBeenCalledTimes(2);
    expect(events.onCommandStart).toHaveBeenCalledWith('false');
    expect(events.onCommandEnd).toHaveBeenCalledWith(1, expect.any(Number));
  });

  it('copies the last command output, joining soft-wrapped lines', async () => {
    const { tracker, write } = setup();
    const long = 'x'.repeat(50); // wraps at 40 columns
    await write(run('ls', 'one\r\ntwo\r\n') + run('cat', long + '\r\nend\r\n') + A + '$ ');
    expect(tracker.lastOutput()).toBe(`${long}\nend`);
  });

  it('returns null before any command finished', async () => {
    const { tracker, write } = setup();
    await write(A + '$ ');
    expect(tracker.lastOutput()).toBeNull();
  });

  it('jumps between prompts in the scrollback', async () => {
    const { term, tracker, write } = setup(4);
    let out = '';
    for (let i = 0; i < 6; i++) out += run(`echo ${i}`, `${i}\r\n${i}\r\n`);
    await write(out + A + '$ ');
    // Each command takes 3 rows: prompts sit on lines 0, 3, 6, ... 18. At
    // the bottom the view starts at line 15, itself a prompt, so "previous"
    // means the one above it.
    const bottom = term.buffer.active.viewportY;
    expect(bottom).toBe(15);
    tracker.jump(-1);
    expect(term.buffer.active.viewportY).toBe(12);
    tracker.jump(-1);
    expect(term.buffer.active.viewportY).toBe(9);
    tracker.jump(1);
    expect(term.buffer.active.viewportY).toBe(12);
    tracker.jump(1);
    tracker.jump(1);
    expect(term.buffer.active.viewportY).toBe(bottom);
  });

  it('ignores a stray D without a started command', async () => {
    const { events, write } = setup();
    await write(A + D(0));
    expect(events.onCommandEnd).not.toHaveBeenCalled();
  });
});

describe('helpers', () => {
  it('unescapes command lines', () => {
    expect(unescapeCommand(String.raw`echo 'a\x3bb\\c'`)).toBe(`echo 'a;b\\c'`);
    expect(unescapeCommand(String.raw`a\x0ab`)).toBe('a\nb');
  });

  it('formats durations', () => {
    expect(formatDuration(420)).toBe('0.4s');
    expect(formatDuration(12_300)).toBe('12s');
    expect(formatDuration(72_000)).toBe('1m 12s');
    expect(formatDuration(3_780_000)).toBe('1h 3m');
  });
});
