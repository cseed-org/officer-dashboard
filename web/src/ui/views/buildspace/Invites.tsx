/**
 * The Invites tab: the selected cohort's builder portal accounts (invites included) and the
 * "Invite builders" dialog, which takes pasted emails or `name,email` CSV and previews what
 * will happen to each row before anything is sent (docs/launchpad-cohorts.md).
 *
 * Invite links are bearer credentials that the server never shows twice, so a link is only
 * copyable right after the invite or a resend created it (kept in `links` for this page view).
 */
import { useMemo, useState } from "preact/hooks";
import type { BuildspaceData, LaunchpadAccount } from "../../../lib/contracts";
import { api } from "../../../lib/api/client";
import type { ResponseOf } from "../../../lib/api/client";
import { Drawer } from "../../components/Drawer";
import { Empty, errorMessage, Notice } from "../../components/states";
import { PeopleChip } from "../../components/PeopleTable";
import { RowMenu, type MenuItem } from "../../components/people";
import {
  ACCOUNT_HUE,
  accountLabel,
  copyText,
  INVITE_STATE_LABEL,
  msDate,
  parseInvites,
  sendable,
  SKIP_REASON,
} from "./launchpad";

export type InviteLinks = Record<string, string>;
type Method = "email" | "link" | "both";

export function InvitesPanel({
  b,
  canDirector,
  readOnly,
  links,
  addLinks,
  reload,
  setNotice,
}: {
  b: BuildspaceData;
  canDirector: boolean;
  readOnly: boolean;
  links: InviteLinks;
  addLinks: (l: InviteLinks) => void;
  reload: () => void;
  setNotice: (s: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [error, setError] = useState("");
  const [q, setQ] = useState("");
  const cohort = b.cohorts.find((c) => c.id === b.cohortId);
  const person = (id: string | null) => (id ? b.people.find((p) => p.id === id) : undefined);
  const rows = b.accounts.filter((a) => !q || `${a.email} ${a.name}`.toLowerCase().includes(q.toLowerCase()));
  const act = async (fn: () => Promise<unknown>, message: string) => {
    setError("");
    try {
      await fn();
      reload();
      setNotice(message);
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  const resend = async (a: LaunchpadAccount) => {
    setError("");
    try {
      const res = await api("launchpadAccountResend", { params: { id: a.id } });
      reload();
      if (res.link) {
        addLinks({ [a.id]: res.link.url });
        const ok = await copyText(res.link.url);
        setNotice(`New invite link for ${a.email}${ok ? " copied to your clipboard" : ""}. The old link no longer works.`);
      } else setNotice(`A new invite email is on its way to ${a.email}. The old link no longer works.`);
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  const counts = {
    invited: b.accounts.filter((a) => a.status === "invited").length,
    active: b.accounts.filter((a) => a.status === "active").length,
    onboarded: b.accounts.filter((a) => a.onboardedAt !== null).length,
  };
  return (
    <div class="lp-invites stack" data-invites-panel>
      <div class="bs-result">
        <span role="status">
          {counts.invited} pending · {counts.active} joined · {counts.onboarded} onboarded
        </span>
        <div class="action-row">
          <label class="bs-search">
            <span class="visually-hidden">Search invites</span>
            <input class="input" type="search" placeholder="Search email or name…" value={q} onInput={(e) => setQ(e.currentTarget.value)} />
          </label>
          {!readOnly && cohort && (
            <button type="button" class="btn btn--primary" data-invite-builders onClick={() => setOpen(true)}>
              Invite builders
            </button>
          )}
        </div>
      </div>
      {error && <Notice tone="negative">{error}</Notice>}
      {!b.accounts.length ? (
        <Empty title="No one invited yet" action={!readOnly && cohort ? <button class="btn" onClick={() => setOpen(true)}>Invite builders</button> : undefined}>
          Paste a list of emails and each builder gets a link to sign in with Discord and set up their profile and project.
        </Empty>
      ) : !rows.length ? (
        <Empty title="No matches">Try a different search.</Empty>
      ) : (
        <div class="table-wrap bs-wrap" tabIndex={0} role="region" aria-label="Builder invites">
          <table class="bs-table" data-invites-table>
            <thead>
              <tr>
                {["Builder", "Status", "Invite", "Joined", "Builder record", ""].map((h) => (
                  <th scope="col">{h ? h : <span class="visually-hidden">Actions</span>}</th>
                ))}
              </tr>
            </thead>
            <tbody class="motion-list">
              {rows.map((a) => {
                const p = person(a.personId);
                const items: MenuItem[] = [];
                if (!readOnly && a.status === "invited") items.push({ label: "Resend invite (new link)", onSelect: () => void resend(a) });
                if (links[a.id]) items.push({ label: "Copy invite link", onSelect: () => void copyText(links[a.id]!).then((ok) => setNotice(ok ? "Invite link copied." : links[a.id]!)) });
                if (!readOnly && a.status === "invited")
                  items.push({
                    label: "Revoke invite",
                    danger: true,
                    onSelect: () => {
                      if (!confirm(`Revoke the invite for ${a.email}? Their link stops working.`)) return;
                      void act(() => api("launchpadAccountRevoke", { params: { id: a.id } }), `Invite for ${a.email} revoked.`);
                    },
                  });
                if (canDirector && a.status === "active")
                  items.push({
                    label: "Disable portal access",
                    danger: true,
                    onSelect: () => {
                      if (!confirm(`Disable ${a.name || a.email}'s portal access? They are signed out at once. Their builder record and history stay.`)) return;
                      void act(() => api("launchpadAccountDisable", { params: { id: a.id } }), `${a.email} disabled.`);
                    },
                  });
                if (canDirector && a.status === "disabled")
                  items.push({ label: "Enable portal access", onSelect: () => void act(() => api("launchpadAccountEnable", { params: { id: a.id } }), `${a.email} enabled.`) });
                return (
                  <tr key={a.id} data-account={a.id} data-status={a.status}>
                    <td data-label="Builder">
                      <strong>{a.name || p?.name || a.email}</strong>
                      <span class="bs-sub">{a.email}</span>
                    </td>
                    <td data-label="Status">
                      <PeopleChip hue={a.status === "invited" && a.inviteExpired ? "coral" : ACCOUNT_HUE[a.status]}>{accountLabel(a)}</PeopleChip>
                    </td>
                    <td data-label="Invite">
                      {a.inviteError ? (
                        <span class="bs-alert" title={a.inviteError}>
                          Email failed
                          <span class="bs-sub">{a.inviteError}</span>
                        </span>
                      ) : a.inviteSentAt ? (
                        <span>Emailed {msDate(a.inviteSentAt)}</span>
                      ) : a.status === "invited" ? (
                        <span class="bs-muted">Link only (not emailed)</span>
                      ) : (
                        <span class="bs-muted">—</span>
                      )}
                      {a.status === "invited" && a.inviteExpiresAt && (
                        <span class="bs-sub">{a.inviteExpired ? "Expired" : "Expires"} {msDate(a.inviteExpiresAt)}</span>
                      )}
                      {links[a.id] && (
                        <button type="button" class="linkish lp-copy" data-copy-link onClick={() => void copyText(links[a.id]!).then((ok) => setNotice(ok ? "Invite link copied." : links[a.id]!))}>
                          Copy link
                        </button>
                      )}
                    </td>
                    <td data-label="Joined">
                      {a.acceptedAt ? (
                        <>
                          {msDate(a.acceptedAt)}
                          {a.lastLoginAt && <span class="bs-sub">Last seen {msDate(a.lastLoginAt)}</span>}
                        </>
                      ) : (
                        <span class="bs-muted">Not yet</span>
                      )}
                    </td>
                    <td data-label="Builder record">
                      {p ? <span>{p.name}{p.designation && <span class="bs-sub">{p.designation}</span>}</span> : a.status === "active" ? <span class="bs-muted">Setting up</span> : <span class="bs-muted">—</span>}
                    </td>
                    <td data-label="Actions">{items.length > 0 && <RowMenu label={`Actions for ${a.email}`} items={items} />}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      <Drawer open={open} onClose={() => setOpen(false)} id="lp-invite-dialog" kind="dialog" size="lg" title={`Invite builders to ${cohort?.name ?? "this cohort"}`} eyebrow="Buildspace + Launchpad">
        {open && cohort && (
          <InviteForm
            accounts={b.accounts}
            cohortId={cohort.id}
            done={(res) => {
              const l: InviteLinks = {};
              for (const x of res.links) l[x.accountId] = x.url;
              addLinks(l);
              reload();
            }}
          />
        )}
      </Drawer>
    </div>
  );
}

let lastEmailConfigured: boolean | null = null;

function InviteForm({ accounts, cohortId, done }: { accounts: LaunchpadAccount[]; cohortId: string; done: (r: ResponseOf<"launchpadInvite">) => void }) {
  const [text, setText] = useState("");
  const [method, setMethod] = useState<Method>("email");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState<ResponseOf<"launchpadInvite"> | null>(null);
  const rows = useMemo(() => parseInvites(text, accounts), [text, accounts]);
  const send = sendable(rows);
  if (result) return <InviteResult result={result} />;
  return (
    <form
      class="bs-form"
      data-invite-form
      onSubmit={async (e) => {
        e.preventDefault();
        if (!send.length) return;
        setBusy(true);
        setError("");
        try {
          const res = await api("launchpadInvite", {
            params: { id: cohortId },
            body: { method, invites: send.slice(0, 200).map((r) => (r.name ? { email: r.email, name: r.name } : { email: r.email })) },
          });
          lastEmailConfigured = res.emailConfigured;
          setResult(res);
          done(res);
        } catch (err) {
          setError(errorMessage(err));
        } finally {
          setBusy(false);
        }
      }}
    >
      <label>
        Emails · one per line, or “name, email” rows pasted from a sheet
        <textarea
          class="textarea"
          rows={7}
          data-invite-input
          value={text}
          onInput={(e) => setText(e.currentTarget.value)}
          placeholder={"ada@uw.example\nGrace Hopper, grace@uw.example"}
        />
      </label>
      {rows.length > 0 && (
        <div class="lp-invite-preview" data-invite-preview>
          <p class="small" role="status">
            {send.length} to send
            {rows.length - send.length > 0 && ` · ${rows.length - send.length} skipped`}
            {send.length > 200 && " · only the first 200 are sent at once"}
          </p>
          <ul class="lp-invite-rows" role="list">
            {rows.map((r, i) => (
              <li key={`${r.line}-${i}`} data-state={r.state} class={r.state === "valid" || r.state === "pending" ? "" : "is-skipped"}>
                <span class="lp-invite-email">{r.state === "invalid" ? r.raw : r.email}</span>
                {r.name && <span class="bs-muted">{r.name}</span>}
                <span class={`lp-invite-state${r.state === "invalid" || r.state === "duplicate" ? " bs-alert" : ""}`}>{INVITE_STATE_LABEL[r.state]}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
      <fieldset>
        <legend>How to send</legend>
        {(
          [
            ["email", "Email each builder their link"],
            ["link", "Just give me the links to share"],
            ["both", "Email them and give me the links"],
          ] as const
        ).map(([v, l]) => (
          <label class="bs-check" key={v}>
            <input type="radio" name="method" value={v} checked={method === v} onChange={() => setMethod(v)} />
            {l}
          </label>
        ))}
        {method !== "link" && lastEmailConfigured !== true && (
          <p class="field-hint">
            {lastEmailConfigured === false
              ? "Email isn't configured on this server (SMTP), so the links will be shown here to share yourself."
              : "If email isn't configured on the server, the links are shown here instead so you can share them."}
          </p>
        )}
        <p class="field-hint">Links work once, for 14 days, and sign the builder in with their own Discord account.</p>
      </fieldset>
      {error && <Notice tone="negative">{error}</Notice>}
      <button class="btn btn--primary" disabled={busy || !send.length} data-invite-send>
        {busy ? "Sending…" : send.length ? `Invite ${send.length} ${send.length === 1 ? "builder" : "builders"}` : "Invite builders"}
      </button>
    </form>
  );
}

function InviteResult({ result }: { result: ResponseOf<"launchpadInvite"> }) {
  const [copied, setCopied] = useState("");
  const all = result.links.map((l) => `${l.email}\t${l.url}`).join("\n");
  return (
    <div class="stack" data-invite-result>
      <Notice tone="positive">
        {result.accounts.length} {result.accounts.length === 1 ? "invite" : "invites"} ready.
        {result.emailConfigured ? " Emails are on their way; delivery shows in the Invites table." : " Email isn't configured, so share the links below."}
      </Notice>
      {result.links.length > 0 && (
        <section class="stack stack--tight">
          <div class="bs-result">
            <h3 class="flush">Invite links</h3>
            <button type="button" class="btn btn--small" onClick={() => void copyText(all).then((ok) => setCopied(ok ? "all" : ""))}>
              {copied === "all" ? "Copied" : "Copy all"}
            </button>
          </div>
          <p class="field-hint">Each link works once. They aren't shown again; use Resend for a new one.</p>
          <ul class="lp-invite-rows" role="list" data-invite-links>
            {result.links.map((l) => (
              <li key={l.accountId}>
                <span class="lp-invite-email">{l.email}</span>
                <code class="lp-link">{l.url}</code>
                <button type="button" class="btn btn--small" onClick={() => void copyText(l.url).then((ok) => setCopied(ok ? l.accountId : ""))}>
                  {copied === l.accountId ? "Copied" : "Copy"}
                </button>
              </li>
            ))}
          </ul>
        </section>
      )}
      {result.skipped.length > 0 && (
        <section class="stack stack--tight">
          <h3 class="flush">Skipped</h3>
          <ul class="lp-invite-rows" role="list">
            {result.skipped.map((s) => (
              <li key={s.email} class="is-skipped">
                <span class="lp-invite-email">{s.email}</span>
                <span class="bs-muted">{SKIP_REASON[s.reason]}</span>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}
