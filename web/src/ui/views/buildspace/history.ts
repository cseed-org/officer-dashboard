/**
 * History for Buildspace + Launchpad's drawers and forms (R-4, review F-2). Every drawer or
 * form this page opens pushes one history entry and records how many of its own entries sit
 * above the list (its depth). Leaving one level steps back one entry; closing everything steps
 * back to the list. So Back never reopens a closed drawer or form, and no dead entry is left.
 * Entries this page did not push (a deep link) have depth 0: their parameters are dropped in
 * place instead.
 */
import { setParams } from "../../../lib/nav";
import { PERSON_MARK } from "../../components/PersonDrawer";

const DEPTH = "cseedBuildspaceDepth";
// program/pnew: the Programs tab's program drawer and new-program form.
const RECORD = ["person", "project", "new", "edit", "program", "pnew"] as const;
const CLOSED = { person: null, project: null, new: null, edit: null, program: null, pnew: null };

/** How many entries this page pushed above the list (a person drawer it opened counts as 1). */
function depth(): number {
  const s = history.state ?? {};
  if (typeof s[DEPTH] === "number") return s[DEPTH];
  return s[PERSON_MARK] ? 1 : 0;
}

/** Open a drawer or form on a new entry, one level above the current one. */
export function pushEntry(patch: Record<string, string | null>) {
  const d = depth();
  setParams(patch);
  history.replaceState({ ...(history.state ?? {}), [DEPTH]: d + 1 }, "", location.href);
}

/** Step back to the entry under this one (a builder form returns to its profile). */
export function leaveEntry(patch: Record<string, null>) {
  if (depth() > 0) history.back();
  else setParams(patch, { replace: true });
}

/** Close every drawer and form: back to the list this page started from. */
export function closeAll() {
  const d = depth();
  if (!d) {
    setParams(CLOSED, { replace: true });
    return;
  }
  // Landing on a deep-linked record (it sits under our entries): drop it in place.
  addEventListener(
    "popstate",
    () => {
      const q = new URLSearchParams(location.search);
      if (RECORD.some((k) => q.has(k))) setParams(CLOSED, { replace: true });
    },
    { once: true },
  );
  history.go(-d);
}
