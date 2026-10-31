import { useEffect, useState } from 'preact/hooks';
import { useApi } from '../api/useApi';
import { DataTable } from '../components/kit';
import { Async, Empty, Loading } from '../components/states';
import { eventColumns } from './OverviewView';

type When = 'all' | 'upcoming' | 'past';
const WHEN: { key: When; label: string }[] = [
  { key: 'all', label: 'all' },
  { key: 'upcoming', label: 'upcoming' },
  { key: 'past', label: 'past' },
];

function readUrl(): { when: When; q: string } {
  const p = new URLSearchParams(location.search);
  const w = p.get('when');
  return { when: w === 'upcoming' || w === 'past' ? w : 'all', q: p.get('q') ?? '' };
}

export default function EventsView({ initialWhen = 'all', initialQ = '' }: { initialWhen?: When; initialQ?: string }) {
  const [when, setWhen] = useState<When>(initialWhen);
  const [q, setQ] = useState(initialQ);
  const [debounced, setDebounced] = useState(initialQ);
  useEffect(() => {
    const t = setTimeout(() => setDebounced(q.trim()), 250);
    return () => clearTimeout(t);
  }, [q]);
  useEffect(() => {
    const url = new URL(location.href);
    when === 'all' ? url.searchParams.delete('when') : url.searchParams.set('when', when);
    debounced ? url.searchParams.set('q', debounced) : url.searchParams.delete('q');
    history.replaceState(null, '', url);
  }, [when, debounced]);
  useEffect(() => {
    const onPop = () => {
      const s = readUrl();
      setWhen(s.when);
      setQ(s.q);
    };
    addEventListener('popstate', onPop);
    return () => removeEventListener('popstate', onPop);
  }, []);

  const state = useApi('eventsList', { query: { when, q: debounced || undefined, limit: 100 } });

  return (
    <div class="stack">
      <div>
        <div class="page-tools">
          <div class="page-tools-main">
            <div class="chips" role="group" aria-label="Show events">
              {WHEN.map((w) => (
                <button type="button" class="chip" aria-pressed={when === w.key} onClick={() => setWhen(w.key)} key={w.key}>
                  {w.label}
                </button>
              ))}
            </div>
            <div class="field toolbar-search">
              <label class="visually-hidden" for="event-search">
                Search events
              </label>
              <input id="event-search" class="input" type="search" placeholder="search by name or place" value={q} onInput={(e) => setQ(e.currentTarget.value)} />
            </div>
          </div>
        </div>
        <Async state={state} loading={<Loading rows={6} label="Loading events" />}>
          {(d) =>
            d.items.length === 0 ? (
              debounced || when !== 'all' ? (
                <Empty
                  title="No matching events"
                  action={
                    <button
                      type="button"
                      class="linkish"
                      onClick={() => {
                        setQ('');
                        setWhen('all');
                      }}
                    >
                      clear filters
                    </button>
                  }
                >
                  Nothing {when !== 'all' ? when : ''} matches{debounced ? ` “${debounced}”` : ''}.
                </Empty>
              ) : (
                <Empty title="No events yet" action={<a href="/events/import">import a Luma guest export →</a>}>
                  Import the guest CSV from Luma to start tracking registrations, check-ins and reflections.
                </Empty>
              )
            ) : (
              <>
                <DataTable rows={d.items} columns={eventColumns()} rowKey={(e) => e.id} caption="Events" />
                {d.nextCursor && <p class="muted small">Showing the first {d.items.length} events. Narrow the search to find older ones.</p>}
              </>
            )
          }
        </Async>
      </div>
    </div>
  );
}
