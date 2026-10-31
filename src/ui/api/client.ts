/**
 * The UI's only way to reach the backend. Every call is named by an ENDPOINTS key from
 * src/core/contracts.ts, so the path, method, body and response types all come from the
 * contract, and responses are validated with the contract's zod schema (contract drift
 * shows up as a ContractError instead of a blank page).
 */
import type { z } from 'zod';
import { ApiError, ENDPOINTS, type EndpointName } from '../../core/contracts';

type Spec<N extends EndpointName> = (typeof ENDPOINTS)[N];

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

function validate<N extends EndpointName>(name: N, data: unknown): ResponseOf<N> {
  const schema = ENDPOINTS[name].response;
  if (typeof schema === 'string') return data as ResponseOf<N>;
  const parsed = (schema as z.ZodType).safeParse(data);
  if (!parsed.success) {
    console.error(`[api] ${name} response failed contract validation`, parsed.error.issues);
    throw new ContractError(name, parsed.error.issues);
  }
  return parsed.data as ResponseOf<N>;
}

export async function api<N extends EndpointName>(name: N, opts: CallOptions<N> = {}): Promise<ResponseOf<N>> {
  const spec = ENDPOINTS[name];
  const url = endpointUrl(name, opts);
  const init: RequestInit = { method: spec.method, credentials: 'same-origin', headers: { accept: 'application/json' }, signal: opts.signal };
  if (opts.body instanceof FormData) init.body = opts.body;
  else if (opts.body !== undefined) {
    init.body = JSON.stringify(opts.body);
    (init.headers as Record<string, string>)['content-type'] = 'application/json';
  }

  let res: Response;
  try {
    res = await fetch(url, init);
  } catch (e) {
    if ((e as Error).name === 'AbortError') throw e;
    throw new ApiCallError(0, 'internal', 'Could not reach the server. Check your connection and try again.');
  }
  if (!res.ok) throw await toApiError(res);
  if (res.status === 204) return validate(name, { ok: true });
  const ct = res.headers.get('content-type') ?? '';
  if (!ct.includes('json')) throw new ApiCallError(res.status, 'internal', `Unexpected ${ct || 'empty'} response from ${spec.path}`);
  return validate(name, await res.json());
}

async function toApiError(res: Response): Promise<ApiCallError> {
  try {
    const parsed = ApiError.safeParse(await res.json());
    if (parsed.success) return new ApiCallError(res.status, parsed.data.error.code, parsed.data.error.message, parsed.data.error.details);
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
