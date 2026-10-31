import type { ComponentChildren } from 'preact';
import { ApiCallError, ContractError } from '../api/client';
import type { Loadable } from '../api/useApi';

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
          <a href={`/login?next=${encodeURIComponent(location.pathname + location.search)}`}>sign in again →</a>
        ) : status === 404 ? (
          <a href="/">back to the overview →</a>
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
  if (state.status === 'loading') return <>{loading ?? <Loading />}</>;
  if (state.status === 'error') return <ErrorState error={state.error} onRetry={state.reload} />;
  return <div data-state="ready">{children(state.data)}</div>;
}

export function Notice({ tone = 'info', children }: { tone?: 'info' | 'positive' | 'negative' | 'attention'; children: ComponentChildren }) {
  return (
    <p class={`notice notice--${tone}`} role={tone === 'negative' ? 'alert' : 'status'}>
      {children}
    </p>
  );
}
