import DashboardTutorial from '../DashboardTutorial';
import type { ComponentChildren } from 'preact';
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'preact/hooks';
import { api } from '../../../lib/api/client';
import { timezoneSnapshot } from '../../../lib/timezone';
import { Icon } from '../../components/Icon';
import { Loading } from '../../components/states';
import { addSlot, blocks, fitsAt, gridColumns, migrateLayout, moveInOrder, pack, settle, sizeLabel, sizeName, trackAt, type GridCell, type Pin, type Placement } from './layout';
import { defaultWidgetLayout, widgetDef, widgetRegistry, type BlockSize } from '../registry';
import '../../../styles/home-grid.css';

import { useGridDrag } from './useGridDrag';
import { useGridResize, snappedSize } from './useGridResize';
import { useGridFlip } from './flip';

type Resize = { id: string; x: number; y: number; size: BlockSize; snapshot: Placement[]; pin: Pin; axis: string; width: number; height: number };
/** `at` is the last valid drop cell (the dragged widget's top-left), or null before the first. */
type Drag = { id: string; x: number; y: number; snapshot: Placement[]; at: { x: number; y: number } | null };

/**
 * Home as a grid of blocks (spec § "V0.4 Home block grid"). The layout lives on the server
 * (/api/me/prefs); V0.3 S/M/L layouts are migrated on load. `children` renders one widget's tile.
 */
