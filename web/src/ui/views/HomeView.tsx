import '../../styles/calendar-widget.css';
import '../../styles/cloudflare-widgets.css';
import { useRef } from 'preact/hooks';
import type { CurrentUser } from '../../lib/contracts';
import { useApi } from '../../lib/api/useApi';
import { Loading } from '../components/states';
import WidgetGrid from '../widgets/grid/WidgetGrid';
import { widgetDef } from '../widgets/registry';
export { auditHref, auditSummary } from '../widgets/activity/ActivityWidget';

export default function HomeView({ me }: { me: CurrentUser }) {
  const overview = useApi('overview');
  const cloudflare = useApi('cloudflareWidgets');
  const committed = useRef(false);
  if (overview.status !== 'loading') committed.current = true;
  if (!committed.current) return <Loading rows={6} label="Loading home" />;
  return <WidgetGrid>{(id, size) => {
    const Component = widgetDef(id)?.component;
    return Component ? <Component id={id} size={size} me={me} overview={overview} cloudflare={cloudflare} /> : null;
  }}</WidgetGrid>;
}
