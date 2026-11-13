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
  byDesigner,
  DESIGN_FILTERS,
  DESIGN_LANES,
  designStats,
  designMoveTargets,
  dueState,
  dueText,
  FILTER_LABEL,
  INDICATOR_LABEL,
  indicatorDetail,
  LANE_LABEL,
  laneOf,
  laneRefusal,
  laneTarget,
  matchesFilter,
  myWork,
  needsApproval,
  railSummary,
  STAGE_LABEL,
  thisWeek,
  TYPE_HUE,
  TYPE_LABEL,
  type DesignFilter,
  type DesignLane,
  fmtDue,
} from '../../lib/design';
import { fmtShort, fmtWeekday, plural, todayPacific } from '../../lib/format';
import { setParams, useSearch } from '../../lib/nav';
import { href } from '../../lib/routes';
import { StatRow } from '../components/StatRow';
import { Drawer } from '../components/Drawer';
import { DesignLinks } from '../components/DesignLinks';
import { Icon, type IconName } from '../components/Icon';
import { DataTable, type Column } from '../components/kit';
import { useFlip } from '../components/motion';
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
type Menu = (i: DesignRequestItem, d: DesignRequestsListResponse) => MenuItem[];
type Toast = { text: string; undo?: () => void };
type Viewer = { id: string; isDesigner: boolean; isWorker: boolean; isLeadership: boolean };

const TYPE_ICON: Record<DesignType, IconName> = { poster: 'image', social: 'social', slides: 'slides', merch: 'shirt', video: 'video', other: 'sparkle' };
/** Delivered work shown in the Done gallery before "Show all". */
const DONE_SHOWN = 6;

