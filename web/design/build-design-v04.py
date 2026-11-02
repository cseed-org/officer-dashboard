#!/usr/bin/env python3
"""V0.4 Design page mockup: upcoming events with design needs, plus the revamped board.

Run from the repository root: python3 web/design/build-design-v04.py
Reuses the app shell (top bar, sidebar, theme script) from mockups/design-requests-v03.html and
the production stylesheet web/src/styles/design-requests.css, so the mockup and the build share
every class. Fixture date: Monday, Nov 2 2026.
"""
from html import escape
from pathlib import Path

DIR = Path(__file__).resolve().parent
src = (DIR / 'mockups' / 'design-requests-v03.html').read_text()

head_end = src.index('<main')
shell_head = src[:head_end]
script_start = src.index('<script>')
shell_script = src[script_start:src.index('</script>', script_start) + len('</script>')]

shell_head = shell_head.replace('<title>Design requests · CSEED design</title>', '<title>Design · CSEED design v0.4</title>')
shell_head = shell_head.replace('<link rel="stylesheet" href="../v03.css">',
                                '<link rel="stylesheet" href="../../src/styles/v03.css"><link rel="stylesheet" href="../../src/styles/design-requests.css">')

E = escape

# ── fixture ──
EVENTS = [
    # id, mon, day, dow, name, status, host, need, sub
    ('founder', 'Nov', '6', 'Fri', 'Founder stories', 'Published', 'Events · Maya Chen', 'requested', 'In progress · Jordan'),
    ('pitch', 'Nov', '12', 'Thu', 'Pitch practice night', 'Draft', 'Programs · Sam Patel', 'needs', ''),
    ('venture', 'Nov', '14', 'Sat', 'Fall venture night', 'Published', 'Events · Maya Chen', 'requested', 'New · unassigned'),
    ('coffee', 'Nov', '19', 'Thu', 'Coffee & cofounders', 'Published', 'Community · Ana Ruiz', 'done', 'Delivered Oct 30'),
    ('social', 'Nov', '25', 'Wed', 'Officer social', 'Draft', 'Leadership · Alex Kim', 'not-needed', ''),
    ('demo', 'Dec', '3', 'Thu', 'Demo day kickoff', 'Draft', 'Programs · Sam Patel', 'needs', ''),
    ('mixer', 'Dec', '10', 'Thu', 'Winter mixer', 'Draft', 'No host yet', 'needs', ''),
]
NEED_LABEL = {'needs': 'Needs design', 'requested': 'Requested', 'done': 'Done', 'not-needed': 'Not needed'}

# id, title, type, hue, stage, due label, due state, urgent, event, event when, assignee, mine
CARDS = [
    ('c1', 'Venture night poster', 'Poster', 'blue', 'new', 'Nov 7', 'ok', True, 'Fall venture night', 'Nov 14', None, False),
    ('c2', 'Speaker headshots carousel', 'Social', 'violet', 'new', 'Nov 3', 'soon', False, 'Founder stories', 'Nov 6', None, False),
    ('c3', 'Mentor coffee social', 'Social', 'violet', 'accepted', 'Nov 9', 'ok', False, None, None, ('JL', 'Jordan Lee', 'teal'), True),
    ('c4', 'Founder stories poster', 'Poster', 'blue', 'in-progress', 'Oct 30', 'overdue', True, 'Founder stories', 'Nov 6', ('JL', 'Jordan Lee', 'teal'), True),
    ('c5', 'Workshop slide template', 'Slides', 'amber', 'in-progress', 'Nov 20', 'ok', False, None, None, ('RS', 'Riley Shah', 'coral'), False),
    ('c6', 'Sticker sheet', 'Merch', 'teal', 'in-review', 'Nov 4', 'soon', False, None, None, ('RS', 'Riley Shah', 'coral'), False),
    ('c7', 'Coffee & cofounders story', 'Social', 'violet', 'delivered', 'Oct 30', 'ok', False, 'Coffee & cofounders', 'Nov 19', ('JL', 'Jordan Lee', 'teal'), True),
]
STAGES = [('new', 'New'), ('accepted', 'Accepted'), ('in-progress', 'In progress'), ('in-review', 'In review'), ('delivered', 'Delivered')]


def rail_event(e):
    eid, mon, day, dow, name, status, host, need, sub = e
    sub_html = f' <span class="design-need-sub">· {E(sub)}</span>' if sub else ''
    actions = []
    if need == 'needs':
        actions.append(f'<button class="btn" data-request="{eid}">Request design</button>')
        actions.append(f'<button class="linkish" data-not-needed="{eid}">Mark not needed</button>')
    elif need == 'requested':
        actions.append('<a class="linkish" href="#board">Open request</a>')
        actions.append(f'<button class="linkish" data-request="{eid}">Request another</button>')
    elif need == 'done':
        actions.append('<a class="linkish" href="#board">See files</a>')
    else:
        actions.append(f'<button class="linkish" data-needed="{eid}">Needs design after all</button>')
    return (f'<li class="rail-event" data-need="{need}" data-event="{eid}">'
            f'<div class="rail-date" aria-hidden="true"><span class="rail-date-mon">{mon}</span><span class="rail-date-day">{day}</span><span class="rail-date-dow">{dow}</span></div>'
            f'<p class="rail-title"><a href="events.html">{E(name)}</a></p>'
            f'<p class="rail-sub"><span class="visually-hidden">{dow} {mon} {day} · </span><span class="rail-status" data-status="{status.lower()}">{status}</span><span>{E(host)}</span></p>'
            f'<p class="rail-sub"><span class="design-need" data-need="{need}">{NEED_LABEL[need]}{sub_html}</span></p>'
            f'<div class="rail-actions">{"".join(actions)}</div></li>')


