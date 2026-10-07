import { tick } from 'svelte';
import type { GroupColor } from './tabs.svelte';
import { activeTerminal } from './terminals';

export type MenuEntry =
  | { label: string; action: () => void; hint?: string; color?: GroupColor; danger?: boolean }
  | { heading: string }
  | 'separator';

export type OverlayState =
  | { kind: 'menu'; x: number; y: number; entries: MenuEntry[] }
  | { kind: 'group'; groupId: number; anchor: DOMRect }
  | { kind: 'palette'; mode: PaletteMode };

export type PaletteMode = 'commands' | 'themes';

/** The one popover (context menu, group editor, palette) that can be open at a time. */
class Overlay {
  current = $state<OverlayState | null>(null);

  togglePalette(mode: PaletteMode = 'commands') {
    if (this.current?.kind === 'palette' && this.current.mode === mode) this.close();
    else this.current = { kind: 'palette', mode };
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
