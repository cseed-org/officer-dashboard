import { savingForm } from '../unsaved';
/**
 * The UI's only way to reach the backend. Every call is named by an ENDPOINTS key from
 * src/core/contracts.ts, so the path, method, body and response types all come from the
 * contract, and responses are validated with the contract's zod schema (contract drift
 * shows up as a ContractError instead of a blank page).
 *
 * The zod contract is about half of a page's JavaScript, and no request needs it to be sent,
 * so it loads in the background after the page's first data (`loadContracts`). Until then a
 * GET response is used as it arrives and validated as soon as the contract has loaded; a
 * mismatch raises the same ContractError, which the page Gate shows as its error state.
 * Writes, and every read once the contract is loaded, are validated before they return.
 */
import type { z } from 'zod';
import type { ApiError, ENDPOINTS as Contract, EndpointName } from '../contracts';
import ENDPOINTS from '../endpoints.generated';
import { href } from '../routes';
import { takeEarly } from '../early';

type Spec<N extends EndpointName> = (typeof Contract)[N];

export type ResponseOf<N extends EndpointName> = Spec<N>['response'] extends z.ZodType ? z.output<Spec<N>['response']> : never;
export type BodyOf<N extends EndpointName> = Spec<N> extends { body: infer B }
  ? B extends z.ZodType
    ? z.input<B>
    : FormData
  : never;
export type QueryOf<N extends EndpointName> = Spec<N> extends { query: infer Q } ? (Q extends z.ZodType ? Partial<z.input<Q>> : never) : never;

type PathParams<P extends string> = P extends `${string}:${infer K}/${infer Rest}`
  ? K | PathParams<`/${Rest}`>
  : P extends `${string}:${infer K}`
    ? K
    : never;
export type ParamsOf<N extends EndpointName> = [PathParams<Spec<N>['path']>] extends [never]
  ? never
  : Record<PathParams<Spec<N>['path']>, string>;

export interface CallOptions<N extends EndpointName> {
  params?: ParamsOf<N>;
  query?: QueryOf<N>;
  body?: BodyOf<N>;
  signal?: AbortSignal;
}

/** A non-2xx response, carrying the contract's ApiError fields. */
export class ApiCallError extends Error {
  constructor(
    readonly status: number,
    readonly code: ApiError['error']['code'],
    message: string,
    readonly details?: unknown,
  ) {
    super(message);
    this.name = 'ApiCallError';
  }
}

/** The response did not match the contract schema. */
export class ContractError extends Error {
  constructor(
    readonly endpoint: string,
    readonly issues: unknown,
  ) {
    super(`The ${endpoint} response did not match the API contract.`);
    this.name = 'ContractError';
  }
}

export function buildPath(path: string, params?: Record<string, string>, query?: Record<string, unknown>): string {
  const filled = path.replace(/:(\w+)/g, (_, key: string) => {
    const v = params?.[key];
    if (v === undefined) throw new Error(`missing path param ${key} for ${path}`);
    return encodeURIComponent(v);
  });
  const qs = new URLSearchParams();
  for (const [k, v] of Object.entries(query ?? {})) {
    if (v === undefined || v === null || v === '') continue;
    qs.set(k, String(v));
  }
  const s = qs.toString();
  return s ? `${filled}?${s}` : filled;
}

/** URL of an endpoint, for links (e.g. CSV downloads). */
export function endpointUrl<N extends EndpointName>(name: N, opts: Pick<CallOptions<N>, 'params' | 'query'> = {}): string {
  return buildPath(ENDPOINTS[name].path, opts.params as Record<string, string> | undefined, opts.query as Record<string, unknown> | undefined);
}

