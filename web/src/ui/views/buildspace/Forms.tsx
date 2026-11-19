import { useEffect, useRef, useState } from "preact/hooks";
import { BUILDSPACE_STAGES, BUILDSPACE_TRACKS } from "../../../core/constants";
import type {
  BuildspaceData,
  BuildspacePerson,
  BuildspaceProject,
} from "../../../lib/contracts";
import { api, fieldErrors } from "../../../lib/api/client";
import type { Member } from "../../../lib/contracts";
import { errorMessage, Notice } from "../../components/states";
export function ProjectForm({
  b,
  project,
  saved,
  readOnly = false,
}: {
  b: BuildspaceData;
  project?: BuildspaceProject;
  saved: () => void;
  readOnly?: boolean;
}) {
  const newId = useRef(project?.id || crypto.randomUUID()).current;
  const [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  const [track, setTrack] = useState(project?.track || "Software");
  const [stage, setStage] = useState(project?.stage || "Ideating");
  return (
    <form
      class="bs-form motion-list"
      onSubmit={async (e) => {
        e.preventDefault();
        if (busy || readOnly) return;
        const f = new FormData(e.currentTarget);
        setBusy(true);
        setError("");
        try {
          await api("buildspaceProjectSave", {
            params: { id: newId },
            body: {
              name: String(f.get("name")),
              description: String(f.get("description")),
              track: String(f.get("track")) as BuildspaceProject["track"],
              stage: String(f.get("stage")) as BuildspaceProject["stage"],
              links: String(f.get("links"))
                .split("\n")
                .map((s) => s.trim())
                .filter(Boolean),
              mentorIds: f.getAll("mentors").map(String),
              // A project's cohort is fixed at creation; the server ignores it on update.
              ...(project ? {} : { cohortId: b.cohortId ?? undefined }),
            },
          });
          saved();
        } catch (err) {
          setError(errorMessage(err));
        } finally {
          setBusy(false);
        }
      }}
    >
      <label>
        Project name
        <input
          class="input"
          name="name"
          required
          maxLength={200}
          defaultValue={project?.name}
        />
      </label>
      <label>
        Description
        <textarea
          class="textarea"
          name="description"
          rows={4}
          maxLength={10000}
          defaultValue={project?.description}
        />
      </label>
      <label>
        Track
        <select
          class="select"
          aria-label="Track"
          name="track"
          value={track}
          onChange={(e) => setTrack(e.currentTarget.value as typeof track)}
        >
          {BUILDSPACE_TRACKS.map((t) => (
            <option>{t}</option>
          ))}
        </select>
      </label>
      <label>
        Stage
        <select
          class="select"
          aria-label="Stage"
          name="stage"
          value={stage}
          onChange={(e) => setStage(e.currentTarget.value as typeof stage)}
        >
          {BUILDSPACE_STAGES.map((t) => (
            <option>{t}</option>
          ))}
        </select>
      </label>
      <fieldset>
        <legend>Mentors · choose up to two</legend>
        {b.mentors.length ? (
          b.mentors.map((m) => (
            <label class="bs-check">
              <input
                type="checkbox"
                name="mentors"
                value={m.id}
                defaultChecked={project?.mentorIds.includes(m.id)}
              />
              {m.name} · {m.org}
              {!m.available && <span class="bs-muted"> · unavailable</span>}
              {m.maxProjects !== null && <span class="bs-muted"> · {m.load}/{m.maxProjects} projects</span>}
            </label>
          ))
        ) : (
          <p>
            Add a mentor in <a href="/contacts">External Contacts</a> first.
          </p>
        )}
      </fieldset>
      <label>
        Links · one per line
        <textarea
          class="textarea"
          name="links"
          rows={3}
          defaultValue={project?.links.join("\n")}
          placeholder="https://github.com/your-team/project"
        />
      </label>
      {error && <Notice tone="negative">{error}</Notice>}
      <button class="btn btn--primary" disabled={busy || readOnly}>
        {busy ? "Saving…" : "Save project"}
      </button>
    </form>
  );
}
export function PersonForm({
  b,
  person,
  saved,
  readOnly = false,
}: {
  b: BuildspaceData;
  person?: BuildspacePerson;
  saved: () => void;
  readOnly?: boolean;
}) {
  const newId = useRef(person?.id || crypto.randomUUID()).current;
  const [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  const [projectId, setProjectId] = useState(person?.projectId || "");
  const [fields, setFields] = useState<Record<string, string>>({});
  const projects = b.projects.filter((p) => !p.archivedAt || p.id === person?.projectId);
  return (
    <form
      class="bs-form motion-list"
      onSubmit={async (e) => {
        e.preventDefault();
        if (busy || readOnly) return;
        const f = new FormData(e.currentTarget);
        setBusy(true);
        setError("");
        try {
          setFields({});
          await api("buildspacePersonSave", {
            params: { id: newId },
            body: {
              name: String(f.get("name")),
              projectId: String(f.get("projectId")),
              joinedOn: String(f.get("joinedOn")),
              memberId: String(f.get("memberId") || "") || null,
              discordId: String(f.get("discordId")) || null,
              designation: String(f.get("designation") || ""),
              pronouns: String(f.get("pronouns") || ""),
              bio: String(f.get("bio") || ""),
              email: String(f.get("email") || "").trim() || null,
            },
          });
          saved();
        } catch (err) {
          setFields(fieldErrors(err));
          setError(errorMessage(err));
        } finally {
          setBusy(false);
        }
      }}
    >
      <label>
        Builder name
        <input
          class="input"
          name="name"
          required
          maxLength={200}
          defaultValue={person?.name}
        />
      </label>
      <label>
        Project
        <select
          class="select"
          aria-label="Project"
          name="projectId"
          required
          value={projectId}
          onChange={(e) => setProjectId(e.currentTarget.value)}
        >
          <option value="" disabled>
            Choose a project
          </option>
          {projects.map((p) => (
            <option value={p.id}>{p.name}</option>
          ))}
        </select>
      </label>
      <label>
        Joined on
        <input
          class="input"
          type="date"
          required
          name="joinedOn"
          defaultValue={person?.joinedOn || b.today}
        />
      </label>
      <label>
        Designation · optional
        <input
          class="input"
          name="designation"
          maxLength={80}
          placeholder="Hardware lead, Designer, Founder…"
          defaultValue={person?.designation || ""}
        />
      </label>
      <label>
        Pronouns · optional
        <input class="input" name="pronouns" maxLength={40} defaultValue={person?.pronouns || ""} />
      </label>
      <label>
        Email · optional
        <input
          class="input"
          type="email"
          name="email"
          maxLength={254}
          aria-invalid={fields.email ? true : undefined}
          defaultValue={person?.email || ""}
        />
      </label>
      <label>
        Short bio · optional
        <textarea class="textarea" name="bio" rows={3} maxLength={500} defaultValue={person?.bio || ""} />
      </label>
      <MemberPicker initial={person?.memberId ?? null} />
      <label>
        Discord user ID · optional
        <input
          class="input"
          name="discordId"
          inputMode="numeric"
          pattern="[0-9]{17,20}"
          defaultValue={person?.discordId || ""}
        />
      </label>
      {error && <Notice tone="negative">{error}</Notice>}
      <button class="btn btn--primary" disabled={busy || readOnly || !projects.length}>
        {busy ? "Saving…" : "Save builder"}
      </button>
    </form>
  );
}

const memberName = (m: Member) => `${m.preferred_name || m.first_name} ${m.last_name}`.trim();

/**
 * Links a builder to their Members record by searching the mirror (name or email) instead of
 * typing an ID. Builders who onboard through the portal are linked by email automatically.
 * Submits as the `memberId` form field; Clear unlinks.
 */
export function MemberPicker({ initial }: { initial: string | null }) {
  const [id, setId] = useState<string | null>(initial);
  const [label, setLabel] = useState<string>(initial ? "Linked member record" : "");
  const [q, setQ] = useState("");
  const [results, setResults] = useState<Member[] | null>(null);
  const [error, setError] = useState("");
  const [active, setActive] = useState(0);
  const listId = useRef(`member-picker-${Math.random().toString(36).slice(2)}`).current;
  useEffect(() => {
    if (!initial) return;
    const ctrl = new AbortController();
    api("memberDetail", { params: { id: initial }, signal: ctrl.signal })
      .then((r) => setLabel(`${memberName(r.member)}${r.member.uw_email ? ` · ${r.member.uw_email}` : ""}`))
      .catch(() => setLabel(`Member ${initial}`));
    return () => ctrl.abort();
  }, [initial]);
  useEffect(() => {
    const term = q.trim();
    if (term.length < 2) {
      setResults(null);
      return;
    }
    const ctrl = new AbortController();
    const t = setTimeout(() => {
      api("membersList", { query: { q: term, limit: 8 }, signal: ctrl.signal })
        .then((r) => {
          setResults(r.items);
          setActive(0);
          setError(r.connected ? "" : "The Members mirror isn't connected, so there is nothing to search yet.");
        })
        .catch((e) => !ctrl.signal.aborted && setError(errorMessage(e)));
    }, 200);
    return () => {
      clearTimeout(t);
      ctrl.abort();
    };
  }, [q]);
  const pick = (m: Member) => {
    setId(m.person_id);
    setLabel(`${memberName(m)}${m.uw_email ? ` · ${m.uw_email}` : ""}`);
    setQ("");
    setResults(null);
  };
  return (
    <div class="lp-member-picker" data-member-picker>
      <input type="hidden" name="memberId" value={id ?? ""} />
      {id ? (
        <div class="lp-picked">
          <span>
            <span class="field-label">Member record</span>
            <strong>{label}</strong>
          </span>
          <button type="button" class="btn btn--small" onClick={() => { setId(null); setLabel(""); }}>
            Clear
          </button>
        </div>
      ) : (
        <label>
          Member record · optional
          <input
            class="input"
            type="search"
            role="combobox"
            list={undefined}
            aria-expanded={!!results?.length}
            aria-controls={listId}
            aria-autocomplete="list"
            aria-activedescendant={results?.length ? `${listId}-${active}` : undefined}
            placeholder="Search members by name or email"
            value={q}
            onInput={(e) => setQ(e.currentTarget.value)}
            onKeyDown={(e) => {
              if (!results?.length) return;
              if (e.key === "ArrowDown") { e.preventDefault(); setActive((a) => Math.min(a + 1, results.length - 1)); }
              else if (e.key === "ArrowUp") { e.preventDefault(); setActive((a) => Math.max(a - 1, 0)); }
              else if (e.key === "Enter") { e.preventDefault(); pick(results[active]!); }
              else if (e.key === "Escape") setResults(null);
            }}
          />
        </label>
      )}
      {!id && results && (
        <ul class="lp-picker-list" role="listbox" id={listId}>
          {results.length ? (
            results.map((m, i) => (
              <li
                key={m.person_id}
                id={`${listId}-${i}`}
                role="option"
                aria-selected={i === active}
                onMouseDown={(e) => { e.preventDefault(); pick(m); }}
              >
                <strong>{memberName(m)}</strong>
                <span class="bs-muted">{[m.uw_email, m.major, m.grad_year].filter(Boolean).join(" · ")}</span>
              </li>
            ))
          ) : (
            <li class="bs-muted" role="option" aria-disabled="true" aria-selected={false}>No members match.</li>
          )}
        </ul>
      )}
      {error && <p class="field-hint">{error}</p>}
    </div>
  );
}