def card(c):
    cid, title, typ, hue, stage, due, due_state, urgent, event, when, who, mine = c
    due_cls = {'soon': ' is-soon', 'overdue': ' is-overdue'}.get(due_state, '')
    due_txt = due + (' · Overdue' if due_state == 'overdue' else ' · Due soon' if due_state == 'soon' else '')
    if stage == 'delivered':
        due_cls, due_txt = '', 'Delivered Oct 30'
    flag = '<span class="urgent-flag" title="High urgency: the date cannot move">High</span>' if urgent and stage != 'delivered' else ''
    ev = (f'<p class="kanban-event"><span aria-hidden="true">◷</span><a href="events.html">{E(event)}</a><span class="kanban-event-when">· {when}</span></p>' if event else '')
    person = (f'<span class="kanban-people"><span class="avatar avatar--sm" data-hue="{who[2]}" aria-hidden="true">{who[0]}</span><span class="kanban-people-name">{E(who[1])}</span></span>'
              if who else '<span class="kanban-unassigned">Unassigned</span>')
    return (f'<li class="kanban-card{" is-mine" if mine else ""}" data-card="{cid}" data-type="{typ.lower()}" data-due="{due_state}" data-mine="{int(mine)}" data-unassigned="{int(who is None)}">'
            f'<div class="kanban-card-top"><p class="kanban-card-title"><a href="#">{E(title)}</a></p>{flag}</div>'
            f'<div class="kanban-card-meta"><span class="kanban-tag" data-hue="{hue}">{typ}</span><span class="due-chip{due_cls}">{due_txt}</span></div>'
            f'{ev}<div class="kanban-card-foot">{person}<button class="card-actions" aria-haspopup="menu" aria-label="Actions for {E(title)}">Move<span class="card-actions-chev" aria-hidden="true">▾</span></button></div></li>')


def board():
    cols = []
    for key, label in STAGES:
        items = [c for c in CARDS if c[4] == key]
        body = ''.join(card(c) for c in items)
        empty = '' if items else '<p class="kanban-empty">Nothing here</p>'
        cols.append(f'<section class="kanban-col" data-stage="{key}"><div class="kanban-col-head"><h3 class="kanban-col-name">{label}</h3><span class="kanban-count">{len(items)}</span></div><ul class="kanban-list">{body}</ul>{empty}</section>')
    return f'<div id="board" class="kanban" role="region" tabindex="0" aria-label="Design request board">{"".join(cols)}</div>'


def mywork():
    mine = [c for c in CARDS if c[11] and c[4] != 'delivered']
    review = [c for c in CARDS if c[0] == 'c6']
    return ('<div id="mywork" class="mywork" hidden>'
            f'<section class="mywork-section"><h3 class="mywork-head">Assigned to you <span class="kanban-count">{len(mine)}</span></h3><ul class="mywork-list">{"".join(card(c) for c in mine)}</ul></section>'
            f'<section class="mywork-section"><h3 class="mywork-head">Waiting on your approval <span class="kanban-count">{len(review)}</span></h3><ul class="mywork-list">{"".join(card(c) for c in review)}</ul></section>'
            '<section class="mywork-section"><h3 class="mywork-head">Up for grabs <span class="kanban-count">2</span></h3><p class="mywork-empty">2 unassigned requests are on the board. Filter by <button class="linkish" data-filter-jump="unassigned">Unassigned</button> to pick one up.</p></section>'
            '</div>')


