import { useState } from "preact/hooks";
import {
  BUILDSPACE_STAGES,
  BUILDSPACE_TRACKS,
  type BuildspaceData,
  type BuildspacePerson,
  type BuildspaceProject,
} from "../../../lib/contracts";
import { api } from "../../../lib/api/client";
import { errorMessage, Notice } from "../../components/states";
export function ProjectForm({
  b,
  project,
  saved,
}: {
  b: BuildspaceData;
  project?: BuildspaceProject;
  saved: () => void;
}) {
  const [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  const [track, setTrack] = useState(project?.track || "Software");
  const [stage, setStage] = useState(project?.stage || "Ideating");
  return (
    <form
      class="bs-form"
      onSubmit={async (e) => {
        e.preventDefault();
        const f = new FormData(e.currentTarget);
        setBusy(true);
        setError("");
        try {
          await api("buildspaceProjectSave", {
            params: { id: project?.id || crypto.randomUUID() },
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
            </label>
          ))
        ) : (
          <p>
            Add a mentor in <a href="/contacts">External Contacts</a> first.
          </p>
        )}
        <p class="bs-muted">
          Projects without a mentor appear in the attention filter.
        </p>
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
      <button class="btn btn--primary" disabled={busy}>
        {busy ? "Saving…" : "Save project"}
      </button>
    </form>
  );
}
export function PersonForm({
  b,
  person,
  saved,
}: {
  b: BuildspaceData;
  person?: BuildspacePerson;
  saved: () => void;
}) {
  const [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  const [projectId, setProjectId] = useState(person?.projectId || "");
  return (
    <form
      class="bs-form"
      onSubmit={async (e) => {
        e.preventDefault();
        const f = new FormData(e.currentTarget);
        setBusy(true);
        setError("");
        try {
          await api("buildspacePersonSave", {
            params: { id: person?.id || crypto.randomUUID() },
            body: {
              name: String(f.get("name")),
              projectId: String(f.get("projectId")),
              joinedOn: String(f.get("joinedOn")),
              memberId: String(f.get("memberId")) || null,
              discordId: String(f.get("discordId")) || null,
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
          {b.projects.map((p) => (
            <option value={p.id}>{p.name}</option>
          ))}
        </select>
      </label>
      <p class="bs-muted">
        Track, partners, and mentors follow the project. Create a project first
        if it isn’t listed.
      </p>
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
        Member record ID · optional
        <input
          class="input"
          name="memberId"
          maxLength={64}
          defaultValue={person?.memberId || ""}
        />
      </label>
      <p class="bs-muted">
        Copy the ID from the <a href="/members">Members directory</a> to link an
        existing member.
      </p>
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
      <p class="bs-muted">
        Needed for the bot to match weekly updates to this builder.
      </p>
      {error && <Notice tone="negative">{error}</Notice>}
      <button class="btn btn--primary" disabled={busy || !b.projects.length}>
        {busy ? "Saving…" : "Save builder"}
      </button>
    </form>
  );
}
