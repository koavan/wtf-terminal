<script lang="ts">
  import { untrack } from 'svelte';
  import { Clipboard } from '@wailsio/runtime';
  import Dialog from './Dialog.svelte';
  import { overlay } from '../lib/overlay.svelte';
  import { toast } from '../lib/toast.svelte';

  // JSON from a line of output, pretty-printed (the terminal's right-click
  // menu → Pretty-print JSON), with keys, strings and numbers colored.

  let { text }: { text: string } = $props();
  const json = untrack(() => text);

  type Part = { text: string; cls: string };

  /** The pretty JSON in colored pieces; JSON.stringify's output is regular enough for a regex. */
  const parts: Part[] = [];
  const re = /("(?:[^"\\]|\\.)*")(\s*:)?|\b(true|false|null)\b|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/g;
  let at = 0;
  for (let m = re.exec(json); m; m = re.exec(json)) {
    if (m.index > at) parts.push({ text: json.slice(at, m.index), cls: '' });
    if (m[1]) {
      parts.push({ text: m[1], cls: m[2] ? 'key' : 'str' });
      if (m[2]) parts.push({ text: m[2], cls: '' });
    } else parts.push({ text: m[0], cls: m[3] ? 'lit' : 'num' });
    at = m.index + m[0].length;
  }
  if (at < json.length) parts.push({ text: json.slice(at), cls: '' });

  const lines = json.split('\n').length;

  function copy() {
    Clipboard.SetText(json).then(() => toast.show('Copied JSON'));
  }
</script>

<Dialog title="JSON" subtitle="{lines} lines, pretty-printed from the terminal's output." width={640}>
  <pre class="json">{#each parts as p, i (i)}{#if p.cls}<span class={p.cls}>{p.text}</span>{:else}{p.text}{/if}{/each}</pre>

  {#snippet buttons()}
    <button class="secondary" onclick={copy}>Copy</button>
    <span class="spacer"></span>
    <button class="primary" onclick={() => overlay.close()}>Done</button>
  {/snippet}
</Dialog>

<style>
  .json {
    max-height: 60vh;
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
  .key {
    color: var(--g-blue);
  }
  .str {
    color: var(--g-green);
  }
  .num {
    color: var(--g-orange);
  }
  .lit {
    color: var(--g-purple);
  }
</style>
