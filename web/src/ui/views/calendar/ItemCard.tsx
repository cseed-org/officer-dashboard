import type { ComponentChildren } from 'preact';
import { useApi } from '../../../lib/api/useApi';
import { addDays, longDay, pacificParts, timeRange } from '../../../lib/calendar';
import type { CalendarFeedItem, CalendarFeedSource, OrgCalendar, OrgCalendarEvent, OrgInvitee } from '../../../lib/contracts';
import { rangeText, reminderText, repeatText } from '../../../lib/orgcal';
import { internalPath } from '../../../lib/url';
import { effectiveTimezone } from '../../../lib/timezone';
import { Notice } from '../../components/states';

type Item = { kind: 'org'; event: OrgCalendarEvent; calendar: OrgCalendar } | { kind: 'feed'; items: CalendarFeedItem[]; source: CalendarFeedSource | undefined };

const isDate = (s: string | null | undefined) => !!s && /^\d{4}-\d{2}-\d{2}$/.test(s);

function Fact({ label, children }: { label: string; children: ComponentChildren }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}

/** "Thursday-style" when line for any org item: timed, all-day, or tentative range. */
export function whenText(e: Pick<OrgCalendarEvent, 'startsAt' | 'endsAt' | 'allDay' | 'tentativeFrom' | 'tentativeTo'>): string {
  if (e.startsAt === null) return `Tentative · sometime ${rangeText(e.tentativeFrom ?? '', e.tentativeTo ?? e.tentativeFrom ?? '')}`;
  if (e.allDay || isDate(e.startsAt)) {
    const last = e.endsAt && isDate(e.endsAt) ? addDays(e.endsAt, -1) : e.startsAt;
    return last > e.startsAt ? `All day · ${rangeText(e.startsAt, last)}` : `All day · ${longDay(e.startsAt)}`;
  }
  return `${longDay(pacificParts(e.startsAt).date)} · ${timeRange(e.startsAt, e.endsAt)} · ${effectiveTimezone()}`;
}

/**
 * The card a calendar item opens. Meetings offer Edit and Delete; Weekly Programs and Events are
 * locked (the dashboard writes them) and link to their source; overlays link to their record.
 */
export function ItemCard({ item, onEdit, onDelete }: { item: Item; onEdit: (e: OrgCalendarEvent) => void; onDelete: (e: OrgCalendarEvent) => void }) {
  if (item.kind === 'feed') {
    const first = item.items[0]!;
    const when = first.startsAt ? `${longDay(pacificParts(first.startsAt).date)} · ${timeRange(first.startsAt, first.endsAt)}` : longDay(first.date);
    if (item.items.length > 1) {
      return (
        <div class="stack orgcal-card" data-item-kind="overlay">
          <p class="muted small">
            {when} · {item.source?.label ?? 'Dashboard'}. A dashboard overlay, shown on this page only.
          </p>
          <ul class="orgcal-overlay-list">
            {item.items.map((it) => {
              const link = internalPath(it.href);
              return <li key={it.id}>{link ? <a href={link}>{it.title}</a> : it.title}</li>;
            })}
          </ul>
        </div>
      );
    }
    const link = internalPath(first.href);
    return (
      <div class="stack orgcal-card" data-item-kind="overlay">
        <dl class="facts">
          <Fact label="When">{when}</Fact>
          {first.detail && <Fact label="Details">{first.detail}</Fact>}
          {first.location && <Fact label="Location">{first.location}</Fact>}
        </dl>
        <p class="muted small">A dashboard overlay from {item.source?.label ?? 'the dashboard'}, shown on this page only.</p>
        {link && (
          <div class="action-row">
            <a class="btn" href={link}>
              Open →
            </a>
          </div>
        )}
      </div>
    );
  }
  const e = item.event;
  const locked = e.sourceKind !== 'manual' || item.calendar.locked;
  const source = e.sourceHref ? internalPath(e.sourceHref) : null;
  const repeat = repeatText(e.rrule, e.startsAt);
  return (
    <div class="stack orgcal-card" data-item-kind={locked ? 'locked' : 'meeting'}>
      {e.startsAt === null && <span class="orgcal-tentative-tag">Tentative · no set date yet</span>}
      <dl class="facts">
        <Fact label="When">{whenText(e)}</Fact>
        {repeat && <Fact label="Repeats">{repeat}</Fact>}
        {e.location && <Fact label="Location">{e.location}</Fact>}
        {!locked && <Fact label="Invites">{e.invitees.length === 0 ? 'Nobody invited' : <Invitees list={e.invitees} />}</Fact>}
        {!locked && e.reminders.length > 0 && <Fact label="Reminders">{e.reminders.map((r) => `${reminderText(r.minutes)} (${r.method === 'email' ? 'email' : 'notification'})`).join(', ')}</Fact>}
        {!locked && <Fact label="Send invites">{e.sendInvites ? 'On: Google emails invitees' : 'Off'}</Fact>}
      </dl>
      {e.description && <p class="orgcal-desc">{e.description}</p>}
      {locked ? (
        <>
          <Notice tone="info">
            {e.calendarKey === 'programs'
              ? 'Weekly Programs is locked. Change sessions from the Buildspace Programs tab.'
              : 'The Events calendar is locked. Change this from the Events page.'}
          </Notice>
          {source && (
            <div class="action-row">
              <a class="btn" href={source} data-source-link>
                {e.calendarKey === 'programs' ? 'Open program →' : 'Open event →'}
              </a>
            </div>
          )}
        </>
      ) : (
        <div class="action-row">
          <button type="button" class="btn btn--primary" onClick={() => onEdit(e)}>
            Edit
          </button>
          <button type="button" class="btn btn--danger" onClick={() => onDelete(e)}>
            Delete
          </button>
        </div>
      )}
    </div>
  );
}

function Invitees({ list }: { list: OrgInvitee[] }) {
  const lookups = useApi('lookups');
  const subteams = lookups.status === 'ready' ? lookups.data.subteams : [];
  const officers = lookups.status === 'ready' ? lookups.data.hosts : [];
  const name = (i: OrgInvitee) => (i.kind === 'subteam' ? subteams.find((s) => s.id === i.ref)?.name : i.kind === 'user' ? officers.find((u) => u.id === i.ref)?.name : i.ref) ?? '…';
  return <span>{list.map(name).join(', ')}</span>;
}
