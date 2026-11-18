/**
 * Launchpad cohorts, portal accounts and lifecycle helpers shared by the officer page
 * (docs/launchpad-cohorts.md). Pure functions only: the parser is unit-testable.
 */
import type { CurrentUser, LaunchpadAccount } from "../../../lib/contracts";
import type { Hue } from "../../../lib/colors";

/** Director-or-higher UI (cohorts, mentor invites, account disable, hard delete). */
export const isDirector = (me: CurrentUser) =>
  me.isOwner || me.role === "director" || me.role === "leadership";

export type ParsedInvite = {
  line: number;
  raw: string;
  email: string;
  name?: string;
  state: "valid" | "duplicate" | "invalid" | "pending" | "active" | "disabled";
};

const EMAIL = /^[^\s@,;<>"]+@[^\s@,;<>"]+\.[^\s@,;<>"]+$/;

/**
 * One email per line, or `name,email` / `email,name` CSV (a header row is skipped). Several
 * bare emails on one line (comma, semicolon or space separated) also work. Each row is
 * classified against the cohort's existing accounts so the preview says what will happen.
 */
export function parseInvites(text: string, accounts: LaunchpadAccount[] = []): ParsedInvite[] {
  const known = new Map(accounts.map((a) => [a.email.toLowerCase(), a.status]));
  const seen = new Set<string>();
  const out: ParsedInvite[] = [];
  text.split(/\r?\n/).forEach((rawLine, i) => {
    const raw = rawLine.trim();
    if (!raw) return;
    if (/^"?name"?\s*[,;\t]\s*"?e-?mail"?$/i.test(raw) || /^"?e-?mail"?\s*[,;\t]\s*"?name"?$/i.test(raw)) return;
    const cells = (raw.match(/(?:[^,;\t"]|"[^"]*")+/g) ?? []).map((c) => c.trim().replace(/^"|"$/g, "").trim()).filter(Boolean);
    let rows: { email: string; name?: string }[];
    const emails = cells.filter((c) => c.includes("@"));
    if (cells.some((c) => c.includes("<"))) {
      rows = cells.map((c) => {
        const m = c.match(/^(.*?)<([^<>\s]+@[^<>\s]+)>$/);
        return m ? { email: m[2]!, name: m[1]!.replace(/"/g, "").trim() || undefined } : { email: c };
      });
    } else if (cells.length === 2 && emails.length === 1) {
      const email = emails[0]!;
      rows = [{ email, name: cells.find((c) => c !== email) }];
    } else if (cells.length >= 1 && emails.length === cells.length) {
      rows = cells.flatMap((c) => c.split(/\s+/)).filter(Boolean).map((email) => ({ email }));
    } else {
      // "Ada Lovelace <ada@uw.edu>" or anything else: take an address if there is exactly one.
      const found = raw.match(/[^\s@,;<>"]+@[^\s@,;<>"]+/g) ?? [];
      rows = found.length === 1 ? [{ email: found[0]!, name: raw.replace(found[0]!, "").replace(/[<>",;]/g, " ").trim() || undefined }] : [{ email: raw }];
    }
    for (const r of rows) {
      const email = r.email.toLowerCase();
      let state: ParsedInvite["state"] = "valid";
      if (!EMAIL.test(email) || email.length > 254) state = "invalid";
      else if (seen.has(email)) state = "duplicate";
      else if (known.get(email) === "active") state = "active";
      else if (known.get(email) === "disabled") state = "disabled";
      else if (known.get(email) === "invited") state = "pending";
      if (state !== "invalid") seen.add(email);
      out.push({ line: i + 1, raw, email, name: r.name?.slice(0, 200), state });
    }
  });
  return out;
}

/** Rows the server will act on: new invites, plus pending ones (a fresh link). */
export const sendable = (rows: ParsedInvite[]) => rows.filter((r) => r.state === "valid" || r.state === "pending");

export const INVITE_STATE_LABEL: Record<ParsedInvite["state"], string> = {
  valid: "New invite",
  pending: "New link (already invited)",
  duplicate: "Duplicate, skipped",
  invalid: "Not an email",
  active: "Already joined, skipped",
  disabled: "Disabled account, skipped",
};

export const ACCOUNT_HUE: Record<LaunchpadAccount["status"], Hue> = {
  invited: "amber",
  active: "green",
  disabled: "coral",
};

export function accountLabel(a: Pick<LaunchpadAccount, "status" | "inviteExpired">): string {
  if (a.status === "invited") return a.inviteExpired ? "Invite expired" : "Invited";
  return a.status === "active" ? "Active" : "Disabled";
}

export const SKIP_REASON = {
  already_active: "already has an active account",
  disabled: "has a disabled account (enable it instead)",
  duplicate: "was listed twice",
} as const;

/** Milliseconds → "Nov 3" style date (account timestamps are epoch ms). */
export const msDate = (ms: number | null) =>
  ms === null ? "" : new Date(ms).toLocaleDateString("en-US", { month: "short", day: "numeric" });

export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}
