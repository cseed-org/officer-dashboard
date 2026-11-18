import { useCallback, useEffect, useRef, useState } from 'preact/hooks';
import type { EndpointName } from '../contracts';
import { api, type CallOptions, type ResponseOf } from './client';

export type Loadable<T> =
  | { status: 'loading'; data?: undefined; error?: undefined }
  | { status: 'ready'; data: T; error?: undefined }
  | { status: 'error'; data?: undefined; error: unknown };

/**
 * Load an endpoint on mount and whenever `key` changes (pass the query/params you depend
 * on). `reload()` preserves same-resource data; `set()` replaces or functionally updates it.
 * Disabled hooks do not fetch. Data from another endpoint/query is never returned as ready.
 */
export function useApi<N extends EndpointName>(name: N, opts: CallOptions<N> = {}, key: unknown = null, enabled = true) {
  const identity = JSON.stringify([name, opts.params, opts.query, key]);
  const [snapshot, setState] = useState<{ identity: string; state: Loadable<ResponseOf<N>> }>({ identity, state: { status: 'loading' } });
  const [tick, setTick] = useState(0);
  const optsRef = useRef(opts);
  optsRef.current = opts;
  const current = useRef(identity);
  current.current = identity;
  const revision = useRef(0);

  useEffect(() => {
    if (!enabled) return;
    const ctrl = new AbortController();
    const version = revision.current;
    const active = () => !ctrl.signal.aborted && current.current === identity && revision.current === version;
    setState((s) => s.identity === identity && s.state.status === 'ready' ? s : { identity, state: { status: 'loading' } });
    api(name, { ...optsRef.current, signal: ctrl.signal })
      .then((data) => active() && setState({ identity, state: { status: 'ready', data } }))
      .catch((error) => active() && setState({ identity, state: { status: 'error', error } }));
    return () => ctrl.abort();
  }, [identity, tick, enabled]);

  const reload = useCallback(() => setTick((t) => t + 1), []);
  type Update = ResponseOf<N> | ((data: ResponseOf<N>) => ResponseOf<N>);
  const set = useCallback((update: Update) => {
    if (current.current !== identity) return;
    revision.current++;
    setState((s) => {
      if (typeof update === 'function') {
        if (s.identity !== identity || s.state.status !== 'ready') return s;
        return { identity, state: { status: 'ready', data: (update as (d: ResponseOf<N>) => ResponseOf<N>)(s.state.data) } };
      }
      return { identity, state: { status: 'ready', data: update } };
    });
  }, [identity]);
  const state = enabled && snapshot.identity === identity ? snapshot.state : { status: 'loading' as const };
  return { ...state, reload, set };
}
