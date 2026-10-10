import { beforeEach, describe, expect, it } from 'vitest';
import { MAX_PIN_CHARS, MAX_PINS, pins, type PinDraft } from './pins.svelte';

const cmd = (over: Partial<PinDraft> = {}): PinDraft => ({
  title: 'kubectl get pods', text: 'NAME READY', paneId: 1, blockId: 7, cwd: '/w', running: false, exitCode: 0, durationMs: 900, ...over,
});

describe('pins', () => {
  beforeEach(() => pins.clear());

  it('puts the newest pin first and keeps a limited number', () => {
    for (let i = 0; i < MAX_PINS + 3; i++) pins.add(cmd({ blockId: i, title: `c${i}` }));
    expect(pins.list).toHaveLength(MAX_PINS);
    expect(pins.list[0].title).toBe(`c${MAX_PINS + 2}`);
  });

  it("doesn't pin the same command twice, but brings its pin up to date", () => {
    pins.add(cmd({ text: 'old' }));
    const p = pins.add(cmd({ text: 'new' }));
    expect(pins.list).toHaveLength(1);
    expect(p.text).toBe('new');
    // Selections are separate pins every time.
    pins.add(cmd({ blockId: null, title: 'Selection' }));
    pins.add(cmd({ blockId: null, title: 'Selection' }));
    expect(pins.list).toHaveLength(3);
  });

  it('follows a running command until it finishes', () => {
    pins.add(cmd({ running: true, text: 'a', exitCode: undefined, durationMs: undefined }));
    expect(pins.live).toHaveLength(1);
    pins.update(1, 7, 'a\nb');
    expect(pins.list[0].text).toBe('a\nb');
    pins.update(1, 7, 'a\nb\nc', { exitCode: 2, durationMs: 4000 });
    expect(pins.list[0]).toMatchObject({ text: 'a\nb\nc', running: false, exitCode: 2, durationMs: 4000 });
    expect(pins.live).toHaveLength(0);
    // Finished pins don't change any more.
    pins.update(1, 7, 'other');
    expect(pins.list[0].text).toBe('a\nb\nc');
  });

  it('stops following when the pane closes, and keeps the pin', () => {
    pins.add(cmd({ running: true }));
    pins.paneGone(1);
    expect(pins.list[0].running).toBe(false);
    expect(pins.list).toHaveLength(1);
  });

  it('keeps the end of very long output', () => {
    const p = pins.add(cmd({ text: 'x'.repeat(MAX_PIN_CHARS) + 'END' }));
    expect(p.truncated).toBe(true);
    expect(p.text).toHaveLength(MAX_PIN_CHARS);
    expect(p.text.endsWith('END')).toBe(true);
  });

  it('unpins', () => {
    const p = pins.add(cmd());
    pins.remove(p.id);
    expect(pins.list).toEqual([]);
  });
});
