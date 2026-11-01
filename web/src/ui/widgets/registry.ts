import type { WidgetPlacement } from '../../lib/contracts';

/** Stable IDs form the persisted contract; feature Panes can extend this registry. */
export const widgetRegistry = [
  { id: 'kpis', title: 'Key figures', size: 'L' },
  { id: 'my-action', title: 'My actions', size: 'M' },
  { id: 'recent-events', title: 'Recent events', size: 'M' },
  { id: 'budget', title: 'Budget vs actual', size: 'M' },
  { id: 'activity', title: 'Activity', size: 'M' },
] satisfies (WidgetPlacement & { title: string })[];
export const defaultWidgetLayout: WidgetPlacement[] = widgetRegistry.map(({ id, size }) => ({ id, size }));
