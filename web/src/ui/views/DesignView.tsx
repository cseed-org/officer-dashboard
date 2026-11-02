import { useEffect, useRef, useState } from 'preact/hooks';
import { api, STORAGE_UNAVAILABLE_MESSAGE } from '../../lib/api/client';
import { useApi } from '../../lib/api/useApi';
import { isDirector } from '../../lib/session';
import { DESIGN_STAGES, type CurrentUser, type DesignRequestItem, type DesignRequestsListResponse, type DesignStage } from '../../lib/contracts';
import { designMoveTargets, dueState, dueText, STAGE_LABEL, TYPE_HUE, TYPE_LABEL } from '../../lib/design';
import { plural } from '../../lib/format';
import { setParams, useSearch } from '../../lib/nav';
import { href } from '../../lib/routes';
import { Drawer } from '../components/Drawer';
import { DataTable, type Column } from '../components/kit';
import { Avatar, RowMenu, type MenuItem } from '../components/people';
import { Async, errorMessage, Loading } from '../components/states';
import DesignRequestDetail, { StageBadge } from './DesignRequestDetail';
import DesignRequestForm, { type Created } from './DesignRequestForm';

type View = 'board' | 'list';
const phone = () => typeof matchMedia !== 'undefined' && matchMedia('(max-width: 639px)').matches;

