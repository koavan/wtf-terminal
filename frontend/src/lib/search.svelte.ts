import { tabs } from './tabs.svelte';
import { activeTerminal } from './terminals';

/** Implemented by the open search bar. */
export interface SearchController {
  next(): void;
  prev(): void;
  focus(): void;
}

/** Find-in-terminal. At most one pane shows its search bar at a time. */
class Search {
  paneId = $state<number | null>(null);
  controller: SearchController | null = null;

  /** ⌘F: opens the bar on the focused pane, or re-focuses it. */
  open() {
    const pane = tabs.activePane;
    if (!pane) return;
    if (this.paneId === pane.id) this.controller?.focus();
    else this.paneId = pane.id;
  }

  /** ⌘G / ⌘⇧G: steps through matches, opening the bar if needed. */
  step(delta: 1 | -1) {
    if (this.paneId !== null && this.paneId === tabs.activePane?.id && this.controller) {
      if (delta > 0) this.controller.next();
      else this.controller.prev();
    } else {
      this.open();
    }
  }

  close() {
    this.paneId = null;
    this.controller = null;
    activeTerminal()?.focus();
  }
}

export const search = new Search();
