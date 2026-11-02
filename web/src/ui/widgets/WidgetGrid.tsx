import type { ComponentChildren } from 'preact';
import { useEffect, useRef, useState } from 'preact/hooks';
import { api } from '../../lib/api/client';
import { Icon } from '../components/Icon';
import { Loading } from '../components/states';
import { blocks, migrateLayout, moveTo, sizeLabel, sizeName, type Placement } from './layout';
import { defaultWidgetLayout, widgetDef, widgetRegistry, type BlockSize } from './registry';
import '../../styles/home-grid.css';

type Drag = { id: string; x: number; y: number; snapshot: Placement[]; over: string | null };

/**
 * Home as a grid of blocks (spec § "V0.4 Home block grid"). The layout lives on the server
 * (/api/me/prefs); V0.3 S/M/L layouts are migrated on load. `children` renders one widget's tile.
 */
export default function WidgetGrid({ children }: { children: (id: string, size: BlockSize) => ComponentChildren }) {
  const [layout, setLayout] = useState<Placement[] | null>(null);
  const [saved, setSaved] = useState<Placement[]>([]);
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [loaded, setLoaded] = useState(false);
  const [tick, setTick] = useState(0);
  const [drag, setDrag] = useState<Drag | null>(null);
  const [moved, setMoved] = useState<string | null>(null);
  const [live, setLive] = useState('');
  const focusAfter = useRef<string | null>(null);
  const gallery = useRef<HTMLDialogElement>(null);
  const grid = useRef<HTMLUListElement>(null);

  useEffect(() => {
    let alive = true;
    setError('');
    api('prefsGet').then(
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
    if (drag) return;
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
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    setDrag({ id, x: e.clientX, y: e.clientY, snapshot: layout!, over: null });
  }
  function onPointerMove(e: PointerEvent) {
    if (!drag) return;
    const hit = document.elementsFromPoint(e.clientX, e.clientY).map((el) => el.closest<HTMLElement>('li[data-widget]')).find((li) => li && grid.current?.contains(li));
    const over = hit?.dataset.widget ?? null;
    if (over && over !== drag.id && over !== drag.over) {
      setLayout((l) => (l ? moveTo(l, drag.id, l.findIndex((w) => w.id === over)) : l));
    }
    setDrag({ ...drag, x: e.clientX, y: e.clientY, over: over === drag.id ? drag.over : over });
  }
  function endDrag() {
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
  function start() { setEditing(true); setError(''); }
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
    <div class={`blocks-bar${editing ? ' is-editing' : ''}`}>
      {editing ? <>
        <div class="blocks-bar-text"><span class="blocks-bar-title">Arrange your home</span><span class="blocks-bar-hint">Drag a grip, or focus it and use the arrow keys. Pick a size in blocks.</span></div>
        <div class="blocks-bar-actions">
          <button class="btn" disabled={busy} onClick={() => gallery.current?.showModal()}>Add widget</button>
          <button class="btn" disabled={busy} onClick={cancel}>Cancel</button>
          <button class="btn btn--primary" disabled={busy} onClick={save}>{busy ? 'Saving…' : 'Done'}</button>
        </div>
      </> : <>
        <div class="blocks-bar-text"><span class="blocks-bar-hint">Your layout follows you across devices.</span></div>
        <div class="blocks-bar-actions"><button class="btn btn--small" disabled={!loaded} onClick={start}>Edit home</button></div>
      </>}
      {error && <p class="small" role="alert">{error} {!loaded && <>Showing the default home. <button class="btn btn--small" onClick={() => setTick(tick + 1)}>Retry preferences</button></>}</p>}
    </div>
    <div class="blocks-wrap">
      <ul ref={grid} class={`blocks${editing ? ' is-editing' : ''}`} aria-label="Home widgets" onPointerMove={onPointerMove} onPointerUp={endDrag} onPointerCancel={endDrag}>
        {layout.map((w, i) => {
          const def = widgetDef(w.id);
          if (!def && !editing) return null; // keep another feature's widget saved until it ships
          const { w: cw, h: ch } = blocks(w.size);
          const name = title(w.id);
          return <li key={w.id} class={`blk${drag?.id === w.id ? ' is-dragging' : ''}${moved === w.id && !drag ? ' is-moved' : ''}`} data-widget={w.id} data-size={w.size} data-w={cw} data-h={ch} style={{ '--i': i }}>
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
              {def && def.sizes.length > 1 && <div class="blk-sizes" role="group" aria-label={`Size of ${name}`}>
                {def.sizes.map((s) => <button type="button" key={s} class="blk-size" aria-pressed={s === w.size} aria-label={sizeName(s)} disabled={busy} onClick={() => resize(w.id, s)}>{sizeLabel(s)}</button>)}
              </div>}
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
      <Icon name="grip" size={16} />{title(drag.id)}<span class="blk-float-size">{sizeLabel(layout.find((w) => w.id === drag.id)?.size ?? '1x1')}</span>
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