type Contracts = typeof import('../contracts');
let contracts: Contracts | null = null;
let contractsLoading: Promise<Contracts> | null = null;
let markLoaded!: (c: Contracts) => void;
/** Resolves when the contract has loaded, without starting the load. */
const contractsLoaded = new Promise<Contracts>((resolve) => { markLoaded = resolve; });
/** The zod contract module, imported once. */
export function loadContracts(): Promise<Contracts> {
  contractsLoading ??= import('../contracts').then(
    (m) => { contracts = m; markLoaded(m); return m; },
    (err) => { contractsLoading = null; throw err; },
  );
  return contractsLoading;
}
let scheduled = false;
/**
 * Load the contract after the page's first render: once the document has loaded (its preloaded
 * modules and images are in) and the main thread is next idle, so evaluating ~350 schemas never
 * competes with the first paint of data.
 */
function loadContractsSoon() {
  if (scheduled || contracts) return;
  scheduled = true;
  const start = () => void loadContracts().catch((err) => { scheduled = false; console.warn('[api] could not load the contract', err); });
  const idle = () => (typeof requestIdleCallback === 'function' ? requestIdleCallback(start, { timeout: 2000 }) : setTimeout(start, 200));
  if (document.readyState === 'complete') idle();
  else window.addEventListener('load', idle, { once: true });
}

/** A response failed validation after it was already returned: the page Gate shows this error. */
export const CONTRACT_ERROR_EVENT = 'cseed-contract-error';

function check<N extends EndpointName>(c: Contracts, name: N, data: unknown): ResponseOf<N> {
  const schema = c.ENDPOINTS[name].response;
  if (typeof schema === 'string') return data as ResponseOf<N>;
  const parsed = (schema as z.ZodType).safeParse(data);
  if (!parsed.success) {
    console.error(`[api] ${name} response failed contract validation`, parsed.error.issues);
    throw new ContractError(name, parsed.error.issues);
  }
  return parsed.data as ResponseOf<N>;
}

async function validate<N extends EndpointName>(name: N, method: string, data: unknown): Promise<ResponseOf<N>> {
  if (contracts) return check(contracts, name, data);
  if (method !== 'GET') return check(await loadContracts(), name, data);
  // A read before the contract has loaded: render it now, check it once the contract is here.
  loadContractsSoon();
  void contractsLoaded.then(
    (c) => { try { check(c, name, data); } catch (err) { window.dispatchEvent(new CustomEvent(CONTRACT_ERROR_EVENT, { detail: err })); } },
    () => {},
  );
  return data as ResponseOf<N>;
}

/**
 * Permission changes reach open tabs: every signed-in API response carries X-Perm-Version, a
 * fingerprint of the user's role, owner flag and subteam. When a response shows a version other
 * than the one the page is rendered for, PERMISSIONS_EVENT fires and lib/session.ts refetches
 * /api/me, which updates the nav and remounts the page for the new permissions.
 */
export const PERMISSIONS_EVENT = 'cseed-permissions';
let pageVersion: string | null = null; // the version of the /api/me the page last applied
let meVersion: string | null = null; // the version on the latest /api/me response

function notePermissions(name: EndpointName, res: Response) {
  const v = res.headers.get('x-perm-version');
  if (!v) return;
  if (name === 'me') meVersion = v;
  if (pageVersion !== null && v !== pageVersion && typeof window !== 'undefined') window.dispatchEvent(new Event(PERMISSIONS_EVENT));
}

export const permissions = {
  /** The latest /api/me shows other permissions than the page was rendered for. */
  changed: () => pageVersion !== null && meVersion !== null && meVersion !== pageVersion,
  /** The page now renders the latest /api/me. */
  accept: () => { pageVersion = meVersion; },
};

export const PERMISSIONS_CHANGED_MESSAGE = 'Your permissions changed, so this is no longer allowed. The page now shows what you can do.';

