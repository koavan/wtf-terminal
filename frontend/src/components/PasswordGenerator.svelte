<script module lang="ts">
  import { DEFAULT_OPTIONS, type PasswordOptions } from '../lib/passwords';

  /** The options stay while wtf runs, so the tab reopens as it was left. Passwords don't. */
  const options = $state<PasswordOptions>({ ...DEFAULT_OPTIONS });
</script>

<script lang="ts">
  import Icon from './Icon.svelte';
  import { copySecret, entropyBits, generatePassword, MAX_LENGTH, MIN_LENGTH } from '../lib/passwords';
  import { redaction } from '../lib/redaction.svelte';
  import { toast } from '../lib/toast.svelte';

  // A random password, made on this computer and never saved.

  let password = $state('');
  /** Shown despite Hide Secrets, after a click. */
  let revealed = $state(false);

  const bits = $derived(entropyBits(options));
  const noClass = $derived(!options.lower && !options.upper && !options.digits && !options.symbols);

  function regenerate() {
    password = generatePassword(options);
    revealed = false;
  }

  // A new password whenever the options change.
  $effect(() => {
    void [options.length, options.lower, options.upper, options.digits, options.symbols, options.avoidAmbiguous];
    regenerate();
  });

  async function copy() {
    if (!password) return;
    try {
      await copySecret(password);
      toast.show('Copied. The clipboard clears in 30 seconds');
    } catch (e) {
      toast.show(String(e), 6000);
    }
  }

  const hidden = $derived(redaction.on && !revealed);
</script>

<div class="password">
  <div class="output" class:hidden title={hidden ? 'Hidden while Hide Secrets is on' : ''}>
    {#if noClass}
      <span class="none">Choose at least one kind of character</span>
    {:else if hidden}
      <span class="dots">{'•'.repeat(Math.min(password.length, 32))}</span>
    {:else}
      <span class="text">{password}</span>
    {/if}
  </div>

  <div class="actions">
    <button class="primary" onclick={copy} disabled={!password}><Icon name="copy" size={12} /> Copy</button>
    <button class="secondary" onclick={regenerate} disabled={noClass} title="Make another"><Icon name="refresh" size={12} /> New</button>
    {#if hidden}<button class="secondary" onclick={() => (revealed = true)}><Icon name="eye" size={12} /> Show</button>{/if}
  </div>
  <p class="hint strength">{noClass ? '' : `About ${bits} bits of randomness`}</p>

  <div class="field">
    <span>Length: {options.length}</span>
    <div class="length">
      <input type="range" min={MIN_LENGTH} max={64} bind:value={options.length} aria-label="Length" />
      <input
        class="num"
        type="number"
        min={MIN_LENGTH}
        max={MAX_LENGTH}
        value={options.length}
        onchange={(e) => {
          const n = Number((e.currentTarget as HTMLInputElement).value);
          options.length = Number.isFinite(n) ? Math.min(MAX_LENGTH, Math.max(MIN_LENGTH, Math.round(n))) : DEFAULT_OPTIONS.length;
        }}
        aria-label="Length (number)"
      />
    </div>
  </div>

  <label class="check"><input type="checkbox" bind:checked={options.lower} /> Lowercase <code>a–z</code></label>
  <label class="check"><input type="checkbox" bind:checked={options.upper} /> Uppercase <code>A–Z</code></label>
  <label class="check"><input type="checkbox" bind:checked={options.digits} /> Digits <code>0–9</code></label>
  <label class="check"><input type="checkbox" bind:checked={options.symbols} /> Symbols <code>!#$%&amp;*+-.:;=?@^_~ …</code></label>
  <label class="check"><input type="checkbox" bind:checked={options.avoidAmbiguous} /> Leave out look-alikes <code>I l 1 O 0 o |</code></label>

  <p class="hint">
    Made on this computer from the system's secure random source. Never saved or sent anywhere. No quotes, spaces or
    backslashes, so it pastes into shells and config files as-is.
  </p>
</div>

<style>
  .output {
    display: flex;
    align-items: center;
    min-height: 44px;
    padding: 10px 12px;
    border: 1px solid var(--hairline);
    border-radius: 9px;
    background: var(--identity);
    color: var(--text);
    font: 500 14px/1.45 'JetBrains Mono Variable', monospace;
    overflow-wrap: anywhere;
    -webkit-user-select: all;
    user-select: all;
  }
  .output.hidden {
    -webkit-user-select: none;
    user-select: none;
  }
  .none {
    color: var(--chrome-muted);
    font: 12px system-ui, sans-serif;
  }
  .dots {
    letter-spacing: 1px;
    color: var(--chrome-muted);
  }
  .actions button {
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
  .strength {
    margin: 0 0 12px;
  }
  .length {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .length input[type='range'] {
    flex: 1;
    min-width: 0;
    margin: 0;
    accent-color: var(--accent);
  }
  .length input.num {
    width: 64px;
    flex: none;
  }
  .check code {
    color: var(--chrome-muted);
  }
  input[type='checkbox'] {
    accent-color: var(--accent);
  }
</style>
