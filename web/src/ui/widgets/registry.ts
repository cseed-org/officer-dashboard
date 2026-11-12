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

import type { WidgetBlockSize } from '../../lib/contracts';
import type { Hue } from '../../lib/colors';
import type { IconName } from '../components/Icon';
import { href } from '../../lib/routes';

export interface WidgetProps { id: string; size: BlockSize; me: CurrentUser; overview: ReturnType<typeof useApi<'overview'>>; cloudflare: ReturnType<typeof useApi<'cloudflareWidgets'>>; }

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
  /** Title chip icon and accent (design/mockups/home-v07-notes.md § Per widget). */
  icon: IconName;
  accent: Hue;
  /** Where the whole card goes; null means the card has no card link (only its rows link). */
  href: (me: CurrentUser) => string | null;
  /** The card link opens another site in a new tab. */
  external?: boolean;
}

const leadership = (me: CurrentUser) => me.role === 'leadership' || me.isOwner;
const director = (me: CurrentUser) => me.role !== 'officer';
const CLOUDFLARE = 'https://dash.cloudflare.com';
const pending = () => href.requests({ scope: 'all', status: 'pending' });

/** Feature Panes can extend this registry. Keep defaults in step with server/internal/api/preferences.go. */
export const widgetRegistry: WidgetDef[] = [
  { id: 'calendar', component: CalendarWidget, title: 'Calendar', sizes: ['2x1', '2x2', '4x2'], size: '2x2', icon: 'calendar', accent: 'blue', href: () => href.events() },
  { id: 'links', component: LinksWidget, title: 'Links', sizes: ['2x1', '1x2', '2x2'], size: '2x1', icon: 'link', accent: 'teal', href: (me) => (leadership(me) ? '/settings#club-links' : null) },
  { id: 'kpi-upcoming', component: KpiWidget, title: 'Upcoming events', sizes: ['1x1', '2x1'], size: '1x1', group: 'kpis', icon: 'clock', accent: 'blue', href: () => href.events({ when: 'upcoming' }) },
  { id: 'kpi-checkin', component: KpiWidget, title: 'Avg check-in rate', sizes: ['1x1', '2x1'], size: '1x1', group: 'kpis', icon: 'check', accent: 'teal', href: () => href.events({ when: 'past' }) },
  { id: 'kpi-reflection', component: KpiWidget, title: 'Reflection rate', sizes: ['1x1', '2x1'], size: '1x1', group: 'kpis', icon: 'message', accent: 'violet', href: () => href.events({ when: 'past' }) },
  { id: 'kpi-budget', component: KpiWidget, title: 'Budget remaining', sizes: ['1x1', '2x1'], size: '1x1', group: 'kpis', icon: 'wallet', accent: 'green', href: () => href.finance('budgets') },
  { id: 'kpi-pending', component: KpiWidget, title: 'Pending requests', sizes: ['1x1', '2x1'], size: '1x1', group: 'kpis', icon: 'inbox', accent: 'amber', href: pending },
  { id: 'my-action', component: MyActionWidget, title: 'My actions', sizes: ['2x1', '2x2', '4x2'], size: '2x1', icon: 'flag', accent: 'amber', href: (me) => (director(me) ? pending() : href.requests({ scope: 'mine' })) },
  { id: 'recent-events', component: RecentEventsWidget, title: 'Recent events', sizes: ['2x2', '4x2'], size: '2x2', icon: 'ticket', accent: 'blue', href: () => href.events() },
  { id: 'budget', component: BudgetWidget, title: 'Budget vs actual', sizes: ['1x1', '2x2', '4x2'], size: '2x2', icon: 'pie', accent: 'green', href: () => href.finance('budgets') },
  { id: 'activity', component: ActivityWidget, title: 'Activity', sizes: ['2x1', '1x2', '2x2', '4x2'], size: '2x1', icon: 'activity', accent: 'coral', href: (me) => (director(me) ? href.admin('audit') : href.requests({ scope: 'all' })) },
  { id: 'web-analytics', component: WebAnalyticsWidget, title: 'Web Analytics', sizes: ['1x1', '2x2', '4x2'], size: '1x1', icon: 'globe', accent: 'blue', href: () => CLOUDFLARE, external: true },
  { id: 'traffic-security', component: TrafficSecurityWidget, title: 'Traffic & security', sizes: ['1x1', '2x2'], size: '1x1', icon: 'shield', accent: 'coral', href: () => CLOUDFLARE, external: true },
  { id: 'performance', component: PerformanceWidget, title: 'Performance', sizes: ['1x1', '2x2'], size: '1x1', icon: 'gauge', accent: 'teal', href: () => CLOUDFLARE, external: true },
  { id: 'workers-d1', component: WorkersD1Widget, title: 'Workers/D1 usage', sizes: ['1x1', '2x2'], size: '1x1', icon: 'database', accent: 'violet', href: () => CLOUDFLARE, external: true },
  { id: 'member-signups', component: MemberSignupsWidget, title: 'Member signups', sizes: ['1x1', '2x2'], size: '1x1', icon: 'users', accent: 'violet', href: () => '/members' },
];

/** The old single "Key figures" widget, split into one stat tile per figure in V0.4. */
export const KPI_GROUP_ID = 'kpis';
export const kpiIds = widgetRegistry.filter((w) => w.group === 'kpis').map((w) => w.id);

export { defaultWidgetLayout } from './defaultLayout';
import { defaultWidgetLayout } from './defaultLayout';

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