export default function WidgetGrid({ children, entering = false }: { children: (id: string, size: BlockSize) => ComponentChildren; entering?: boolean }) {
  const [layout, setLayout] = useState<Placement[] | null>(() => {
    const prefs = timezoneSnapshot().prefs;
    return prefs ? migrateLayout(prefs.widgetLayout).layout : null;
  });
  const [saved, setSaved] = useState<Placement[]>([]);
  const [userEditing, setEditing] = useState(false);
  const [tourEditing, setTourEditing] = useState(false);
  const editing = userEditing || tourEditing;
  const tourSnapshot = useRef<Placement[] | null>(null);
  const previewTour = useCallback((edit: boolean, active: boolean) => {
    setTourEditing(edit && active);
    setLayout(current => {
      if (active && current && !tourSnapshot.current) {
        tourSnapshot.current = current;
        if (!current.some(w => w.id === 'links')) return [...current, { id: 'links', size: '2x1' }];
      }
      if (!active && tourSnapshot.current) {
        const original = tourSnapshot.current; tourSnapshot.current = null; return original;
      }
      return current;
    });
  }, []);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [loaded, setLoaded] = useState(false);
  const [tick, setTick] = useState(0);
  const [drag, setDrag] = useState<Drag | null>(null);
  const [resizing, setResizing] = useState<Resize | null>(null);
  const [removed, setRemoved] = useState<Placement[] | null>(null);
  const previewSize = useGridResize();
  const floating = useGridDrag();
  const pendingDrag = useRef<Drag | null>(null);
  const captured = useRef<{ element: HTMLElement; id: number } | null>(null);
  const [gridWidth, setGridWidth] = useState(0);
  const layoutCommit = useRef({ layout, count: 0 });
  if (layoutCommit.current.layout !== layout) {
    layoutCommit.current = { layout, count: layoutCommit.current.count + 1 };
  }
  const [moved, setMoved] = useState<string | null>(null);
  const [live, setLive] = useState('');
  const pointerY = useRef(0);
  const focusAfter = useRef<string | null>(null);
  const gallery = useRef<HTMLDialogElement>(null);
  const grid = useRef<HTMLUListElement>(null);

  useLayoutEffect(() => {
    const el = grid.current;
    if (!el) return;
    const measure = () => setGridWidth(el.clientWidth);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, [!!layout]);
  // Positions are always the dense packing of the order, so the edit preview, a save and a reload
  // agree. Edits keep `layout` in reading order (settle), so tab order follows the picture.
  const placed = useMemo<GridCell[]>(() => (layout && gridWidth ? pack(layout, gridWidth) : []), [layout, gridWidth]);
  useGridFlip(grid, placed, drag?.id, resizing?.id);
  // Every cell the dragged widget can really end up covering: try each spot its footprint fits
  // (one row past the end too), pack as a drop there would, and keep where it lands. Computed once
  // per drag from the snapshot, so the outlines hold still while the preview moves around them.
  const spots = useMemo(() => {
    const item = drag && drag.snapshot.find(w => w.id === drag.id);
    if (!drag || !item || !gridWidth) return [];
    const cols = gridColumns(gridWidth);
    const bottom = pack(drag.snapshot, gridWidth).reduce((m, c) => Math.max(m, c.y + c.h), 0);
    const covered = new Set<number>();
    for (let y = 0; y <= bottom; y++) for (let x = 0; x < cols; x++) {
      if (!fitsAt(item.size, { x, y }, gridWidth)) continue;
      const c = pack(settle(drag.snapshot, gridWidth, { id: drag.id, x, y }), gridWidth).find(p => p.id === drag.id)!;
      for (let dy = 0; dy < c.h; dy++) for (let dx = 0; dx < c.w; dx++) covered.add((c.y + dy) * cols + c.x + dx);
    }
    return [...covered].sort((a, b) => a - b).map(i => ({ x: i % cols, y: Math.floor(i / cols) }));
  }, [drag?.id, drag?.snapshot, gridWidth]);
  // The target ring scales in when the drop spot changes (not at drag start; never with reduced motion).
  const target = drag ? placed.find(c => c.id === drag.id) : undefined;
  const targetKey = target ? `${target.x},${target.y}` : '';
  const lastTarget = useRef('');
  useLayoutEffect(() => {
    const previous = lastTarget.current;
    lastTarget.current = targetKey;
    if (!previous || !targetKey || previous === targetKey || matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    grid.current?.querySelector('li.is-dragging')?.animate(
      [{ transform: 'scale(.94)', opacity: 0.35 }, { transform: 'scale(1)', opacity: 1 }],
      { duration: 220, easing: 'cubic-bezier(.2, .8, .2, 1)', pseudoElement: '::after' });
  }, [targetKey]);

  function releasePointer() {
    const capture = captured.current;
    if (capture?.element.hasPointerCapture(capture.id)) capture.element.releasePointerCapture(capture.id);
    captured.current = null;
    pendingDrag.current = null;
    floating.clear();
  }
  function cancelPointer() {
    const snapshot = drag?.snapshot ?? resizing?.snapshot ?? pendingDrag.current?.snapshot;
    if (snapshot) setLayout(snapshot);
    releasePointer();
    setDrag(null); setResizing(null);
    setLive(resizing ? 'Resize cancelled' : 'Move cancelled');
  }

  useEffect(() => {
    let alive = true;
    setError('');
    // Gate already fetched preferences for timezone initialization. Reuse that snapshot
    // on first mount; explicit retries still read the server. Drag/resize state is unchanged.
    const prefs = tick === 0 ? timezoneSnapshot().prefs : null;
    (prefs ? Promise.resolve(prefs) : api('prefsGet')).then(
      async (p) => {
        if (!alive) return;
        const { layout: l } = migrateLayout(p.widgetLayout);
        setLayout(l); setSaved(l);
        // Upgrade only legacy storage, once; ordinary loads must not write preferences.
        if (p.widgetLayout.some((w) => w.id === 'kpis' || ['S', 'M', 'L'].includes(w.size))) {
          try {
            await api('prefsPut', { body: { widgetLayout: l } });
          } catch (e) {
            if (alive) setError(`Couldn’t save the upgraded layout. Use Edit home and Done to retry. ${(e as Error).message}`);
          }
        }
        if (alive) setLoaded(true);
      },
      (e: Error) => { if (alive) { setError(e.message); setLayout(defaultWidgetLayout); setLoaded(false); } },
    );
    return () => { alive = false; };
  }, [tick]);

  // Keep focus on the control the person used after Preact moves or removes nodes.
  useEffect(() => {
    const sel = focusAfter.current;
    if (!sel) return;
    focusAfter.current = null;
    document.querySelector<HTMLElement>(sel)?.focus();
  });
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && (drag || resizing || pendingDrag.current)) {
        e.preventDefault(); cancelPointer();
      }
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [drag, resizing]);
  useEffect(() => {
    if (!moved) return;
    const t = setTimeout(() => setMoved(null), 700);
    return () => clearTimeout(t);
  }, [moved, layout]);

  useEffect(() => {
    if (!drag && !resizing) return;
    let frame: number;
    const scroll = () => {
      const y = pointerY.current;
      const delta = y < 90 ? -12 : y > innerHeight - 90 ? 12 : 0;
      if (delta) window.scrollBy(0, delta);
      frame = requestAnimationFrame(scroll);
    };
    frame = requestAnimationFrame(scroll);
    return () => cancelAnimationFrame(frame);
  }, [!!drag, !!resizing]);

  if (!layout) return <Loading rows={4} label="Loading widgets" />;
  const title = (id: string) => widgetDef(id)?.title ?? `Unavailable widget (${id})`;
  // Widgets carry explicit grid positions, so the Add tile must too: left to auto-placement it
  // would drop into the first hole above the last widget.
  const slot = placed.length ? addSlot(placed, gridWidth) : null;
  const available = widgetRegistry.filter((w) => !layout.some((p) => p.id === w.id));
  const announce = (id: string, l: Placement[]) => setLive(`${title(id)}, position ${l.findIndex((w) => w.id === id) + 1} of ${l.length}`);

  function moveBy(id: string, to: number) {
    const next = moveInOrder(layout!, id, to, gridWidth);
    if (next === layout) return;
    setLayout(next); setMoved(id); announce(id, next);
    focusAfter.current = `[data-widget="${CSS.escape(id)}"] .blk-handle`;
  }
  function onHandleKey(e: KeyboardEvent, id: string, i: number) {
    if (drag || resizing) return;
    if (e.shiftKey && e.key.startsWith('Arrow')) {
      e.preventDefault();
      const current = layout!.find(w => w.id === id)!;
      const b = blocks(current.size);
      const horizontal = e.key === 'ArrowLeft' || e.key === 'ArrowRight';
      const direction = e.key === 'ArrowRight' || e.key === 'ArrowDown' ? 1 : -1;
      const sizes = widgetDef(id)?.sizes.filter(s => horizontal ? (blocks(s).w - b.w) * direction > 0 : (blocks(s).h - b.h) * direction > 0) ?? [];
      const next = sizes.sort((a, c) => blocks(a).area - blocks(c).area);
      const target = direction > 0 ? next[0] : next.at(-1);
      if (target) resize(id, target);
      else setLive('No allowed size in that direction. Use the corner grip for other shapes.');
      return;
    }
    const to = { ArrowLeft: i - 1, ArrowUp: i - 1, ArrowRight: i + 1, ArrowDown: i + 1, Home: 0, End: layout!.length - 1 }[e.key];
    if (to === undefined) return;
    e.preventDefault();
    moveBy(id, to);
  }
  // Capture the tile, but leave clicks alone until the pointer crosses the 4px threshold.
  function onPointerDown(e: PointerEvent, id: string) {
    if (!editing || busy || e.button !== 0 || (e.target as HTMLElement).closest('.blk-remove, .blk-resize')) return;
    pointerY.current = e.clientY;
    const element = e.currentTarget as HTMLElement;
    element.setPointerCapture(e.pointerId);
    captured.current = { element, id: e.pointerId };
    pendingDrag.current = { id, x: e.clientX, y: e.clientY, snapshot: layout!, at: null };
  }
  function beginResize(e: PointerEvent, id: string, axis: string) {
    if (busy || e.button !== 0) return;
    e.preventDefault();
    e.stopPropagation();
    const cell = placed.find(c => c.id === id)!;
    previewSize.current = layout!.find(w => w.id === id)!.size;
    captured.current = { element: e.currentTarget as HTMLElement, id: e.pointerId };
    pointerY.current = e.clientY;
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    const rect = (e.currentTarget as HTMLElement).closest('li')!.getBoundingClientRect();
    setResizing({ id, x: e.clientX, y: e.clientY, size: layout!.find(w => w.id === id)!.size, snapshot: layout!, pin: { id, x: cell.x, y: cell.y }, axis, width: rect.width, height: rect.height });
  }
  function onPointerMove(e: PointerEvent) {
    pointerY.current = e.clientY;
    if (resizing) {
      const target = snappedSize(widgetDef(resizing.id)!.sizes, resizing.size, resizing.axis,
        e.clientX - resizing.x, e.clientY - resizing.y, resizing.width, resizing.height);
      if (previewSize.current !== target) {
        previewSize.current = target;
        // The resized widget keeps its top-left; the others pack around it.
        setLayout(settle(resizing.snapshot.map(w => w.id === resizing.id ? { ...w, size: target } : w), gridWidth, resizing.pin));
      }
      return;
    }
    let active = drag;
    if (!active && pendingDrag.current) {
      const pending = pendingDrag.current;
      if (Math.hypot(e.clientX - pending.x, e.clientY - pending.y) < 4) return;
      const tile = grid.current?.querySelector<HTMLElement>(`[data-widget="${CSS.escape(pending.id)}"]`);
      if (!tile) return;
      floating.start(tile);
      active = pending;
      pendingDrag.current = null;
    }
    if (!active) return;
    e.preventDefault();
    floating.move(e.clientX, e.clientY);
    // The target is the grid cell under the pointer (the floating copy's top-left), from the grid's
    // own tracks, so empty gaps and the row below the last tile are targets too. A spot the
    // footprint can't fit keeps the last valid target. The dragged widget is pinned there and the
    // rest pack around it, always from the drag-start snapshot, so nothing hops while the target
    // holds; the placeholder then sits where the drop will really land.
    const at = cellAt(e.clientX, e.clientY);
    const size = active.snapshot.find(w => w.id === active!.id)?.size;
    let target = active.at;
    if (at && size && fitsAt(size, at, gridWidth) && (at.x !== target?.x || at.y !== target?.y)) {
      target = at;
      setLayout(settle(active.snapshot, gridWidth, { id: active.id, ...at }));
    }
    setDrag({ ...active, x: e.clientX, y: e.clientY, at: target });
  }
  /** The grid cell (column, row) under a viewport point; one row past the last also counts. */
  function cellAt(clientX: number, clientY: number) {
    const el = grid.current;
    if (!el) return null;
    const s = getComputedStyle(el);
    const r = el.getBoundingClientRect();
    const tracks = (v: string) => (v === 'none' ? [] : v.split(' ').map(parseFloat));
    const x = trackAt(tracks(s.gridTemplateColumns), parseFloat(s.columnGap) || 0, clientX - r.left - el.clientLeft - parseFloat(s.paddingLeft));
    const y = trackAt(tracks(s.gridTemplateRows), parseFloat(s.rowGap) || 0, clientY - r.top - el.clientTop - parseFloat(s.paddingTop), 1);
    return x === null || y === null ? null : { x, y };
  }
  function endDrag() {
    releasePointer();
    if (resizing) { setLive(`${title(resizing.id)} resized to ${sizeName(layout!.find(w => w.id === resizing.id)!.size)}`); setResizing(null); return; }
    if (!drag) return;
    announce(drag.id, layout!);
    setMoved(drag.id);
    setDrag(null);
  }
  function resize(id: string, size: BlockSize) {
    const cell = placed.find(c => c.id === id);
    const next = layout!.map((p) => (p.id === id ? { ...p, size } : p));
    setLayout(gridWidth ? settle(next, gridWidth, cell && { id, x: cell.x, y: cell.y }) : next);
    setLive(`${title(id)} is now ${sizeName(size)}`);
  }
  function remove(id: string, i: number) {
    const el = grid.current?.querySelector<HTMLElement>(`[data-widget="${CSS.escape(id)}"]`);
    if (el && !matchMedia('(prefers-reduced-motion: reduce)').matches) {
      const ghost = el.cloneNode(true) as HTMLElement;
      const rect = el.getBoundingClientRect();
      ghost.removeAttribute('data-widget'); ghost.setAttribute('aria-hidden', 'true'); ghost.inert = true;
      Object.assign(ghost.style, { position: 'fixed', left: `${rect.left}px`, top: `${rect.top}px`, width: `${rect.width}px`, height: `${rect.height}px`, zIndex: '20', pointerEvents: 'none' });
      document.body.appendChild(ghost);
      ghost.animate([{ opacity: 1, transform: 'scale(1)' }, { opacity: 0, transform: 'scale(.94)' }], { duration: 160 }).finished.finally(() => ghost.remove());
    }
    setRemoved(layout);
    // Widgets below rise, and a later widget that fits the freed spot fills it.
    const kept = layout!.filter((p) => p.id !== id);
    const next = gridWidth ? settle(kept, gridWidth) : kept;
    setLayout(next);
    setLive(`Removed ${title(id)}`);
    const neighbour = next[Math.min(i, next.length - 1)];
    focusAfter.current = neighbour ? `[data-widget="${CSS.escape(neighbour.id)}"] .blk-handle` : '.blk-add';
  }
  // A new widget starts at its smallest size. Packed last, it takes the highest free spot that fits
  // it (the first gap no other widget fits); then the order is settled to reading order.
  function add(id: string) {
    const def = widgetDef(id)!;
    const item: Placement = { id, size: def.sizes[0]! };
    setLayout(gridWidth ? settle([...layout!, item], gridWidth) : [...layout!, item]);
    setLive(`Added ${def.title}, ${sizeName(item.size)}`);
    focusAfter.current = '.blk-gallery [data-add], .blk-gallery [data-close]';
  }
  // Editing works on the reading order (same picture), so keyboard moves and tab order match it.
  function start() { if (gridWidth) setLayout(settle(layout!, gridWidth)); setRemoved(null); setEditing(true); setError(''); }
  function cancel() { releasePointer(); setResizing(null); setLayout(saved); setEditing(false); setDrag(null); setError(''); }
  async function save() {
    if (!loaded) return;
    setBusy(true); setError('');
    try {
      const p = await api('prefsPut', { body: { widgetLayout: layout! } });
      const l = migrateLayout(p.widgetLayout).layout;
      setSaved(l); setLayout(l); setEditing(false); setLive('Layout saved');
    } catch (e) { setError((e as Error).message); }
    finally { setBusy(false); }
  }

  return <>
    {loaded && <DashboardTutorial onPreview={previewTour} />}
    <div class={`blocks-bar${editing ? ' is-editing' : ''}`}>
      {editing ? <>
        <div class="blocks-bar-text"><span class="blocks-bar-title">Arrange your home</span><span class="blocks-bar-hint">Drag a tile, or focus its move handle and use the arrow keys. Drag a corner to resize; Shift + arrow keys also resize.</span></div>
        <div class="blocks-bar-actions">
          <button class="btn" data-tour="add" disabled={busy} onClick={() => gallery.current?.showModal()}>Add widget</button>
          <button class="btn" disabled={busy} onClick={cancel}>Cancel</button>
          <button class="btn btn--primary" disabled={busy} onClick={save}>{busy ? 'Saving…' : 'Done'}</button>
        </div>
      </> : <>
        <div class="blocks-bar-actions"><button class="btn btn--small" data-tour="edit" disabled={!loaded} onClick={start}>Edit home</button></div>
      </>}
      {error && <p class="small" role="alert">{error} {!loaded && <>Showing the default home. <button class="btn btn--small" onClick={() => setTick(tick + 1)}>Retry preferences</button></>}</p>}
    </div>
    {editing && removed && <p role="status">Widget removed. <button class="btn btn--small" onClick={() => { setLayout(removed); setRemoved(null); }}>Undo</button></p>}
    <div class="blocks-wrap">
      <ul ref={grid} style={gridWidth ? { '--cols': gridColumns(gridWidth) } : undefined} data-layout-revision={layoutCommit.current.count} class={`blocks${editing ? ' is-editing' : ''}${entering ? ' is-entering' : ''}`} aria-label="Home widgets" onPointerMove={onPointerMove} onPointerUp={endDrag} onPointerCancel={cancelPointer}>
        {/* First in the list so tiles paint over them: outlines show only in the grid's empty space. */}
        {spots.map(c => <li key={`spot-${c.x}-${c.y}`} class="blk-spot" aria-hidden="true" style={{ gridColumn: `${c.x + 1} / span 1`, gridRow: `${c.y + 1} / span 1` }} />)}
        {layout.map((w, i) => {
          const def = widgetDef(w.id);
          if (!def && !editing) return null; // keep another feature's widget saved until it ships
          const { w: cw, h: ch } = blocks(w.size);
          const name = title(w.id);
          const cell = placed.find(p => p.id === w.id);
          return <li key={w.id} class={`blk${drag?.id === w.id ? ' is-dragging is-placeholder' : ''}${resizing?.id === w.id ? ' is-resizing' : ''}${moved === w.id && !drag ? ' is-moved' : ''}`} data-widget={w.id} data-flip={w.id} onPointerDown={e => onPointerDown(e, w.id)} data-size={w.size} data-size-label={sizeLabel(w.size).replace('×', ' × ')} data-w={cw} data-h={ch} style={{ '--i': i, ...(cell ? { gridColumn: `${cell.x + 1} / span ${cell.w}`, gridRow: `${cell.y + 1} / span ${cell.h}` } : {}) }}>
            <div class="blk-body" inert={editing || undefined}>
              {def ? children(w.id, w.size) : <section class="tile" aria-label={name}><p class="tile-title">{name}</p></section>}
            </div>
            {editing && <div class="blk-tools">
              <div class="blk-tools-top">
                <button type="button" class="blk-handle" aria-label={`Move ${name}. Use the arrow keys to move it earlier or later.`} aria-describedby="blocks-live" disabled={busy}
                  onKeyDown={(e) => onHandleKey(e, w.id, i)}><Icon name="grip" size={18} /></button>
                <span class="blk-name" aria-hidden="true">{name}</span>
                <button type="button" class="blk-remove" aria-label={`Remove ${name}`} disabled={busy} onClick={() => remove(w.id, i)}><Icon name="close" size={14} /></button>
              </div>
              {def && def.sizes.length > 1 && <>
                {(['x', 'y', 'both'] as const).filter(axis => axis === 'both' || def.sizes.some(s => axis === 'x' ? blocks(s).h === ch && blocks(s).w !== cw : blocks(s).w === cw && blocks(s).h !== ch)).map(axis =>
                  <button type="button" class={`blk-resize blk-resize--${axis}`} disabled={busy} aria-label={`Resize ${name}. Shift and arrow keys change size.`} onKeyDown={e => onHandleKey(e, w.id, i)} onPointerDown={e => beginResize(e, w.id, axis)}>{axis === 'both' ? <svg aria-hidden="true" viewBox="0 0 24 24" width="24" height="24"><path d="M6 18 18 6M11 18l7-7M16 18l2-2" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" /></svg> : ''}</button>)}
              </>}
            </div>}
          </li>;
        })}
        {editing && <li class="blk blk-add-cell" data-w="1" data-h="1" style={slot ? { gridColumn: `${slot.x + 1} / span 1`, gridRow: `${slot.y + 1} / span 1` } : undefined}>
          <button type="button" class="blk-add" disabled={busy} onClick={() => gallery.current?.showModal()}>
            <span class="blk-add-plus" aria-hidden="true">+</span>Add widget<span class="blk-add-sub">{available.length} available</span>
          </button>
        </li>}
      </ul>
    </div>
    {layout.length === 0 && !editing && <p class="small muted">No widgets on your home. Use Edit home to add some.</p>}
    <p class="visually-hidden" id="blocks-live" aria-live="polite">{live}</p>
    <dialog ref={gallery} class="blk-gallery" aria-labelledby="blk-gallery-title" onClick={(e) => { if (e.target === gallery.current) gallery.current?.close(); }}>
      <div class="blk-gallery-inner">
        <div class="blk-gallery-head"><h2 id="blk-gallery-title">Add a widget</h2><button type="button" class="btn btn--small" data-close onClick={() => gallery.current?.close()}>Close</button></div>
        <div class="blk-gallery-body">
          {available.length === 0 ? <p class="blk-gallery-empty">Every widget is already on your home.</p> :
            <ul class="blk-gallery-list">
              {available.map((d) => <li key={d.id} class="blk-gallery-item">
                <p class="blk-gallery-name">{d.title}</p>
                <p class="blk-gallery-sub">Sizes {d.sizes.map(sizeLabel).join(', ')}</p>
                <div class="blk-gallery-sizes" aria-hidden="true">
                  {d.sizes.map((s) => { const b = blocks(s); return <span key={s} class="blk-glyph" style={{ '--w': b.w }}>{Array.from({ length: b.area }, (_, k) => <i key={k} />)}</span>; })}
                </div>
                <button type="button" class="btn btn--small" data-add onClick={() => add(d.id)}>Add {d.title}</button>
              </li>)}
            </ul>}
        </div>
      </div>
    </dialog>
  </>;
}
