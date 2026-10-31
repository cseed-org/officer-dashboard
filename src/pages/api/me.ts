import type { APIRoute } from 'astro';
import type { MeResponse } from '../../core/contracts';
import { requireRole } from '../../server/auth/guard';

export const GET: APIRoute = ({ locals }) => {
  const user = requireRole(locals, 'officer');
  const body: MeResponse = { user };
  return Response.json(body);
};
