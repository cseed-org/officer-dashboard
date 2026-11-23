import type { EventListItem } from '../../../lib/contracts';
import { whenShort } from '../../../lib/calendar';
import { rangeText } from '../../../lib/orgcal';
import type { CalEntry } from '../../components/calendar/layout';
import { MonthView as SharedMonthView } from '../../components/calendar/MonthView';
import { WeekView as SharedWeekView } from '../../components/calendar/WeekView';
import { shownStatus, statusLabel } from './shared';

type Open = (e: EventListItem) => void;

const draftSuffix = (e: EventListItem) => (e.status === 'draft' ? ' · Draft' : e.status === 'cancelled' ? ' · Cancelled' : '');

/**
 * The shared calendar's entry for one club event. Month chips and the month agenda mark drafts;
 * week blocks name drafts and cancellations after the title, as before the extraction.
 */
function toEntry(e: EventListItem, week: boolean): CalEntry {
  // A tentative event has no start yet (startsAt is ""): an all-day "Tentative" span.
  if (!e.startsAt && e.tentativeFrom) {
    const to = e.tentativeTo ?? e.tentativeFrom;
    return { id: e.id, title: e.name, from: e.tentativeFrom, to, tentative: true, draft: e.status === 'draft', note: ` · Tentative${draftSuffix(e)}`, agendaWhen: `Tentative · ${rangeText(e.tentativeFrom, to)}`, agendaSuffix: draftSuffix(e) };
  }
  const status = shownStatus(e);
  const when = whenShort(e.startsAt, e.endsAt);
  return {
    id: e.id,
    title: e.name,
    startsAt: e.startsAt,
    endsAt: e.endsAt,
    draft: e.status === 'draft',
    note: week ? (status === 'draft' || status === 'cancelled' ? ` · ${statusLabel(status)}` : '') : draftSuffix(e),
    agendaWhen: week ? when : `${when}${e.status === 'draft' ? ' · Draft' : ''}`,
    agendaSuffix: week ? draftSuffix(e) : '',
  };
}

export function MonthView({ month, events, onOpen, onMonth, onWeek }: { month: string; events: EventListItem[]; onOpen: Open; onMonth: (ym: string) => void; onWeek: (monday: string) => void }) {
  const byId = new Map(events.map((e) => [e.id, e]));
  return (
    <SharedMonthView
      month={month}
      entries={events.map((e) => toEntry(e, false))}
      onOpen={(en) => onOpen(byId.get(en.id)!)}
      onMonth={onMonth}
      onWeek={onWeek}
    />
  );
}

export function WeekView({ monday, events, onOpen, onWeek }: { monday: string; events: EventListItem[]; onOpen: Open; onWeek: (monday: string) => void }) {
  const byId = new Map(events.map((e) => [e.id, e]));
  return <SharedWeekView monday={monday} entries={events.map((e) => toEntry(e, true))} onOpen={(en) => onOpen(byId.get(en.id)!)} onWeek={onWeek} />;
}
