/**
 * The Home tour, one entry per stop. Targets are `data-tour="…"` attributes, which the tour's
 * owners keep stable while the page around them changes; the first listed target that is on
 * screen is used, and a stop whose target is missing is shown centred.
 */
export interface TourStep {
  id: string;
  title: string;
  text: string;
  /** Selectors tried in order (e.g. the sidebar on desktop, the menu button on phones). */
  targets: string[];
  /** Shows the grid's edit controls behind the tour (a preview; nothing is saved). */
  edit?: boolean;
  /** Left out of the tour when its target isn't on the page (a feature that may not be merged or enabled). */
  optional?: boolean;
}

const sel = (...names: string[]) => names.map((n) => `[data-tour="${n}"]`);

export const TOUR_STEPS: TourStep[] = [
  { id: 'greeting', title: 'This is Home', text: 'A short hello, then everything your club is up to, in one place.', targets: sel('greeting') },
  { id: 'grid', title: 'Your widgets', text: 'Each card is a widget: events, requests, budget, links. They keep themselves up to date.', targets: sel('grid') },
  { id: 'edit', title: 'Make it yours', text: 'Edit home lets you rearrange. Done saves your layout across devices; Cancel puts everything back.', targets: sel('edit') },
  { id: 'drag', title: 'Drag to move', text: 'Drag a card to move it. With the grip focused, the arrow keys move it earlier or later, and Escape cancels.', targets: sel('drag'), edit: true },
  { id: 'resize', title: 'Resize from the corner', text: 'Drag a corner to preview an allowed size, or hold Shift and use the arrow keys. Escape cancels.', targets: sel('resize'), edit: true },
  { id: 'add', title: 'Add and remove', text: 'Add widget opens the gallery. The × on a card removes it, and you can always bring it back. This preview saves nothing.', targets: sel('add'), edit: true },
  { id: 'sidebar', title: 'Find your way', text: 'The sidebar takes you to every part of the dashboard.', targets: sel('sidebar', 'nav-toggle') },
  { id: 'sidebar-resize', title: 'Resize the sidebar', text: 'Drag the sidebar’s edge to make it wider or narrower. Double-click it to switch between icons only and your last width.', targets: sel('sidebar-resize'), optional: true },
  { id: 'search', title: 'Jump anywhere', text: 'Open the command palette to find any page, event or person. On a keyboard, press Ctrl K (⌘K on a Mac).', targets: sel('search') },
];

/** The stops to show now: optional ones only when their target exists on this page. */
export const availableSteps = (): TourStep[] => TOUR_STEPS.filter((t) => !t.optional || t.targets.some((q) => Array.from(document.querySelectorAll(q)).some((el) => el.getBoundingClientRect().width > 0)));
