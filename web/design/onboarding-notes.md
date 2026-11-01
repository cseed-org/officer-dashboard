# Officer onboarding · V0.3 design handoff

Open `mockups/onboarding-v03.html`. The bottom review strip is a mockup aid, excluded from production. Query parameters: `step=1…6`, `state=ready|empty|loading|error|expired`, `role=officer|director|leadership|owner`, `theme=light|dark`. Controls switch these examples locally; no data is sent. Images reuse the design system's local cseed.co assets.

## Flow and behavior

1. Hello: welcoming CSEED voice, approximate duration, community polaroids on large screens.
2. Confirm: leadership's name, Discord ID and subteam are read-only. Show the authenticated Discord identity. A mismatch must be resolved before acceptance; “Tell Alex” is a sample contact, to be replaced with the inviter's actual contact route. Access is assigned by leadership. Owner displays Leadership plus Owner; ownership is an additional flag, not a fourth rank.
3. Profile: phone and birthday required; optional fields expand in a disclosure. Validate required fields, email, URL and dates on the server too. Errors stay beside the fields, linked with aria-describedby; focus the first invalid field. Phone is visible to director and leadership, including owners. Do not infer birthday visibility from phone permissions: confirm with the implementation spec before exposing it beyond the profile.
4. Calendar: native saved-email select intentionally supplies native keyboard/typeahead semantics in this flow. Up to five unique addresses; Add another is disabled at five with a written reason. Removal belongs in Settings. The selected email is the event delivery destination; saved addresses are not all implicitly subscribed. A production picker must retain that selection after navigation. Choose .ics invites or Google sync. Consent opens on Google, cancel returns to this step, revoked/failed authorization offers Retry and .ics fallback. The prototype only illustrates this boundary. Calendar can be deferred; no email means no invitations sent. If Google mode is selected but not connected, Continue must offer an explicit “Set up later” or connection action; never imply Google is synced.
5. Widgets: Web Analytics is on by default. Native checkbox cards support keyboard selection. None selected is valid: Home offers Add widgets. Disconnected services say Not connected, never zero. Selected widget IDs, supported default sizes and order are saved in user_prefs on completion. No permission is granted by choosing a widget; endpoints still enforce roles.
6. Done: shown only after the final save succeeds. Dashboard handoff goes to Home. Production summary distinguishes a deferred calendar or Google consent from a completed connection. Back remains available before dashboard handoff; saving is idempotent.

The preview retains profile entries and widget choices across Back within the document. Production persists draft progress server-side, with a clearly acknowledged save, and resumes only for the matching invited Discord ID. Do not store phone or birthday in browser storage. Non-invitees go through existing sign-in/access handling, not onboarding. Expired/revoked/invalid invitations all use the unavailable screen without revealing another invitee's details; retry with the correct Discord account or request a fresh invite from the inviter. Its sign-in link is a design reference; implementation routes to `/login`.

## States and roles

Loading replaces step content, exposes aria-busy and status text, and disables navigation. Error retains entries with a persistent alert and Retry; no success navigation until save succeeds. Empty Calendar and Widgets are meaningful first-run states. The review strip also provides a generic unsaved-profile empty example. All four role previews share the same profile requirements: officer works with events/contacts/design; director additionally invites and writes member records; leadership manages roles; owner additionally manages ownership, preserving at least one owner. Only leadership changes role grants, and no role selector is part of production setup.

## Keyboard, layout and motion

Tab follows the document order; native radio groups use arrow keys; select uses OS arrow/typeahead behavior; Space toggles widget checkboxes; Enter activates focused actions. Back and Alt+Left navigate to the previous step without discarding input. Browser refresh resumes server-saved draft in production. Focus lands on the new heading after step change. No global Enter interception on form inputs. Phone uses one column, scrollable content and a persistent footer above safe-area padding. Desktop art is decorative context and hides under 1100px; all essential information remains in the content.

Shared v03 motion: content enters in 420ms with 120ms stagger; backward navigation mirrors the direction. Production may use the shared 240ms leave before entry. The preview swaps then enters, without holding controls during animation. Photos settle over 900ms with a 120ms stagger on first load only. No loops, no confetti. Highlighter stays static. With prefers-reduced-motion, swaps are immediate, opacity is 1 and photos retain their final static tilt; focus behavior is identical.

## Open implementation questions

- Confirm inviter contact routing for mismatched invitation details.
- Confirm birthday read permissions; the product decision specifies only phone visibility.

Neither question blocks review of the six-step visual design. OAuth, draft persistence and final saves remain build-Pane work, not implemented functionality in this mockup.
