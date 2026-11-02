import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import { api, ApiCallError } from '../../lib/api/client';
import type { CurrentUser, SearchResult } from '../../lib/contracts';
import { fmtDate, formatCents, STATUS_LABEL } from '../../lib/format';
import { navigate } from '../../lib/nav';
import { href } from '../../lib/routes';
import { sanitizeResults } from '../../lib/search';
import { loadMe } from '../../lib/session';
import { Icon } from '../components/Icon';

type Kind = SearchResult['kind'] | 'page';
type Item = { kind: Kind; id: string; title: string; subtitle: string; href: string };

const GROUP_LABEL: Record<Kind, string> = { page: 'go to', request: 'requests', event: 'events', user: 'people', account: 'accounts', contact: 'contacts' };
const GROUP_ORDER: Kind[] = ['request', 'event', 'user', 'account', 'contact', 'page'];

const pages = (director: boolean): Item[] => [
  { kind: 'page', id: 'new-request', title: 'New purchase request', subtitle: 'finance', href: href.newRequest() },
  { kind: 'page', id: 'home', title: 'Home', subtitle: 'overview', href: href.home },
  { kind: 'page', id: 'events', title: 'Events', subtitle: 'turnout and guests', href: href.events() },
  { kind: 'page', id: 'import', title: 'Import a Luma CSV', subtitle: 'events', href: href.eventImport() },
  { kind: 'page', id: 'finance', title: 'Requests & finance', subtitle: 'requests, budgets, reports', href: href.finance() },
  { kind: 'page', id: 'reports', title: 'Finance reports', subtitle: 'statements and CSV export', href: href.finance('reports') },
  ...(director ? [{ kind: 'page' as const, id: 'admin', title: 'Admin', subtitle: 'officers, tokens, audit log', href: href.admin() }] : []),
  { kind: 'page', id: 'settings', title: 'Settings', subtitle: 'theme, density, sign out', href: href.settings },
];

/** Until the Go server's /api/search exists (v1 answers 404), search requests and events with their list endpoints. */
let fallback = false;
async function searchApi(q: string, signal: AbortSignal): Promise<Item[]> {
  if (!fallback) {
    try {
      return (await api('search', { query: { q, limit: 20 }, signal })).results;
    } catch (e) {
      if (!(e instanceof ApiCallError && e.status === 404)) throw e;
      fallback = true;
    }
  }
  const [reqs, evs] = await Promise.all([api('requestsList', { query: { scope: 'all', q, limit: 8 }, signal }), api('eventsList', { query: { q, limit: 8 }, signal })]);
  return [
    ...reqs.items.map((r) => ({ kind: 'request' as const, id: r.id, title: r.title, subtitle: `#${r.number} · ${r.requester.name} · ${formatCents(r.totalCents)} · ${STATUS_LABEL[r.status]}`, href: href.request(r.id) })),
    ...evs.items.map((e) => ({ kind: 'event' as const, id: e.id, title: e.name, subtitle: [fmtDate(e.startsAt), e.location].filter(Boolean).join(' · '), href: href.event(e.id) })),
  ];
}

const isMac = () => typeof navigator !== 'undefined' && /Mac|iP(hone|ad|od)/.test(navigator.platform);

