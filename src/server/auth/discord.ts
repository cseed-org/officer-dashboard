import { Discord } from 'arctic';
import { appUrl, getEnv } from '../env';

export const OAUTH_STATE_COOKIE = 'cseed_oauth_state';
export const OAUTH_VERIFIER_COOKIE = 'cseed_oauth_verifier';
export const DISCORD_SCOPES = ['identify'];

export function discordClient(request: Request): Discord {
  const env = getEnv();
  if (!env.DISCORD_CLIENT_ID || !env.DISCORD_CLIENT_SECRET) {
    throw new Error('DISCORD_CLIENT_ID and DISCORD_CLIENT_SECRET must be set (see README)');
  }
  return new Discord(env.DISCORD_CLIENT_ID, env.DISCORD_CLIENT_SECRET, `${appUrl(request)}/auth/callback`);
}

export interface DiscordProfile {
  id: string;
  username: string;
  global_name: string | null;
  avatar: string | null;
}

export async function fetchDiscordProfile(accessToken: string): Promise<DiscordProfile> {
  const res = await fetch('https://discord.com/api/v10/users/@me', {
    headers: { Authorization: `Bearer ${accessToken}` },
  });
  if (!res.ok) throw new Error(`Discord /users/@me failed: ${res.status}`);
  return (await res.json()) as DiscordProfile;
}

export function avatarUrl(p: DiscordProfile): string | null {
  return p.avatar ? `https://cdn.discordapp.com/avatars/${p.id}/${p.avatar}.png?size=128` : null;
}
