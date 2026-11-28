/**
 * The access email: where Cloudflare Access will send a person's sign-in code once the
 * dashboard manages Cloudflare access (a later release). Today it is only stored, so the UI
 * says plainly that saving it changes nothing in Cloudflare.
 */
export const ACCESS_EMAIL_LABEL = 'Cloudflare sign-in email';

export const ACCESS_EMAIL_HINT =
  'Where Cloudflare will send their sign-in code once the dashboard manages Cloudflare access. That isn’t switched on yet, so saving this changes nothing in Cloudflare: still add them to the Officers group there by hand. Only directors and leadership see it.';

/** Trimmed and lower-cased, as the server stores it; empty means none. */
export const normalizeAccessEmail = (s: string): string | null => s.trim().toLowerCase() || null;

/** Who may change whose access email, mirroring the server: directors for officers,
 *  leadership for directors and leadership, and only an owner for an owner. */
export function canSetAccessEmail(me: { role: string; isOwner: boolean }, target: { role: string; isOwner: boolean }): boolean {
  if (target.isOwner) return me.isOwner;
  if (target.role !== 'officer') return me.role === 'leadership';
  return me.role === 'director' || me.role === 'leadership';
}
