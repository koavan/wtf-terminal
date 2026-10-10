import { redact } from './secrets';

// Hide Secrets, for sharing your screen: while on, secrets found in the
// output are covered on screen (lib/secretmask), and text that leaves a
// terminal — copies, pins, command names in the address bar, tab titles,
// History and notifications — has them replaced with [REDACTED]. The
// output itself is untouched. One switch for every window's terminals;
// `redact-secrets` in config.toml sets it at startup.

class Redaction {
  on = $state(false);

  /** The text as it may be shown or copied now. */
  text(s: string): string {
    return this.on && s ? redact(s) : s;
  }
}

export const redaction = new Redaction();
