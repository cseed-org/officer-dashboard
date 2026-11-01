import type { ComponentChildren } from 'preact';
import { useEffect, useState } from 'preact/hooks';
import { api } from '../../lib/api/client';
import type { WidgetPlacement } from '../../lib/contracts';
import { defaultWidgetLayout, widgetRegistry } from './registry';
import { Loading } from '../components/states';

/** Layout stays on the server. Theme and density remain browser preferences. */
export default function WidgetGrid({ children }: { children: (id: string) => ComponentChildren }) {
  const [layout, setLayout] = useState<WidgetPlacement[] | null>(null);
  const [saved, setSaved] = useState<WidgetPlacement[]>([]);
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [loaded, setLoaded] = useState(false);
  const [tick, setTick] = useState(0);
  const [dragged, setDragged] = useState<string | null>(null);
  useEffect(() => {
    let live = true;
    setError('');
    api('prefsGet').then((p) => { if (live) { setLayout(p.widgetLayout); setSaved(p.widgetLayout); setLoaded(true); } }, (e: Error) => { if (live) { setError(e.message); setLayout(defaultWidgetLayout); setLoaded(false); } });
    return () => { live = false; };
  }, [tick]);
  const move = (id: string, to: number) => setLayout((items) => {
    if (!items) return items;
    const next = [...items];
    const from = next.findIndex((w) => w.id === id);
    if (from < 0) return items;
    const [item] = next.splice(from, 1);
    next.splice(Math.max(0, Math.min(to, next.length)), 0, item!);
    return next;
  });
  async function save() {
    if (!layout || !loaded) return;
    setBusy(true); setError('');
    try { const p = await api('prefsPut', { body: { widgetLayout: layout } }); setSaved(p.widgetLayout); setLayout(p.widgetLayout); setEditing(false); }
    catch (e) { setError((e as Error).message); }
    finally { setBusy(false); }
  }
  if (!layout) return <Loading rows={4} label="Loading widgets" />;
  return <>
    <div class="widget-toolbar">
      {editing ? <><button class="btn" disabled={busy} onClick={save}>{busy ? 'Saving…' : 'Save layout'}</button><button class="btn" disabled={busy} onClick={() => { setLayout(saved); setEditing(false); setError(''); }}>Cancel</button></> : <button class="btn btn--small" disabled={!loaded} onClick={() => setEditing(true)}>Edit widgets</button>}
      {error && <span role="alert">{error} {!loaded && <>Showing default widgets. <button class="btn btn--small" onClick={() => setTick(tick + 1)}>Retry preferences</button></>}</span>}
    </div>
    {editing && <fieldset class="widget-gallery" disabled={busy}><legend>Add widgets</legend>{widgetRegistry.filter((w) => !layout.some((p) => p.id === w.id)).map((w) => <button class="btn btn--small" key={w.id} onClick={() => setLayout([...layout, { id: w.id, size: w.size }])}>Add {w.title}</button>)}</fieldset>}
    <div class="home-grid">
      {layout.map((w, i) => {
        const definition = widgetRegistry.find((r) => r.id === w.id);
        // Preserve unknown widgets in saved layouts while another feature is unavailable.
        if (!definition && !editing) return null;
        const title = definition?.title ?? `Unavailable widget (${w.id})`;
        return <div key={w.id} class={`widget-slot widget-slot--${w.size}`} data-widget={w.id} draggable={editing && !busy} onDragStart={() => setDragged(w.id)} onDragEnd={() => setDragged(null)} onDragOver={(e) => { if (editing) e.preventDefault(); }} onDrop={(e) => { e.preventDefault(); if (editing && !busy && dragged) move(dragged, i); setDragged(null); }}>
          {editing && <fieldset disabled={busy} class="widget-controls" aria-label={`${title} layout`}>
            <legend>{title}</legend>
            <button class="btn btn--small" disabled={i === 0} onClick={() => move(w.id, i - 1)} aria-label={`Move ${title} earlier`}>↑</button>
            <button class="btn btn--small" disabled={i === layout.length - 1} onClick={() => move(w.id, i + 1)} aria-label={`Move ${title} later`}>↓</button>
            <label>Size <select value={w.size} onChange={(e) => setLayout(layout.map((p) => p.id === w.id ? { ...p, size: e.currentTarget.value as WidgetPlacement['size'] } : p))}><option>S</option><option>M</option><option>L</option></select></label>
            <button class="btn btn--small" onClick={() => setLayout(layout.filter((p) => p.id !== w.id))}>Remove {title}</button>
          </fieldset>}
          {children(w.id)}
        </div>;
      })}
    </div>
    {layout.length === 0 && <p>No widgets selected. Use Edit widgets to add one.</p>}
  </>;
}
