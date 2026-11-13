# Kanban / planning references (V0.10 Design page)

Source: hands-on product knowledge of each tool (no live web browsing this round; nothing here
is quoted from docs). Notes are about what makes each pleasant to work in, not feature lists.

| Tool | What works | Take for CSEED |
|---|---|---|
| **Linear** (board view) | Calm, dense cards; status icon + priority + assignee avatar in one row; columns have a count and a tiny status glyph; side "peek" panel keeps the board visible; keyboard first (`⌘K`, move with shortcuts); smooth, short motion. | Peek side panel, compact meta row, keyboard move, quiet colour. |
| **Trello** | Big friendly drop zones (full-height lists), tilt + shadow on the lifted card, cover images make cards recognisable, labels as colour chips. | Full-height columns (also today's bug), lifted-card tilt, type colour stripe. |
| **GitHub Projects** | Same items as board / table / roadmap; **group by** any field gives swimlanes; column limits and per-column counts; "sum" of a field in the header. | Group-by (event / designer) swimlanes as an option, counts in headers. |
| **Jira** | Swimlanes by assignee/epic; transitions that aren't allowed grey out the column while dragging; WIP limits turn the header red. | Disabled targets while dragging (permission-aware), lane-by-event. |
| **Notion boards** | Card properties are chosen per view; hidden columns; "+ New" at the column foot; generous white space; the page feels like a document. | "+ Request" at the To do foot, airy spacing. |
| **Height / Asana** | Progress shown per section (x/y done); due dates turn amber/red as they near; celebratory moment on completion (Asana's unicorn) — fun but skippable. | Header progress bars, urgency colours, a small Done celebration (reduced-motion safe). |

## Principles for the build
1. Columns are full-height drop lanes; disallowed lanes dim with a "Not allowed" hint while dragging.
2. Cards show: type icon + colour, title, event + date, due chip with urgency, assignee avatar, requester, design-file buttons.
3. Detail opens as a side panel ("peek") so the board stays in view; deep link `?id=`.
4. Quick filters as chips (Mine, Due soon, Unassigned, type); counts always visible.
5. Motion: lift on drag, slide into place on drop, a short confetti/check on Done; all off under reduced motion.
6. Keyboard: Space to pick up, arrows to choose lane, Space to drop, Esc to cancel (kept from today).
