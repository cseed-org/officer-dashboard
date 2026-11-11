// Widget stylesheets load here, not from the widget modules: tests import registry.ts in Node.
import '../widgets/shell/widget.css';
import '../widgets/calendar/calendar.css';
import '../widgets/links/links.css';
import '../widgets/budget/budget.css';
import '../widgets/cloudflare/cloudflare.css';
import './home/greeting.css';
import { localDay, pickGreeting, readState, writeState } from './home/greeting';
import { useEffect, useRef } from 'preact/hooks';
import type { CurrentUser } from '../../lib/contracts';
import { useApi } from '../../lib/api/useApi';
import { Loading } from '../components/states';
import WidgetGrid from '../widgets/grid/WidgetGrid';
import { widgetDef } from '../widgets/registry';
import { useEntrance } from '../widgets/shell/entrance';
export { auditHref, auditSummary } from '../widgets/activity/ActivityWidget';

export default function HomeView({ me }: { me: CurrentUser }) {
  useEffect(() => {
    const heading = document.querySelector<HTMLElement>('.home-heading');
    if (!heading) return;
    const local = () => window.localStorage;
    const session = () => window.sessionStorage;
    const memory = readState(me.id, local, session);
    if (!memory.available || (memory.fallback && memory.state)) return;
    const now = new Date();
    const greeting = pickGreeting(now, me.name.trim().split(/\s+/)[0] ?? '', memory.state, Math.random);
    if (!greeting || !writeState(me.id, { day: localDay(now), signedOut: false }, local, session, memory.fallback)) return;
    const old = document.createElement('span');
    old.className = 'home-heading-old';
    old.textContent = 'Home';
    old.setAttribute('aria-hidden', 'true');
    const next = document.createElement('span');
    next.className = 'home-heading-new';
    next.textContent = greeting;
    heading.replaceChildren(old, next);
    old.addEventListener('animationend', () => old.remove(), { once: true });
    const announcement = document.querySelector('[data-home-greeting-announcement]');
    if (announcement) announcement.textContent = greeting;
  }, []);
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
