import type { APIRoute } from 'astro';
import { handleApi } from '../../server/api/handler';
export const ALL: APIRoute = handleApi;
