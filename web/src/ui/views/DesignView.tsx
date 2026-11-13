import { effectiveTimezone } from '../../lib/timezone';
import { useEffect, useRef, useState } from 'preact/hooks';
import { api, STORAGE_UNAVAILABLE_MESSAGE } from '../../lib/api/client';
import { useApi } from '../../lib/api/useApi';
import { isDirector } from '../../lib/session';
import {
  DESIGN_STAGES,
  DESIGN_TYPES,
  type CurrentUser,
  type DesignNeed,
  type DesignRequestItem,
  type DesignRequestsListResponse,
  type DesignStage,
  type DesignType,
  type DesignUpcomingEvent,
} from '../../lib/contracts';
import {
  DESIGN_FILTERS,
  designMoveTargets,
  dueState,
  dueText,
  FILTER_LABEL,
  INDICATOR_LABEL,
  indicatorDetail,
  matchesFilter,
  myWork,
  railSummary,
  STAGE_LABEL,
  TYPE_HUE,
  TYPE_LABEL,
  type DesignFilter,
  fmtDue,
} from '../../lib/design';
import { fmtShort, fmtWeekday, plural, todayPacific } from '../../lib/format';
import { setParams, useSearch } from '../../lib/nav';
import { href } from '../../lib/routes';
import { StatRow } from '../components/StatRow';
import { Drawer } from '../components/Drawer';
import { DataTable, type Column } from '../components/kit';
import { Avatar, RowMenu, type MenuItem } from '../components/people';
import { Async, errorMessage, Loading } from '../components/states';
import DesignRequestDetail, { StageBadge } from './DesignRequestDetail';
import DesignRequestForm, { type Created } from './DesignRequestForm';

type View = 'board' | 'list' | 'mine';
const VIEW_LABEL: Record<View, string> = {
  board: 'Board',
  list: 'List',
  mine: 'My work',
};
const phone = () => typeof matchMedia !== 'undefined' && matchMedia('(max-width: 639px)').matches;
type Menu = (i: DesignRequestItem, d: DesignRequestsListResponse) => MenuItem[];

function designStats(items: DesignRequestItem[], meId: string) {
  const today = todayPacific();
  const open = items.filter((i) => i.stage !== 'delivered');
  const w = myWork(items, meId);
  return {
    open: open.length,
    inProgress: items.filter((i) => i.stage === 'in-progress').length,
    mine: w.assigned.length + w.approve.length,
    due: items.filter((i) => matchesFilter(i, 'due', meId, today)).length,
    overdue: open.filter((i) => dueState(i, today) === 'overdue').length,
  };
}

