import { useCallback, useEffect, useRef, useState } from 'preact/hooks';
import type { EndpointName } from '../../core/contracts';
import { api, type CallOptions, type ResponseOf } from './client';

export type Loadable<T> =
  | { status: 'loading'; data?: undefined; error?: undefined }
  | { status: 'ready'; data: T; error?: undefined }
  | { status: 'error'; data?: undefined; error: unknown };

/**
 * Load an endpoint on mount and whenever `key` changes (pass the query/params you depend
 * on). `reload()` refetches; `set()` replaces the data after a mutation.
 */
export function useApi<N extends EndpointName>(name: N, opts: CallOptions<N> = {}, key: unknown = null) {
  const [state, setState] = useState<Loadable<ResponseOf<N>>>({ status: 'loading' });
  const [tick, setTick] = useState(0);
  const optsRef = useRef(opts);
  optsRef.current = opts;
  const serial = JSON.stringify(key ?? { p: opts.params, q: opts.query });

  useEffect(() => {
    const ctrl = new AbortController();
    setState((s) => (s.status === 'ready' ? s : { status: 'loading' }));
    api(name, { ...optsRef.current, signal: ctrl.signal })
      .then((data) => !ctrl.signal.aborted && setState({ status: 'ready', data }))
      .catch((error) => !ctrl.signal.aborted && setState({ status: 'error', error }));
    return () => ctrl.abort();
  }, [name, serial, tick]);

  const reload = useCallback(() => setTick((t) => t + 1), []);
  const set = useCallback((data: ResponseOf<N>) => setState({ status: 'ready', data }), []);
  return { ...state, reload, set } as Loadable<ResponseOf<N>> & { reload: () => void; set: (d: ResponseOf<N>) => void };
}
