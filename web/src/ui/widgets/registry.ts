import type { WidgetPlacement } from '../../lib/contracts';

/** Stable IDs form the persisted contract; feature Panes can extend this registry. */
export const widgetRegistry = [
  { id: 'kpis', title: 'Key figures', size: 'L' },
  { id: 'my-action', title: 'My actions', size: 'M' },
  { id: 'recent-events', title: 'Recent events', size: 'M' },
  { id: 'budget', title: 'Budget vs actual', size: 'M' },
  { id: 'activity', title: 'Activity', size: 'M' },
  { id: 'web-analytics', title: 'Web Analytics', size: 'L' },
  { id: 'traffic-security', title: 'Traffic & security', size: 'M' },
  { id: 'performance', title: 'Performance', size: 'M' },
  { id: 'workers-d1', title: 'Workers/D1 usage', size: 'M' },
  { id: 'member-signups', title: 'Member signups', size: 'S' },
] satisfies (WidgetPlacement & { title: string })[];
export const defaultWidgetLayout: WidgetPlacement[] = widgetRegistry.filter(w=>!['traffic-security','performance','workers-d1','member-signups'].includes(w.id)).map(({ id, size }) => ({ id, size }));
