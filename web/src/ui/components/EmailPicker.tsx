import { useEffect, useId, useRef, useState } from 'preact/hooks';
import { api, ApiCallError, fieldErrors } from '../../lib/api/client';
import type { CalendarEmail } from '../../lib/contracts';
import { Icon } from './Icon';

const MAX = 5;

/**
 * The one calendar-email picker (spec.md "Calendar-email picker"): a select-only combobox
 * over the officer's saved addresses plus "Add another…", disabled at five. Selecting an
 * address makes it the active calendar destination on the server. Removal lives in
 * Settings (`removable`), outside the picker.
 */
export function EmailPicker({
  label = 'Send calendar invites to',
  onChange,
  removable = false,
}: {
  label?: string;
  /** Called with the active address after every load or change. */
  onChange?: (active: string | null, count: number) => void;
  removable?: boolean;
}) {
  const uid = useId().replace(/[^a-zA-Z0-9_-]/g, '');
  const [emails, setEmails] = useState<CalendarEmail[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [activeIdx, setActiveIdx] = useState(0);
  const [adding, setAdding] = useState(false);
  const [draft, setDraft] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [status, setStatus] = useState('');
  const [busy, setBusy] = useState(false);
  const btn = useRef<HTMLButtonElement>(null);
  const list = useRef<HTMLUListElement>(null);
  const input = useRef<HTMLInputElement>(null);
  const root = useRef<HTMLDivElement>(null);
  const changeRef = useRef(onChange);
  changeRef.current = onChange;

  const apply = (next: CalendarEmail[]) => {
    setEmails(next);
    changeRef.current?.(next.find((e) => e.active)?.email ?? null, next.length);
  };
  const load = () =>
    api('calendarEmailsList').then(
      (r) => {
        setLoadError(null);
        apply(r.emails);
      },
      (e) => setLoadError(e instanceof Error ? e.message : 'Could not load your calendar emails.'),
    );
  useEffect(() => {
    load();
  }, []);

  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent) => {
      if (root.current && !root.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', close);
    requestAnimationFrame(() => list.current?.focus());
    return () => document.removeEventListener('mousedown', close);
  }, [open]);

  useEffect(() => {
    if (adding) requestAnimationFrame(() => input.current?.focus());
  }, [adding]);

  if (loadError)
    return (
      <div class="email-picker">
        <span class="field-label">{label}</span>
        <p class="notice notice--negative" role="alert">
          {loadError}{' '}
          <button type="button" class="btn btn--small" onClick={load}>
            Try again
          </button>
        </p>
      </div>
    );
  if (!emails)
    return (
      <div class="email-picker" aria-busy="true">
        <span class="field-label">{label}</span>
        <div class="skel skel--row" aria-hidden="true" />
      </div>
    );

  const full = emails.length >= MAX;
  const selected = emails.find((e) => e.active) ?? null;
  // Options: each email, then "Add another…".
  const count = emails.length + 1;
  const optionId = (i: number) => `${uid}-opt-${i}`;

  const select = async (i: number) => {
    if (i === emails.length) {
      if (full) return;
      setOpen(false);
      setAdding(true);
      return;
    }
    const target = emails[i]!;
    setOpen(false);
    btn.current?.focus();
    if (target.active) return;
    setBusy(true);
    try {
      await api('calendarEmailUpdate', { params: { id: target.id }, body: { active: true } });
      apply(emails.map((e) => ({ ...e, active: e.id === target.id })));
      setStatus(`Calendar invites go to ${target.email}.`);
    } catch (e) {
      setStatus(e instanceof Error ? e.message : 'Could not change the calendar email.');
    } finally {
      setBusy(false);
    }
  };

  const openList = () => {
    setActiveIdx(Math.max(0, emails.findIndex((e) => e.active)));
    setOpen(true);
  };

  const onListKey = (e: KeyboardEvent) => {
    const step = (d: number) => {
      e.preventDefault();
      setActiveIdx((i) => (i + d + count) % count);
    };
    switch (e.key) {
      case 'ArrowDown':
        return step(1);
      case 'ArrowUp':
        return step(-1);
      case 'Home':
        e.preventDefault();
        return setActiveIdx(0);
      case 'End':
        e.preventDefault();
        return setActiveIdx(count - 1);
      case 'Enter':
      case ' ':
        e.preventDefault();
        return select(activeIdx);
      case 'Escape':
      case 'Tab':
        if (e.key === 'Escape') e.preventDefault();
        setOpen(false);
        btn.current?.focus();
    }
  };

  const save = async (e: Event) => {
    e.preventDefault();
    const v = draft.trim().toLowerCase();
    if (!v || !/^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/.test(v)) return setError('Enter a valid email, like you@example.org.');
    if (emails.some((x) => x.email === v)) return setError(`${v} is already saved.`);
    setBusy(true);
    try {
      const r = await api('calendarEmailCreate', { body: { email: v, active: true } });
      const next = [...emails.map((x) => ({ ...x, active: false })), { ...r.email, active: true }];
      apply(next);
      setDraft('');
      setError(null);
      setAdding(false);
      setStatus(`Saved. ${next.length} of ${MAX}.`);
      requestAnimationFrame(() => btn.current?.focus());
    } catch (err) {
      setError(fieldErrors(err).email ?? (err instanceof ApiCallError ? err.message : 'Could not save that email.'));
    } finally {
      setBusy(false);
    }
  };

  const remove = async (target: CalendarEmail) => {
    setBusy(true);
    try {
      await api('calendarEmailDelete', { params: { id: target.id } });
      const r = await api('calendarEmailsList');
      apply(r.emails);
      setStatus(`Removed ${target.email}.`);
    } catch (e) {
      setStatus(e instanceof Error ? e.message : 'Could not remove that email.');
    } finally {
      setBusy(false);
    }
  };

  return (
    <div class="stack stack--tight">
      <div class="email-picker" ref={root} data-email-picker>
        <span class="field-label" id={`${uid}-l`}>
          {label}
        </span>
        {adding ? (
          <form class="email-picker-add" onSubmit={save} noValidate>
            <label class="visually-hidden" for={`${uid}-new`}>
              New calendar email
            </label>
            <input
              ref={input}
              id={`${uid}-new`}
              class="input"
              type="email"
              autocomplete="email"
              placeholder="you@example.org"
              value={draft}
              aria-invalid={error ? true : undefined}
              aria-describedby={`${uid}-add-note${error ? ` ${uid}-add-err` : ''}`}
              onInput={(e) => {
                setDraft((e.target as HTMLInputElement).value);
                setError(null);
              }}
              onKeyDown={(e) => {
                if (e.key === 'Escape') {
                  e.preventDefault();
                  setAdding(false);
                  setError(null);
                  requestAnimationFrame(() => btn.current?.focus());
                }
              }}
            />
            <button class="btn btn--primary" type="submit" disabled={busy}>
              Save
            </button>
            <button
              class="btn"
              type="button"
              onClick={() => {
                setAdding(false);
                setError(null);
                requestAnimationFrame(() => btn.current?.focus());
              }}
            >
              Cancel
            </button>
            {error && (
              <p class="field-error" id={`${uid}-add-err`} style="grid-column:1/-1">
                {error}
              </p>
            )}
            <p class="email-picker-count" id={`${uid}-add-note`} style="grid-column:1/-1">
              {emails.length + 1} of {MAX} after saving
            </p>
          </form>
        ) : emails.length === 0 ? (
          <>
            <button type="button" class="email-picker-btn" onClick={() => setAdding(true)} aria-labelledby={`${uid}-l ${uid}-v`} ref={btn}>
              <span class="email-picker-value muted" id={`${uid}-v`}>
                No saved emails yet · add one
              </span>
              <Icon name="plus" size={16} />
            </button>
          </>
        ) : (
          <>
            <button
              ref={btn}
              type="button"
              class="email-picker-btn"
              aria-haspopup="listbox"
              aria-expanded={open}
              aria-labelledby={`${uid}-l ${uid}-v`}
              disabled={busy}
              onClick={() => (open ? setOpen(false) : openList())}
              onKeyDown={(e) => {
                if (e.key === 'ArrowDown' || e.key === 'ArrowUp' || e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  openList();
                }
              }}
            >
              <span class="email-picker-value" id={`${uid}-v`}>
                {selected?.email ?? 'Choose a saved email'}
              </span>
              <span class="email-picker-chev">
                <Icon name="chevron-down" size={16} />
              </span>
            </button>
            {open && (
              <ul
                ref={list}
                class="email-picker-list email-picker-list--float"
                role="listbox"
                tabIndex={-1}
                aria-label="Calendar emails"
                aria-activedescendant={optionId(activeIdx)}
                onKeyDown={onListKey}
              >
                {emails.map((e, i) => (
                  <li
                    key={e.id}
                    id={optionId(i)}
                    class={`email-option${activeIdx === i ? ' is-active' : ''}`}
                    role="option"
                    aria-selected={e.active}
                    onMouseEnter={() => setActiveIdx(i)}
                    onClick={() => select(i)}
                  >
                    <span class="email-option-check">
                      <Icon name="check" size={16} />
                    </span>
                    <span class="email-option-text">{e.email}</span>
                  </li>
                ))}
                <li
                  id={optionId(emails.length)}
                  class={`email-option email-option--add${activeIdx === emails.length && !full ? ' is-active' : ''}`}
                  role="option"
                  aria-selected={false}
                  aria-disabled={full}
                  onMouseEnter={() => setActiveIdx(emails.length)}
                  onClick={() => select(emails.length)}
                >
                  <span class="email-option-check" style="visibility:visible">
                    <Icon name="plus" size={16} />
                  </span>
                  Add another…
                </li>
                {full && <li class="email-picker-note" role="presentation">5 of 5 saved. Remove one in Settings to add another.</li>}
              </ul>
            )}
          </>
        )}
        {!adding && (
          <span class="email-picker-count" data-email-count>
            {emails.length} of {MAX} saved
          </span>
        )}
        <span class="visually-hidden" role="status">
          {status}
        </span>
      </div>
      {removable && emails.length > 0 && (
        <ul class="list" aria-label="Saved calendar emails" data-saved-emails>
          {emails.map((e) => (
            <li key={e.id} class="list-row">
              <span class="list-main">
                {e.email}
                {e.active && <span class="muted small"> · receives invites</span>}
              </span>
              <button type="button" class="btn btn--small" disabled={busy} onClick={() => remove(e)} aria-label={`Remove ${e.email}`}>
                Remove
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
