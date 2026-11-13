/**
 * Client copies of the server's profile rules (PUT /api/me/profile in
 * server/internal/api/officers.go), so the form refuses exactly what the server would.
 */

/** Server length limits per field. */
export const PROFILE_MAX = { phone: 32, preferredName: 60, pronouns: 40, personalEmail: 254, schoolEmail: 254, title: 100, team: 60, linkedin: 300 } as const;

/** isLinkedIn: https and a linkedin.com host (any subdomain); the path is free. */
export function isLinkedIn(s: string): boolean {
  try {
    const u = new URL(s);
    const h = u.hostname.toLowerCase();
    return u.protocol === 'https:' && (h === 'linkedin.com' || h.endsWith('.linkedin.com'));
  } catch {
    return false;
  }
}

/** gradDateRE: YYYY-MM. Browsers without a month picker show a plain text box. */
export const isGradMonth = (s: string) => /^\d{4}-(0[1-9]|1[0-2])$/.test(s);
