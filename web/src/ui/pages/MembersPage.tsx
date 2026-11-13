import { fmtDateTime } from '../../lib/format';
import { useEffect, useRef, useState } from "preact/hooks";
import { Gate } from "../shell/Gate";
import { ActivityList, PersonDrawer, PersonFacts, PersonSection, usePersonParam } from "../components/PersonDrawer";
import { PeopleCard, PeopleChip, PeopleGroup, PeopleSearch, PersonCell, PersonRow, type Column } from "../components/PeopleTable";
import { api, endpointUrl, fieldErrors } from "../../lib/api/client";
import type { BodyOf, ResponseOf } from "../../lib/api/client";
import type { CurrentUser, Member, OfficerActivityResponse } from "../../lib/contracts";
import { ErrorState, Loading } from "../components/states";
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
  n ? fmtDateTime(new Date(n).toISOString()) : "never";
export default function MembersPage() {
  return <Gate label="members">{(me) => <Members me={me} />}</Gate>;
}
const fullName = (m: Pick<Member, "first_name" | "last_name">) => `${m.first_name} ${m.last_name}`;
const MEMBER_COLUMNS: Column[] = [
  { label: "Name", width: "30%" },
  { label: "UW email", width: "22%" },
  { label: "Major", width: "14%" },
  { label: "Grad year", width: "9%" },
  { label: "Campus", width: "10%" },
  { label: "Joined", width: "9%" },
  { label: "Officer", width: "6%" },
];
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
  const person = usePersonParam("id");
  const detailId = person.personId;
  const [member, setMember] = useState<Member | null>(null),
    [detailError, setDetailError] = useState<unknown>(null);
  const limit = 25;
  // An officer with no membership record opens here too, as a read-only officer summary.
  const openOfficer = detailId
    ? (data?.officerRows.find((o) => o.id === detailId) ??
      data?.officers.find((o) => o.id === detailId && !o.memberId))
    : undefined;
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
    setMember(null);
    setDetailError(null);
    // Wait for the list so an officer id is not looked up as a member.
    if (!detailId || !data || openOfficer) return;
    const controller = new AbortController();
    api("memberDetail", {
      params: { id: detailId },
      signal: controller.signal,
    }).then(
      (d) => setMember(d.member),
      (e) => {
        if (e.name !== "AbortError") setDetailError(e);
      },
    );
    return () => controller.abort();
  }, [detailId, !!data, openOfficer?.id]);
  // Officers are club members: current officers are listed too (display only). One that shares an
  // email with a member row badges that row instead of adding a second line.
  const officers = data?.officers ?? [];
  const officerOf = new Map(officers.flatMap((o) => (o.memberId ? [[o.memberId, o] as const] : [])));
  // Officer-only rows are part of the paged directory: the server counts and pages them with the members.
  const extras = data?.officerRows ?? [];
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
  // Connection state can insert a notice and change the status width. Commit the
  // initial directory with that state known, rather than moving a painted table.
  if (!data && loading) return <Loading rows={6} label="Loading members" />;
  const officerOfOpen = member ? officerOf.get(member.person_id) : undefined;
  return (
    <div class="members-page">
      <header class="page-header">
        <h1>Members</h1>
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
      {status && (
        <p role="status" class="notice members-notice">
          {status}
        </p>
      )}
      <PeopleCard label="Member directory" busy={loading} data="members">
        <div class="people-status members-sync">
          <strong>
            {!data
              ? "Loading connection…"
              : data.connected
                ? "D1 membership mirror"
                : "Not connected"}
          </strong>
          <span>Synced daily, last {stamp(data?.lastSync ?? null)}</span>
          {data && !data.connected && (
            <span>A director can add the server credentials to begin daily sync.</span>
          )}
          {data?.syncError && (
            <span role="status" class="warn">The latest sync failed. Showing the last successful mirror.</span>
          )}
        </div>
        <form class="people-tools members-filters" onSubmit={(e) => e.preventDefault()}>
          <PeopleSearch label="Search members" value={search} placeholder="Name, preferred name or email" onInput={setSearch} />
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
          <p role="alert" class="notice members-notice">
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
              <PeopleGroup
                section="members"
                title={`${data.total.toLocaleString()} members`}
                caption="CSEED membership directory"
                collapsible={false}
                columns={MEMBER_COLUMNS}
                empty={
                  data.items.length === 0 && extras.length === 0 ? (
                    <div class="people-empty">
                      <p class="state-title">
                        {data.connected
                          ? "No members found"
                          : "Waiting for Cloudflare"}
                      </p>
                      <p class="muted">
                        {data.connected
                          ? "Try a different search or clear your filters."
                          : "Your directory will appear after the first sync."}
                      </p>
                    </div>
                  ) : undefined
                }
              >
                {extras.map((o) => (
                  <PersonRow key={o.id} onOpen={() => person.open(o.id)} data={{ "data-officer-row": o.id, "data-person-row": o.id }}>
                    <td>
                      <PersonCell id={o.id} name={o.name} sub={[o.title, o.subteam].filter(Boolean).join(" · ") || "Officer"} onOpen={() => person.open(o.id)} />
                    </td>
                    <td>{o.email || "—"}</td>
                    <td>—</td>
                    <td>—</td>
                    <td>—</td>
                    <td class="nowrap">—</td>
                    <td data-cell="chip">
                      <PeopleChip hue="blue" data={{ "data-officer-badge": "" }}>Officer</PeopleChip>
                    </td>
                  </PersonRow>
                ))}
                {data.items.map((p) => (
                  <PersonRow key={p.person_id} onOpen={() => person.open(p.person_id)} data={{ "data-person-row": p.person_id }}>
                    <td>
                      <PersonCell
                        id={p.person_id}
                        name={fullName(p)}
                        sub={p.preferred_name && p.preferred_name !== p.first_name ? `Goes by ${p.preferred_name}` : p.pronouns || undefined}
                        onOpen={() => person.open(p.person_id)}
                      />
                    </td>
                    <td>{p.uw_email || "—"}</td>
                    <td>{p.major || "—"}</td>
                    <td>{p.grad_year || "—"}</td>
                    <td>{p.campus || "—"}</td>
                    <td class="nowrap">{p.join_date.slice(0, 10)}</td>
                    <td data-cell="chip">
                      {officerOf.has(p.person_id) ? (
                        <PeopleChip hue="blue" data={{ "data-officer-badge": "" }}>Officer</PeopleChip>
                      ) : p.has_been_cseed_officer ? (
                        <PeopleChip ghost>Former</PeopleChip>
                      ) : (
                        <span class="muted">—</span>
                      )}
                    </td>
                  </PersonRow>
                ))}
              </PeopleGroup>
              <nav class="people-foot members-pagination" aria-label="Member pagination">
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
      </PeopleCard>
      {openOfficer ? (
        <OfficerSummaryDrawer officer={openOfficer} onClose={person.close} />
      ) : (
      <PersonDrawer
        open={detailId !== null}
        onClose={person.close}
        id="member-detail"
        personKey={detailId ?? ""}
        name={member ? fullName(member) : "Member"}
        eyebrow="Member"
        subtitle={member ? [member.preferred_name && member.preferred_name !== member.first_name ? `Goes by ${member.preferred_name}` : null, member.pronouns, member.major].filter(Boolean).join(" · ") || undefined : undefined}
        chips={
          member && (officerOfOpen ? (
            <PeopleChip hue="blue">Officer</PeopleChip>
          ) : member.has_been_cseed_officer ? (
            <PeopleChip>Former officer</PeopleChip>
          ) : (
            <PeopleChip hue="green">Member</PeopleChip>
          ))
        }
        actions={
          member
            ? [
                ...(member.uw_email || member.personal_email
                  ? [{ label: "Email", icon: "mail" as const, href: `mailto:${member.uw_email || member.personal_email}`, primary: true }]
                  : []),
                ...(officerOfOpen ? [{ label: "Officer profile", icon: "user" as const, href: `/officers?person=${encodeURIComponent(officerOfOpen.id)}` }] : []),
              ]
            : []
        }
        stats={
          member
            ? [
                { label: "Joined", value: member.join_date.slice(0, 10) },
                { label: "Graduation year", value: member.grad_year || "—" },
                { label: "Campus", value: member.campus || "—" },
              ]
            : []
        }
        status={detailError ? <div class="person-body"><ErrorState error={detailError} /></div> : !member ? <div class="person-body"><Loading rows={6} /></div> : undefined}
        tabs={
          member
            ? [
                {
                  key: "overview",
                  label: "Overview",
                  render: () => (
                    <>
                      <PersonSection title="Membership record">
                        <PersonFacts
                          facts={[
                            ["Person ID", <span class="mono">{member.person_id}</span>],
                            ["Join date", member.join_date.slice(0, 10)],
                            ["Has been officer", member.has_been_cseed_officer ? "Yes" : "No"],
                          ]}
                        />
                      </PersonSection>
                      <PersonSection title="Contact and profile">
                        <PersonFacts facts={fields.map(([key, label]) => [label, member[key] || "Not provided", key])} />
                      </PersonSection>
                      {!director && <p class="muted small">Directors and leadership can edit member records.</p>}
                    </>
                  ),
                },
                ...(director
                  ? [
                      {
                        key: "manage",
                        label: "Manage",
                        render: () => (
                          <MemberEditor
                            key={member.person_id}
                            member={member}
                            actorName={me.name}
                            connected={data?.connected ?? false}
                            onSaved={(p) => {
                              setMember(p);
                              setTick((t) => t + 1);
                              setStatus("Member saved to Cloudflare D1.");
                            }}
                          />
                        ),
                      },
                    ]
                  : []),
              ]
            : []
        }
      />
      )}
    </div>
  );
}
/** An officer without a membership record, opened from Members: read-only; edits stay on Board Members. */
function OfficerSummaryDrawer({ officer: o, onClose }: { officer: ResponseOf<"membersList">["officers"][number]; onClose: () => void }) {
  const [activity, setActivity] = useState<OfficerActivityResponse | null>(null);
  useEffect(() => {
    setActivity(null);
    const controller = new AbortController();
    api("officerActivity", { params: { id: o.id }, signal: controller.signal }).then(
      (r) => !controller.signal.aborted && setActivity(r),
      () => {},
    );
    return () => controller.abort();
  }, [o.id]);
  const num = (n: number | undefined) => (n === undefined ? "…" : String(n));
  const profile = `/officers?person=${encodeURIComponent(o.id)}`;
  return (
    <PersonDrawer
      open
      onClose={onClose}
      id="member-detail"
      personKey={o.id}
      name={o.name}
      eyebrow="Officer"
      subtitle={[o.title, o.subteam].filter(Boolean).join(" · ") || undefined}
      chips={<PeopleChip hue="blue">Officer</PeopleChip>}
      actions={[
        ...(o.email ? [{ label: "Email", icon: "mail" as const, href: `mailto:${o.email}`, primary: true }] : []),
        { label: "Officer profile", icon: "user" as const, href: profile },
      ]}
      stats={[
        { label: "Events hosted", value: num(activity?.stats.eventsHosted) },
        { label: "Open design requests", value: num(activity?.stats.openDesign) },
        { label: "Open purchases", value: num(activity?.stats.openPurchases) },
      ]}
      tabs={[
        {
          key: "overview",
          label: "Overview",
          render: () => (
            <>
              <PersonSection title="Officer">
                <PersonFacts
                  facts={[
                    ["Title", o.title || "Not set"],
                    ["Subteam", o.subteam || "Not set"],
                    ["Email", o.email || "Not provided"],
                  ]}
                />
              </PersonSection>
              <p class="muted small" data-no-member-record>
                No membership record matches this officer’s email yet. Edit officer details on Board Members.
              </p>
            </>
          ),
        },
        {
          key: "activity",
          label: "Activity",
          render: () => (activity ? <ActivityList items={activity.items} /> : <Loading rows={3} />),
        },
      ]}
    />
  );
}
function MemberEditor({
  member,
  connected,
  actorName,
  onSaved,
}: {
  member: Member;
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