export default function DesignView({ me }: { me: CurrentUser }) {
  const search = useSearch();
  const v = search.get('view');
  const view: View = v === 'board' || v === 'list' || v === 'mine' ? v : 'board';
  const f = search.get('filter');
  const filter: DesignFilter = (DESIGN_FILTERS as readonly string[]).includes(f ?? '') ? (f as DesignFilter) : 'all';
  const t = search.get('type');
  const type: DesignType | null = (DESIGN_TYPES as readonly string[]).includes(t ?? '') ? (t as DesignType) : null;
  const l = search.get('lane');
  // Phones show one lane at a time (stage tabs); wider screens show all four.
  const lane: DesignLane = (DESIGN_LANES as readonly string[]).includes(l ?? '') ? (l as DesignLane) : 'todo';
  const openId = search.get('id');
  const creating = search.get('new') === '1';
  const eventId = search.get('eventId') ?? undefined;

  const state = useApi('designRequestsList');
  const [title, setTitle] = useState('Design request');
  const [toast, setToast] = useState<Toast | null>(null);
  const [live, setLive] = useState('');
  /** Bumped when something lands in Done: the lane plays its short celebration. */
  const [party, setParty] = useState(0);
  useEffect(() => setTitle('Design request'), [openId]);
  useEffect(() => {
    if (!toast) return;
    const t = setTimeout(() => setToast(null), toast.undo ? 8000 : 6000);
    return () => clearTimeout(t);
  }, [toast]);
  useEffect(() => {
    if (!party) return;
    const t = setTimeout(() => setParty(0), 1400);
    return () => clearTimeout(t);
  }, [party]);

  const viewer = (d: DesignRequestsListResponse): Viewer => ({
    id: me.id,
    isDesigner: d.viewer.isDesigner,
    isWorker: d.viewer.canAssign || isDirector(me),
    isLeadership: me.role === 'leadership',
  });

  const moving = useRef(false);
  const [movePending, setMovePending] = useState(false);
  const move = async (item: DesignRequestItem, to: DesignStage, opts: { undoing?: boolean } = {}) => {
    if (to === 'delivered' && !confirm('Deliver without the requester’s approval? This leadership override will be recorded in the audit log.')) return;
    if (state.status !== 'ready' || moving.current) return;
    moving.current = true;
    setMovePending(true);
    const restoreFocus = document.activeElement?.getAttribute('data-request') === item.id;
    const focusCard = () => { if (restoreFocus) requestAnimationFrame(() => document.querySelector<HTMLElement>(`[data-request="${item.id}"]`)?.focus()); };
    const before = state.data;
    const v = viewer(before);
    state.set({ ...before, items: before.items.map((i) => (i.id === item.id ? { ...i, stage: to } : i)) });
    focusCard();
    try {
      await api('designRequestMove', {
        params: { id: item.id },
        body: { stage: to, confirmDeliveryOverride: to === 'delivered' },
      });
      setLive(`${item.title} moved to ${LANE_LABEL[laneOf(to)]} (${STAGE_LABEL[to]}).`);
      if (to === 'delivered') setParty(Date.now());
      // Undo puts it back where it was, when the same rules allow that move.
      const back = !opts.undoing && designMoveTargets({ ...item, stage: to }, v).includes(item.stage);
      setToast({
        text: to === 'delivered' ? `“${item.title}” delivered` : opts.undoing ? `“${item.title}” moved back to ${LANE_LABEL[laneOf(to)]}` : `“${item.title}” moved to ${LANE_LABEL[laneOf(to)]}`,
        undo: back ? () => void move({ ...item, stage: to }, item.stage, { undoing: true }) : undefined,
      });
      state.reload();
    } catch (err) {
      state.set(before);
      setToast({ text: `Couldn't move “${item.title}”: ${errorMessage(err)}` });
    } finally {
      moving.current = false;
      setMovePending(false);
      focusCard();
    }
  };

  // Requests that reach Done some other way (the requester approves in the detail) celebrate too.
  const stages = useRef<Map<string, DesignStage> | null>(null);
  const items = state.status === 'ready' ? state.data.items : null;
  useEffect(() => {
    if (!items) return;
    const prev = stages.current;
    stages.current = new Map(items.map((i) => [i.id, i.stage]));
    if (!prev || moving.current) return;
    if (items.some((i) => i.stage === 'delivered' && prev.has(i.id) && prev.get(i.id) !== 'delivered')) setParty(Date.now());
  }, [items]);

  const assignMe = async (item: DesignRequestItem) => {
    try {
      await api('designRequestAssign', {
        params: { id: item.id },
        body: { assigneeId: me.id },
      });
      setLive(`${item.title} assigned to you.`);
      state.reload();
    } catch (err) {
      setToast({ text: `Couldn't assign “${item.title}”: ${errorMessage(err)}` });
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
      setToast({ text: `Couldn't update “${e.name}”: ${errorMessage(err)}` });
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
    setToast({
      text: uploadFailed
        ? storageOff
          ? STORAGE_UNAVAILABLE_MESSAGE
          : `Request created, but ${plural(uploadFailed, 'file')} didn't upload. Add them from the request.`
        : `“${request.title}” is on the board.`,
    });
  };

  const stats = state.status === 'ready' && designStats(state.data.items, me.id);
  return (
    <div class="stack design-page">
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

      <Async state={state} loading={<Loading rows={6} label="Loading design requests" />}>
        {(d) => {
          const today = todayPacific();
          const shown = d.items.filter((i) => matchesFilter(i, filter, me.id, today) && (!type || i.type === type));
          return (
            <div class="stack design-body">
              {d.items.length > 0 && view === 'board' && <Pipeline items={d.items} />}
              {d.items.length > 0 && view === 'board' && <ThisWeek items={thisWeek(d.items, me.id, today)} onOpen={(id) => setParams({ id })} />}
              <div class="design-toolbar">
                <a class="btn btn--primary" href={href.newDesignRequest()} onClick={(e) => (e.preventDefault(), setParams({ new: 1, eventId: null }))} data-new-design>
                  <Icon name="plus" size={16} /> New request
                </a>
                {view !== 'mine' && d.items.length > 0 && (
                  <div class="design-filters" role="group" aria-label="Filter requests">
                    {DESIGN_FILTERS.map((k) => (
                      <button type="button" class="design-filter" key={k} aria-pressed={filter === k} data-filter={k} onClick={() => setParams({ filter: k === 'all' ? null : k }, { replace: true })}>
                        {FILTER_LABEL[k]}
                      </button>
                    ))}
                    <label class="design-type">
                      <span class="visually-hidden">Type</span>
                      <select class="select" value={type ?? ''} onChange={(e) => setParams({ type: e.currentTarget.value || null }, { replace: true })} data-type-filter aria-label="Type">
                        <option value="">All types</option>
                        {DESIGN_TYPES.map((k) => (
                          <option value={k} key={k}>
                            {TYPE_LABEL[k]}
                          </option>
                        ))}
                      </select>
                    </label>
                  </div>
                )}
                <div class="segmented" role="group" aria-label="Board view">
                  {(['board', 'list', 'mine'] as const).map((k) => (
                    <button type="button" class="segment" key={k} aria-pressed={view === k} data-view={k} onClick={() => setParams({ view: k }, { replace: true })}>
                      {VIEW_LABEL[k]}
                    </button>
                  ))}
                </div>
              </div>
              {d.designers.length === 0 && d.items.length > 0 && <p class="notice notice--attention">Nobody is on the Design subteam yet, so no one can pick these up.</p>}
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
              ) : shown.length === 0 ? (
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
                <Board
                  pending={movePending}
                  data={d}
                  items={shown}
                  me={me}
                  viewer={viewer(d)}
                  menu={menu}
                  onMove={move}
                  lane={lane}
                  onLane={(k) => setParams({ lane: k === 'todo' ? null : k }, { replace: true })}
                  scope={`${filter}|${type ?? ''}`}
                  party={party}
                  onOpen={(id) => setParams({ id })}
                />
              ) : (
                <List data={d} items={shown} menu={menu} />
              )}
              <UpcomingRail events={d.upcomingEvents} onRequest={(e) => setParams({ new: 1, eventId: e.id, id: null })} onOpen={(id) => setParams({ id })} onSetNeed={setNeed} />
            </div>
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
              setToast({ text: `“${t}” was removed.` });
            }}
          />
        )}
      </Drawer>

      {toast && (
        <p class="toast design-toast" role="status">
          {party > 0 && <Icon name="check" size={16} />}
          <span>{toast.text}</span>
          {toast.undo && (
            <button
              type="button"
              class="linkish design-undo"
              onClick={() => {
                const undo = toast.undo!;
                setToast(null);
                undo();
              }}
            >
              Undo
            </button>
          )}
        </p>
      )}
    </div>
  );
}

