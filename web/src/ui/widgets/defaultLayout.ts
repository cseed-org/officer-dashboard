// No UI imports here: scripts/contract-check.mjs bundles this file in Node without web deps.
import type { WidgetBlockSize } from '../../lib/contracts';

/** Calendar adds two full desktop rows to the existing packed layout. */
export const defaultWidgetLayout: { id: string; size: WidgetBlockSize }[] = [
  { id: 'links', size: '2x1' },
  { id: 'kpi-upcoming', size: '1x1' },
  { id: 'kpi-checkin', size: '1x1' },
  { id: 'kpi-reflection', size: '1x1' },
  { id: 'kpi-budget', size: '1x1' },
  { id: 'kpi-pending', size: '1x1' },
  { id: 'web-analytics', size: '1x1' },
  { id: 'calendar', size: '4x2' },
  { id: 'recent-events', size: '2x2' },
  { id: 'budget', size: '2x2' },
  { id: 'my-action', size: '2x1' },
  { id: 'activity', size: '2x1' },
];
