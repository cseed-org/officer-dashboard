import { useEffect, useRef, useState } from "preact/hooks";
import { Gate } from "../shell/Gate";
import { Drawer } from "../components/Drawer";
import { api, endpointUrl, fieldErrors } from "../../lib/api/client";
import type { BodyOf, ResponseOf } from "../../lib/api/client";
import type { CurrentUser, Member } from "../../lib/contracts";
import { Loading } from "../components/states";
import "../../styles/members.css";

type Filters = {
  q: string;
  campus: string;
  gradYear: string;
  officer: "" | "yes" | "no";
};
const EMPTY: Filters = { q: "", campus: "", gradYear: "", officer: "" };
const fields = [
  ["first_name", "First name"],
  ["last_name", "Last name"],
  ["preferred_name", "Preferred name"],
  ["uw_email", "UW email"],
  ["cs_email", "CS email"],
  ["personal_email", "Personal email"],
  ["major", "Major"],
  ["grad_year", "Graduation year"],
  ["campus", "Campus"],
  ["pronouns", "Pronouns"],
] as const;
const stamp = (n: number | null) =>
  n ? new Date(n).toLocaleString() : "never";
export default function MembersPage() {
  return <Gate label="members">{(me) => <Members me={me} />}</Gate>;
}
function Members({ me }: { me: CurrentUser }) {
  const director = me.role !== "officer";
  const [filters, setFilters] = useState<Filters>(EMPTY),
    [search, setSearch] = useState(""),
    [offset, setOffset] = useState(0),
    [tick, setTick] = useState(0);
  const [data, setData] = useState<ResponseOf<"membersList"> | null>(null),
    [loading, setLoading] = useState(true),
    [error, setError] = useState(""),
    [status, setStatus] = useState(""),
    [syncing, setSyncing] = useState(false);
  const [member, setMember] = useState<Member | null>(null),
    [detailLoading, setDetailLoading] = useState(false),
    [detailId, setDetailId] = useState<string | null>(null);
  const limit = 25;
  useEffect(() => {
    if (filters.q === search) return;
    const timer = setTimeout(() => {
      setFilters((f) => (f.q === search ? f : { ...f, q: search }));
      setOffset(0);
    }, 250);
    return () => clearTimeout(timer);
  }, [search]);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError("");
    api("membersList", {
      query: {
        ...filters,
        officer: filters.officer || undefined,
        limit,
        offset,
      },
      signal: controller.signal,
    }).then(
      (d) => {
        setData(d);
        setLoading(false);
      },
      (e) => {
        if (e.name !== "AbortError") {
          setError(e.message);
          setLoading(false);
        }
      },
    );
    return () => controller.abort();
  }, [filters, offset, tick]);
  useEffect(() => {
    if (!detailId) return;
    const controller = new AbortController();
    setDetailLoading(true);
    api("memberDetail", {
      params: { id: detailId },
      signal: controller.signal,
    }).then(
      (d) => {
        setMember(d.member);
        setDetailLoading(false);
      },
      (e) => {
        if (e.name !== "AbortError") {
          setError(e.message);
          setDetailId(null);
          setDetailLoading(false);
        }
      },
    );
    return () => controller.abort();
  }, [detailId]);
  const change = (key: keyof Filters, value: string) => {
    setFilters({ ...filters, [key]: value });
    setOffset(0);
  };
  async function sync() {
    setSyncing(true);
    setStatus("");
    try {
      const d = await api("cloudflareSync");
      setStatus(
        d.connected ? "Sync complete." : "Server credentials are not set.",
      );
      setTick((t) => t + 1);
    } catch (e) {
      setError((e as Error).message);
      setTick((t) => t + 1);
    } finally {
      setSyncing(false);
    }
  }
  return (
    <div class="members-page">
      <header class="page-header">
        <div>
          <h1>Members</h1>
          <p class="page-sub">A growing community, one signup at a time.</p>
        </div>
        <div class="action-row">
          {director && (
            <>
              <a
                class="btn"
                href={endpointUrl("membersExport", {
                  query: { ...filters, officer: filters.officer || undefined },
                })}
              >
                Export CSV
              </a>
              <button class="btn" disabled={syncing} onClick={sync}>
                {syncing ? "Syncing…" : "Sync now"}
              </button>
            </>
          )}
        </div>
      </header>
      <div class="members-sync notice notice--info">
        <strong>
          {!data
            ? "Loading connection…"
            : data.connected
              ? "D1 membership mirror"
              : "Not connected"}
        </strong>
        <span>Synced daily, last {stamp(data?.lastSync ?? null)}</span>
        <span class="muted">Source: cseed-membership · People</span>
      </div>
      {data && !data.connected && (
        <p class="notice">
          Cloudflare is not connected. A director can add the server credentials
          to begin daily sync.
        </p>
      )}
      {data?.syncError && (
        <p class="notice" role="status">
          The latest sync failed. Showing the last successful mirror.
        </p>
      )}
      {status && (
        <p role="status" class="notice">
          {status}
        </p>
      )}
      <section class="block" aria-label="Member directory" aria-busy={loading}>
        <form class="members-filters" onSubmit={(e) => e.preventDefault()}>
          <label class="field members-search">
            <span class="field-label">Search members</span>
            <input
              class="input"
              type="search"
              placeholder="Name, preferred name or email"
              value={search}
              onInput={(e) => setSearch(e.currentTarget.value)}
            />
          </label>
          <label class="field">
            <span class="field-label">Campus</span>
            <select
              class="select"
              value={filters.campus}
              onChange={(e) => change("campus", e.currentTarget.value)}
            >
              <option value="">All campuses</option>
              {data?.facets.campus.map((v) => (
                <option key={v}>{v}</option>
              ))}
            </select>
          </label>
          <label class="field">
            <span class="field-label">Graduation year</span>
            <select
              class="select"
              value={filters.gradYear}
              onChange={(e) => change("gradYear", e.currentTarget.value)}
            >
              <option value="">All years</option>
              {data?.facets.grad_year.map((v) => (
                <option key={v}>{v}</option>
              ))}
            </select>
          </label>
          <label class="field">
            <span class="field-label">Has been officer</span>
            <select
              class="select"
              value={filters.officer}
              onChange={(e) => change("officer", e.currentTarget.value)}
            >
              <option value="">Everyone</option>
              <option value="yes">Yes</option>
              <option value="no">No</option>
            </select>
          </label>
          <button
            class="btn"
            type="button"
            onClick={() => {
              setFilters(EMPTY);
              setSearch("");
              setOffset(0);
            }}
          >
            Clear filters
          </button>
        </form>
        {error && (
          <p role="alert" class="notice">
            {error}{" "}
            <button class="btn" onClick={() => setTick((t) => t + 1)}>
              Retry
            </button>
          </p>
        )}
        {loading && !data ? (
          <Loading rows={8} label="Loading members" />
        ) : (
          data && (
            <>
              <div class="block-head">
                <h2>{data.total.toLocaleString()} members</h2>
                <span class="muted">
                  Read-only mirror · open a name for details
                </span>
              </div>
              {data.items.length === 0 ? (
                <div class="state">
                  <h3>
                    {data.connected
                      ? "No members found"
                      : "Waiting for Cloudflare"}
                  </h3>
                  <p>
                    {data.connected
                      ? "Try a different search or clear your filters."
                      : "Your directory will appear after the first sync."}
                  </p>
                </div>
              ) : (
                <div class="table-wrap members-table">
                  <table class="dt">
                    <caption class="visually-hidden">
                      CSEED membership directory
                    </caption>
                    <thead>
                      <tr>
                        {[
                          "Name",
                          "UW email",
                          "Major",
                          "Grad year",
                          "Campus",
                          "Joined",
                          "Has been officer",
                        ].map((h) => (
                          <th scope="col" key={h}>
                            {h}
                          </th>
                        ))}
                      </tr>
                    </thead>
                    <tbody>
                      {data.items.map((p) => (
                        <tr key={p.person_id}>
                          <td class="dt-primary" data-label="Name">
                            <button
                              class="member-name"
                              onClick={() => {
                                setMember(null);
                                setDetailId(p.person_id);
                              }}
                            >
                              {p.first_name} {p.last_name}
                            </button>
                            {p.preferred_name &&
                              p.preferred_name !== p.first_name && (
                                <span class="member-preferred">
                                  Goes by {p.preferred_name}
                                </span>
                              )}
                          </td>
                          <td data-label="UW email">{p.uw_email || "—"}</td>
                          <td data-label="Major">{p.major || "—"}</td>
                          <td data-label="Grad year">{p.grad_year || "—"}</td>
                          <td data-label="Campus">{p.campus || "—"}</td>
                          <td class="nowrap" data-label="Joined">
                            {p.join_date.slice(0, 10)}
                          </td>
                          <td data-label="Has been officer">
                            {p.has_been_cseed_officer ? "Yes" : "No"}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
              <nav class="members-pagination" aria-label="Member pagination">
                <span role="status">
                  {data.total === 0
                    ? "0 members"
                    : `${data.offset + 1}–${Math.min(data.offset + data.limit, data.total)} of ${data.total.toLocaleString()}`}
                </span>
                <div class="action-row">
                  <button
                    class="btn"
                    disabled={offset === 0}
                    aria-disabled={loading ? "true" : undefined}
                    onClick={() => {
                      if (!loading) setOffset(Math.max(0, offset - limit));
                    }}
                  >
                    Previous
                  </button>
                  <button
                    class="btn"
                    disabled={offset + limit >= data.total}
                    aria-disabled={loading ? "true" : undefined}
                    onClick={() => {
                      if (!loading) setOffset(offset + limit);
                    }}
                  >
                    Next
                  </button>
                </div>
              </nav>
            </>
          )
        )}
      </section>
      <Drawer
        open={detailId !== null}
        onClose={() => {
          setDetailId(null);
          setMember(null);
        }}
        title={
          member ? `${member.first_name} ${member.last_name}` : "Member details"
        }
        eyebrow="Membership record"
        id="member-detail"
      >
        {detailLoading || !member ? (
          <Loading rows={6} />
        ) : (
          <MemberEditor
            key={member.person_id}
            member={member}
            director={director}
            actorName={me.name}
            connected={data?.connected ?? false}
            onSaved={(p) => {
              setMember(p);
              setTick((t) => t + 1);
              setStatus("Member saved to Cloudflare D1.");
            }}
          />
        )}
      </Drawer>
    </div>
  );
}
function MemberEditor({
  member,
  director,
  connected,
  actorName,
  onSaved,
}: {
  member: Member;
  director: boolean;
  connected: boolean;
  actorName: string;
  onSaved: (p: Member) => void;
}) {
  const [draft, setDraft] = useState(member),
    [note, setNote] = useState(""),
    [review, setReview] = useState(false),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [issues, setIssues] = useState<Record<string, string>>({});
  const changed = fields.filter(
    ([key]) => (draft[key] || null) !== (member[key] || null),
  );
  async function save() {
    setBusy(true);
    setError("");
    setIssues({});
    const body: BodyOf<"memberUpdate"> = { auditNote: note };
    for (const [key] of changed) {
      if (key === "first_name" || key === "last_name") body[key] = draft[key];
      else body[key] = draft[key] || null;
    }
    try {
      const d = await api("memberUpdate", {
        params: { id: member.person_id },
        body,
      });
      onSaved(d.member);
      setDraft(d.member);
      setNote("");
      setReview(false);
    } catch (e) {
      setError((e as Error).message);
      const errors = fieldErrors(e);
      setIssues(errors);
      if (Object.keys(errors).length) setReview(false);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div class="stack">
      <dl class="facts">
        <div>
          <dt>Person ID</dt>
          <dd>{member.person_id}</dd>
        </div>
        <div>
          <dt>Join date</dt>
          <dd>{member.join_date.slice(0, 10)} · Read-only</dd>
        </div>
        <div>
          <dt>Has been officer</dt>
          <dd>{member.has_been_cseed_officer ? "Yes" : "No"} · Read-only</dd>
        </div>
      </dl>
      {!director ? (
        <>
          <p class="notice">
            Read-only. Directors and leadership can edit member records.
          </p>
          <dl class="facts">
            {fields.map(([key, label]) => (
              <div key={key}>
                <dt>{label}</dt>
                <dd>{member[key] || "Not provided"}</dd>
              </div>
            ))}
          </dl>
        </>
      ) : (
        <form
          class="stack"
          onSubmit={(e) => {
            e.preventDefault();
            setReview(true);
          }}
        >
          <h3>Edit website record</h3>
          <p class="notice">
            Saving writes to Cloudflare D1, the source for the public membership
            site.
          </p>
          <fieldset
            disabled={busy || !connected || review}
            class="members-edit-fields"
          >
            {fields.map(([key, label]) => (
              <label class="field" key={key}>
                <span class="field-label">{label}</span>
                <input
                  class="input"
                  type="text"
                  inputMode={key.endsWith("email") ? "email" : "text"}
                  maxLength={254}
                  required={key === "first_name" || key === "last_name"}
                  value={draft[key] ?? ""}
                  onInput={(e) =>
                    setDraft({ ...draft, [key]: e.currentTarget.value })
                  }
                  aria-invalid={issues[key] ? "true" : "false"}
                  aria-describedby={
                    issues[key] ? "member-" + key + "-error" : undefined
                  }
                />
                {issues[key] && (
                  <span id={"member-" + key + "-error"} role="alert">
                    {issues[key]}
                  </span>
                )}
              </label>
            ))}
            <label class="field members-audit">
              <span class="field-label">Audit note</span>
              <textarea
                class="textarea"
                required
                maxLength={2000}
                value={note}
                onInput={(e) => setNote(e.currentTarget.value)}
                placeholder="Why is this record changing?"
              />
              <span class="field-hint">
                Required. Saved with your name and the changed fields.
              </span>
            </label>
          </fieldset>
          {!connected && (
            <p class="notice">
              Not connected. Editing is available after Cloudflare is
              configured.
            </p>
          )}
          {error && !review && (
            <p role="alert" class="notice">
              {error}
            </p>
          )}
          {review ? (
            <ConfirmDialog busy={busy} onClose={() => setReview(false)}>
              <div class="stack">
                <h3 tabIndex={-1}>Confirm D1 update</h3>
                <p>
                  {member.first_name} {member.last_name} · {member.person_id}
                </p>
                <div class="table-wrap">
                  <table class="dt">
                    <caption class="visually-hidden">
                      Fields that will change
                    </caption>
                    <thead>
                      <tr>
                        <th scope="col">Field</th>
                        <th scope="col">Current</th>
                        <th scope="col">New</th>
                      </tr>
                    </thead>
                    <tbody>
                      {changed.map(([key, label]) => (
                        <tr key={key}>
                          <td class="dt-primary" data-label="Field">
                            {label}
                          </td>
                          <td data-label="Current">
                            {member[key] || "Not provided"}
                          </td>
                          <td data-label="New">
                            {draft[key] || "Not provided"}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
                <p class="field-hint">Changing this record as {actorName}.</p>
                {error && (
                  <p class="notice" role="alert">
                    {error}
                  </p>
                )}
                <p>Audit note: {note}</p>
                <div class="action-row">
                  <button
                    class="btn btn--primary"
                    type="button"
                    disabled={busy}
                    onClick={save}
                  >
                    {busy ? "Saving…" : "Confirm and write to D1"}
                  </button>
                  <button
                    class="btn"
                    type="button"
                    disabled={busy}
                    onClick={() => setReview(false)}
                  >
                    Back to edit
                  </button>
                </div>
              </div>
            </ConfirmDialog>
          ) : (
            <button
              class="btn btn--primary"
              type="submit"
              disabled={!connected || !changed.length || !note.trim()}
            >
              Review changes
            </button>
          )}
        </form>
      )}
    </div>
  );
}

// This modal stacks above the detail drawer; the drawer continues to own scroll lock.
function ConfirmDialog({
  children,
  onClose,
  busy,
}: {
  busy: boolean;
  children: import("preact").ComponentChildren;
  onClose: () => void;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    const opener = document.activeElement as HTMLElement | null;
    d.showModal();
    d.querySelector<HTMLElement>("h3")?.focus();
    return () => {
      d.close();
      if (opener?.isConnected) opener.focus();
    };
  }, []);
  return (
    <dialog
      ref={ref}
      class="dialog"
      aria-label="Confirm D1 update"
      onCancel={(event) => {
        if (busy) event.preventDefault();
        else onClose();
      }}
      onClose={onClose}
    >
      <div class="dialog-inner">
        <div class="dialog-body">{children}</div>
      </div>
    </dialog>
  );
}
