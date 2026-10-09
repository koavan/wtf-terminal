import { tick } from 'svelte';
import type { Host, HostDraft } from './ssh.svelte';
import type { Key, TunnelStatus } from './sshAdmin.svelte';
import type { GroupColor } from './tabs.svelte';
import { activeTerminal } from './terminals';

export type MenuEntry =
  | { label: string; action: () => void; hint?: string; color?: GroupColor; danger?: boolean }
  | { heading: string }
  | 'separator';

export type OverlayState =
  | { kind: 'menu'; x: number; y: number; entries: MenuEntry[] }
  | { kind: 'group'; groupId: number; anchor: DOMRect }
  | { kind: 'palette'; mode: PaletteMode }
  | { kind: 'overview' }
  | ({ kind: 'confirm' } & ConfirmRequest)
  | { kind: 'sshHost'; host: Host | null; prefill?: Partial<HostDraft> }
  | { kind: 'keyGen' }
  | { kind: 'keyInstall'; key: Key | null; host: Host | null }
  | { kind: 'hostKeys'; host: Host }
  | { kind: 'tunnel'; tunnel: TunnelStatus | null; host: Host | null }
  | { kind: 'transfer'; host: Host; direction: 'upload' | 'download'; locals: string[] };

export interface ConfirmRequest {
  title: string;
  message: string;
  /** Things that will be lost, e.g. running programs. */
  items: string[];
  confirmLabel: string;
  onConfirm: () => void;
}

export type PaletteMode = 'commands' | 'themes';

/** The one popover (context menu, group editor, palette) that can be open at a time. */
class Overlay {
  current = $state<OverlayState | null>(null);

  togglePalette(mode: PaletteMode = 'commands') {
    if (this.current?.kind === 'palette' && this.current.mode === mode) this.close();
    else this.current = { kind: 'palette', mode };
  }

  toggleOverview() {
    if (this.current?.kind === 'overview') this.close();
    else this.current = { kind: 'overview' };
  }

  confirm(request: ConfirmRequest) {
    this.current = { kind: 'confirm', ...request };
  }

  /** The SSH host editor: a new host (`host` null, maybe prefilled) or an entry to change. */
  editSshHost(host: Host | null, prefill?: Partial<HostDraft>) {
    this.current = { kind: 'sshHost', host, prefill };
  }

  /** Opens one of the SSH admin dialogs. */
  show(state: Exclude<OverlayState, { kind: 'menu' | 'group' | 'palette' | 'overview' | 'confirm' | 'sshHost' }>) {
    this.current = state;
  }

  openMenu(x: number, y: number, entries: MenuEntry[]) {
    this.current = { kind: 'menu', x, y, entries };
  }

  /** Opens the editor under the group's chip, once the chip is on screen. */
  async openGroupEditor(groupId: number) {
    await tick();
    const chip = document.querySelector<HTMLElement>(`[data-group-chip="${groupId}"]`);
    if (!chip) return;
    this.current = { kind: 'group', groupId, anchor: chip.getBoundingClientRect() };
  }

  close() {
    if (!this.current) return;
    this.current = null;
    activeTerminal()?.focus();
  }
}

export const overlay = new Overlay();
