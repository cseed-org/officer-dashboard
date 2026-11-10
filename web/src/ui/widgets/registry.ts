import type { ComponentType } from 'preact';
import type { CurrentUser } from '../../lib/contracts';
import type { useApi } from '../../lib/api/useApi';
import CalendarWidget from './calendar/CalendarWidget';
import LinksWidget from './links/LinksWidget';
import KpiWidget from './kpi/KpiWidget';
import MyActionWidget from './my-action/MyActionWidget';
import RecentEventsWidget from './recent-events/RecentEventsWidget';
import BudgetWidget from './budget/BudgetWidget';
import ActivityWidget from './activity/ActivityWidget';
import WebAnalyticsWidget from './cloudflare/WebAnalyticsWidget';
import TrafficSecurityWidget from './cloudflare/TrafficSecurityWidget';
import PerformanceWidget from './cloudflare/PerformanceWidget';
import WorkersD1Widget from './cloudflare/WorkersD1Widget';
import MemberSignupsWidget from './cloudflare/MemberSignupsWidget';

export interface WidgetProps { id: string; size: BlockSize; me: CurrentUser; overview: ReturnType<typeof useApi<'overview'>>; cloudflare: ReturnType<typeof useApi<'cloudflareWidgets'>>; }
import type { WidgetBlockSize } from '../../lib/contracts';

export type BlockSize = WidgetBlockSize;
export interface WidgetDef {
  /** Stable ID: part of the persisted contract (user_prefs). */
  id: string;
  title: string;
  component: ComponentType<WidgetProps>;
  /** Sizes this widget supports, smallest first. */
  sizes: BlockSize[];
  /** Size used when the widget is added. */
  size: BlockSize;
  /** Widgets offered together as one onboarding choice (the old "Key figures" row). */
  group?: 'kpis';
}

/** Feature Panes can extend this registry. Keep defaults in step with server/internal/api/preferences.go. */
export const widgetRegistry: WidgetDef[] = [
  { id: 'calendar', component: CalendarWidget, title: 'Calendar', sizes: ['2x1', '2x2', '4x2'], size: '2x2' },
  { id: 'links', component: LinksWidget, title: 'Links', sizes: ['2x1', '1x2', '2x2'], size: '2x1' },
  { id: 'kpi-upcoming', component: KpiWidget, title: 'Upcoming events', sizes: ['1x1', '2x1'], size: '1x1', group: 'kpis' },
  { id: 'kpi-checkin', component: KpiWidget, title: 'Avg check-in rate', sizes: ['1x1', '2x1'], size: '1x1', group: 'kpis' },
  { id: 'kpi-reflection', component: KpiWidget, title: 'Reflection rate', sizes: ['1x1', '2x1'], size: '1x1', group: 'kpis' },
  { id: 'kpi-budget', component: KpiWidget, title: 'Budget remaining', sizes: ['1x1', '2x1'], size: '1x1', group: 'kpis' },
  { id: 'kpi-pending', component: KpiWidget, title: 'Pending requests', sizes: ['1x1', '2x1'], size: '1x1', group: 'kpis' },
  { id: 'my-action', component: MyActionWidget, title: 'My actions', sizes: ['2x1', '2x2', '4x2'], size: '2x1' },
  { id: 'recent-events', component: RecentEventsWidget, title: 'Recent events', sizes: ['2x2', '4x2'], size: '2x2' },
  { id: 'budget', component: BudgetWidget, title: 'Budget vs actual', sizes: ['2x2', '4x2'], size: '2x2' },
  { id: 'activity', component: ActivityWidget, title: 'Activity', sizes: ['2x1', '1x2', '2x2', '4x2'], size: '2x1' },
  { id: 'web-analytics', component: WebAnalyticsWidget, title: 'Web Analytics', sizes: ['1x1', '2x2', '4x2'], size: '1x1' },
  { id: 'traffic-security', component: TrafficSecurityWidget, title: 'Traffic & security', sizes: ['1x1', '2x2'], size: '1x1' },
  { id: 'performance', component: PerformanceWidget, title: 'Performance', sizes: ['1x1', '2x2'], size: '1x1' },
  { id: 'workers-d1', component: WorkersD1Widget, title: 'Workers/D1 usage', sizes: ['1x1', '2x2'], size: '1x1' },
  { id: 'member-signups', component: MemberSignupsWidget, title: 'Member signups', sizes: ['1x1', '2x2'], size: '1x1' },
];

/** The old single "Key figures" widget, split into one stat tile per figure in V0.4. */
export const KPI_GROUP_ID = 'kpis';
export const kpiIds = widgetRegistry.filter((w) => w.group === 'kpis').map((w) => w.id);

/** Calendar adds two full desktop rows to the existing packed layout. */
export const defaultWidgetLayout: { id: string; size: BlockSize }[] = [
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

export const widgetDef = (id: string) => widgetRegistry.find((w) => w.id === id);

/** Onboarding offers the stat tiles as one "Key figures" choice; everything else one by one. */
export const onboardingChoices: { id: string; title: string; ids: string[] }[] = [
  { id: KPI_GROUP_ID, title: 'Key figures', ids: kpiIds },
  ...widgetRegistry.filter((w) => !w.group).map((w) => ({ id: w.id, title: w.title, ids: [w.id] })),
];
/** The onboarding choice a saved widget ID belongs to (V0.3 "kpis" included). */
export const choiceOf = (id: string) => onboardingChoices.find((c) => c.id === id || c.ids.includes(id))?.id;
/** A layout for the picked choices: default order and sizes first, then the rest at their default size. */
export function layoutForChoices(picked: Set<string>): { id: string; size: BlockSize }[] {
  const ids = onboardingChoices.filter((c) => picked.has(c.id)).flatMap((c) => c.ids);
  const first = defaultWidgetLayout.filter((p) => ids.includes(p.id));
  const rest = ids.filter((id) => !first.some((p) => p.id === id)).map((id) => ({ id, size: widgetDef(id)!.size }));
  return [...first, ...rest];
}
