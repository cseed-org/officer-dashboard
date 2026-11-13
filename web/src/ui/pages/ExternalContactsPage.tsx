import { fromLocalInput, toLocalInput } from '../../lib/calendar';
import { effectiveTimezone } from '../../lib/timezone';
import { useEffect, useRef, useState } from "preact/hooks";
import type { Contact, CurrentUser, EventListItem } from "../../lib/contracts";
import { api, fieldErrors, type BodyOf } from "../../lib/api/client";
import { useApi } from "../../lib/api/useApi";
import { usePersonParam, PersonDrawer, PersonFacts, PersonSection, type PersonTab } from "../components/PersonDrawer";
import { ContactLinks, PeopleCard, PeopleChip, PeopleGroup, PeopleSearch, PersonCell, PersonRow, sortBy, type Sort } from "../components/PeopleTable";
import type { Hue } from "../../lib/colors";
import { safeUrl, fmtDate, fmtDateTime } from "../../lib/format";
import { Drawer } from "../components/Drawer";
import { Async, ErrorState, errorMessage } from "../components/states";
import { Gate } from "../shell/Gate";

const label = (s: string) =>
  s === "linkedin" ? "LinkedIn" : s
    .split("_")
    .map((w) => w[0]!.toUpperCase() + w.slice(1))
    .join(" ");
const formError = (error: unknown) => {
  const fields = Object.entries(fieldErrors(error));
  return fields.length ? fields.map(([key, message]) => `${key === 'linkedin' ? 'LinkedIn' : key === 'eventIds' ? 'Linked events' : label(key)}: ${message}`).join(' · ') : errorMessage(error);
};
const blank = {
  hasSpokenForUs: false,
  clubSupport: false,
  name: "",
  org: "",
  role: "",
  type: "speaker" as Contact["type"],
  topics: "",
  email: "",
  linkedin: "",
  notes: "",
  eventIds: [] as string[],
};
type Draft = typeof blank;
const draftOf = (c: Contact): Draft => ({
  hasSpokenForUs: c.hasSpokenForUs,
  clubSupport: c.clubSupport,
  name: c.name,
  org: c.org,
  role: c.role,
  type: c.type,
  topics: c.topics.join(", "),
  email: c.email,
  linkedin: c.linkedin,
  notes: c.notes,
  eventIds: c.events.map((e) => e.id),
});

