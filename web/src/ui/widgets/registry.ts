import type { ComponentType } from 'preact';
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
import { widgetCatalog, type WidgetCatalogItem, type WidgetProps } from './catalog';

export * from './catalog';

export interface WidgetDef extends WidgetCatalogItem {
  component: ComponentType<WidgetProps>;
}

const components: Record<string, ComponentType<WidgetProps>> = {
  calendar: CalendarWidget,
  links: LinksWidget,
  'kpi-upcoming': KpiWidget,
  'kpi-checkin': KpiWidget,
  'kpi-reflection': KpiWidget,
  'kpi-budget': KpiWidget,
  'kpi-pending': KpiWidget,
  'my-action': MyActionWidget,
  'recent-events': RecentEventsWidget,
  budget: BudgetWidget,
  activity: ActivityWidget,
  'web-analytics': WebAnalyticsWidget,
  'traffic-security': TrafficSecurityWidget,
  performance: PerformanceWidget,
  'workers-d1': WorkersD1Widget,
  'member-signups': MemberSignupsWidget,
};

/** Full Home registry. Other pages import the component-free catalog instead. */
export const widgetRegistry: WidgetDef[] = widgetCatalog.map((w) => ({ ...w, component: components[w.id]! }));
export const widgetDef = (id: string) => widgetRegistry.find((w) => w.id === id);
