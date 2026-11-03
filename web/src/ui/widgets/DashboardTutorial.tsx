import { useEffect, useRef, useState } from 'preact/hooks';
import { api } from '../../lib/api/client';

const steps = [
  ['Your home base', 'See club activity and your next actions in one place.', '.blocks'],
  ['Widgets work for you', 'Each card shows one part of your club’s work. Follow its links to see more.', '.blk'],
  ['Make it yours', 'Choose Edit home to arrange your dashboard. Done saves it across devices; Cancel restores your layout.', '.blocks-bar'],
  ['Move things around', 'In edit mode, drag a card’s top grip. With the grip focused, arrow keys move it; Escape cancels a drag.', '.blk'],
  ['Grab a corner', 'Drag a corner or edge to resize. The outline previews an allowed size. Shift + arrow keys resize from a focused grip.', '.blk'],
  ['Add what you need', 'Edit home → Add widget opens the gallery. Add Links for your club website, Notion and Google Drive. Leadership sets their URLs in Settings.', '.blocks-bar'],
];
export default function DashboardTutorial() {
  const dialog = useRef<HTMLDialogElement>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  const [step, setStep] = useState(0);
  const [active, setActive] = useState(false);
  const [error, setError] = useState('');
  useEffect(() => {
    const replay = new URLSearchParams(location.search).has('tutorial');
    api('prefsGet').then(p => { if (replay || !p.dashboardTutorialSeen) setActive(true); }).catch(() => {});
  }, []);
  useEffect(() => { if (active) dialog.current?.showModal(); }, [active]);
  useEffect(() => {
    if (!active) return;
    heading.current?.focus();
    const el = document.querySelector<HTMLElement>(steps[step]![2]!);
    el?.classList.add('tutorial-spotlight');
    return () => el?.classList.remove('tutorial-spotlight');
  }, [active, step]);
  async function finish() {
    try {
      await api('prefsPut', { body: { dashboardTutorialSeen: true } });
      dialog.current?.close(); setActive(false);
      const url = new URL(location.href); url.searchParams.delete('tutorial'); history.replaceState(null, '', url);
    } catch (e) { setError(`Couldn’t save tutorial status. ${(e as Error).message}`); }
  }
  return <dialog ref={dialog} class="dashboard-tour" aria-labelledby="tour-title" onCancel={e => { e.preventDefault(); void finish(); }}>
    <p class="small muted">Dashboard tour · {step + 1} of {steps.length}</p>
    <h2 ref={heading} tabIndex={-1} id="tour-title">{steps[step]![0]}</h2><p>{steps[step]![1]}</p>
    {error && <p role="alert">{error}</p>}
    <div class="action-row"><button class="btn" onClick={finish}>Skip tour</button><button class="btn" disabled={step === 0} onClick={() => setStep(step - 1)}>Back</button><button class="btn btn--primary" onClick={() => step === steps.length - 1 ? void finish() : setStep(step + 1)}>{step === steps.length - 1 ? 'Finish' : 'Next'}</button></div>
  </dialog>;
}