export default function CommandPalette() {
  const ref = useRef<HTMLDialogElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const opener = useRef<HTMLElement | null>(null);
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState('');
  const [results, setResults] = useState<Item[]>([]);
  const [status, setStatus] = useState<'idle' | 'loading' | 'error'>('idle');
  const [active, setActive] = useState(0);
  const [me, setMe] = useState<CurrentUser | null>(null);

  useEffect(() => {
    loadMe().then(setMe, () => {});
    const show = () => {
      opener.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      setOpen(true);
    };
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && !e.altKey && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        if (ref.current?.open) ref.current.close();
        else show();
      }
    };
    const onClick = (e: MouseEvent) => {
      if ((e.target as Element | null)?.closest?.('[data-open-search]')) {
        e.preventDefault();
        show();
      }
    };
    document.addEventListener('keydown', onKey);
    document.addEventListener('click', onClick);
    document.querySelectorAll('[data-shortcut]').forEach((el) => (el.textContent = isMac() ? '⌘K' : 'Ctrl K'));
    return () => {
      document.removeEventListener('keydown', onKey);
      document.removeEventListener('click', onClick);
    };
  }, []);

  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) {
      d.showModal();
      document.documentElement.classList.add('has-modal');
      inputRef.current?.focus();
      inputRef.current?.select();
    }
  }, [open]);

  // Search as you type (debounced); empty query shows the page list.
  useEffect(() => {
    const term = q.trim();
    if (!open || term.length < 2) {
      setResults([]);
      setStatus('idle');
      return;
    }
    const ctrl = new AbortController();
    setStatus('loading');
    const t = setTimeout(() => {
      searchApi(term, ctrl.signal).then(
        (r) => {
          if (ctrl.signal.aborted) return;
          setResults(sanitizeResults(r));
          setStatus('idle');
        },
        () => !ctrl.signal.aborted && setStatus('error'),
      );
    }, 150);
    return () => {
      clearTimeout(t);
      ctrl.abort();
    };
  }, [q, open]);

  const items = useMemo(() => {
    const term = q.trim().toLowerCase();
    const pageHits = pages(!!me && me.role !== 'officer').filter((p) => !term || p.title.toLowerCase().includes(term) || p.subtitle.includes(term));
    const all = [...results, ...pageHits];
    return GROUP_ORDER.flatMap((k) => all.filter((i) => i.kind === k));
  }, [results, q, me]);
  useEffect(() => setActive(0), [items.length, q]);

  const go = (item: Item | undefined) => {
    if (!item) return;
    ref.current?.close();
    navigate(item.href);
  };
  const onKeyDown = (e: KeyboardEvent) => {
    if (e.isComposing || e.keyCode === 229) return; // IME candidate selection, not a command
    if (e.key === 'ArrowDown' || (e.key === 'n' && e.ctrlKey)) {
      e.preventDefault();
      setActive((a) => (items.length ? (a + 1) % items.length : 0));
    } else if (e.key === 'ArrowUp' || (e.key === 'p' && e.ctrlKey)) {
      e.preventDefault();
      setActive((a) => (items.length ? (a - 1 + items.length) % items.length : 0));
    } else if (e.key === 'Home' && e.ctrlKey) setActive(0);
    else if (e.key === 'End' && e.ctrlKey) setActive(items.length - 1);
    else if (e.key === 'Enter') {
      e.preventDefault();
      go(items[active]);
    }
  };
  useEffect(() => {
    document.getElementById(`cmd-opt-${active}`)?.scrollIntoView({ block: 'nearest' });
  }, [active]);

  let idx = -1;
  const groups = GROUP_ORDER.map((k) => ({ k, list: items.filter((i) => i.kind === k) })).filter((g) => g.list.length);
  const term = q.trim();

  return (
    <dialog
      ref={ref}
      class="palette"
      aria-label="Search"
      onClose={() => {
        setOpen(false);
        setQ('');
        document.documentElement.classList.remove('has-modal');
        if (opener.current?.isConnected) opener.current.focus();
      }}
      onClick={(e) => {
        if (e.target === ref.current) ref.current?.close();
      }}
    >
      <div class="palette-inner">
        <div class="palette-search">
          <Icon name="search" />
          <input
            ref={inputRef}
            class="palette-input"
            type="text"
            role="combobox"
            aria-expanded={items.length > 0}
            aria-controls="cmd-list"
            aria-activedescendant={items.length ? `cmd-opt-${active}` : undefined}
            aria-autocomplete="list"
            aria-label="Search requests, events and pages"
            placeholder="Search requests, events, people…"
            autoComplete="off"
            spellcheck={false}
            value={q}
            onInput={(e) => setQ(e.currentTarget.value)}
            onKeyDown={onKeyDown}
          />
          <kbd class="palette-esc">esc</kbd>
        </div>
        <div class="palette-results" id="cmd-list" role="listbox" aria-label="Results">
          {groups.map((g) => (
            <div role="group" aria-labelledby={`cmd-g-${g.k}`} class="palette-group" key={g.k} data-group={g.k}>
              <div class="palette-group-label" id={`cmd-g-${g.k}`} role="presentation">
                {GROUP_LABEL[g.k]}
              </div>
              {g.list.map((item) => {
                idx += 1;
                const i = idx;
                return (
                  <div
                    role="option"
                    id={`cmd-opt-${i}`}
                    key={`${item.kind}-${item.id}`}
                    class="palette-option"
                    aria-selected={i === active}
                    data-kind={item.kind}
                    data-href={item.href}
                    onMouseMove={() => setActive(i)}
                    onClick={() => go(item)}
                  >
                    <span class="palette-option-title">{item.title}</span>
                    <span class="palette-option-sub">{item.subtitle}</span>
                  </div>
                );
              })}
            </div>
          ))}
          <p class="palette-status" role="status" aria-live="polite">
            {status === 'loading'
              ? 'Searching…'
              : status === 'error'
                ? 'Search failed. Check your connection and try again.'
                : term.length >= 2 && results.length === 0
                  ? `No requests or events match “${term}”.`
                  : term.length === 1
                    ? 'Keep typing to search.'
                    : ''}
          </p>
        </div>
        <footer class="palette-foot small muted">
          <span>
            <kbd>↑</kbd> <kbd>↓</kbd> to move
          </span>
          <span>
            <kbd>enter</kbd> to open
          </span>
        </footer>
      </div>
    </dialog>
  );
}
