import { useEffect, useRef, useState } from "preact/hooks";
import type { Contact, CurrentUser, EventListItem } from "../../lib/contracts";
import { api, type BodyOf } from "../../lib/api/client";
import { useApi } from "../../lib/api/useApi";
import { setParams, useSearch } from "../../lib/nav";
import { safeUrl, fmtDate, fmtDateTime } from "../../lib/format";
import { Drawer } from "../components/Drawer";
import { Async, ErrorState, errorMessage } from "../components/states";
import { Gate } from "../shell/Gate";

const label = (s: string) =>
  s
    .split("_")
    .map((w) => w[0]!.toUpperCase() + w.slice(1))
    .join(" ");
const blank = {
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

export default function SpeakersPage() {
  return <Gate label="speakers">{(me) => <Speakers me={me} />}</Gate>;
}
function Speakers({ me }: { me: CurrentUser }) {
  const search = useSearch();
  const openId = search.get("id");
  const list = useApi("contactsList");
  const [q, setQ] = useState("");
  const [type, setType] = useState("");
  const [topic, setTopic] = useState("");
  const [adding, setAdding] = useState(false);
  const [draft, setDraft] = useState<Draft>({ ...blank });
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
  const logButton = useRef<HTMLButtonElement>(null);
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
      requestAnimationFrame(() => logButton.current?.focus());
    }
  }, [outreach]);
  function closeContact() {
    if (busy) return;
    setAdding(false);
    setParams({ id: null });
    setError("");
  }
  function add() {
    setDraft({ ...blank, eventIds: [] });
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
        setAdding(false);
        setParams({ id: d.contact.id });
      }
    } catch (e) {
      setError(errorMessage(e));
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
      setParams({ id: null });
    } catch (e) {
      setError(errorMessage(e));
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
      const d = await api("contactOutreachCreate", {
        params: { id: openId! },
        body: { ...outDraft, date: new Date(outDraft.date).toISOString() },
      });
      setContact(d.contact);
      list.reload();
      setNotice("Outreach logged.");
      setOutreach(false);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  const field = (
    key: Exclude<keyof Draft, "type" | "eventIds">,
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
  return (
    <div class="stack speakers-page">
      <p class="muted">
        People who share what they know. Keep the next conversation in context.
      </p>
      <div class="toolbar">
        <div class="toolbar-main speakers-filters">
          <label class="field">
            <span class="field-label">Find a contact</span>
            <input
              class="input"
              type="search"
              placeholder="Name, organization or topic"
              value={q}
              onInput={(e) => setQ(e.currentTarget.value)}
            />
          </label>
          <label class="field">
            <span class="field-label">Type</span>
            <select
              class="select"
              value={type}
              onChange={(e) => setType(e.currentTarget.value)}
            >
              <option value="">All types</option>
              {["speaker", "mentor", "both"].map((t) => (
                <option value={t}>{label(t)}</option>
              ))}
            </select>
          </label>
          <label class="field">
            <span class="field-label">Topic</span>
            <select
              class="select"
              value={topic}
              onChange={(e) => setTopic(e.currentTarget.value)}
            >
              <option value="">All topics</option>
              {topics.map((t) => (
                <option value={t}>{t}</option>
              ))}
            </select>
          </label>
        </div>
        <button class="btn btn--primary" onClick={add}>
          Add contact
        </button>
      </div>
      <p role="status" class="small muted">
        {notice}
      </p>
      <Async state={list}>
        {() =>
          filtered.length ? (
            <>
              <div class="table-wrap speakers-desktop">
                <table class="dt">
                  <caption class="visually-hidden">
                    Speaker and mentor directory
                  </caption>
                  <thead>
                    <tr>
                      {[
                        "Contact",
                        "Type & topics",
                        "Reach them",
                        "Past events",
                        "Last reached out by",
                      ].map((h) => (
                        <th scope="col">{h}</th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {filtered.map((c) => (
                      <tr key={c.id}>
                        <td>
                          <strong>{c.name}</strong>
                          <span class="cell-sub">
                            {[c.org, c.role].filter(Boolean).join(" · ")}
                          </span>
                          <button
                            class="linkish"
                            onClick={() => setParams({ id: c.id })}
                          >
                            View contact
                            <span class="visually-hidden"> {c.name}</span>
                          </button>
                        </td>
                        <td>
                          <span class="badge badge--info">{label(c.type)}</span>
                          <span class="cell-sub">{c.topics.join(" · ")}</span>
                        </td>
                        <td>
                          <Reach c={c} />
                        </td>
                        <td>
                          <Past c={c} />
                        </td>
                        <td>{reached(c)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <div class="speakers-mobile">
                {filtered.map((c) => (
                  <article class="block" key={c.id}>
                    <div class="block-body stack">
                      <strong>{c.name}</strong>
                      <span>{[c.org, c.role].filter(Boolean).join(" · ")}</span>
                      <span>
                        {label(c.type)} · {c.topics.join(" · ")}
                      </span>
                      <Reach c={c} />
                      <div>
                        <span class="field-label">Past events</span>
                        <Past c={c} />
                      </div>
                      <div>
                        <span class="field-label">Last reached out by</span>
                        {reached(c)}
                      </div>
                      <button
                        class="btn"
                        onClick={() => setParams({ id: c.id })}
                      >
                        View contact
                        <span class="visually-hidden"> {c.name}</span>
                      </button>
                    </div>
                  </article>
                ))}
              </div>
              <p class="small muted">
                {filtered.length}{" "}
                {filtered.length === 1 ? "contact" : "contacts"} · All officers
                can edit contacts and log outreach.
              </p>
            </>
          ) : (
            <div class={`state ${items.length ? "" : "state--brand"}`}>
              <p class="state-title">
                {items.length
                  ? "No matching contacts"
                  : "Make room for what's next."}
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
                        setTopic("");
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
      <Drawer
        dismissible={!busy}
        id="contact"
        open={(adding || !!openId) && !outreach}
        title={adding ? "Add contact" : (contact?.name ?? "Contact")}
        eyebrow="All officers can edit"
        onClose={() => {
          if (!outreach) closeContact();
        }}
      >
        {detailError && !adding ? (
          <ErrorState error={detailError} />
        ) : !adding && !contact ? (
          <p role="status">Loading contact…</p>
        ) : (
          <div class="stack">
            <form class="stack" onSubmit={save}>
              <fieldset disabled={busy} class="speakers-fieldset stack">
                {field("name", "Name")}
                {field("org", "Organization")}
                {field("role", "Role")}
                <label class="field">
                  <span class="field-label">Type</span>
                  <select
                    class="select"
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
                {error && <p role="alert">{error}</p>}
                <button class="btn btn--primary" type="submit">
                  {busy ? "Saving…" : adding ? "Add contact" : "Save contact"}
                </button>
              </fieldset>
            </form>
            {!adding && contact && (
              <>
                <h3>Past events</h3>
                <Past c={contact} />
                <h3>Outreach log</h3>
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
                <button
                  ref={logButton}
                  class="btn btn--primary"
                  disabled={busy}
                  onClick={() => {
                    setError("");
                    const now = new Date();
                    setOutDraft({
                      date: new Date(
                        now.getTime() - now.getTimezoneOffset() * 60000,
                      )
                        .toISOString()
                        .slice(0, 16),
                      channel: "email",
                      outcome: "awaiting_reply",
                      note: "",
                    });
                    returnToLog.current = true;
                    setOutreach(true);
                  }}
                >
                  Log outreach
                </button>
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
              </>
            )}
          </div>
        )}
      </Drawer>
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
              <span class="field-label">When</span>
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
            {error && <p role="alert">{error}</p>}
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
