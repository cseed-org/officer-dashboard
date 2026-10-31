import type { APIContext } from 'astro';
import { ZodError, z } from 'zod';
import { eq } from 'drizzle-orm';
import { ENDPOINTS } from '../../core/contracts';
import { getDb } from '../db/client';
import { apiError, HttpError } from '../auth/guard';
import { getFileStore, ReceiptStorageUnavailable } from '../services';
import { attachments } from '../db/schema';
import {
  type ApiContext,
  authenticateToken,
  director,
  officer,
  found,
  fail,
} from './context';
import { purchases } from './purchases';
import { events } from './events';
import { admin } from './admin';
import { finance } from './finance';
import { jobs } from './jobs';
/** Contract-indexed dispatcher; static /health and /me handlers remain standalone. */
export async function handleApi(context: APIContext): Promise<Response> {
  const c: ApiContext = {
    db: getDb(),
    user: context.locals.user,
    tokenId: null,
    request: context.request,
    url: context.url,
  };
  try {
    if (
      c.request.method === 'GET' &&
      /^\/api\/attachments\/[^/]+\/?$/.test(c.url.pathname)
    ) {
      officer(c);
      const a = found(
        await c.db
          .select()
          .from(attachments)
          .where(eq(attachments.id, c.url.pathname.split('/').filter(Boolean).at(-1)!))
          .get(),
      );
      if (a.storage === 'drive')
        return Response.redirect(found(a.webViewLink), 302);
      const file = found(await getFileStore().get(a.storageKey));
      return new Response(file.body, {
        headers: {
          'Content-Type': file.mimeType,
          'Content-Disposition': `attachment; filename*=UTF-8''${encodeURIComponent(a.filename)}`,
          'X-Content-Type-Options': 'nosniff',
          'Cache-Control': 'private, no-store',
        },
      });
    }
    let matched:
      | {
          name: string;
          spec: (typeof ENDPOINTS)[keyof typeof ENDPOINTS];
          id?: string;
        }
      | undefined;
    for (const [name, spec] of Object.entries(ENDPOINTS)) {
      if (spec.method !== c.request.method || !spec.path.startsWith('/api/'))
        continue;
      const pattern = new RegExp(
        `^${spec.path.replace(/:[^/]+/g, '([^/]+)')}/?$`,
      );
      const match = pattern.exec(c.url.pathname);
      if (match) {
        matched = {
          name,
          spec,
          id: match[1] ? decodeURIComponent(match[1]) : undefined,
        };
        break;
      }
    }
    if (!matched) fail(404, 'not_found', 'Endpoint not found');
    const { name, spec, id } = matched;
    if (spec.auth.startsWith('token:'))
      await authenticateToken(c, spec.auth.slice(6));
    else if (spec.auth === 'director') director(c);
    else if (spec.auth === 'officer') officer(c);
    const query =
      'query' in spec
        ? spec.query.parse(Object.fromEntries(c.url.searchParams))
        : {};
    let body: unknown;
    if ('body' in spec && spec.body !== 'multipart') {
      try {
        body = await c.request.json();
      } catch {
        fail(400, 'bad_request', 'Expected a JSON body');
      }
      body = spec.body.parse(body);
    }
    let result: unknown;
    if (name.startsWith('request'))
      result = await purchases(c, name, id, body, query);
    else if (name.startsWith('event') || name === 'ingestLuma')
      result = await events(c, name, id, body, query);
    else if (name.startsWith('admin'))
      result = await admin(c, name, id, body, query);
    else if (name.startsWith('agent'))
      result = await jobs(c, name, id, body, query);
    else result = await finance(c, name, id, body, query);
    if (result instanceof Response) return result;
    const output =
      typeof spec.response === 'string'
        ? null
        : spec.response.safeParse(result);
    if (output && !output.success) {
      console.error('API response contract failed', name, output.error.issues);
      return apiError(500, 'internal', 'Response validation failed');
    }
    const parsed = output?.success ? output.data : result;
    return Response.json(parsed, { headers: { 'Cache-Control': 'no-store' } });
  } catch (e) {
    if (e instanceof HttpError) return e.response;
    if (e instanceof ReceiptStorageUnavailable) return apiError(503, 'internal', e.message);
    if (e instanceof ZodError)
      return apiError(
        400,
        'bad_request',
        'Validation failed',
        z.flattenError(e),
      );
    const message = e instanceof Error ? e.message : String(e);
    const cause =
      e instanceof Error && e.cause instanceof Error ? e.cause.message : '';
    if (
      /constraint|mutation_guard|last active director|closed|open period|immutable|UNIQUE/i.test(
        message + ' ' + cause,
      )
    )
      return apiError(
        409,
        'conflict',
        /last active director/.test(message + ' ' + cause)
          ? 'Cannot remove the last active director'
          : 'Conflicting state, closed period, or duplicate record',
      );
    if (/release exceeds/.test(message))
      return apiError(
        409,
        'conflict',
        'Actual cost exceeds available restricted funding; review the fund allocation',
      );
    if (/MIME|10 MB/.test(message))
      return apiError(400, 'bad_request', message);
    console.error('API operation failed', message);
    return apiError(500, 'internal', 'Operation failed');
  }
}