export async function api<N extends EndpointName>(name: N, opts: CallOptions<N> = {}): Promise<ResponseOf<N>> {
  const spec = ENDPOINTS[name];
  const url = endpointUrl(name, opts);
  const init: RequestInit = { method: spec.method, credentials: 'same-origin', headers: { accept: 'application/json' }, signal: opts.signal };
  if (opts.body instanceof FormData) init.body = opts.body;
  else if (opts.body !== undefined) {
    init.body = JSON.stringify(opts.body);
    (init.headers as Record<string, string>)['content-type'] = 'application/json';
  }

  const saved = spec.method === 'GET' ? () => {} : savingForm();
  let res: Response;
  try {
    // A read the page's head script already started (lib/early.ts) is used once, as is.
    const early = spec.method === 'GET' && !opts.signal?.aborted ? takeEarly(url) : undefined;
    res = await (early ?? fetch(url, init));
  } catch (e) {
    if ((e as Error).name === 'AbortError') throw e;
    throw new ApiCallError(0, 'internal', 'Could not reach the server. Check your connection and try again.');
  }
  notePermissions(name, res);
  if (!res.ok) {
    let err = await toApiError(res);
    if (res.status === 401) redirectToLogin();
    if (res.status === 403 && err.code !== 'onboarding_required') {
      // A refusal may mean this tab is out of date: say so plainly, and recheck /api/me.
      const v = res.headers.get('x-perm-version');
      if (pageVersion !== null && v !== null && v !== pageVersion) err = new ApiCallError(403, err.code, PERMISSIONS_CHANGED_MESSAGE, err.details);
      if (name !== 'me' && typeof window !== 'undefined') window.dispatchEvent(new Event(PERMISSIONS_EVENT));
    }
    // The server holds invitees on setup until they finish (403 onboarding_required).
    if (err.code === 'onboarding_required' && typeof location !== 'undefined' && location.pathname !== '/onboarding') location.replace('/onboarding');
    throw err;
  }
  if (res.status === 204) { const result = await validate(name, spec.method, { ok: true }); saved(); return result; }
  const ct = res.headers.get('content-type') ?? '';
  if (!ct.includes('json')) throw new ApiCallError(res.status, 'internal', `Unexpected ${ct || 'empty'} response from ${spec.path}`);
  const result = await validate(name, spec.method, await res.json());
  saved();
  return result;
}

let redirecting = false;
/** The session is gone (any endpoint answered 401): go to /login and come back here after. */
export function redirectToLogin() {
  if (redirecting || typeof location === 'undefined') return;
  redirecting = true;
  try {
    localStorage.removeItem('cseed-role');
  } catch {
    /* storage blocked */
  }
  location.replace(href.login(location.pathname + location.search));
}

async function toApiError(res: Response): Promise<ApiCallError> {
  try {
    // The ApiError shape, checked by hand so error handling never waits for the zod contract.
    const body = (await res.json()) as Partial<ApiError> | null;
    const e = body?.error;
    if (e && typeof e.code === 'string' && typeof e.message === 'string') return new ApiCallError(res.status, e.code, e.message, e.details);
  } catch {
    /* not JSON */
  }
  const code = res.status === 401 ? 'unauthenticated' : res.status === 403 ? 'forbidden' : res.status === 404 ? 'not_found' : 'internal';
  const message =
    res.status === 404 ? 'This endpoint is not available yet.' : res.status === 401 ? 'Your session ended. Sign in again.' : `Request failed (${res.status}).`;
  return new ApiCallError(res.status, code, message);
}

/** Field errors from a 400 response (zod `flatten()` shape), keyed by field. */
export function fieldErrors(err: unknown): Record<string, string> {
  if (!(err instanceof ApiCallError) || err.code !== 'bad_request') return {};
  const d = err.details as { fieldErrors?: Record<string, string[] | undefined> } | undefined;
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(d?.fieldErrors ?? {})) if (v?.[0]) out[k] = v[0];
  return out;
}

/** Receipt storage (Google Drive) is not configured on the server: uploads answer 503. */
export const isStorageUnavailable = (err: unknown) => err instanceof ApiCallError && err.status === 503;

export const STORAGE_UNAVAILABLE_MESSAGE =
  "Receipt storage isn't connected (Google Drive is not configured on the server), so files can't be uploaded right now. Nothing else was lost: ask a director to set up Drive, then add the files from the request page.";
