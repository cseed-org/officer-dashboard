/** Track edits across all native forms, including drawers mounted after page load. */
import { useEffect } from 'preact/hooks';
type Control = HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement;
const dirty = new Map<Control, string>();
const initial = new WeakMap<Control, string>();
const completing = new Set<HTMLFormElement>();
let submitting: HTMLFormElement | null = null;
const value = (el: Control) => el instanceof HTMLInputElement && ['checkbox', 'radio'].includes(el.type) ? String(el.checked) : el.value;
const control = (target: EventTarget | null): target is Control => target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement || target instanceof HTMLSelectElement;
let notification: ReturnType<typeof setTimeout> | undefined;
// Native dialog close events and form handlers must settle before a possible remount.
const changed = () => {
  if (notification !== undefined) return;
  notification = setTimeout(() => {
    notification = undefined;
    window.dispatchEvent(new Event('cseed-unsaved'));
  }, 0);
};
const declared = new Set<symbol>();
/** For editors that are not native forms (row editors, action panels): report dirtiness explicitly. */
export function useUnsavedEdits(isDirty: boolean) {
  useEffect(() => {
    if (!isDirty) return;
    const key = Symbol('unsaved');
    declared.add(key);
    changed();
    return () => { declared.delete(key); changed(); };
  }, [isDirty]);
}
export function hasUnsavedEdits() {
  if (declared.size > 0) return true;
  for (const el of dirty.keys()) if (!el.isConnected || el.closest('dialog:not([open])') || initial.get(el) === value(el)) dirty.delete(el);
  for (const form of completing) {
    if (form.isConnected && form.querySelector('fieldset:disabled, button[type=submit]:disabled, input[type=submit]:disabled')) return true;
    completing.delete(form);
  }
  return dirty.size > 0;
}
/** Capture the submitting form before the request starts; clear only the values it saved. */
export function savingForm() {
  const form = submitting;
  const saved = [...dirty].filter(([el]) => el.form === form && form !== null).map(([el]) => [el, value(el)] as const);
  return () => {
    if (form) completing.add(form);
    for (const [el, v] of saved) if (value(el) === v && !(el instanceof HTMLInputElement && el.type === 'file')) { dirty.delete(el); initial.set(el, v); }
    // Let the save handler finish updating its UI before applying a pending clock.
    setTimeout(changed, 0);
  };
}
let installed = false;
export function watchUnsavedEdits() {
  if (installed) return;
  installed = true;
  document.addEventListener('focusin', (e) => { if (control(e.target) && !initial.has(e.target)) initial.set(e.target, value(e.target)); }, true);
  const edit = (e: Event) => {
    if (!control(e.target)) return;
    const el = e.target;
    if (!el.form) return;
    if (initial.get(el) === value(el)) dirty.delete(el);
    else dirty.set(el, value(el));
    changed();
  };
  document.addEventListener('input', edit, true);
  document.addEventListener('change', edit, true);
  document.addEventListener('submit', (e) => {
    submitting = e.target instanceof HTMLFormElement ? e.target : null;
    setTimeout(() => { submitting = null; }, 0);
  }, true);
  new MutationObserver(changed).observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ['open', 'disabled'] });
}
