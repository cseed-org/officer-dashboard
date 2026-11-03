import { useEffect, useRef, useState } from 'preact/hooks';
import { api } from '../../lib/api/client';
import { timezoneSnapshot } from '../../lib/timezone';

const steps = [
  { title: 'Your home base', text: 'Your widgets bring club activity and next actions into one place. This short tour shows how to make Home yours.', target: '[data-widget="links"]', edit: false },
  { title: 'Make it yours', text: 'Choose Edit home to arrange your widgets. Done saves your layout across devices; Cancel restores it.', target: '[data-tour="edit"]', edit: false },
  { title: 'Move things around', text: 'Drag this top grip to move a card. With the grip focused, arrow keys move it earlier or later. Escape cancels a drag.', target: '[data-widget="links"] .blk-handle', edit: true },
  { title: 'Grab a corner', text: 'This corner resizes the card. Drag it to preview an allowed footprint, or use Shift + arrow keys from a focused grip. Escape cancels a drag.', target: '[data-widget="links"] .blk-resize--both', edit: true },
  { title: 'Add what you need', text: 'Add widget opens the gallery. Choose the cards you need, then use Done to save. The edit mode in this tour is only a preview.', target: '[data-tour="add"]', edit: true },
  { title: 'Your club’s links', text: 'Open your club website, Notion and Google Drive from these rows. Leadership can use Add link to set a missing URL in Settings.', target: '[data-widget="links"] .club-links', edit: false },
];
type Rect = { x: number; y: number; width: number; height: number };

export default function DashboardTutorial({ onPreview }: { onPreview: (editing: boolean, active: boolean) => void }) {
  const dialog = useRef<HTMLDialogElement>(null);
  const card = useRef<HTMLDivElement>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  const previousFocus = useRef<HTMLElement | null>(null);
  const [step, setStep] = useState(0);
  const [active, setActive] = useState(false);
  const [error, setError] = useState('');
  const [target, setTarget] = useState<Rect | null>(null);
  const [position, setPosition] = useState({ left: 16, top: 16 });
  const [saving, setSaving] = useState(false);
  const maskId = 'dashboard-tour-mask';

  useEffect(() => {
    let alive = true;
    const replay = new URLSearchParams(location.search).get('tutorial') === '1';
    const prefs = timezoneSnapshot().prefs;
    (prefs ? Promise.resolve(prefs) : api('prefsGet')).then(p => { if (alive && (replay || !p.dashboardTutorialSeen)) setActive(true); }).catch(() => {});
    return () => { alive = false; };
  }, []);
  useEffect(() => {
    if (!active) return;
    previousFocus.current = document.activeElement as HTMLElement;
    dialog.current?.showModal();
    return () => { onPreview(false, false); previousFocus.current?.focus(); };
  }, [active, onPreview]);
  useEffect(() => {
    if (!active) return;
    onPreview(steps[step]!.edit, true);
    heading.current?.focus({ preventScroll: true });
    let frame = 0;
    let first = true;
    let spotlight: HTMLElement | null = null;
    const measure = () => {
      const el = document.querySelector<HTMLElement>(steps[step]!.target) ?? document.querySelector<HTMLElement>('[data-widget="links"]');
      if (!el) return;
      if (spotlight !== el) {
        spotlight?.classList.remove('is-tour-target');
        spotlight = el; el.classList.add('is-tour-target');
      }
      const rect = el.getBoundingClientRect();
      if (first) {
        first = false;
        if (rect.top < 90 || rect.bottom > innerHeight - 260) {
          el.scrollIntoView({ block: 'center', behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'instant' : 'smooth' });
        }
      }
      const pad = 8;
      const x = Math.max(8, rect.left - pad), y = Math.max(8, rect.top - pad);
      const width = Math.min(innerWidth - x - 8, rect.width + pad * 2);
      const height = Math.min(innerHeight - y - 8, rect.height + pad * 2);
      setTarget({ x, y, width, height });
      const cw = card.current?.offsetWidth ?? 400, ch = card.current?.offsetHeight ?? 260;
      // Prefer beside the spotlight. On phones use the free space above or below it.
      let left = x + width + 20, top = y;
      if (left + cw > innerWidth - 16) {
        if (x - cw - 20 >= 16) left = x - cw - 20;
        else { left = Math.min(x, innerWidth - cw - 16); top = y + height + 20; }
      }
      if (top + ch > innerHeight - 16) top = y - ch - 20;
      setPosition({ left: Math.max(16, Math.min(left, innerWidth - cw - 16)), top: Math.max(16, Math.min(top, innerHeight - ch - 16)) });
    };
    const update = () => { cancelAnimationFrame(frame); frame = requestAnimationFrame(measure); };
    // Wait for temporary edit controls to render before resolving their anchors.
    update();
    const mutations = new MutationObserver(update);
    const grid = document.querySelector('.blocks');
    if (grid) mutations.observe(grid, { childList: true, subtree: true });
    const observer = new ResizeObserver(update);
    if (card.current) observer.observe(card.current);
    window.addEventListener('resize', update);
    window.addEventListener('scroll', update, true);
    return () => {
      cancelAnimationFrame(frame); observer.disconnect(); mutations.disconnect(); spotlight?.classList.remove('is-tour-target');
      window.removeEventListener('resize', update); window.removeEventListener('scroll', update, true);
    };
  }, [active, step, onPreview]);

  async function finish() {
    if (saving) return;
    setSaving(true); setError('');
    try {
      await api('prefsPut', { body: { dashboardTutorialSeen: true } });
      dialog.current?.close(); setActive(false); setTarget(null);
      const url = new URL(location.href); url.searchParams.delete('tutorial'); history.replaceState(null, '', url);
    } catch (e) { setError(`Couldn’t save tutorial status. ${(e as Error).message}`); }
    finally { setSaving(false); }
  }
  return <dialog ref={dialog} class="dashboard-tour" aria-labelledby="tour-title" onCancel={e => { e.preventDefault(); void finish(); }}>
    <svg class="tour-shade" aria-hidden="true" width="100%" height="100%">
      <defs><mask id={maskId}><rect width="100%" height="100%" fill="white" />{target && <rect class="tour-cutout" x={target.x} y={target.y} width={target.width} height={target.height} rx="12" fill="black" />}</mask></defs>
      <rect width="100%" height="100%" fill="rgb(0 0 0 / .62)" mask={`url(#${maskId})`} />
      {target && <rect class="tour-cutout" x={target.x} y={target.y} width={target.width} height={target.height} rx="12" fill="none" stroke="var(--c-blue)" stroke-width="3" />}
    </svg>
    <div ref={card} class="tour-card" style={{ left: 0, top: 0, transform: `translate(${position.left}px, ${position.top}px)` }} data-tour-step={step + 1}>
      <div class="tour-card-content" key={step}>
      <p class="small muted">Dashboard tour · {step + 1} of {steps.length}</p>
      <h2 ref={heading} tabIndex={-1} id="tour-title">{steps[step]!.title}</h2><p>{steps[step]!.text}</p>
      {error && <p role="alert">{error}</p>}
      <div class="action-row"><button class="btn" disabled={saving} onClick={finish}>Skip tour</button><button class="btn" disabled={saving || step === 0} onClick={() => setStep(step - 1)}>Back</button><button class="btn btn--primary" disabled={saving} onClick={() => step === steps.length - 1 ? void finish() : setStep(step + 1)}>{step === steps.length - 1 ? 'Finish' : 'Next'}</button></div>
      </div>
    </div>
  </dialog>;
}
