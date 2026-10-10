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
    expect(events.onCommandStart).toHaveBeenCalledWith('false', 1);
    expect(events.onCommandEnd).toHaveBeenCalledWith(1, expect.any(Number));
  });

  it('copies the last command output, joining soft-wrapped lines', async () => {
    const { tracker, write } = setup();
    const long = 'x'.repeat(50); // wraps at 40 columns
    await write(run('ls', 'one\r\ntwo\r\n') + run('cat', long + '\r\nend\r\n') + A + '$ ');
    expect(tracker.lastOutput()).toBe(`${long}\nend`);
  });

  it('lists every command with its output and how it went, for a transcript', async () => {
    const { tracker, write } = setup();
    await write(run('ls', 'a\r\nb\r\n') + run('false', '', 1) + A + '$ ');
    const t = tracker.transcript();
    expect(t.map((b) => [b.text, b.output, b.exitCode])).toEqual([
      ['ls', 'a\nb', 0],
      ['false', '', 1],
    ]);
    expect(t[0].startedAt).toBeInstanceOf(Date);
  });

  it('reports the context variables the shell sends with a prompt', async () => {
    const { term, write } = setup();
    const onEnv = vi.fn();
    new PromptTracker(term as unknown as Terminal, { onPrompt() {}, onCommandStart() {}, onCommandEnd() {}, onEnv });
    await write('\x1b]633;P;WtfEnv=PWD=/tmp\\x0aAWS_PROFILE=a\\x3bb\\x0a\x07' + A + '$ ');
    expect(onEnv).toHaveBeenCalledWith('PWD=/tmp\nAWS_PROFILE=a;b\n');
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

  it('gives each command an id and can scroll back to it', async () => {
    const { term, events, tracker, write } = setup(4);
    let out = '';
    for (let i = 0; i < 6; i++) out += run(`echo ${i}`, `${i}\r\n${i}\r\n`);
    await write(out + A + '$ ');
    const ids = events.onCommandStart.mock.calls.map((c) => c[1]);
    expect(ids).toEqual([1, 2, 3, 4, 5, 6]);
    expect(tracker.reveal(2)).toBe(true);
    expect(term.buffer.active.viewportY).toBe(3); // command 2's prompt is on line 3
    expect(tracker.reveal(99)).toBe(false);
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

describe('command blocks', () => {
  it('finds the command a line belongs to, from its prompt to the next', async () => {
    const { tracker, write } = setup(10);
    await write(run('ls', 'a\r\nb\r\n') + run('false', 'oops\r\n', 1) + A + '$ ');
    // Lines: 0 "$ ls", 1 a, 2 b, 3 "$ false", 4 oops, 5 "$ "
    expect(tracker.blockAt(0)).toBe(1);
    expect(tracker.blockAt(2)).toBe(1);
    expect(tracker.blockAt(3)).toBe(2);
    expect(tracker.blockAt(4)).toBe(2);
    expect(tracker.blockAt(5)).toBeNull(); // the prompt waiting for a command
  });

  it('knows each command’s text, status and output', async () => {
    const { tracker, write } = setup(10);
    await write(run('ls', 'a\r\nb\r\n') + run('false', 'oops\r\n', 1) + A + '$ ');
    expect(tracker.info(1)).toMatchObject({ text: 'ls', running: false, exitCode: 0, hasOutput: true });
    expect(tracker.info(2)).toMatchObject({ text: 'false', exitCode: 1 });
    expect(tracker.info(2)!.durationMs).toBeGreaterThanOrEqual(0);
    expect(tracker.info(2)!.finishedAt).toBeInstanceOf(Date);
    expect(tracker.output(1)).toBe('a\nb');
    expect(tracker.output(2)).toBe('oops');
    expect(tracker.info(99)).toBeNull();
  });

  it('gives a running command’s output so far', async () => {
    const { tracker, write } = setup(10);
    await write(`${A}$ make\r\n${E('make')}${C}step 1\r\nstep 2`);
    expect(tracker.info(1)).toMatchObject({ text: 'make', running: true });
    expect(tracker.output(1)).toBe('step 1\nstep 2');
  });

  it('selects a command’s output lines', async () => {
    const { term, tracker, write } = setup(10);
    const selectLines = vi.fn();
    (term as unknown as { selectLines: typeof selectLines }).selectLines = selectLines;
    await write(run('ls', 'a\r\nb\r\n') + A + '$ ');
    expect(tracker.selectOutput(1)).toBe(true);
    expect(selectLines).toHaveBeenCalledWith(1, 2);
  });
});
