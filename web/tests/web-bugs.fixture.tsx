/** Browser-only component harness, served by Astro in the regression specs. */
import { h, render, type ComponentType } from 'preact';
import { useState } from 'preact/hooks';
import { useApi } from '../src/lib/api/useApi';
import { api } from '../src/lib/api/client';
import { Drawer } from '../src/ui/components/Drawer';
import Finance from '../src/ui/views/FinanceView';
import Events from '../src/ui/views/EventsView';
import Officers from '../src/ui/views/OfficersView';
import Admin from '../src/ui/views/AdminUsersView';
import Buildspace from '../src/ui/views/BuildspaceView';
import Request from '../src/ui/views/RequestDetail';
import RequestForm from '../src/ui/views/RequestForm';
import DesignForm from '../src/ui/views/DesignRequestForm';
import DesignDetail from '../src/ui/views/DesignRequestDetail';
import { ProfileForm } from '../src/ui/views/ProfileForm';
import { CalendarPanel } from '../src/ui/views/CalendarPanel';
import { TimezonePanel } from '../src/ui/views/TimezonePanel';
import Calendar from '../src/ui/widgets/calendar/CalendarWidget';
import Budget from '../src/ui/widgets/budget/BudgetWidget';
import Links from '../src/ui/widgets/links/LinksWidget';
import MyAction from '../src/ui/widgets/my-action/MyActionWidget';
import { CalendarChip } from '../src/ui/widgets/calendar/CalendarChip';
import Tour from '../src/ui/tour/HomeTour';
import Search from '../src/ui/shell/CommandPalette';
import { BuilderHome } from '../src/ui/portal/Builder';
import { MentorHome } from '../src/ui/portal/Mentor';
import { ProjectForm, PersonForm } from '../src/ui/views/buildspace/Forms';
import { MonthView, WeekView } from '../src/ui/views/events/EventCalendar';
import { Report } from '../src/ui/views/finance-parts';
import { CohortBar } from '../src/ui/views/buildspace/Cohorts';
import { MentorRoster } from '../src/ui/views/buildspace/Mentors';
import { PersonCard, ProjectCard } from '../src/ui/views/buildspace/Cards';
import { applyTimezone, refreshTimezone } from '../src/lib/timezone';
import { applySidebarWidth, saveSidebarWidth } from '../src/lib/sidebar';
import '../src/styles/base.css';
import '../src/styles/events.css';
import '../src/styles/components.css';
import '../src/styles/home-grid.css';
import '../src/styles/buildspace.css';
import '../src/styles/launchpad.css';
import '../src/ui/widgets/shell/widget.css';
import '../src/ui/widgets/budget/budget.css';
import '../src/ui/widgets/calendar/calendar.css';

const noop = () => {};
function Settings(props: any) { return <><TimezonePanel {...props} /><Links {...props} /></>; }
function Hook() {
  const [id, setId] = useState('a');
  const [enabled, setEnabled] = useState(true);
  const state = useApi('eventDetail', { params: { id } }, null, enabled);
  return <><button onClick={() => setId('b')}>Switch</button><button onClick={state.reload}>Reload</button><button onClick={() => setEnabled(!enabled)}>Enable</button><p data-hook>{state.status === 'ready' ? state.data.event.name : state.status}</p></>;
}
function Drawers() {
  const [first, setFirst] = useState(true);
  const [second, setSecond] = useState(true);
  return <>{first && <Drawer id="first" open title="First" onClose={() => setFirst(false)}><button onClick={() => setSecond(false)}>Unmount second</button></Drawer>}{second && <Drawer id="second" open title="Second" onClose={() => setSecond(false)}><button onClick={() => setFirst(false)}>Unmount first</button></Drawer>}<Search /></>;
}
function Sidebar() {
  return <>{[200, 280].map((w) => <button onClick={() => { applySidebarWidth(w); saveSidebarWidth(w, (sidebarWidth) => api('prefsPut', { body: { sidebarWidth } })); }}>{w}</button>)}</>;
}
function Write() {
  const [status, setStatus] = useState('idle');
  return <><button onClick={() => { void api('contactCreate', { body: { name: 'Saved once', type: 'speaker' } as never }).then(() => setStatus('saved'), () => setStatus('failed')); }}>Save</button><p>{status}</p></>;
}
const components: Record<string, ComponentType<any>> = {
  Hook, Drawers, Sidebar, Write, Finance, Events, Officers, Admin, Buildspace, Request,
  RequestForm, DesignForm, DesignDetail, ProfileForm, CalendarPanel, TimezonePanel,
  Calendar, Budget, Links, MyAction, CalendarChip, Tour, Search, BuilderHome, MentorHome,
  ProjectForm, PersonForm, CohortBar, MentorRoster, PersonCard, ProjectCard, Settings, MonthView, WeekView, Report,
};
export { applyTimezone, refreshTimezone };
export function mount(kind: string, props: Record<string, unknown>) {
  const el = document.getElementById('fixture')!;
  render(h(components[kind]!, { onDone: noop, onSaved: noop, onCreated: () => document.body.dataset.created = 'yes', onCancel: noop, onTitle: noop, onChanged: noop, onDeleted: noop, refreshMe: noop, onPreview: noop, onPerson: noop, onOpen: noop, onMonth: noop, onWeek: noop, saved: noop, changed: noop, reload: noop, setNotice: noop, ...props }), el);
}