export default function DesignView({ me }: { me: CurrentUser }) {
  const search = useSearch();
  const v = search.get('view');
  // Phones default to the list; the board is one tap away.
  const view: View = v === 'board' || v === 'list' || v === 'mine' ? v : phone() ? 'list' : 'board';
  const f = search.get('filter');
  const filter: DesignFilter = (DESIGN_FILTERS as readonly string[]).includes(f ?? '') ? (f as DesignFilter) : 'all';
  const t = search.get('type');
  const type: DesignType | null = (DESIGN_TYPES as readonly string[]).includes(t ?? '') ? (t as DesignType) : null;
  const openId = search.get('id');
  const creating = search.get('new') === '1';
  const eventId = search.get('eventId') ?? undefined;

  const state = useApi('designRequestsList');
  const [title, setTitle] = useState('Design request');
  const [toast, setToast] = useState<string | null>(null);
  const [live, setLive] = useState('');
  useEffect(() => setTitle('Design request'), [openId]);
  useEffect(() => {
    if (!toast) return;
    const t = setTimeout(() => setToast(null), 6000);
    return () => clearTimeout(t);
  }, [toast]);

  const viewer = (d: DesignRequestsListResponse) => ({
    id: me.id,
    isDesigner: d.viewer.isDesigner,
    isWorker: d.viewer.canAssign,
    isLeadership: me.role === 'leadership',
  });

  const moving = useRef(false);
  const [movePending, setMovePending] = useState(false);
  const move = async (item: DesignRequestItem, to: DesignStage) => {
    if (to === 'delivered' && !confirm('Deliver without the requester’s approval? This leadership override will be recorded in the audit log.')) return;
    if (state.status !== 'ready' || moving.current) return;
    moving.current = true;
    setMovePending(true);
    const restoreFocus = document.activeElement?.getAttribute('data-request') === item.id;
    const focusCard = () => { if (restoreFocus) requestAnimationFrame(() => document.querySelector<HTMLElement>(`[data-request="${item.id}"]`)?.focus()); };
    const before = state.data;
    focusCard();
    state.set({ ...before, items: before.items.map((i) => i.id === item.id ? { ...i, stage: to } : i) });
    try {
      await api('designRequestMove', {
        params: { id: item.id },
        body: { stage: to, confirmDeliveryOverride: to === 'delivered' },
      });
      setLive(`${item.title} moved to ${STAGE_LABEL[to]}.`);
      state.reload();
    } catch (err) {
      state.set(before);
      setToast(`Couldn't move “${item.title}”: ${errorMessage(err)}`);
    } finally { moving.current = false; setMovePending(false); focusCard(); }
  };
  const assignMe = async (item: DesignRequestItem) => {
    try {
      await api('designRequestAssign', {
        params: { id: item.id },
        body: { assigneeId: me.id },
      });
      setLive(`${item.title} assigned to you.`);
      state.reload();
    } catch (err) {
      setToast(`Couldn't assign “${item.title}”: ${errorMessage(err)}`);
    }
  };
  const setNeed = async (e: DesignUpcomingEvent, need: DesignNeed) => {
    try {
      await api('eventDesignNeed', {
        params: { id: e.id },
        body: { designNeed: need },
      });
      setLive(need === 'not_needed' ? `${e.name} marked as not needing design.` : `${e.name} needs design.`);
      state.reload();
    } catch (err) {
      setToast(`Couldn't update “${e.name}”: ${errorMessage(err)}`);
    }
  };

  const menu: Menu = (item, d) => {
    const items: MenuItem[] = [{ label: 'Open request', onSelect: () => setParams({ id: item.id }) }];
    if (d.viewer.isDesigner && item.stage !== 'delivered' && item.assignee?.id !== me.id)
      items.push({
        label: 'Assign to me',
        onSelect: () => void assignMe(item),
      });
    // The requester decides in the drawer, next to the version: approve or request changes.
    if (item.stage === 'in-review' && item.requester.id === me.id)
      items.push({
        label: 'Review and approve',
        onSelect: () => setParams({ id: item.id }),
      });
    if (!d.viewer.canAssign) {
      items.push({ label: 'Only designers and directors can move requests', disabled: true, onSelect: () => {} });
      return items;
    }
    for (const s of designMoveTargets(item, viewer(d)))
      items.push({
        label: `Move to ${STAGE_LABEL[s]}`,
        disabled: movePending,
        onSelect: () => void move(item, s),
      });
    return items;
  };

  const onCreated = ({ request, uploadFailed, storageOff }: Created) => {
    state.reload();
    setParams({ new: null, eventId: null, id: request.id });
    setToast(
      uploadFailed
        ? storageOff
          ? STORAGE_UNAVAILABLE_MESSAGE
          : `Request created, but ${plural(uploadFailed, 'file')} didn't upload. Add them from the request.`
        : `“${request.title}” is on the board.`,
    );
  };

  const stats = state.status === 'ready' && designStats(state.data.items, me.id);
  return (
    <div class="stack">
      {state.status !== 'error' && (
        <StatRow
          label="Design requests summary"
          tiles={[
            { label: 'Open', value: stats ? stats.open : 0, sub: 'not delivered yet', loading: !stats },
            { label: 'In progress', value: stats ? stats.inProgress : 0, sub: 'being designed now', loading: !stats },
            { label: 'Waiting on me', value: stats ? stats.mine : 0, sub: 'assigned to me or to approve', alert: !!stats && stats.mine > 0, loading: !stats, href: '/design?filter=mine', pressed: filter === 'mine' },
            { label: 'Due soon', value: stats ? stats.due : 0, sub: stats && stats.overdue ? `${stats.overdue} overdue` : 'this week or overdue', alert: !!stats && stats.overdue > 0, loading: !stats, href: '/design?filter=due', pressed: filter === 'due' },
          ]}
        />
      )}
      <div class="toolbar">
        <div class="toolbar-main">
          <a class="btn btn--primary" href={href.newDesignRequest()} onClick={(e) => (e.preventDefault(), setParams({ new: 1, eventId: null }))} data-new-design>
            New design request
          </a>
        </div>
        <div class="segmented" role="group" aria-label="Board view">
          {(['board', 'list', 'mine'] as const).map((k) => (
            <button type="button" class="segment" key={k} aria-pressed={view === k} data-view={k} onClick={() => setParams({ view: k }, { replace: true })}>
              {VIEW_LABEL[k]}
            </button>
          ))}
        </div>
      </div>

      <Async state={state} loading={<Loading rows={6} label="Loading design requests" />}>
        {(d) => {
          const today = todayPacific();
          const open = d.items.filter((i) => i.stage !== 'delivered');
          const shown = d.items.filter((i) => matchesFilter(i, filter, me.id, today) && (!type || i.type === type));
          const count = (k: DesignFilter) => d.items.filter((i) => matchesFilter(i, k, me.id, today)).length;
          const overdue = open.filter((i) => dueState(i, today) === 'overdue').length;
          return (
            <>
              <UpcomingRail events={d.upcomingEvents} onRequest={(e) => setParams({ new: 1, eventId: e.id, id: null })} onOpen={(id) => setParams({ id })} onSetNeed={setNeed} />
              {d.items.length === 0 ? (
                <div class="state state--brand" data-state="empty">
                  <p class="state-title">No design requests yet</p>
                  <p class="state-body">Your next event deserves a good first impression. Give the design team a clear brief.</p>
                  <div class="state-action">
                    <button type="button" class="btn" onClick={() => setParams({ new: 1 })}>
                      Request your first design
                    </button>
                  </div>
                </div>
              ) : view === 'mine' ? (
                <MyWork data={d} me={me} menu={menu} />
              ) : (
                <>
                  <div class="design-summary">
                    <div class="design-filters" role="group" aria-label="Filter requests">
                      {DESIGN_FILTERS.map((k) => {
                        const n = count(k);
                        return (
                          <button
                            type="button"
                            class="design-filter"
                            key={k}
                            aria-pressed={filter === k}
                            data-filter={k}
                            onClick={() => setParams({ filter: k === 'all' ? null : k }, { replace: true })}
                          >
                            {FILTER_LABEL[k]}
                            <span class={`design-filter-n${k === 'due' && n > 0 ? ' is-alert' : ''}`}>{n}</span>
                          </button>
                        );
                      })}
                    </div>
                    <label class="design-type">
                      <span>Type</span>
                      <select class="select" value={type ?? ''} onChange={(e) => setParams({ type: e.currentTarget.value || null }, { replace: true })} data-type-filter>
                        <option value="">All types</option>
                        {DESIGN_TYPES.map((k) => (
                          <option value={k} key={k}>
                            {TYPE_LABEL[k]}
                          </option>
                        ))}
                      </select>
                    </label>
                  </div>
                  <p class="small muted design-meta" data-board-meta>
                    {filter === 'all' && !type
                      ? `${plural(open.length, 'open request')} · ${d.items.length - open.length} delivered`
                      : `Showing ${shown.length} of ${plural(d.items.length, 'request')}`}
                    {overdue > 0 && ` · ${overdue} overdue`} · Shared with everyone · Deadlines shown in {effectiveTimezone()}
                    {d.designers.length === 0 && ' · Nobody is on the Design subteam yet, so no one can pick these up'}
                  </p>
                  {shown.length === 0 ? (
                    <div class="state" data-state="filtered">
                      <p class="state-title">Nothing matches</p>
                      <p class="state-body">No requests fit these filters.</p>
                      <div class="state-action">
                        <button type="button" class="btn" onClick={() => setParams({ filter: null, type: null }, { replace: true })}>
                          Clear filters
                        </button>
                      </div>
                    </div>
                  ) : view === 'board' ? (
                    <Board pending={movePending} data={d} items={shown} me={me} menu={menu} onMove={move} />
                  ) : (
                    <List data={d} items={shown} menu={menu} />
                  )}
                </>
              )}
            </>
          );
        }}
      </Async>
      <p class="visually-hidden" aria-live="polite">
        {live}
      </p>

      <Drawer id="design-new" open={creating} onClose={() => creating && setParams({ new: null, eventId: null })} title="New design request" size="lg">
        {creating && <DesignRequestForm eventId={eventId} onDone={onCreated} onCancel={() => setParams({ new: null, eventId: null })} />}
      </Drawer>

      <Drawer id="design-detail" open={!!openId && !creating} onClose={() => openId && setParams({ id: null })} title={title} eyebrow="Design request" size="lg">
        {openId && (
          <DesignRequestDetail
            key={openId}
            id={openId}
            me={me}
            onTitle={setTitle}
            onChanged={state.reload}
            onDeleted={(t) => {
              setParams({ id: null });
              state.reload();
              setToast(`“${t}” was removed.`);
            }}
          />
        )}
      </Drawer>

      {toast && (
        <p class="toast" role="status">
          {toast}
        </p>
      )}
    </div>
  );
}

