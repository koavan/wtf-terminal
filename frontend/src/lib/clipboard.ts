import { Clipboard } from '@wailsio/runtime';
import { settings } from './settings.svelte';
import { toast } from './toast.svelte';

// Secrets wtf revealed — a Kubernetes secret's value, decoded base64, a JWT,
// a generated password — leave the clipboard after `clipboard-clear`
// seconds, unless something else was copied meanwhile.

/** Seconds a copied secret stays on the clipboard; 0 keeps it. */
export function clearSeconds(): number {
  return settings.config?.clipboardClear ?? 30;
}

/** Copies a secret, says so (and when it goes), and empties the clipboard later if it still holds it. */
export async function copySecret(text: string, what: string) {
  await Clipboard.SetText(text);
  const s = clearSeconds();
  if (s > 0) {
    setTimeout(async () => {
      try {
        if ((await Clipboard.Text()) === text) await Clipboard.SetText('');
      } catch {
        // the clipboard is busy or gone; nothing to clean
      }
    }, s * 1000);
  }
  toast.show(s > 0 ? `Copied ${what}. The clipboard clears in ${s} ${s === 1 ? 'second' : 'seconds'}` : `Copied ${what}`);
}