/** How the work splits across the four lanes, sized by count. */
function Pipeline({ items }: { items: DesignRequestItem[] }) {
  const n = (k: DesignLane) => items.filter((i) => laneOf(i.stage) === k).length;
  const label = DESIGN_LANES.map((k) => `${LANE_LABEL[k]} ${n(k)}`).join(', ');
  return (
    <div class="pipeline" role="img" aria-label={`Pipeline: ${label}`} data-pipeline>
      {DESIGN_LANES.map((k) => (
        <span class="pipeline-seg" data-lane={k} key={k} style={{ flexGrow: Math.max(n(k), 0.6) }}>
          <b>{n(k)}</b> {LANE_LABEL[k]}
        </span>
      ))}
    </div>
  );
}

/** The few requests that need attention this week: overdue, due within seven days, or waiting on my approval. */
function ThisWeek({ items, onOpen }: { items: DesignRequestItem[]; onOpen: (id: string) => void }) {
  return (
    <section class="thisweek" aria-labelledby="thisweek-h" data-this-week>
      <h2 class="thisweek-head" id="thisweek-h">
        <Icon name="clock" size={16} /> This week
      </h2>
      {items.length === 0 ? (
        <p class="thisweek-empty">Nothing due this week.</p>
      ) : (
        <ul class="thisweek-list">
          {items.map((i) => (
            <li key={i.id}>
              <a
                class="thisweek-item"
                href={href.designRequest(i.id)}
                data-hue={TYPE_HUE[i.type]}
                onClick={(e) => {
                  if (e.metaKey || e.ctrlKey || e.shiftKey) return;
                  e.preventDefault();
                  onOpen(i.id);
                }}
              >
                <span class="type-chip" aria-hidden="true">
                  <Icon name={TYPE_ICON[i.type]} size={15} />
                </span>
                <span class="thisweek-main">
                  <span class="thisweek-title">{i.title}</span>
                  <DueChip item={i} />
                </span>
                {i.assignee ? <Avatar name={i.assignee.name} id={i.assignee.id} src={i.assignee.avatarUrl} size="sm" /> : <span class="avatar-empty" aria-hidden="true">?</span>}
              </a>
            </li>
          ))}
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
  if (item.stage === 'delivered') return <span class="due-chip is-done"><Icon name="check" size={12} /> Delivered</span>;
  if (!item.dueDate) return <span class="due-chip is-none">No due date</span>;
  const s = dueState(item);
  return (
    <span class={`due-chip${s === 'overdue' ? ' is-overdue' : s === 'soon' ? ' is-soon' : ''}`} data-due={s}>
      <Icon name="clock" size={12} />
      {fmtDue(item.dueDate)}
      {s === 'overdue' ? ' · Overdue' : s === 'soon' ? ' · Due soon' : ''}
    </span>
  );
}

/**
 * One request card: type icon and title, the event and its date, approval/due/urgency chips,
 * then the designer, who asked, the design-file buttons and the ⋯ menu.
 */
function CardBody({ item, menu, grip }: { item: DesignRequestItem; menu: MenuItem[]; grip?: preact.ComponentChildren }) {
  return (
    <>
      <div class="kanban-card-top">
        <span class="type-chip" data-hue={TYPE_HUE[item.type]} title={TYPE_LABEL[item.type]}>
          <Icon name={TYPE_ICON[item.type]} size={15} />
          <span class="visually-hidden">{TYPE_LABEL[item.type]}</span>
        </span>
        <p class="kanban-card-title">
          <a href={href.designRequest(item.id)} draggable={false}>{item.title}</a>
        </p>
        {grip}
      </div>
      {item.event ? (
        <p class="kanban-event">
          <Icon name="calendar" size={13} />
          <a href={href.event(item.event.id)} draggable={false}>{item.event.name}</a>
          <span class="kanban-event-when">· {fmtShort(item.event.startsAt)}</span>
        </p>
      ) : (
        item.program && <p class="kanban-event">{item.program}</p>
      )}
      <div class="kanban-card-meta">
        {needsApproval(item) && (
          <span class="approval-chip" title="New request: the design team hasn't accepted it yet">
            Needs approval
          </span>
        )}
        <DueChip item={item} />
        {item.urgency === 'high' && item.stage !== 'delivered' && (
          <span class="urgent-flag" title="High urgency: the date cannot move">
            High
          </span>
        )}
      </div>
      <div class="kanban-card-foot">
        {item.assignee ? (
          <Avatar name={item.assignee.name} id={item.assignee.id} src={item.assignee.avatarUrl} size="sm" />
        ) : (
          <span class="avatar-empty" title="Unassigned" aria-hidden="true">?</span>
        )}
        <span class="kanban-from">
          <span class="visually-hidden">{item.assignee ? `Designer ${item.assignee.name}, ` : 'Unassigned, '}</span>from {item.requester.name.split(/\s+/)[0]}
        </span>
        <DesignLinks links={{}} title={item.title} />
        <RowMenu label={`Actions for ${item.title}`} items={menu} floating />
      </div>
    </>
  );
}

/** A short burst over the Done lane when work lands there (nothing under reduced motion). */
function Confetti() {
  return (
    <span class="confetti" aria-hidden="true" data-confetti>
      {Array.from({ length: 14 }, (_, i) => (
        <i key={i} style={{ '--a': `${(i / 14) * 360}deg`, '--d': `${26 + (i % 3) * 12}px`, '--c': `var(--c-${['green', 'blue', 'amber', 'coral', 'violet', 'teal', 'green'][i % 7]})` }} />
      ))}
    </span>
  );
}

function Board({
  pending,
  data,
  items: all,
  me,
  viewer,
  menu,
  onMove,
  lane: shownLane,
  onLane,
  scope,
  party,
  onOpen,
}: {
  pending: boolean;
  data: DesignRequestsListResponse;
  items: DesignRequestItem[];
  me: CurrentUser;
  viewer: Viewer;
  menu: Menu;
  onMove: (i: DesignRequestItem, to: DesignStage) => Promise<void>;
  lane: DesignLane;
  onLane: (l: DesignLane) => void;
  scope: string;
  party: number;
  onOpen: (id: string) => void;
}) {
  const [dragging, setDragging] = useState<string | null>(null);
  const [over, setOver] = useState<DesignLane | null>(null);
  const [allDone, setAllDone] = useState(false);
  const [announcement, setAnnouncement] = useState('');
  const dragged = useRef<DesignRequestItem | null>(null);
  const root = useRef<HTMLDivElement>(null);
  // Cards glide to their new lane after a move (FLIP; off under reduced motion).
  useFlip(root, scope, { skip: (id) => id === dragging, revision: all.map((i) => i.id + i.stage).join() });

  const allowed = (l: DesignLane) => (dragged.current ? laneTarget(dragged.current, l, viewer) !== null : false);
  const lanesFor = (item: DesignRequestItem) => DESIGN_LANES.filter((l) => laneTarget(item, l, viewer) !== null);
  const reset = () => {
    dragged.current = null;
    setDragging(null);
    setOver(null);
  };
  const drop = (item: DesignRequestItem, l: DesignLane | null) => {
    const to = l ? laneTarget(item, l, viewer) : null;
    reset();
    if (to) {
      if (l) onLane(l);
      void onMove(item, to);
    } else setAnnouncement('Move cancelled.');
  };
  const pick = (item: DesignRequestItem) => {
    dragged.current = item;
    setDragging(item.id);
    setOver(null);
  };
  const laneAt = (x: number, y: number) => {
    const el = document.elementFromPoint(x, y)?.closest<HTMLElement>('[data-lane]');
    return el && root.current?.contains(el) ? (el.dataset.lane as DesignLane) : null;
  };

  const count = (l: DesignLane) => all.filter((i) => laneOf(i.stage) === l).length;
  // While dragging: allowed lanes are outlined, refused lanes fade; the card's own lane stays as it is.
  const laneClass = (l: DesignLane) => {
    const from = dragged.current && laneOf(dragged.current.stage);
    if (!dragging || from === l) return '';
    return `${allowed(l) ? ' is-target' : ' is-unavailable'}${over === l && allowed(l) ? ' is-over' : ''}`;
  };

  const card = (item: DesignRequestItem, gallery = false) => {
    const movable = viewer.isWorker && lanesFor(item).length > 0;
    const grip = movable ? (
      <button
        type="button"
        class="design-drag-handle"
        disabled={pending}
        tabIndex={-1}
        aria-label={`Drag ${item.title}`}
        title="Drag to another lane"
        style={{ touchAction: 'none' }}
        onPointerDown={(e) => {
          if (e.button !== 0) return;
          e.currentTarget.setPointerCapture(e.pointerId);
          pick(item);
        }}
        onPointerMove={(e) => {
          if (!e.currentTarget.hasPointerCapture(e.pointerId)) return;
          const scroller = e.currentTarget.closest<HTMLElement>('.board-lanes');
          if (scroller && scroller.scrollWidth > scroller.clientWidth) {
            const rect = scroller.getBoundingClientRect();
            if (e.clientX > Math.min(rect.right, innerWidth) - 32) scroller.scrollLeft += 24;
            else if (e.clientX < Math.max(rect.left, 0) + 32) scroller.scrollLeft -= 24;
          }
          const l = laneAt(e.clientX, e.clientY);
          setOver(l && laneTarget(item, l, viewer) ? l : null);
        }}
        onPointerUp={(e) => {
          if (!e.currentTarget.hasPointerCapture(e.pointerId)) return;
          e.currentTarget.releasePointerCapture(e.pointerId);
          drop(item, laneAt(e.clientX, e.clientY));
        }}
        onPointerCancel={reset}
      >
        <Icon name="grip" size={14} />
      </button>
    ) : undefined;
    return (
      <li
        class={`kanban-card${gallery ? ' kanban-card--shipped' : ''}${dragging === item.id ? ' is-dragging' : ''}${item.assignee?.id === me.id ? ' is-mine' : ''}`}
        key={item.id}
        data-request={item.id}
        data-flip={item.id}
        data-hue={TYPE_HUE[item.type]}
        tabIndex={movable ? 0 : undefined}
        aria-label={movable ? `${item.title}, ${LANE_LABEL[laneOf(item.stage)]}` : undefined}
        aria-describedby={movable ? 'design-drag-help' : undefined}
        data-movable={movable}
        draggable={movable && !pending}
        onKeyDown={(e) => {
          if (e.target !== e.currentTarget) return;
          if (e.key === 'Enter' && !dragging) {
            e.preventDefault();
            onOpen(item.id);
            return;
          }
          if (!movable || pending) return;
          const targets = lanesFor(item);
          if (e.key === 'Escape' && dragging === item.id) {
            e.preventDefault();
            reset();
            setAnnouncement('Move cancelled.');
          } else if (e.key === ' ') {
            e.preventDefault();
            if (dragging === item.id) drop(item, over);
            else {
              pick(item);
              setAnnouncement(`${item.title} picked up. Arrow keys choose a lane: ${targets.map((l) => LANE_LABEL[l]).join(', ')}. Space drops, Escape cancels.`);
            }
          } else if (dragging === item.id && ['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(e.key)) {
            e.preventDefault();
            const delta = e.key === 'ArrowLeft' || e.key === 'ArrowUp' ? -1 : 1;
            const index = over ? targets.indexOf(over) : delta > 0 ? -1 : 0;
            const next = targets[(index + delta + targets.length) % targets.length]!;
            setOver(next);
            setAnnouncement(`${LANE_LABEL[next]}. Space to drop.`);
            document.getElementById(`lane-${next}`)?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
          }
        }}
        onDragStart={(e) => {
          if (!movable || pending || dragged.current) {
            e.preventDefault();
            return;
          }
          if (e.dataTransfer) {
            e.dataTransfer.effectAllowed = 'move';
            e.dataTransfer.setData('text/plain', item.id);
          }
          pick(item);
        }}
        onDragEnd={reset}
      >
        {gallery ? (
          <>
            <a class="shipped-thumb" href={href.designRequest(item.id)} draggable={false} tabIndex={-1} aria-hidden="true">
              <Icon name={TYPE_ICON[item.type]} size={22} />
            </a>
            <div class="shipped-row">
              <p class="kanban-card-title">
                <a href={href.designRequest(item.id)} draggable={false}>{item.title}</a>
              </p>
              {grip}
            </div>
            <div class="shipped-foot">
              <span class="shipped-when">{item.event ? item.event.name : TYPE_LABEL[item.type]}</span>
              <DesignLinks links={{}} title={item.title} />
              <RowMenu label={`Actions for ${item.title}`} items={menu(item, data)} floating />
            </div>
          </>
        ) : (
          <CardBody item={item} menu={menu(item, data)} grip={grip} />
        )}
      </li>
    );
  };

  return (
    <div class="board" ref={root} role="region" aria-label="Design request board" aria-busy={pending} data-board data-dragging={dragging ? 'true' : undefined}>
      <p class="visually-hidden" id="design-drag-help">
        Enter opens the request. Space picks the card up, arrow keys choose a lane, Space drops it, Escape cancels.
      </p>
      <p class="visually-hidden" role="status">
        {announcement}
      </p>
      {/* Phones: one lane at a time. The tabs are drop targets too, for the drag handle. */}
      <div class="board-tabs" role="tablist" aria-label="Lanes">
        {DESIGN_LANES.map((l) => (
          <button
            type="button"
            role="tab"
            class={`board-tab${laneClass(l)}`}
            key={l}
            data-lane={l}
            data-drop-allowed={dragging ? String(allowed(l)) : undefined}
            aria-selected={shownLane === l}
            aria-controls={`lane-panel-${l}`}
            onClick={() => onLane(l)}
          >
            <b>{count(l)}</b>
            <span>{LANE_LABEL[l]}</span>
            {l === 'done' && party > 0 && <Confetti />}
          </button>
        ))}
      </div>
      <div class="board-lanes">
        {DESIGN_LANES.map((l) => {
          const items = all.filter((i) => laneOf(i.stage) === l);
          const canDrop = dragging !== null && allowed(l);
          const done = l === 'done' ? items.slice().sort((a, b) => b.updatedAt.localeCompare(a.updatedAt)) : [];
          return (
            <section
              class={`lane kanban-col${laneClass(l)}`}
              data-lane={l}
              data-active={shownLane === l ? 'true' : undefined}
              data-drop-allowed={dragging ? String(canDrop) : undefined}
              id={`lane-panel-${l}`}
              key={l}
              aria-labelledby={`lane-${l}`}
              onDragOver={(e) => {
                if (!canDrop) return;
                e.preventDefault();
                if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
                if (over !== l) setOver(l);
              }}
              onDragLeave={(e) => {
                if (!(e.currentTarget as HTMLElement).contains(e.relatedTarget as Node | null)) setOver((o) => (o === l ? null : o));
              }}
              onDrop={(e) => {
                e.preventDefault();
                const it = dragged.current;
                if (it) drop(it, canDrop ? l : null);
              }}
            >
              <header class="lane-head">
                <span class="lane-dot" aria-hidden="true" />
                <h3 class="lane-name" id={`lane-${l}`}>
                  {LANE_LABEL[l]}
                </h3>
                <span class="lane-count">
                  {items.length}
                  <span class="visually-hidden"> {items.length === 1 ? 'request' : 'requests'}</span>
                </span>
                {l === 'done' && party > 0 && <Confetti />}
              </header>
              {dragging && !canDrop && dragged.current && laneOf(dragged.current.stage) !== l && <p class="lane-refusal">{laneRefusal(l, viewer)}</p>}
              {l === 'done' ? (
                <>
                  {items.length > 0 && (
                    <ul class="shipped-grid kanban-list" aria-label="Delivered work">
                      {(allDone ? done : done.slice(0, DONE_SHOWN)).map((i) => card(i, true))}
                    </ul>
                  )}
                  {done.length > DONE_SHOWN && (
                    <button type="button" class="linkish lane-more" onClick={() => setAllDone((v) => !v)}>
                      {allDone ? 'Show fewer' : `Show all ${done.length}`}
                    </button>
                  )}
                </>
              ) : (
                byDesigner(items, me.id).map((g) => (
                  <div class="lane-group" key={g.key || 'none'}>
                    <p class="lane-group-head">
                      {g.person ? <Avatar name={g.person.name} id={g.person.id} src={g.person.avatarUrl} size="sm" /> : <span class="avatar-empty" aria-hidden="true">?</span>}
                      <span class="lane-group-name">{g.name}</span>
                      <span class="lane-group-n">{g.items.length}</span>
                    </p>
                    <ul class="kanban-list">{g.items.map((i) => card(i))}</ul>
                  </div>
                ))
              )}
              {canDrop && over === l && <div class="kanban-drop" aria-hidden="true" />}
              {items.length === 0 && !(canDrop && over === l) && <p class="kanban-empty">{l === 'done' ? 'Nothing delivered yet' : l === 'review' ? 'Nothing waiting for review' : 'Nothing here'}</p>}
            </section>
          );
        })}
      </div>
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