needs = sum(1 for e in EVENTS if e[7] == 'needs')
main = f'''<main class="main" id="main" tabindex="-1"><header class="page-header"><div><h1>Design</h1><p class="page-sub">What the design team is making, and what's coming up next.</p></div></header>
<div class="stack">
<section class="section stack"><h2>Preview controls</h2><div class="action-row"><label class="field"><span class="field-label">Viewer</span><select aria-label="Viewer" class="select" id="viewer"><option>Designer (Jordan Lee)</option><option>Officer</option><option>Director</option></select></label></div><p class="small muted">Fixture date Monday, Nov 2 2026. Filters, views and the rail's "Mark not needed" work in this prototype; nothing is saved.</p></section>
<p id="result" role="status" class="small"></p>

<div class="toolbar"><div class="toolbar-main"><button class="btn btn--primary" data-request="">New design request</button></div>
<div class="segmented" role="group" aria-label="Board view"><button class="segment" aria-pressed="true" data-view="board">Board</button><button class="segment" aria-pressed="false" data-view="list">List</button><button class="segment" aria-pressed="false" data-view="mywork">My work</button></div></div>

<section class="design-rail" aria-labelledby="rail-title">
<div class="design-rail-head"><h2 class="design-rail-title" id="rail-title">Upcoming events <span class="brand-eyebrow">next six weeks</span></h2>
<p class="design-rail-note" id="rail-note"><strong>{needs} need design</strong> · 2 requested · 1 done · 1 not needed</p></div>
<ul class="design-rail-list" tabindex="0" aria-label="Upcoming events and their design status">{"".join(rail_event(e) for e in EVENTS)}</ul>
</section>

<div class="design-summary" role="group" aria-label="Filter requests">
<div class="design-filters">
<button class="design-filter" aria-pressed="true" data-filter="all">All open <span class="design-filter-n">6</span></button>
<button class="design-filter" aria-pressed="false" data-filter="mine">Mine <span class="design-filter-n">2</span></button>
<button class="design-filter" aria-pressed="false" data-filter="unassigned">Unassigned <span class="design-filter-n">2</span></button>
<button class="design-filter" aria-pressed="false" data-filter="due">Due soon <span class="design-filter-n is-alert">3</span></button>
</div>
<label class="design-type"><span>Type</span><select class="select" id="type-filter"><option value="">All types</option><option value="poster">Poster</option><option value="social">Social</option><option value="slides">Slides</option><option value="merch">Merch</option></select></label>
</div>
<p class="small muted design-meta">7 requests · 1 overdue · Shared with everyone · Deadlines shown in Vancouver time</p>
{board()}
<div id="list" hidden><p class="small muted">The list keeps the V0.3 table (Request, Stage, Due, Designer, Sizes) with the same filters applied.</p></div>
{mywork()}
</div></main></div>'''

script = '''<script>
const res=document.getElementById('result');
document.querySelectorAll('[data-view]').forEach(b=>b.onclick=()=>{document.querySelectorAll('[data-view]').forEach(x=>x.setAttribute('aria-pressed',x===b));for(const id of ['board','list','mywork'])document.getElementById(id).hidden=b.dataset.view!==id});
let filter='all';
function apply(){const t=document.getElementById('type-filter').value;document.querySelectorAll('#board .kanban-card').forEach(c=>{const d=c.dataset;const ok=(filter==='all'||(filter==='mine'&&d.mine==='1')||(filter==='unassigned'&&d.unassigned==='1')||(filter==='due'&&(d.due==='soon'||d.due==='overdue')))&&(!t||d.type===t);c.hidden=!ok});document.querySelectorAll('#board .kanban-col').forEach(col=>col.querySelector('.kanban-count').textContent=col.querySelectorAll('.kanban-card:not([hidden])').length)}
document.querySelectorAll('[data-filter]').forEach(b=>b.onclick=()=>{filter=b.dataset.filter;document.querySelectorAll('[data-filter]').forEach(x=>x.setAttribute('aria-pressed',x===b));apply()});
document.getElementById('type-filter').onchange=apply;
document.querySelectorAll('[data-filter-jump]').forEach(b=>b.onclick=()=>{document.querySelector('[data-view=board]').click();document.querySelector('[data-filter=unassigned]').click()});
document.querySelectorAll('[data-request]').forEach(b=>b.onclick=()=>{res.textContent=b.dataset.request?'Opens New design request, prefilled from the event (title, due a week before, brief, link).':'Opens New design request.'});
function setNeed(id,need){const li=document.querySelector(`[data-event="${id}"]`);li.dataset.need=need;const n=li.querySelector('.design-need');n.dataset.need=need;n.textContent=need==='needs'?'Needs design':'Not needed';li.querySelector('.rail-actions').innerHTML=need==='needs'?`<button class="btn" data-request="${id}">Request design</button><button class="linkish" data-not-needed="${id}">Mark not needed</button>`:`<button class="linkish" data-needed="${id}">Needs design after all</button>`;wire();res.textContent=li.querySelector('.rail-title').textContent+(need==='needs'?' needs design again.':' marked as not needing design.')}
function wire(){document.querySelectorAll('[data-not-needed]').forEach(b=>b.onclick=()=>setNeed(b.dataset.notNeeded,'not-needed'));document.querySelectorAll('[data-needed]').forEach(b=>b.onclick=()=>setNeed(b.dataset.needed,'needs'))}
wire();
document.getElementById('viewer').onchange=e=>{const officer=e.target.selectedIndex===1;document.querySelectorAll('[data-not-needed]').forEach(b=>b.hidden=officer);res.textContent=officer?'Officer view: "Mark not needed" shows only on events you host.':''};
</script>'''

out = shell_head.replace('</head>', '</head>', 1) + main + shell_script + script + '</body></html>\n'
(DIR / 'mockups' / 'design-v04.html').write_text(out)
print('wrote mockups/design-v04.html')
