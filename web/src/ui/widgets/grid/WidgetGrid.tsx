import DashboardTutorial from '../DashboardTutorial';
import type { ComponentChildren } from 'preact';
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { api } from '../../../lib/api/client';
import { timezoneSnapshot } from '../../../lib/timezone';
import { Icon } from '../../components/Icon';
import { Loading } from '../../components/states';
import { blocks, migrateLayout, moveTo, sizeLabel, sizeName, type Placement } from './layout';
import { defaultWidgetLayout, widgetDef, widgetRegistry, type BlockSize } from '../registry';
import '../../../styles/home-grid.css';

type Resize = { id: string; x: number; y: number; size: BlockSize; snapshot: Placement[]; axis: string; width: number; height: number };
type Drag = { id: string; x: number; y: number; snapshot: Placement[]; over: string | null };

/**
 * Home as a grid of blocks (spec § "V0.4 Home block grid"). The layout lives on the server
 * (/api/me/prefs); V0.3 S/M/L layouts are migrated on load. `children` renders one widget's tile.
 */
export default function WidgetGrid({ children }: { children: (id: string, size: BlockSize) => ComponentChildren }) {
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
  const rects = useRef(new Map<string, DOMRect>());
  const [moved, setMoved] = useState<string | null>(null);
  const [live, setLive] = useState('');
  const pointerY = useRef(0);
  const focusAfter = useRef<string | null>(null);
  const gallery = useRef<HTMLDialogElement>(null);
  const grid = useRef<HTMLUListElement>(null);

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
  // Escape cancels a pointer drag and puts every widget back.
  useEffect(() => {
    if (!drag) return;
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') { e.preventDefault(); setLayout(drag.snapshot); setDrag(null); setLive('Move cancelled'); } };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [drag?.id]);
  useEffect(() => {
    if (!moved) return;
    const t = setTimeout(() => setMoved(null), 700);
    return () => clearTimeout(t);
  }, [moved, layout]);

  useLayoutEffect(() => {
    const next = new Map<string, DOMRect>();
    grid.current?.querySelectorAll<HTMLElement>('li[data-widget]').forEach(el => {
      const id = el.dataset.widget!;
      const rect = el.getBoundingClientRect();
      const old = rects.current.get(id);
      if (old && id !== drag?.id && !matchMedia('(prefers-reduced-motion: reduce)').matches && (old.x !== rect.x || old.y !== rect.y)) {
        el.animate([{ transform: `translate(${old.x - rect.x}px, ${old.y - rect.y}px)` }, { transform: 'translate(0,0)' }], { duration: 180, easing: 'ease-out' });
      }
      next.set(id, rect);
    });
    rects.current = next;
  }, [layout]);
  useEffect(() => {
    if (!resizing) return;
    const cancel = (e: KeyboardEvent) => { if (e.key === 'Escape') { setLayout(resizing.snapshot); setResizing(null); setLive('Resize cancelled'); } };
    document.addEventListener('keydown', cancel);
    return () => document.removeEventListener('keydown', cancel);
  }, [resizing]);
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
  const available = widgetRegistry.filter((w) => !layout.some((p) => p.id === w.id));
  const announce = (id: string, l: Placement[]) => setLive(`${title(id)}, position ${l.findIndex((w) => w.id === id) + 1} of ${l.length}`);

  function moveBy(id: string, to: number) {
    const next = moveTo(layout!, id, to);
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
  // Pointer drag (mouse, pen, touch): the grip captures the pointer; crossing another widget moves
  // the dragged one into its place, so the dragged widget's own cell is always the drop target.
  function onPointerDown(e: PointerEvent, id: string) {
    if (!editing || busy || e.button !== 0) return;
    e.preventDefault();
    pointerY.current = e.clientY;
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    setDrag({ id, x: e.clientX, y: e.clientY, snapshot: layout!, over: null });
  }
  function beginResize(e: PointerEvent, id: string, axis: string) {
    if (busy || e.button !== 0) return;
    e.preventDefault();
    pointerY.current = e.clientY;
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    const rect = (e.currentTarget as HTMLElement).closest('li')!.getBoundingClientRect();
    setResizing({ id, x: e.clientX, y: e.clientY, size: layout!.find(w => w.id === id)!.size, snapshot: layout!, axis, width: rect.width, height: rect.height });
  }
  function onPointerMove(e: PointerEvent) {
    pointerY.current = e.clientY;
    if (resizing) {
      const b = blocks(resizing.size);
      const wantW = b.w + (e.clientX - resizing.x) / (resizing.width / b.w);
      const wantH = b.h + (e.clientY - resizing.y) / (resizing.height / b.h);
      const sizes = widgetDef(resizing.id)!.sizes.filter(s => resizing.axis === 'x' ? blocks(s).h === b.h : resizing.axis === 'y' ? blocks(s).w === b.w : true);
      const target = [...sizes].sort((a, c) => {
        const distance = (s: BlockSize) => (blocks(s).w - wantW) ** 2 + (blocks(s).h - wantH) ** 2;
        return distance(a) - distance(c);
      })[0]!;
      setLayout(resizing.snapshot.map(w => w.id === resizing.id ? { ...w, size: target } : w));
      return;
    }
    if (!drag) return;
    const hit = document.elementsFromPoint(e.clientX, e.clientY).map((el) => el.closest<HTMLElement>('li[data-widget]')).find((li) => li && grid.current?.contains(li));
    const over = hit?.dataset.widget ?? null;
    if (over && over !== drag.id && over !== drag.over) {
      setLayout((l) => (l ? moveTo(l, drag.id, l.findIndex((w) => w.id === over)) : l));
    }
    setDrag({ ...drag, x: e.clientX, y: e.clientY, over: over === drag.id ? drag.over : over });
  }
  function endDrag() {
    if (resizing) { setLive(`${title(resizing.id)} resized to ${sizeName(layout!.find(w => w.id === resizing.id)!.size)}`); setResizing(null); return; }
    if (!drag) return;
    announce(drag.id, layout!);
    setMoved(drag.id);
    setDrag(null);
  }
  function resize(id: string, size: BlockSize) {
    setLayout(layout!.map((p) => (p.id === id ? { ...p, size } : p)));
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
    const next = layout!.filter((p) => p.id !== id);
    setLayout(next);
    setLive(`Removed ${title(id)}`);
    const neighbour = next[Math.min(i, next.length - 1)];
    focusAfter.current = neighbour ? `[data-widget="${CSS.escape(neighbour.id)}"] .blk-handle` : '.blk-add';
  }
  function add(id: string) {
    const def = widgetDef(id)!;
    setLayout([...layout!, { id, size: def.size }]);
    setLive(`Added ${def.title} at the end`);
    focusAfter.current = '.blk-gallery [data-add], .blk-gallery [data-close]';
  }
  function start() { setRemoved(null); setEditing(true); setError(''); }
  function cancel() { setLayout(saved); setEditing(false); setDrag(null); setError(''); }
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
        <div class="blocks-bar-text"><span class="blocks-bar-title">Arrange your home</span><span class="blocks-bar-hint">Drag a grip, or focus it and use the arrow keys. Drag a corner to resize; Shift + arrow keys also resize.</span></div>
        <div class="blocks-bar-actions">
          <button class="btn" data-tour="add" disabled={busy} onClick={() => gallery.current?.showModal()}>Add widget</button>
          <button class="btn" disabled={busy} onClick={cancel}>Cancel</button>
          <button class="btn btn--primary" disabled={busy} onClick={save}>{busy ? 'Saving…' : 'Done'}</button>
        </div>
      </> : <>
        <div class="blocks-bar-text"><span class="blocks-bar-hint">Your layout follows you across devices.</span></div>
        <div class="blocks-bar-actions"><button class="btn btn--small" data-tour="edit" disabled={!loaded} onClick={start}>Edit home</button></div>
      </>}
      {error && <p class="small" role="alert">{error} {!loaded && <>Showing the default home. <button class="btn btn--small" onClick={() => setTick(tick + 1)}>Retry preferences</button></>}</p>}
    </div>
    {editing && removed && <p role="status">Widget removed. <button class="btn btn--small" onClick={() => { setLayout(removed); setRemoved(null); }}>Undo</button></p>}
    <div class="blocks-wrap">
      <ul ref={grid} class={`blocks${editing ? ' is-editing' : ''}`} aria-label="Home widgets" onPointerMove={onPointerMove} onPointerUp={endDrag} onPointerCancel={() => { setLayout(drag?.snapshot ?? resizing?.snapshot ?? layout); setDrag(null); setResizing(null); }}>
        {layout.map((w, i) => {
          const def = widgetDef(w.id);
          if (!def && !editing) return null; // keep another feature's widget saved until it ships
          const { w: cw, h: ch } = blocks(w.size);
          const name = title(w.id);
          return <li key={w.id} class={`blk${drag?.id === w.id ? ' is-dragging' : ''}${resizing?.id === w.id ? ' is-resizing' : ''}${moved === w.id && !drag ? ' is-moved' : ''}`} data-widget={w.id} data-size={w.size} data-size-label={sizeLabel(w.size).replace('×', ' × ')} data-w={cw} data-h={ch} style={{ '--i': i }}>
            <div class="blk-body" inert={editing || undefined}>
              {def ? children(w.id, w.size) : <section class="tile" aria-label={name}><p class="tile-title">{name}</p></section>}
            </div>
            {editing && <div class="blk-tools">
              <div class="blk-tools-top">
                <button type="button" class="blk-handle" aria-label={`Move ${name}. Use the arrow keys to move it earlier or later.`} aria-describedby="blocks-live" disabled={busy}
                  onPointerDown={(e) => onPointerDown(e, w.id)} onKeyDown={(e) => onHandleKey(e, w.id, i)}><Icon name="grip" size={18} /></button>
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
        {editing && <li class="blk" data-w="1" data-h="1">
          <button type="button" class="blk-add" disabled={busy} onClick={() => gallery.current?.showModal()}>
            <span class="blk-add-plus" aria-hidden="true">+</span>Add widget<span class="blk-add-sub">{available.length} available</span>
          </button>
        </li>}
      </ul>
    </div>
    {layout.length === 0 && !editing && <p class="small muted">No widgets on your home. Use Edit home to add some.</p>}
    {drag && <div class="blk-float" aria-hidden="true" style={{ left: `${drag.x + 14}px`, top: `${drag.y + 14}px` }}>
      {children(drag.id, layout.find(w => w.id === drag.id)!.size)}
    </div>}
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
