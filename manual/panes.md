# Split panes

A tab can hold several terminals side by side or stacked.

- {{cmd:pane.splitRight}} and {{cmd:pane.splitDown}} split the focused pane. The new pane starts in the same folder.
- Move between panes with {{cmd:pane.next}} / {{cmd:pane.prev}}, or by direction: {{cmd:pane.left}}, {{cmd:pane.right}}, {{cmd:pane.up}}, {{cmd:pane.down}}. Clicking a pane focuses it too.
- {{cmd:pane.zoom}} lets the focused pane fill the tab for a while; press it again to bring the others back.
- Drag a divider to resize; double-click it to even it out.
- {{cmd:pane.close}} closes the focused pane.

Unfocused panes are dimmed a little, so you can tell where you're typing. Change how much with `unfocused-split-dim` in [Settings](#settings).

> To type the same thing into every pane of a tab, right-click the tab → **Broadcast Input to Panes**. See [Broadcast input](#broadcast).
