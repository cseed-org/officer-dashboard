import { href } from '../../lib/routes';
import type { ComponentChildren } from 'preact';
import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { ApiCallError, ContractError } from '../../lib/api/client';
import type { Loadable } from '../../lib/api/useApi';

/** Skeleton shown while data loads. `aria-busy` lets tests and screen readers know. */
export function Loading({ label = 'Loading', rows = 4, tiles = 0 }: { label?: string; rows?: number; tiles?: number }) {
  return (
    <div class="state state--loading" aria-busy="true" role="status" data-state="loading">
      <span class="visually-hidden">{label}…</span>
      {tiles > 0 && (
        <div class="kpis">
          {Array.from({ length: tiles }, (_, i) => (
            <div class="kpi" key={i}>
              <span class="skel skel--label" />
              <span class="skel skel--kpi" />
            </div>
          ))}
        </div>
      )}
      {Array.from({ length: rows }, (_, i) => (
        <span class="skel skel--row" key={i} style={{ width: `${92 - (i % 3) * 14}%` }} />
      ))}
    </div>
  );
}

export function Empty({ title, children, action }: { title: string; children?: ComponentChildren; action?: ComponentChildren }) {
  return (
    <div class="state state--empty" data-state="empty">
      <p class="state-title">{title}</p>
      {children && <p class="state-body">{children}</p>}
      {action && <div class="state-action">{action}</div>}
    </div>
  );
}

export function errorMessage(error: unknown): string {
  if (error instanceof ApiCallError) return error.message;
  if (error instanceof ContractError) return `${error.message} Tell whoever is working on the backend.`;
  if (error instanceof Error) return error.message;
  return 'Something went wrong.';
}

export function ErrorState({ error, onRetry, title }: { error: unknown; onRetry?: () => void; title?: string }) {
  const status = error instanceof ApiCallError ? error.status : null;
  const heading =
    title ??
    (status === 404 ? 'Not found' : status === 403 ? 'Not allowed' : status === 401 ? 'Signed out' : "Couldn't load this");
  return (
    <div class="state state--error" role="alert" data-state="error">
      <p class="state-title">{heading}</p>
      <p class="state-body">{errorMessage(error)}</p>
      <div class="state-action">
        {status === 401 ? (
          <a href={href.login(location.pathname + location.search)}>sign in again →</a>
        ) : status === 404 ? (
          <a href={href.home}>back home →</a>
        ) : (
          onRetry && (
            <button type="button" class="btn" onClick={onRetry}>
              try again
            </button>
          )
        )}
      </div>
    </div>
  );
}

/** Render loading / error / ready for a useApi() result. */
export function Async<T>({
  state,
  children,
  loading,
}: {
  state: Loadable<T> & { reload?: () => void };
  children: (data: T) => ComponentChildren;
  loading?: ComponentChildren;
}) {
  const box = useRef<HTMLDivElement>(null);
  const reservedHeight = useRef(0);
  const placeholder = useRef(loading ?? <Loading />);
  const [outgoing, setOutgoing] = useState(state.status === 'loading');
  if (state.status === 'loading') placeholder.current = loading ?? <Loading />;
  useLayoutEffect(() => {
    if (state.status === 'loading' && box.current) {
      // One read at the state boundary, never in an animation frame.
      reservedHeight.current = box.current.getBoundingClientRect().height;
    }
  }, [state.status]);
  useEffect(() => {
    if (state.status === 'loading') { setOutgoing(true); return; }
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) { setOutgoing(false); return; }
    const timer = setTimeout(() => setOutgoing(false), 180);
    return () => clearTimeout(timer);
  }, [state.status]);
  if (state.status === 'error') return <ErrorState error={state.error} onRetry={state.reload} />;
  return <div ref={box} class="async-state" data-state={state.status} style={state.status === 'ready' ? { minHeight: reservedHeight.current } : undefined}>
    {state.status === 'loading' ? placeholder.current : <>
      <div class="async-content">{children(state.data)}</div>
      {outgoing && <div class="async-outgoing" aria-hidden="true" inert>{placeholder.current}</div>}
    </>}
  </div>;
}

export function Notice({ tone = 'info', children }: { tone?: 'info' | 'positive' | 'negative' | 'attention'; children: ComponentChildren }) {
  return (
    <p class={`notice notice--${tone}`} role={tone === 'negative' ? 'alert' : 'status'}>
      {children}
    </p>
  );
}
