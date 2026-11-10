// Widget stylesheets load here, not from the widget modules: tests import registry.ts in Node.
import '../widgets/shell/widget.css';
import '../widgets/calendar/calendar.css';
import '../widgets/links/links.css';
import '../widgets/budget/budget.css';
import '../widgets/cloudflare/cloudflare.css';
import { useRef } from 'preact/hooks';
import type { CurrentUser } from '../../lib/contracts';
import { useApi } from '../../lib/api/useApi';
import { Loading } from '../components/states';
import WidgetGrid from '../widgets/grid/WidgetGrid';
import { widgetDef } from '../widgets/registry';
import { useEntrance } from '../widgets/shell/entrance';
export { auditHref, auditSummary } from '../widgets/activity/ActivityWidget';

export default function HomeView({ me }: { me: CurrentUser }) {
  const overview = useApi('overview');
  const cloudflare = useApi('cloudflareWidgets');
  const committed = useRef(false);
  if (overview.status !== 'loading') committed.current = true;
  const entering = useEntrance(committed.current);
  if (!committed.current) return <Loading rows={6} label="Loading home" />;
  return <WidgetGrid entering={entering}>{(id, size) => {
    const Component = widgetDef(id)?.component;
    return Component ? <Component id={id} size={size} me={me} overview={overview} cloudflare={cloudflare} /> : null;
  }}</WidgetGrid>;
}