export default function ExternalContactsPage() {
  return <Gate label="external contacts">{(me) => <ExternalContacts me={me} />}</Gate>;
}
function Support({ c }: { c: Contact }) {
  return c.clubSupport ? <span class="badge badge--positive" data-club-support>Club support</span> : null;
}
function ExternalContacts({ me }: { me: CurrentUser }) {
  const person = usePersonParam("id");
  const openId = person.personId;
  const [sort, setSort] = useState<Sort<"name" | "last"> | null>(null);
  const list = useApi("contactsList");
  const [q, setQ] = useState("");
  const [spoken, setSpoken] = useState("");
  const [type, setType] = useState("");
  const [topic, setTopic] = useState("");
  const [support, setSupport] = useState(false);
  const [adding, setAdding] = useState(false);
  const [draft, setDraft] = useState<Draft>({ ...blank });
  const addDraft = useRef<Draft | null>(null);
  const [contact, setContact] = useState<Contact | null>(null);
  const [detailError, setDetailError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [outreach, setOutreach] = useState(false);
  const [outDraft, setOutDraft] = useState({
    date: "",
    channel: "email" as BodyOf<"contactOutreachCreate">["channel"],
    outcome: "awaiting_reply" as BodyOf<"contactOutreachCreate">["outcome"],
    note: "",
  });
  const [confirmDelete, setConfirmDelete] = useState(false);
  const returnToLog = useRef(false);
  useEffect(() => {
    setError("");
    setConfirmDelete(false);
    setContact(null);
    setDetailError(null);
    if (!openId) return;
    const ctrl = new AbortController();
    api("contactDetail", { params: { id: openId }, signal: ctrl.signal })
      .then((d) => {
        if (!ctrl.signal.aborted) {
          setContact(d.contact);
          setDraft(draftOf(d.contact));
        }
      })
      .catch((e) => {
        if (!ctrl.signal.aborted) setDetailError(e);
      });
    return () => ctrl.abort();
  }, [openId]);
  useEffect(() => {
    if (!outreach && returnToLog.current) {
      returnToLog.current = false;
      requestAnimationFrame(() => document.querySelector<HTMLElement>('#contact [data-person-action="log-outreach"]')?.focus());
    }
  }, [outreach]);
  useEffect(() => {
    if (!adding && openId && contact?.id) requestAnimationFrame(() => document.getElementById('contact-title')?.focus());
  }, [adding, contact?.id]);
  // The Add drawer's close event also fires after a successful create (adding is already
  // false and ?person= points at the new contact), so it acts only while still adding.
  const addingNow = useRef(adding);
  addingNow.current = adding;
  function closeAdd() {
    if (busy || !addingNow.current) return;
    addDraft.current = draft;
    setAdding(false);
    setError("");
  }
  function closeContact() {
    if (busy) return;
    person.close();
    setError("");
  }
  function add() {
    setDraft(addDraft.current ?? { ...blank, eventIds: [] });
    setError("");
    setAdding(true);
  }
  async function save(e: SubmitEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    const body = {
      ...draft,
      topics: draft.topics
        .split(",")
        .map((t) => t.trim())
        .filter(Boolean),
    };
    try {
      const d = adding
        ? await api("contactCreate", { body })
        : await api("contactUpdate", { params: { id: openId! }, body });
      setContact(d.contact);
      setDraft(draftOf(d.contact));
      list.reload();
      setNotice(adding ? "Contact added." : "Contact saved.");
      if (adding) {
        addDraft.current = null;
        setAdding(false);
        person.open(d.contact.id);
      }
    } catch (e) {
      setError(formError(e));
    } finally {
      setBusy(false);
    }
  }
  async function remove() {
    if (!confirmDelete) {
      setConfirmDelete(true);
      return;
    }
    setBusy(true);
    setError("");
    try {
      await api("contactDelete", { params: { id: openId! } });
      list.reload();
      setNotice("Contact deleted.");
      person.close();
    } catch (e) {
      setError(formError(e));
    } finally {
      setBusy(false);
    }
  }
  async function log(e: SubmitEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const date = fromLocalInput(outDraft.date);
      if (!date) throw new Error('Choose a valid time in the selected time zone (outside the daylight-saving gap).');
      const d = await api("contactOutreachCreate", {
        params: { id: openId! },
        body: { ...outDraft, date },
      });
      setContact(d.contact);
      list.reload();
      setNotice("Outreach logged.");
      setOutreach(false);
    } catch (e) {
      setError(formError(e));
    } finally {
      setBusy(false);
    }
  }
  const field = (
    key: Exclude<keyof Draft, "type" | "eventIds" | "hasSpokenForUs" | "clubSupport">,
    title: string,
    inputType: "text" | "email" | "url" = "text",
  ) => {
    const props = {
      class: "input",
      value: draft[key],
      required: key === "name",
      maxLength: key === "email" ? 254 : key === "linkedin" ? 2000 : key === "topics" ? undefined : 200,
      onInput: (e: { currentTarget: HTMLInputElement }) => setDraft({ ...draft, [key]: e.currentTarget.value }),
    };
    return <label class="field"><span class="field-label">{title}</span>{inputType === "email" ? <input type="email" {...props} /> : inputType === "url" ? <input type="url" {...props} /> : <input type="text" {...props} />}</label>;
  };
  const [eventCursor, setEventCursor] = useState<string | undefined>();
  const events = useApi("eventsList", {
    query: { limit: 100, when: "all", cursor: eventCursor },
  });
  const [eventOptions, setEventOptions] = useState<EventListItem[]>([]);
  useEffect(() => {
    if (events.status === "ready")
      setEventOptions((prev) => [
        ...new Map(
          [...prev, ...events.data.items].map((e) => [e.id, e]),
        ).values(),
      ]);
  }, [events.status === "ready" ? events.data : null]);
  const items = list.status === "ready" ? list.data.items : [];
  const topics = [
    ...new Map(
      items.flatMap((c) => c.topics).map((t) => [t.toLocaleLowerCase(), t]),
    ).values(),
  ].sort();
  const filtered = items.filter(
    (c) =>
      (!spoken || c.hasSpokenForUs === (spoken === "true")) &&
      (!support || c.clubSupport) &&
      (!type || c.type === type || c.type === "both") &&
      (!topic ||
        c.topics.some(
          (t) => t.toLocaleLowerCase() === topic.toLocaleLowerCase(),
        )) &&
      `${c.name} ${c.org} ${c.role} ${c.email} ${c.topics.join(" ")}`
        .toLocaleLowerCase()
        .includes(q.trim().toLocaleLowerCase()),
  );
  const reached = (c: Contact) =>
    c.lastOutreach ? (
      <>
        <strong>
          {c.lastOutreach.officerName} · {fmtDate(c.lastOutreach.date)}
        </strong>
        <span class="cell-sub">
          {label(c.lastOutreach.channel)} · {label(c.lastOutreach.outcome)}
        </span>
      </>
    ) : (
      <span class="muted">No outreach yet</span>
    );
  const editForm = (
            <form class="stack" onSubmit={save}>
              <fieldset disabled={busy} class="speakers-fieldset stack">
                {field("name", "Name")}
                {field("org", "Organization")}
                {field("role", "Role")}
                <label class="field">
                  <span class="field-label">Type</span>
                  <select
                    class="select"
                    aria-label="Type"
 value={draft.type}
                    onChange={(e) =>
                      setDraft({
                        ...draft,
                        type: e.currentTarget.value as Contact["type"],
                      })
                    }
                  >
                    {["speaker", "mentor", "both"].map((t) => (
                      <option value={t}>{label(t)}</option>
                    ))}
                  </select>
                </label>
                <label class="choice">
                  <input type="checkbox" checked={draft.clubSupport} onChange={(e) => setDraft({ ...draft, clubSupport: e.currentTarget.checked })} data-club-support-input />
                  <span>Club support contact (shown under Board Members)</span>
                </label>
                <label class="choice">
                  <input type="checkbox" checked={draft.hasSpokenForUs} onChange={(e) => setDraft({ ...draft, hasSpokenForUs: e.currentTarget.checked })} />
                  <span>Has spoken for CSEED before</span>
                </label>
                {field("topics", "Topics")}
                <p class="field-hint">Separate topics with commas.</p>
                {field("email", "Email", "email")}
                {field("linkedin", "LinkedIn", "url")}
                <label class="field">
                  <span class="field-label">General notes</span>
                  <textarea
                    class="textarea"
                    maxLength={10000}
                    value={draft.notes}
                    onInput={(e) =>
                      setDraft({ ...draft, notes: e.currentTarget.value })
                    }
                  />
                </label>
                <fieldset class="speakers-fieldset stack">
                  <legend class="field-label">Linked events</legend>
                  {events.status === "ready" ? (
                    <>
                      {eventOptions.map((ev) => (
                        <label class="choice">
                          <input
                            type="checkbox"
                            checked={draft.eventIds.includes(ev.id)}
                            onChange={(e) =>
                              setDraft({
                                ...draft,
                                eventIds: e.currentTarget.checked
                                  ? [...draft.eventIds, ev.id]
                                  : draft.eventIds.filter((id) => id !== ev.id),
                              })
                            }
                          />
                          <span>
                            {ev.name} · {fmtDate(ev.startsAt)}
                          </span>
                        </label>
                      ))}
                      {contact?.events
                        .filter(
                          (ev) => !eventOptions.some((e) => e.id === ev.id),
                        )
                        .map((ev) => (
                          <label class="choice">
                            <input
                              type="checkbox"
                              checked={draft.eventIds.includes(ev.id)}
                              onChange={(e) =>
                                setDraft({
                                  ...draft,
                                  eventIds: e.currentTarget.checked
                                    ? [...draft.eventIds, ev.id]
                                    : draft.eventIds.filter(
                                        (id) => id !== ev.id,
                                      ),
                                })
                              }
                            />
                            <span>{ev.name}</span>
                          </label>
                        ))}
                      {events.data.nextCursor && (
                        <button
                          type="button"
                          class="btn"
                          onClick={() =>
                            setEventCursor(events.data.nextCursor ?? undefined)
                          }
                        >
                          Load more events
                        </button>
                      )}
                    </>
                  ) : events.status === "error" ? (
                    <ErrorState error={events.error} onRetry={events.reload} />
                  ) : (
                    <p>Loading events…</p>
                  )}
                </fieldset>
                {error && <p class="field-error" role="alert">{error}</p>}
                <button class="btn btn--primary" type="submit">
                  {busy ? "Saving…" : adding ? "Add contact" : "Save contact"}
                </button>
              </fieldset>
            </form>
  );
  const openLog = () => {
    setError("");
    const now = new Date();
    setOutDraft({
      date: toLocalInput(now.toISOString()),
      channel: "email",
      outcome: "awaiting_reply",
      note: "",
    });
    returnToLog.current = true;
    setOutreach(true);
  };
  const sorted = sortBy(filtered, sort, (c, k) => (k === "name" ? c.name : c.lastOutreach?.date ?? null));
  const linkedin = contact ? safeUrl(contact.linkedin) : null;
  const pastCount = contact ? contact.events.filter((e) => new Date(e.startsAt).getTime() < Date.now()).length : 0;
  type Tab = "overview" | "activity" | "manage";
  const tabs: PersonTab<Tab>[] = contact
    ? [
        {
          key: "overview",
          label: "Overview",
          render: () => (
            <>
              <PersonSection title="Contact">
                <PersonFacts
                  facts={[
                    ["Email", contact.email && <a href={`mailto:${contact.email}`}>{contact.email}</a>],
                    ["LinkedIn", linkedin && <a href={linkedin} target="_blank" rel="noopener noreferrer">{linkedin.replace(/^https:\/\/(www\.)?/, "")}</a>],
                    ["Organization", contact.org],
                    ["Role", contact.role],
                    ["Topics", contact.topics.join(" · ")],
                    ["Last reached out by", contact.lastOutreach && `${contact.lastOutreach.officerName} · ${fmtDate(contact.lastOutreach.date)}`],
                  ]}
                />
              </PersonSection>
              {contact.notes && (
                <PersonSection title="Notes">
                  <p class="speakers-note">{contact.notes}</p>
                </PersonSection>
              )}
              <PersonSection title="Past events">
                <span class="stack-tight"><Past c={contact} /></span>
              </PersonSection>
            </>
          ),
        },
        {
          key: "activity",
          label: "Activity",
          render: () => (
            <PersonSection title="Outreach log" data="outreach">
              {contact.outreach.length ? (
                <ol class="timeline">
                  {contact.outreach.map((o) => (
                    <li key={o.id}>
                      <strong>
                        {o.officerName} · {fmtDateTime(o.date)}
                      </strong>
                      <p>
                        {label(o.channel)} · {label(o.outcome)}
                      </p>
                      <p class="speakers-note">{o.note}</p>
                    </li>
                  ))}
                </ol>
              ) : (
                <p class="muted">No outreach yet.</p>
              )}
            </PersonSection>
          ),
        },
        {
          key: "manage",
          label: "Manage",
          render: () => (
            <div class="stack" data-contact-manage>
              {editForm}
              <div class="danger-zone action-row">
                <button class="btn" disabled={busy} onClick={remove}>
                  {confirmDelete
                    ? "Confirm delete contact and outreach history"
                    : "Delete contact"}
                </button>
                {confirmDelete && (
                  <button class="btn" onClick={() => setConfirmDelete(false)}>
                    Keep contact
                  </button>
                )}
              </div>
            </div>
          ),
        },
      ]
    : [];
  return (
    <div class="stack speakers-page">
      <p role="status" class="visually-hidden">
        {notice}
      </p>
      <PeopleCard label="External Contacts directory" busy={list.status === "loading"} data="contacts">
        <div class="people-tools speakers-filters">
          <PeopleSearch label="Find a contact" value={q} placeholder="Name, organization or topic" onInput={setQ} />
          <label class="field">
            <span class="field-label">Type</span>
            <select class="select" aria-label="Type" value={type} onChange={(e) => setType(e.currentTarget.value)}>
              <option value="">All types</option>
              {["speaker", "mentor", "both"].map((t) => (
                <option value={t}>{label(t)}</option>
              ))}
            </select>
          </label>
          <label class="field">
            <span class="field-label">Speaking history</span>
            <select class="select" value={spoken} onChange={(e) => setSpoken(e.currentTarget.value)}>
              <option value="">All</option>
              <option value="true">Has spoken</option>
              <option value="false">Not yet</option>
            </select>
          </label>
          <label class="field">
            <span class="field-label">Topic</span>
            <select class="select" aria-label="Topic" value={topic} onChange={(e) => setTopic(e.currentTarget.value)}>
              <option value="">All topics</option>
              {topics.map((t) => (
                <option value={t}>{t}</option>
              ))}
            </select>
          </label>
          <label class="choice speakers-support">
            <input type="checkbox" checked={support} onChange={(e) => setSupport(e.currentTarget.checked)} data-filter-support />
            <span>Club support only</span>
          </label>
          <div class="people-tools-end">
            <button class="btn btn--primary" onClick={add}>
              Add contact
            </button>
          </div>
        </div>
        <Async state={list}>
          {() =>
            filtered.length ? (
              <PeopleGroup
                section="contacts"
                title="Contacts"
                count={filtered.length}
                caption="External Contacts directory"
                collapsible={false}
                columns={[
                  { label: "Name", sort: "name", width: "30%" },
                  { label: "Type & topics", width: "24%" },
                  { label: "Reach", width: "12%" },
                  { label: "Past events", width: "14%" },
                  { label: "Last reached out by", sort: "last", width: "20%" },
                ]}
                sort={sort}
                onSort={setSort}
              >
                {sorted.map((c) => (
                  <PersonRow key={c.id} onOpen={() => person.open(c.id)} data={{ "data-contact-row": c.id, "data-person-row": c.id }}>
                    <td>
                      <PersonCell id={c.id} name={c.name} sub={[c.org, c.role].filter(Boolean).join(" · ") || undefined} onOpen={() => person.open(c.id)} />
                    </td>
                    <td data-cell="chip">
                      <span class="people-chips">
                        <PeopleChip hue={TYPE_HUE[c.type]}>{label(c.type)}</PeopleChip>
                        {c.clubSupport && <PeopleChip hue="green" data={{ "data-club-support": "" }}>Club support</PeopleChip>}
                      </span>
                      <span class="person-sub">{[c.hasSpokenForUs ? "Has spoken" : "Not yet", ...c.topics].join(" · ")}</span>
                    </td>
                    <td>
                      <ContactLinks email={c.email} linkedin={safeUrl(c.linkedin)} />
                    </td>
                    <td>
                      <PastCount c={c} />
                    </td>
                    <td>
                      {c.lastOutreach ? (
                        <span class="person-cell-text">
                          <span>
                            {c.lastOutreach.officerName} · {fmtDate(c.lastOutreach.date)}
                          </span>
                          <span class="person-sub">
                            {label(c.lastOutreach.channel)} · {label(c.lastOutreach.outcome)}
                          </span>
                        </span>
                      ) : (
                        <span class="muted">No outreach yet</span>
                      )}
                    </td>
                  </PersonRow>
                ))}
              </PeopleGroup>
            ) : (
              <div class={`state people-empty ${items.length ? "" : "state--brand"}`}>
                <p class="state-title">
                  {items.length
                    ? "No matching contacts"
                    : "Build your External Contacts directory."}
                </p>
                <p class="state-body">
                  {items.length
                    ? "Try another name, type or topic."
                    : "Start a shared directory so the next officer has the context."}
                </p>
                <button
                  class={items.length ? "btn" : "btn btn--primary"}
                  onClick={
                    items.length
                      ? () => {
                          setQ("");
                          setType("");
                          setSpoken("");
                          setTopic("");
                          setSupport(false);
                        }
                      : add
                  }
                >
                  {items.length ? "Clear filters" : "Add your first contact"}
                </button>
              </div>
            )
          }
        </Async>
      </PeopleCard>
      <Drawer
        dismissible={!busy}
        id="contact-new"
        open={adding && !outreach}
        title="Add contact"
        onClose={() => {
          if (!outreach) closeAdd();
        }}
      >
        {editForm}
      </Drawer>
      <PersonDrawer
        dismissible={!busy}
        id="contact"
        open={!adding && !!openId && !outreach}
        onClose={() => {
          if (!outreach) closeContact();
        }}
        personKey={openId ?? ""}
        name={contact?.name ?? "Contact"}
        eyebrow="External contact"
        subtitle={contact ? [contact.role, contact.org].filter(Boolean).join(" · ") || undefined : undefined}
        chips={
          contact && (
            <>
              <PeopleChip hue={TYPE_HUE[contact.type]}>{label(contact.type)}</PeopleChip>
              <PeopleChip hue={contact.hasSpokenForUs ? "blue" : null}>{contact.hasSpokenForUs ? "Has spoken" : "Not yet"}</PeopleChip>
              {contact.clubSupport && <PeopleChip hue="green">Club support</PeopleChip>}
            </>
          )
        }
        actions={
          contact
            ? [
                ...(contact.email ? [{ label: "Email", icon: "mail" as const, href: `mailto:${contact.email}`, primary: true }] : []),
                ...(linkedin ? [{ label: "LinkedIn", icon: "link" as const, href: linkedin, external: true }] : []),
                { label: "Log outreach", icon: "message" as const, onClick: openLog, data: "log-outreach" },
              ]
            : []
        }
        stats={
          contact
            ? [
                { label: "Past events", value: pastCount },
                { label: "Outreach logged", value: contact.outreach.length },
                { label: "Last reached out", value: contact.lastOutreach ? fmtDate(contact.lastOutreach.date) : "—" },
              ]
            : []
        }
        status={detailError ? <div class="person-body"><ErrorState error={detailError} /></div> : !contact ? <div class="person-body"><p role="status">Loading contact…</p></div> : undefined}
        tabs={tabs}
      />
      <Drawer
        dismissible={!busy}
        id="outreach"
        open={outreach}
        title="Log outreach"
        onClose={() => {
          if (!busy) {
            setOutreach(false);
            setError("");
          }
        }}
      >
        <form class="stack" onSubmit={log}>
          <fieldset class="speakers-fieldset stack" disabled={busy}>
            <p>
              <strong>{contact?.name}</strong>
            </p>
            <p>Reached out by {me.name} (you)</p>
            <label class="field">
              <span class="field-label">When · {effectiveTimezone()}</span>
              <input
                class="input"
                type="datetime-local"
                required
                value={outDraft.date}
                onInput={(e) =>
                  setOutDraft({ ...outDraft, date: e.currentTarget.value })
                }
              />
            </label>
            <label class="field">
              <span class="field-label">Channel</span>
              <select
                class="select"
                aria-label="Channel"
 value={outDraft.channel}
                onChange={(e) =>
                  setOutDraft({
                    ...outDraft,
                    channel: e.currentTarget.value as typeof outDraft.channel,
                  })
                }
              >
                {["email", "linkedin", "discord", "phone", "in_person"].map(
                  (x) => (
                    <option value={x}>{label(x)}</option>
                  ),
                )}
              </select>
            </label>
            <label class="field">
              <span class="field-label">Outcome</span>
              <select
                class="select"
                aria-label="Outcome"
 value={outDraft.outcome}
                onChange={(e) =>
                  setOutDraft({
                    ...outDraft,
                    outcome: e.currentTarget.value as typeof outDraft.outcome,
                  })
                }
              >
                {[
                  "awaiting_reply",
                  "interested",
                  "confirmed",
                  "declined",
                  "follow_up",
                ].map((x) => (
                  <option value={x}>{label(x)}</option>
                ))}
              </select>
            </label>
            <label class="field">
              <span class="field-label">Notes</span>
              <textarea
                class="textarea"
                maxLength={10000}
                value={outDraft.note}
                onInput={(e) =>
                  setOutDraft({ ...outDraft, note: e.currentTarget.value })
                }
              />
            </label>
            <p class="small muted">
              Latest dated entry updates “Last reached out by”. Contact notes
              stay separate.
            </p>
            {error && <p class="field-error" role="alert">{error}</p>}
            <button class="btn btn--primary" type="submit">
              {busy ? "Saving…" : "Save outreach"}
            </button>
          </fieldset>
        </form>
      </Drawer>
    </div>
  );
}
function Reach({ c }: { c: Contact }) {
  return (
    <>
      {c.email && <a href={`mailto:${c.email}`}>{c.email}</a>}
      {safeUrl(c.linkedin) && (
        <span class="cell-sub">
          <a
            href={safeUrl(c.linkedin)!}
            target="_blank"
            rel="noopener noreferrer"
          >
            LinkedIn ↗
          </a>
        </span>
      )}
      {!c.email && !c.linkedin && <span class="muted">No contact details</span>}
    </>
  );
}
function Past({ c }: { c: Contact }) {
  const past = c.events.filter(
    (e) => new Date(e.startsAt).getTime() < Date.now(),
  );
  return past.length ? (
    <>
      <span class="cell-sub">Has done an event with us</span>
      {past.map((ev) => (
        <span class="cell-sub">
          <a href={`/events?id=${encodeURIComponent(ev.id)}`}>{ev.name}</a>
        </span>
      ))}
    </>
  ) : (
    <span class="muted">No past events yet</span>
  );
}

function Spoken({ c }: { c: Contact }) {
 return <span class={`badge ${c.hasSpokenForUs ? "badge--info" : ""}`}>{c.hasSpokenForUs ? "Has spoken" : "Not yet"}</span>;
}

const TYPE_HUE: Record<Contact["type"], Hue> = { speaker: "blue", mentor: "teal", both: "violet" };
function PastCount({ c }: { c: Contact }) {
  const n = c.events.filter((e) => new Date(e.startsAt).getTime() < Date.now()).length;
  return n ? <span>{n} {n === 1 ? "event" : "events"}</span> : <span class="muted">None yet</span>;
}