export default function DesignView({ me }: { me: CurrentUser }) {
  const search = useSearch();
  const v = search.get('view');
  // Phones default to the list; the board is one tap away.
  const view: View = v === 'board' || v === 'list' ? v : phone() ? 'list' : 'board';
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

  const viewer = (d: DesignRequestsListResponse) => ({ id: me.id, isDesigner: d.viewer.isDesigner, isWorker: d.viewer.canAssign });

  const move = async (item: DesignRequestItem, to: DesignStage) => {
    try {
      await api('designRequestMove', { params: { id: item.id }, body: { stage: to } });
      setLive(`${item.title} moved to ${STAGE_LABEL[to]}.`);
      state.reload();
    } catch (err) {
      setToast(`Couldn't move “${item.title}”: ${errorMessage(err)}`);
    }
  };
  const assignMe = async (item: DesignRequestItem) => {
    try {
      await api('designRequestAssign', { params: { id: item.id }, body: { assigneeId: me.id } });
      setLive(`${item.title} assigned to you.`);
      state.reload();
    } catch (err) {
      setToast(`Couldn't assign “${item.title}”: ${errorMessage(err)}`);
    }
  };

  const menu = (item: DesignRequestItem, d: DesignRequestsListResponse): MenuItem[] => {
    const items: MenuItem[] = [{ label: 'Open request', onSelect: () => setParams({ id: item.id }) }];
    if (d.viewer.isDesigner && item.stage !== 'delivered' && item.assignee?.id !== me.id) items.push({ label: 'Assign to me', onSelect: () => void assignMe(item) });
    if (!d.viewer.canAssign) {
      // The requester decides in the drawer, next to the version: approve or request changes.
      if (item.stage === 'in-review' && item.requester.id === me.id) items.push({ label: 'Review and approve', onSelect: () => setParams({ id: item.id }) });
      return items;
    }
    for (const s of designMoveTargets(item, viewer(d))) items.push({ label: `Move to ${STAGE_LABEL[s]}`, onSelect: () => void move(item, s) });
    return items;
  };

  const onCreated = ({ request, uploadFailed, storageOff }: Created) => {
    state.reload();
    setParams({ new: null, eventId: null, id: request.id });
    setToast(uploadFailed ? (storageOff ? STORAGE_UNAVAILABLE_MESSAGE : `Request created, but ${plural(uploadFailed, 'file')} didn't upload. Add them from the request.`) : `“${request.title}” is on the board.`);
  };

  return (
    <div class="stack">
      <div class="toolbar">
        <div class="toolbar-main">
          <a class="btn btn--primary" href={href.newDesignRequest()} onClick={(e) => (e.preventDefault(), setParams({ new: 1, eventId: null }))} data-new-design>
            New design request
          </a>
        </div>
        <div class="segmented" role="group" aria-label="Board view">
          {(['board', 'list'] as const).map((k) => (
            <button type="button" class="segment" key={k} aria-pressed={view === k} onClick={() => setParams({ view: k }, { replace: true })}>
              {k === 'board' ? 'Board' : 'List'}
            </button>
          ))}
        </div>
      </div>

      <Async state={state} loading={<Loading rows={6} label="Loading design requests" />}>
        {(d) =>
          d.items.length === 0 ? (
            <div class="state state--brand" data-state="empty">
              <p class="state-title">No design requests yet</p>
              <p class="state-body">Your next event deserves a good first impression. Give the design team a clear brief.</p>
              <div class="state-action">
                <button type="button" class="btn" onClick={() => setParams({ new: 1 })}>
                  Request your first design
                </button>
              </div>
            </div>
          ) : (
            <>
              <p class="small muted">
                {plural(d.items.length, 'request')} · Shared with everyone · Deadlines shown in Vancouver time
                {d.designers.length === 0 && ' · Nobody is on the Design subteam yet, so no one can pick these up'}
              </p>
              {view === 'board' ? <Board data={d} me={me} menu={menu} onMove={move} /> : <List data={d} menu={menu} />}
            </>
          )
        }
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

function Assignee({ item }: { item: DesignRequestItem }) {
  return item.assignee ? (
    <span class="kanban-people">
      <Avatar name={item.assignee.name} id={item.assignee.id} src={item.assignee.avatarUrl} size="sm" />
      {item.assignee.name}
    </span>
  ) : (
    <span class="kanban-unassigned">Unassigned</span>
  );
}

function Due({ item }: { item: DesignRequestItem }) {
  const s = dueState(item);
  return <span class={`kanban-due${s === 'overdue' ? ' is-overdue' : s === 'soon' ? ' is-soon' : ''}`}>{dueText(item)}</span>;
}

function Board({
  data,
  me,
  menu,
  onMove,
}: {
  data: DesignRequestsListResponse;
  me: CurrentUser;
  menu: (i: DesignRequestItem, d: DesignRequestsListResponse) => MenuItem[];
  onMove: (i: DesignRequestItem, to: DesignStage) => Promise<void>;
}) {
  const [dragging, setDragging] = useState<string | null>(null);
  const [over, setOver] = useState<DesignStage | null>(null);
  const dragged = useRef<DesignRequestItem | null>(null);
  const viewer = { id: me.id, isDesigner: data.viewer.isDesigner, isWorker: data.viewer.canAssign || isDirector(me) };
  const targets = dragged.current ? designMoveTargets(dragged.current, viewer) : [];

  return (
    <div class="kanban" role="region" tabIndex={0} aria-label="Design request board" data-board>
      {DESIGN_STAGES.map((stage) => {
        const items = data.items.filter((i) => i.stage === stage);
        const canDrop = dragging !== null && targets.includes(stage);
        return (
          <section
            class={`kanban-col${over === stage && canDrop ? ' is-over' : ''}`}
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
                    class={`kanban-card${dragging === item.id ? ' is-dragging' : ''}`}
                    key={item.id}
                    data-request={item.id}
                    draggable={movable}
                    onDragStart={(e) => {
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
                    <p class="kanban-card-title">
                      <a href={href.designRequest(item.id)}>{item.title}</a>
                    </p>
                    <div class="kanban-card-meta">
                      <span class="kanban-tag" data-hue={TYPE_HUE[item.type]}>
                        {TYPE_LABEL[item.type]}
                      </span>
                      <Due item={item} />
                    </div>
                    {item.event ? (
                      <a class="kanban-link" href={href.event(item.event.id)}>
                        {item.event.name} <span aria-hidden="true">↗</span>
                      </a>
                    ) : (
                      item.program && <span class="kanban-link">{item.program}</span>
                    )}
                    {item.formats && <p class="kanban-card-sub">{item.formats}</p>}
                    <div class="kanban-card-foot">
                      <Assignee item={item} />
                      <RowMenu label={`Actions for ${item.title}`} items={menu(item, data)} />
                    </div>
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

function List({ data, menu }: { data: DesignRequestsListResponse; menu: (i: DesignRequestItem, d: DesignRequestsListResponse) => MenuItem[] }) {
  const order = (s: DesignStage) => DESIGN_STAGES.indexOf(s);
  const rows = data.items.slice().sort((a, b) => order(a.stage) - order(b.stage) || (a.dueDate ?? '9999').localeCompare(b.dueDate ?? '9999'));
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
            {i.event ? ` · ${i.event.name}` : i.program ? ` · ${i.program}` : ''}
          </span>
        </>
      ),
    },
    { key: 'stage', label: 'Stage', render: (i) => <StageBadge stage={i.stage} /> },
    { key: 'due', label: 'Due', render: (i) => <Due item={i} /> },
    { key: 'designer', label: 'Designer', render: (i) => <Assignee item={i} /> },
    { key: 'formats', label: 'Sizes / formats', render: (i) => i.formats || '—' },
    { key: 'actions', label: <span class="visually-hidden">Actions</span>, cardLabel: 'Actions', render: (i) => <RowMenu label={`Actions for ${i.title}`} items={menu(i, data)} /> },
  ];
  return <DataTable rows={rows} columns={columns} rowKey={(i) => i.id} caption="Design requests list" />;
}