/** Events being planned in the next six weeks, each with its design indicator and one-click actions. */
function UpcomingRail({
  events,
  onRequest,
  onOpen,
  onSetNeed,
}: {
  events: DesignUpcomingEvent[];
  onRequest: (e: DesignUpcomingEvent) => void;
  onOpen: (requestId: string) => void;
  onSetNeed: (e: DesignUpcomingEvent, need: DesignNeed) => Promise<void>;
}) {
  const sum = railSummary(events);
  return (
    <section class="design-rail" aria-labelledby="design-rail-title" data-rail>
      <div class="design-rail-head">
        <h2 class="design-rail-title" id="design-rail-title">
          Upcoming events <span class="brand-eyebrow">next six weeks</span>
        </h2>
        {events.length > 0 && (
          <p class="design-rail-note">
            <strong>{sum.needs > 0 ? `${sum.needs} need${sum.needs === 1 ? 's' : ''} design` : 'Nothing needs design'}</strong>
            {sum.rest && ` · ${sum.rest}`}
          </p>
        )}
      </div>
      {events.length === 0 ? (
        <p class="rail-empty">
          No draft or published events in the next six weeks. <a href={href.events()}>Plan one on Events</a>.
        </p>
      ) : (
        <ul class="design-rail-list" tabIndex={0} aria-label="Upcoming events and their design status">
          {events.map((e) => {
            const [mon, day] = fmtShort(e.startsAt).split(' ');
            const detail = indicatorDetail(e);
            const openReq = e.requests.find((r) => r.stage !== 'delivered') ?? e.requests[0];
            const host = [e.hostSubteam?.name, e.host?.name].filter(Boolean).join(' · ') || 'No host yet';
            return (
              <li class="rail-event" key={e.id} data-need={e.indicator === 'needs-design' ? 'needs' : e.indicator} data-rail-event={e.id}>
                <div class="rail-date" aria-hidden="true">
                  <span class="rail-date-mon">{mon}</span>
                  <span class="rail-date-day">{day}</span>
                  <span class="rail-date-dow">{fmtWeekday(e.startsAt)}</span>
                </div>
                <p class="rail-title">
                  <a href={href.event(e.id)}>{e.name}</a>
                </p>
                <p class="rail-sub">
                  <span class="visually-hidden">
                    {fmtWeekday(e.startsAt)} {fmtShort(e.startsAt)} ·{' '}
                  </span>
                  <span class="rail-status" data-status={e.status}>
                    {e.status === 'draft' ? 'Draft' : 'Published'}
                  </span>
                  <span>{host}</span>
                </p>
                <p class="rail-sub">
                  <span class="design-need" data-need={e.indicator === 'needs-design' ? 'needs' : e.indicator} data-indicator={e.indicator}>
                    {INDICATOR_LABEL[e.indicator]}
                    {detail && <span class="design-need-sub"> · {detail}</span>}
                  </span>
                </p>
                <div class="rail-actions">
                  {e.indicator === 'needs-design' && (
                    <>
                      <button type="button" class="btn" onClick={() => onRequest(e)} data-rail-request>
                        Request design
                      </button>
                      {e.canSetNotNeeded && (
                        <button type="button" class="linkish" onClick={() => void onSetNeed(e, 'not_needed')} data-rail-skip>
                          Mark not needed
                        </button>
                      )}
                    </>
                  )}
                  {(e.indicator === 'requested' || e.indicator === 'done') && openReq && (
                    <button type="button" class="linkish" onClick={() => onOpen(openReq.id)}>
                      {e.indicator === 'done' ? 'See delivered files' : 'Open request'}
                    </button>
                  )}
                  {e.indicator === 'requested' && (
                    <button type="button" class="linkish" onClick={() => onRequest(e)}>
                      Request another
                    </button>
                  )}
                  {e.indicator === 'not-needed' && (
                    <button type="button" class="linkish" onClick={() => void onSetNeed(e, 'needed')} data-rail-needed>
                      Needs design after all
                    </button>
                  )}
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}

function Assignee({ item }: { item: DesignRequestItem }) {
  return item.assignee ? (
    <span class="kanban-people">
      <Avatar name={item.assignee.name} id={item.assignee.id} src={item.assignee.avatarUrl} size="sm" />
      <span class="kanban-people-name">{item.assignee.name}</span>
    </span>
  ) : (
    <span class="kanban-unassigned">Unassigned</span>
  );
}

function Due({ item }: { item: DesignRequestItem }) {
  const s = dueState(item);
  return <span class={`kanban-due${s === 'overdue' ? ' is-overdue' : s === 'soon' ? ' is-soon' : ''}`}>{dueText(item)}</span>;
}

/** The due chip on a card: date plus "Overdue"/"Due soon" in words; delivered cards say so. */
function DueChip({ item }: { item: DesignRequestItem }) {
  if (item.stage === 'delivered') return <span class="due-chip">Delivered</span>;
  if (!item.dueDate) return <span class="due-chip is-none">No due date</span>;
  const s = dueState(item);
  return (
    <span class={`due-chip${s === 'overdue' ? ' is-overdue' : s === 'soon' ? ' is-soon' : ''}`} data-due={s}>
      {fmtDue(item.dueDate)}
      {s === 'overdue' ? ' · Overdue' : s === 'soon' ? ' · Due soon' : ''}
    </span>
  );
}

/** One request card: title + urgency, type and due chip, linked event with its date, assignee and actions. */
function CardBody({ item, menu }: { item: DesignRequestItem; menu: MenuItem[] }) {
  return (
    <>
      <div class="kanban-card-top">
        <p class="kanban-card-title">
          <a href={href.designRequest(item.id)}>{item.title}</a>
        </p>
        {item.urgency === 'high' && item.stage !== 'delivered' && (
          <span class="urgent-flag" title="High urgency: the date cannot move">
            High
          </span>
        )}
      </div>
      <div class="kanban-card-meta">
        <span class="kanban-tag" data-hue={TYPE_HUE[item.type]}>
          {TYPE_LABEL[item.type]}
        </span>
        <DueChip item={item} />
      </div>
      {item.event ? (
        <p class="kanban-event">
          <span aria-hidden="true">◷</span>
          <a href={href.event(item.event.id)}>{item.event.name}</a>
          <span class="kanban-event-when">· {fmtShort(item.event.startsAt)}</span>
        </p>
      ) : (
        item.program && <p class="kanban-event">{item.program}</p>
      )}
      <div class="kanban-card-foot">
        <Assignee item={item} />
        <RowMenu label={`Actions for ${item.title}`} items={menu} trigger="Actions" floating />
      </div>
    </>
  );
}

function Board({
  pending,
  data,
  items: all,
  me,
  menu,
  onMove,
}: {
  pending: boolean;
  data: DesignRequestsListResponse;
  items: DesignRequestItem[];
  me: CurrentUser;
  menu: Menu;
  onMove: (i: DesignRequestItem, to: DesignStage) => Promise<void>;
}) {
  const [dragging, setDragging] = useState<string | null>(null);
  const [over, setOver] = useState<DesignStage | null>(null);
  const dragged = useRef<DesignRequestItem | null>(null);
  const viewer = {
    id: me.id,
    isDesigner: data.viewer.isDesigner,
    isWorker: data.viewer.canAssign || isDirector(me),
    isLeadership: me.role === 'leadership',
  };
  const [announcement, setAnnouncement] = useState('');
  const targets = dragged.current ? designMoveTargets(dragged.current, viewer) : [];

  return (
    <div class="kanban" role="region" tabIndex={0} aria-label="Design request board" aria-busy={pending} data-board>
      <p class="visually-hidden" id="design-drag-help">Space to pick up a card, arrow keys to choose a stage, Space to drop, Escape to cancel.</p>
      <p class="visually-hidden" role="status">{announcement}</p>
      {DESIGN_STAGES.map((stage) => {
        const items = all.filter((i) => i.stage === stage);
        const canDrop = dragging !== null && targets.includes(stage);
        return (
          <section
            class={`kanban-col${over === stage && canDrop ? ' is-over' : ''}${dragging ? canDrop ? ' is-target' : ' is-unavailable' : ''}`}
            data-drop-allowed={dragging ? String(canDrop) : undefined}
            data-stage={stage}
            key={stage}
            aria-labelledby={`col-${stage}`}
            onDragOver={(e) => {
              if (!canDrop) return;
              e.preventDefault();
              setOver(stage);
            }}
            onDragLeave={() => setOver((o) => (o === stage ? null : o))}
            onDrop={(e) => {
              e.preventDefault();
              const it = dragged.current;
              setOver(null);
              setDragging(null);
              dragged.current = null;
              if (it && canDrop) void onMove(it, stage);
            }}
          >
            <div class="kanban-col-head">
              <h3 class="kanban-col-name" id={`col-${stage}`}>
                {STAGE_LABEL[stage]}
              </h3>
              <span class="kanban-count">
                {items.length}
                <span class="visually-hidden"> {items.length === 1 ? 'request' : 'requests'}</span>
              </span>
            </div>
            <ul class="kanban-list">
              {items.map((item) => {
                const movable = designMoveTargets(item, viewer).length > 0 && viewer.isWorker;
                return (
                  <li
                    class={`kanban-card${dragging === item.id ? ' is-dragging' : ''}${item.assignee?.id === me.id ? ' is-mine' : ''}`}
                    key={item.id}
                    data-request={item.id}
                    tabIndex={movable ? 0 : undefined}
                    aria-label={movable ? `${item.title}, ${STAGE_LABEL[item.stage]}` : undefined}
                    aria-describedby={movable ? 'design-drag-help' : undefined}
                    onKeyDown={(e) => {
                      if (e.target !== e.currentTarget || !movable || pending) return;
                      if (e.key === 'Escape') {
                        e.preventDefault(); dragged.current = null; setDragging(null); setOver(null); setAnnouncement('Move cancelled.');
                      } else if (e.key === ' ') {
                        e.preventDefault();
                        if (dragging === item.id) {
                          const destination = over;
                          dragged.current = null; setDragging(null); setOver(null);
                          if (destination && targets.includes(destination)) void onMove(item, destination);
                          else setAnnouncement('Move cancelled.');
                        } else {
                          dragged.current = item; setDragging(item.id); setOver(null);
                          setAnnouncement(`${item.title} picked up. Use arrow keys to choose a stage.`);
                        }
                      } else if (dragging === item.id && ['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(e.key)) {
                        e.preventDefault();
                        const delta = e.key === 'ArrowLeft' || e.key === 'ArrowUp' ? -1 : 1;
                        const index = over ? targets.indexOf(over) : delta > 0 ? -1 : 0;
                        const destination = targets[(index + delta + targets.length) % targets.length]!;
                        setOver(destination); setAnnouncement(`${STAGE_LABEL[destination]}. Space to drop.`);
                        document.getElementById(`col-${destination}`)?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
                      }
                    }}
                    data-movable={movable}
                    draggable={movable && !pending}
                    onDragStart={(e) => {
                      if (!movable || pending || dragged.current) { e.preventDefault(); return; }
                      dragged.current = item;
                      setDragging(item.id);
                      e.dataTransfer?.setData('text/plain', item.id);
                    }}
                    onDragEnd={() => {
                      dragged.current = null;
                      setDragging(null);
                      setOver(null);
                    }}
                  >
                    {movable && <button type="button" class="linkish design-drag-handle" disabled={pending} tabIndex={-1} aria-label={`Drag ${item.title}`} style={{ touchAction: 'none' }}
                      onPointerDown={(e) => {
                        if (e.button !== 0) return;
                        e.currentTarget.setPointerCapture(e.pointerId);
                        dragged.current = item; setDragging(item.id); setOver(null);
                      }}
                      onPointerMove={(e) => {
                        if (!e.currentTarget.hasPointerCapture(e.pointerId)) return;
                        const board = e.currentTarget.closest<HTMLElement>('.kanban');
                        if (board) {
                          const rect = board.getBoundingClientRect();
                          if (e.clientX > Math.min(rect.right, innerWidth) - 32) board.scrollLeft += 24;
                          else if (e.clientX < Math.max(rect.left, 0) + 32) board.scrollLeft -= 24;
                        }
                        const col = document.elementFromPoint(e.clientX, e.clientY)?.closest<HTMLElement>('.kanban-col');
                        const target = col?.dataset.stage as DesignStage | undefined;
                        setOver(target && targets.includes(target) ? target : null);
                      }}
                      onPointerUp={(e) => {
                        if (!e.currentTarget.hasPointerCapture(e.pointerId)) return;
                        e.currentTarget.releasePointerCapture(e.pointerId);
                        const col = document.elementFromPoint(e.clientX, e.clientY)?.closest<HTMLElement>('.kanban-col');
                        const target = col?.dataset.stage as DesignStage | undefined;
                        dragged.current = null; setDragging(null); setOver(null);
                        if (target && targets.includes(target)) void onMove(item, target);
                      }}
                      onPointerCancel={() => { dragged.current = null; setDragging(null); setOver(null); }}
                    >⠿ <span class="small">Drag</span></button>}
                    <CardBody item={item} menu={menu(item, data)} />
                  </li>
                );
              })}
              {canDrop && over === stage && <li class="kanban-drop" aria-hidden="true" />}
            </ul>
            {items.length === 0 && !(canDrop && over === stage) && <p class="kanban-empty">Nothing {stage === 'delivered' ? 'delivered yet' : 'here'}</p>}
          </section>
        );
      })}
    </div>
  );
}

/** My work: assigned to me, waiting on my approval, my open requests, and what nobody has taken. */
function MyWork({ data, me, menu }: { data: DesignRequestsListResponse; me: CurrentUser; menu: Menu }) {
  const w = myWork(data.items, me.id);
  const sections: {
    key: string;
    title: string;
    items: DesignRequestItem[];
    empty: string;
    show: boolean;
  }[] = [
    {
      key: 'assigned',
      title: 'Assigned to you',
      items: w.assigned,
      empty: data.viewer.isDesigner ? 'Nothing assigned to you. Pick something up below.' : '',
      show: data.viewer.isDesigner || w.assigned.length > 0,
    },
    {
      key: 'approve',
      title: 'Waiting on your approval',
      items: w.approve,
      empty: 'No versions waiting for you.',
      show: true,
    },
    {
      key: 'requested',
      title: 'Your open requests',
      items: w.requested,
      empty: "You haven't requested anything that's still open.",
      show: true,
    },
    {
      key: 'unassigned',
      title: 'Up for grabs',
      items: w.unassigned,
      empty: 'Every open request has a designer.',
      show: data.viewer.isDesigner || data.viewer.canAssign,
    },
  ];
  return (
    <div class="mywork" data-mywork>
      {sections
        .filter((s) => s.show)
        .map((s) => (
          <section class="mywork-section" key={s.key} aria-labelledby={`mywork-${s.key}`} data-mywork-section={s.key}>
            <h3 class="mywork-head" id={`mywork-${s.key}`}>
              {s.title} <span class="kanban-count">{s.items.length}</span>
            </h3>
            {s.items.length === 0 ? (
              <p class="mywork-empty">{s.empty}</p>
            ) : (
              <ul class="mywork-list">
                {s.items.map((item) => (
                  <li class={`kanban-card${item.assignee?.id === me.id ? ' is-mine' : ''}`} key={item.id} data-request={item.id}>
                    <CardBody item={item} menu={menu(item, data)} />
                  </li>
                ))}
              </ul>
            )}
          </section>
        ))}
    </div>
  );
}

function List({ data, items, menu }: { data: DesignRequestsListResponse; items: DesignRequestItem[]; menu: Menu }) {
  const order = (s: DesignStage) => DESIGN_STAGES.indexOf(s);
  const rows = items.slice().sort((a, b) => order(a.stage) - order(b.stage) || (a.dueDate ?? '9999').localeCompare(b.dueDate ?? '9999'));
  const columns: Column<DesignRequestItem>[] = [
    {
      key: 'title',
      label: 'Request',
      primary: true,
      render: (i) => (
        <>
          <a class="cell-title" href={href.designRequest(i.id)}>
            {i.title}
          </a>
          <span class="cell-sub">
            {TYPE_LABEL[i.type]}
            {i.event ? ` · ${i.event.name} · ${fmtShort(i.event.startsAt)}` : i.program ? ` · ${i.program}` : ''}
          </span>
        </>
      ),
    },
    {
      key: 'stage',
      label: 'Stage',
      render: (i) => <StageBadge stage={i.stage} />,
    },
    { key: 'due', label: 'Due', render: (i) => <Due item={i} /> },
    {
      key: 'designer',
      label: 'Designer',
      render: (i) => <Assignee item={i} />,
    },
    {
      key: 'formats',
      label: 'Sizes / formats',
      render: (i) => i.formats || '—',
    },
    {
      key: 'actions',
      label: <span class="visually-hidden">Actions</span>,
      cardLabel: 'Actions',
      render: (i) => <RowMenu label={`Actions for ${i.title}`} items={menu(i, data)} />,
    },
  ];
  return <DataTable rows={rows} columns={columns} rowKey={(i) => i.id} caption="Design requests list" />;
}
