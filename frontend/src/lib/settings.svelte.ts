import { Events } from '@wailsio/runtime';
import {
  Service as ConfigService,
  type Config,
  type Resolved,
  type Theme,
} from '../../bindings/github.com/dayanraj/wtf/internal/config';
import { redaction } from './redaction.svelte';
import { toast } from './toast.svelte';

export type { Theme } from '../../bindings/github.com/dayanraj/wtf/internal/config';
export type Scheme = 'dark' | 'light';

const MIN_FONT_SIZE = 6;
const MAX_FONT_SIZE = 72;

const darkQuery = window.matchMedia('(prefers-color-scheme: dark)');

/** The config file (see internal/config), plus per-session state like zoom. */
class Settings {
  config = $state.raw<Config>(null!);
  #dark = $state.raw<Theme>(null!);
  #light = $state.raw<Theme>(null!);
  /** System appearance. */
  scheme = $state<Scheme>(darkQuery.matches ? 'dark' : 'light');
  /** A theme shown temporarily while browsing themes in the palette. */
  preview = $state.raw<Theme | null>(null);
  /** Current text size: starts at the configured size, ⌘+/⌘- change it. */
  fontSize = $state(13);

  #lastProblems = '';

  get theme(): Theme {
    return this.preview ?? (this.scheme === 'dark' ? this.#dark : this.#light);
  }

  /** Loads the config and follows changes to it. Call once before mounting. */
  async load() {
    darkQuery.addEventListener('change', (e) => (this.scheme = e.matches ? 'dark' : 'light'));
    this.#apply(await ConfigService.Current(), true);
    Events.On('config:changed', (e) => this.#apply(e.data, false));
  }

  #apply(r: Resolved, initial: boolean) {
    const sizeChanged = !initial && r.config.fontSize !== this.config.fontSize;
    // Hide Secrets starts as the setting says, and turns on when it's switched on. Switching it
    // off only changes the next start: secrets never show by surprise in the middle of a share.
    if (initial) redaction.on = r.config.redactSecrets;
    else if (r.config.redactSecrets && !this.config.redactSecrets) redaction.on = true;
    this.config = r.config;
    this.#dark = r.dark;
    this.#light = r.light;
    this.preview = null;
    if (initial || sizeChanged) this.fontSize = r.config.fontSize;

    const list = r.problems ?? [];
    const problems = list.join('\n');
    if (problems && problems !== this.#lastProblems) {
      const more = list.length > 1 ? ` (+${list.length - 1} more)` : '';
      toast.show(`Settings: ${list[0]}${more}`, 6000);
    }
    this.#lastProblems = problems;
  }

  bigger() {
    this.setFontSize(this.fontSize + 1);
  }
  smaller() {
    this.setFontSize(this.fontSize - 1);
  }
  resetFont() {
    this.fontSize = this.config.fontSize;
  }
  setFontSize(size: number) {
    if (Number.isFinite(size)) this.fontSize = Math.round(Math.max(MIN_FONT_SIZE, Math.min(MAX_FONT_SIZE, size)));
  }
}

export const settings = new Settings();
