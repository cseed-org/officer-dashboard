import type { APIRoute } from 'astro';
import { generateCodeVerifier, generateState } from 'arctic';
import { DISCORD_SCOPES, OAUTH_STATE_COOKIE, OAUTH_VERIFIER_COOKIE, discordClient } from '../../server/auth/discord';
import { safeNext } from '../../server/auth/next';

export const GET: APIRoute = ({ request, cookies, redirect, url }) => {
  const state = generateState();
  const verifier = generateCodeVerifier();
  const authUrl = discordClient(request).createAuthorizationURL(state, verifier, DISCORD_SCOPES);
  const opts = { httpOnly: true, secure: !import.meta.env.DEV, sameSite: 'lax', path: '/', maxAge: 600 } as const;
  cookies.set(OAUTH_STATE_COOKIE, state, opts);
  cookies.set(OAUTH_VERIFIER_COOKIE, verifier, opts);
  cookies.set('cseed_next', safeNext(url.searchParams.get('next')), opts);
  return redirect(authUrl.toString(), 302);
};
