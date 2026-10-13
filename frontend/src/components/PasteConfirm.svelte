<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { Service as ConfigService } from '../../bindings/github.com/dayanraj/wtf/internal/config';
  import Dialog from './Dialog.svelte';
  import { overlay } from '../lib/overlay.svelte';
  import { pasteFacts, showControls, withoutFinalNewline } from '../lib/paste';
  import { redaction } from '../lib/redaction.svelte';
  import { toast } from '../lib/toast.svelte';

  // A paste with more than one line, a final newline or control characters
  // (lib/paste), shown as it would arrive before anything reaches the shell.

  let { text, bracketed, panes, onPaste }: {
    text: string;
    /** The program asked for bracketed paste: it gets the text as one paste and runs nothing until Enter. */
    bracketed: boolean;
    /** Panes it goes to: more than one while broadcasting. */
    panes: number;
    onPaste: (text: string) => void;
  } = $props();

  const raw = untrack(() => text);
  const f = pasteFacts(raw);
  const shown = $derived(showControls(redaction.text(raw)));
  let primary = $state<HTMLButtonElement>();

  const title = f.lines > 1 ? `Paste ${f.lines} lines?` : f.controls ? 'Paste text with control characters?' : 'Paste with a newline?';
  const has = [
    f.controls ? `${f.controls} control ${f.controls === 1 ? 'character' : 'characters'} (shown as ␛ and the like)` : '',
    f.finalNewline ? 'a newline at the end' : '',
  ].filter(Boolean);
  const subtitle = [
    has.length ? `It has ${has.join(' and ')}.` : '',
    untrack(() => bracketed)
      ? 'The program here takes it as one paste and runs nothing until you press Enter.'
      : 'The program here hasn’t asked for bracketed paste: a shell runs each line as it arrives.',
    untrack(() => panes) > 1 ? `Broadcast input is on: it goes to ${untrack(() => panes)} panes.` : '',
  ]
    .filter(Boolean)
    .join(' ');

  function paste(t: string) {
    // Props are read live from overlay state, which closing clears.
    const done = onPaste;
    overlay.close();
    done(t);
  }

  /** Pastes this one, and the next ones without asking. */
  function dontAsk() {
    ConfigService.SetConfirmPaste(false)
      .then(() => toast.show('Pastes go straight in now. Toggle Paste Confirmation in the palette to change it'))
      .catch((e) => toast.show(`Couldn't save the setting: ${e}`));
    paste(raw);
  }

  onMount(() => primary?.focus());
</script>

<Dialog {title} {subtitle} width={600} onsubmit={() => paste(raw)}>
  <pre class="preview">{shown}</pre>

  {#snippet buttons()}
    <button class="secondary" onclick={() => overlay.close()}>Cancel</button>
    <button class="secondary" title="Turns off confirm-paste. Toggle Paste Confirmation in the palette turns it back on." onclick={dontAsk}>Don’t Ask Again</button>
    <span class="spacer"></span>
    {#if f.finalNewline}
      <button class="secondary" onclick={() => paste(withoutFinalNewline(raw))}>Paste Without Final Newline</button>
    {/if}
    <button class="primary" bind:this={primary} onclick={() => paste(raw)}>Paste</button>
  {/snippet}
</Dialog>

<style>
  .preview {
    max-height: 50vh;
    margin: 0;
    padding: 10px 12px;
    overflow: auto;
    border-radius: 8px;
    background: var(--identity);
    font: 12px/1.5 'JetBrains Mono Variable', monospace;
    white-space: pre;
    -webkit-user-select: text;
    user-select: text;
  }
</style>
