"""V0.3 component mockup: web/design/mockups/components-v03.html.

Reuses the v2 builder's shell and helpers (running it also regenerates the v2 pages, which is
deterministic), then adds the proposed v03.css and the V0.3 nav. Run: python3 web/design/build-v03.py
"""
from pathlib import Path
import runpy

root = Path(__file__).resolve().parent
ns = runpy.run_path(str(root / 'build-mockups.py'))
shell, icon, field, button = ns['shell'], ns['icon'], ns['field'], ns['button']
g = shell.__globals__
g['paths'].update({
    'users': 'M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM2 21a7 7 0 0 1 14 0M16 3.5a4 4 0 0 1 0 7.5M22 21a7 7 0 0 0-4.5-6.5',
    'mic': 'M12 3a3 3 0 0 0-3 3v6a3 3 0 0 0 6 0V6a3 3 0 0 0-3-3zM5 11a7 7 0 0 0 14 0M12 18v3',
    'palette': 'M12 3a9 9 0 1 0 0 18c1.1 0 1.5-.8 1.5-1.5 0-1-.8-1.4-.8-2.3 0-.9.7-1.7 1.7-1.7H17a4 4 0 0 0 4-4C21 6.6 17 3 12 3zM7.5 11h.01M10 7.5h.01M15 7.5h.01',
    'chart': 'M4 20V10M10 20V4M16 20v-7M2 20h20',
    'globe': 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18',
    'gauge': 'M12 14l4-4M3.5 18a9 9 0 1 1 17 0',
    'database': 'M4 6c0-1.7 3.6-3 8-3s8 1.3 8 3-3.6 3-8 3-8-1.3-8-3zM4 6v12c0 1.7 3.6 3 8 3s8-1.3 8-3V6M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3',
    'chevron-down': 'M6 9l6 6 6-6', 'chevron-left': 'M15 6l-6 6 6 6', 'chevron-right': 'M9 6l6 6-6 6',
    'copy': 'M9 9h11v11H9zM5 15H4V4h11v1',
    'link': 'M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1',
    'send': 'M21 3 10 14M21 3l-7 18-4-7-7-4z',
    'edit': 'M4 20h4L19 9l-4-4L4 16zM14 6l4 4',
    'grid': 'M4 4h7v7H4zM13 4h7v7h-7zM4 13h7v7H4zM13 13h7v7h-7z',
    'arrow-right': 'M5 12h14M13 6l6 6-6 6',
})
g['nav'] = [('home', 'Home', 'home'), ('events', 'Events', 'calendar'), ('officers', 'Officers', 'users'),
            ('members', 'Members', 'user'), ('speakers', 'Speakers', 'mic'), ('design', 'Design', 'palette'),
            ('finance', 'Requests & Finance', 'wallet'), ('admin', 'Admin', 'shield')]

GRIP = '<svg class="icon" width="16" height="16" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">' + ''.join(
    f'<circle cx="{x}" cy="{y}" r="1.4"/>' for x in (5.5, 10.5) for y in (3.5, 8, 12.5)) + '</svg>'
LOGO = ('<img class="wordmark-logo wordmark-logo--ink" src="../../public/images/cseed-logo.png" alt="CSEED">'
        '<img class="wordmark-logo wordmark-logo--white" src="../../public/images/cseed-logo-white.png" alt="CSEED">')


def section(id, title, sub, body):
    return f'<section class="section" id="{id}" aria-labelledby="{id}-h"><div class="section-head"><h2 id="{id}-h">{title}</h2><span class="small muted">{sub}</span></div>{body}</section>'


def demo(label, body, cls=''):
    return f'<div class="demo {cls}"><p class="demo-label">{label}</p>{body}</div>'


def role(r, sm=False):
    return f'<span class="role-badge{" role-badge--sm" if sm else ""}" data-role="{r}">{r}</span>'


def avatar(initials, hue, size='sm'):
    return f'<span class="avatar avatar--{size}" data-hue="{hue}" aria-hidden="true">{initials}</span>'


def spark(points, w=150, h=44):
    step = w / (len(points) - 1)
    lo, hi = min(points), max(points)
    pts = [(i * step, h - 4 - (p - lo) / (hi - lo or 1) * (h - 8)) for i, p in enumerate(points)]
    d = 'M' + 'L'.join(f'{x:.1f} {y:.1f}' for x, y in pts)
    x, y = pts[-1]
    return f'<svg class="sparkline" width="{w}" height="{h}" viewBox="0 0 {w} {h}" aria-hidden="true"><path class="sparkline-line" d="{d}"/><circle class="sparkline-dot" cx="{x:.1f}" cy="{y:.1f}" r="3"/></svg>'


# ───────────── 1. Typography and tokens ─────────────
type_rows = [
    ('Display', '<span class="display display--m">Playfair Display</span>', '400 (italic for one word at most) · 56 / 36 / 28 / 22px · −0.035 to −0.01em', 'Onboarding headlines, gallery title, brand empty states, the Home greeting'),
    ('Brand body', '<span style="font-family:var(--font-brand)">Poppins</span>', '400 / 500 · 16px, line 1.65 · never below 14px', 'Running copy on onboarding and the gallery intro only'),
    ('Brand mono', '<span class="brand-eyebrow">anonymous pro</span>', '400 · 13px lowercase · +0.02em', 'Step counters, eyebrows, polaroid captions'),
    ('Work', '<span>System sans</span>', '400 / 500 / 600 · v2 scale unchanged', 'Everything else: tables, forms, Kanban, calendar, widgets, nav'),
    ('Identifiers', '<span class="mono">ui-monospace</span>', '400 · 11–13px', 'Discord IDs, tokens, invite links'),
]
type_table = '<div class="table-wrap"><table class="dt"><caption class="visually-hidden">Typography roles</caption><thead><tr><th scope="col">Role</th><th scope="col">Face</th><th scope="col">Weights and sizes</th><th scope="col">Used on</th></tr></thead><tbody>' + ''.join(
    f'<tr><td data-label="Role" class="dt-primary"><span class="cell-title">{a}</span></td><td data-label="Face">{b}</td><td data-label="Weights and sizes" class="small">{c}</td><td data-label="Used on" class="small">{d}</td></tr>' for a, b, c, d in type_rows) + '</tbody></table></div>'
brand_sample = ('<div class="demo-paper"><p class="brand-eyebrow">brand surface · onboarding, gallery, empty states</p>'
                '<p class="display display--xl" style="margin:8px 0 12px">hey alex, <span class="mark">welcome in.</span></p>'
                '<p class="brand-lede" style="margin:0">Six short steps and your dashboard is set up the way you like it. Paper ground, ink type, one yellow stroke.</p></div>')
work_sample = ('<div class="demo-work"><p class="eyebrow">work surface · tables, Kanban, calendar, widgets</p><h1 style="margin:6px 0 4px">Members</h1>'
               '<p class="page-sub" style="margin-bottom:12px">1,086 people from cseed.co/join · synced 06:00</p>'
               '<div class="table-wrap"><table class="dt dt--dense"><caption class="visually-hidden">Sample members</caption><thead><tr><th scope="col">Name</th><th scope="col">Program</th><th scope="col" class="num">Joined</th></tr></thead><tbody>'
               '<tr><td data-label="Name" class="dt-primary"><span class="cell-title">Priya Natarajan</span></td><td data-label="Program">buildspace</td><td data-label="Joined" class="num">Oct 30</td></tr>'
               '<tr><td data-label="Name" class="dt-primary"><span class="cell-title">Marcus Oyelaran</span></td><td data-label="Program">saturdays</td><td data-label="Joined" class="num">Oct 29</td></tr></tbody></table></div></div>')
swatches = [('--paper', 'Brand ground'), ('--paper-raised', 'Fields, raised paper'), ('--brand-ink', 'Brand text'), ('--brand-ink-soft', 'Brand secondary text'),
            ('--brand-control', 'Field borders on paper'), ('--brand-line', 'Decorative rules, progress track'), ('--seed', 'Highlighter stroke'),
            ('--role-leadership-bg', 'Leadership badge'), ('--polaroid', 'Photo frame')]
swatch_html = '<ul class="demo-swatches">' + ''.join(
    f'<li><span class="demo-swatch" style="background:var({t})"></span><span><code class="mono small">{t}</code><span class="cell-sub">{d}</span></span></li>' for t, d in swatches) + '</ul>'
typo = section('type', '1 · Brand typography and tokens', 'Brand-forward on brand surfaces, calm on work surfaces',
               '<div class="demo-grid">' + demo('Brand surface', brand_sample) + demo('Work surface', work_sample) + '</div>' + type_table +
               demo('New tokens (values switch with the theme; ratios in contrast.md)', swatch_html))

# ───────────── 2. Role badges ─────────────
people_rows = [('JK', 'teal', 'Jordan Kim', 'President · Operations', 'leadership'), ('AC', 'violet', 'Alex Chen', 'Director · Community', 'director'), ('MP', 'amber', 'Maya Patel', 'Events officer · Community', 'officer')]
roles_table = '<div class="table-wrap"><table class="dt dt--people"><caption class="visually-hidden">Officers with roles</caption><thead><tr><th scope="col">Person</th><th scope="col" class="people-role">Role</th></tr></thead><tbody>' + ''.join(
    f'<tr><td class="people-who-cell" data-label="Person"><span class="people-who">{avatar(i, h, "md")}<span class="people-text"><span class="cell-title">{n}</span><span class="cell-sub">{t}</span></span></span></td><td class="people-role" data-label="Role">{role(r)}</td></tr>' for i, h, n, t, r in people_rows) + '</tbody></table></div>'
toggle = ('<div class="role-toggle" role="group" aria-label="Role for Maya Patel">'
          '<button class="role-toggle-opt" data-hue="blue" aria-pressed="true">officer</button>'
          '<button class="role-toggle-opt" data-hue="violet" aria-pressed="false">director</button>'
          '<button class="role-toggle-opt" data-role="leadership" aria-pressed="false" disabled aria-describedby="lead-hint">leadership</button></div>'
          '<p class="field-hint" id="lead-hint">Only leadership can grant leadership.</p>')
chip = f'<a class="user-chip" href="#roles" style="max-width:240px">{avatar("JK", "teal", "md")}<span class="user-chip-text"><span class="user-chip-name">Jordan Kim</span><span class="user-chip-role">{role("leadership", True)}</span></span></a>'
roles = section('roles', '2 · Role badges', 'leadership > director > officer',
                '<div class="demo-grid demo-grid--3">' + demo('All three', '<div class="dot-labels" style="gap:8px">' + role('officer') + role('director') + role('leadership') + '</div>') +
                demo('Sidebar user chip (small)', chip) + demo('Role toggle as a director sees it', toggle) + '</div>' + roles_table)

# ───────────── 3. Widgets ─────────────
W = {
    'analytics': dict(title='Web Analytics', icon='globe', hue='blue', size='m', body='<div class="widget-split"><div><div class="widget-figure">12.4k</div><div class="widget-sub">visitors · last 7 days</div></div>' + spark([31, 28, 40, 36, 52, 47, 61, 58]) + '</div>', foot='<span>cseed.co</span><span>+18% vs prior week</span>'),
    'signups': dict(title='Member signups', icon='users', hue='green', size='s', body='<div class="widget-figure">1,086</div><div class="widget-sub">+23 this week</div>'),
    'next': dict(title='Next event', icon='calendar', hue='amber', size='s', body='<div class="widget-figure" style="font-size:22px;letter-spacing:-.02em">Thu, Nov 6</div><div class="widget-sub">Founder stories II · The Nest · 64 going</div>'),
    'requests': dict(title='Requests needing action', icon='inbox', hue='violet', size='l', link='Review →', body='<ul class="widget-list">' + ''.join(f'<li><span class="list-main"><span class="cell-title">{a}</span><span class="cell-sub">{b}</span></span><span class="badge badge--attention">{c}</span></li>' for a, b, c in [('Venture night catering', 'REQ-0042 · Maya · $286.40', 'Pending'), ('Workshop materials', 'REQ-0041 · Jordan · $124.80', 'Pending'), ('Luma cover · Founder stories II', 'Design · due Nov 4', 'In review'), ('Speaker dinner deposit', 'REQ-0043 · Alex · $150.00', 'Pending')]) + '</ul>', foot='<span>4 waiting on you</span><span>Updated 2 min ago</span>'),
    'traffic': dict(title='Traffic & security', icon='shield', hue='teal', size='m', body='<div class="widget-state"><p class="widget-state-title">Not connected</p><p class="widget-state-body">Add a read-only Cloudflare token to see requests and threats blocked. Nothing is counted until then.</p><a class="small" href="#widgets">How to connect →</a></div>'),
    'perf': dict(title='Performance', icon='gauge', hue='coral', size='s', body='<div class="widget-figure">1.8s</div><div class="widget-sub">LCP p75 · Good</div>'),
}


def widget(key, i=0, edit=False, size=None, lifted=False):
    w = W[key]
    size = size or w['size']
    link = f'<a class="widget-link" href="#widgets">{w["link"]}</a>' if w.get('link') else ''
    foot = f'<div class="widget-foot">{w["foot"]}</div>' if w.get('foot') else ''
    sizes = ''.join(f'<button class="widget-size" aria-pressed="{str(s == size).lower()}">{s.upper()}</button>' for s in 'sml')
    ed = (f'<div class="widget-edit"><button class="widget-handle" aria-label="Move {w["title"]}. Arrow keys move it, Enter drops it">{GRIP}</button>'
          f'<span class="widget-label">{w["title"]}</span><button class="widget-remove" aria-label="Remove {w["title"]}">{icon("close")}</button>'
          f'<div class="widget-sizes" role="group" aria-label="{w["title"]} size">{sizes}</div></div>') if edit else ''
    return (f'<li class="widget widget--{size}{" is-lifted" if lifted else ""}" data-hue="{w["hue"]}" style="--i:{i}" aria-label="{w["title"]}">'
            f'<div class="widget-head"><span class="widget-icon">{icon(w["icon"])}</span><span class="widget-title">{w["title"]}</span>{link}</div>'
            f'<div class="widget-body">{w["body"]}</div>{foot}{ed}</li>')


order = ['analytics', 'signups', 'next', 'requests', 'traffic', 'perf']
normal = '<ul class="widget-grid">' + ''.join(widget(k) for k in order) + '</ul>'
edit_bar = ('<div class="edit-bar" role="region" aria-label="Edit home"><div class="edit-bar-text"><span class="edit-bar-title">Editing your home</span>'
            '<span class="edit-bar-hint">Drag a handle to move · S, M or L to resize · saved to your account</span></div>'
            '<div class="action-row">' + button(icon('plus') + ' Add widget', False, 'data-open="gallery"') + button('Done', True) + '</div></div>')
editing = ('<ul class="widget-grid is-editing">' + widget('analytics', 0, True) + widget('signups', 1, True) + widget('next', 2, True, lifted=True) +
           widget('requests', 3, True) + '<li class="widget-slot" style="--span-c:1;--span-r:1" aria-hidden="true"></li>' + widget('traffic', 4, True) + widget('perf', 5, True) +
           '<li><button class="widget-add" data-open="gallery" style="width:100%;height:100%"><span class="widget-add-plus">' + icon('plus') + '</span>Add widget<span class="widget-add-sub">From the gallery</span></button></li></ul>'
           '<p class="visually-hidden" role="status">Next event, picked up. Position 3 of 6.</p>')
gal_items = [('analytics', 'Web Analytics', 'Visitors and top pages on cseed.co. On by default.', -1.2, True, 'm'), ('traffic', 'Traffic & security', 'Requests, cache rate and threats blocked.', .8, False, 'm'),
             ('perf', 'Performance', 'Core Web Vitals from real visits.', -.6, False, 's'), ('signups', 'Member signups', 'New people from cseed.co/join, from D1.', 1.1, True, 's'),
             ('next', 'Next event', 'The next event, its venue and RSVPs.', -.9, False, 's'), ('requests', 'Requests needing action', 'Purchase and design requests waiting on you.', .6, False, 'l')]


def gallery_body():
    items = ''.join(
        f'<li class="gallery-item{" is-added" if added else ""}" style="--tilt:{tilt}deg"><div class="gallery-preview" aria-hidden="true"><ul class="widget-grid" style="grid-template-columns:1fr;grid-auto-rows:auto">{widget(k, size="m" if size=="l" else size).replace("widget--s", "widget--m")}</ul></div>'
        f'<div><p class="gallery-name">{name}</p><p class="gallery-sub">{sub}</p></div><div class="gallery-actions"><div class="widget-sizes" role="group" aria-label="{name} size">' +
        ''.join(f'<button class="widget-size" aria-pressed="{str(s == size).lower()}">{s.upper()}</button>' for s in 'sml') + '</div>' +
        (f'<span class="gallery-added">{icon("check")} On your home</span>' if added else button(icon('plus') + ' Add', False, 'class="btn btn--small"').replace('class="btn" class="btn btn--small"', 'class="btn btn--small"')) + '</div></li>'
        for k, name, sub, tilt, added, size in gal_items)
    return ('<div class="gallery-inner"><header class="gallery-head"><div><p class="brand-eyebrow" style="margin:0 0 4px">widget gallery</p><h2 class="display display--l" id="gallery-title">make home <span class="mark">yours.</span></h2></div>'
            f'<button class="icon-btn" aria-label="Close gallery" data-close>{icon("close")}</button>'
            '<p class="brand-lede">Pick what you want to see first. Widgets that need Cloudflare show “Not connected” until a token is added.</p></header>'
            '<div class="gallery-cats" role="group" aria-label="Categories"><button class="chip" aria-pressed="true">All</button><button class="chip" aria-pressed="false">Cloudflare</button><button class="chip" aria-pressed="false">Members</button><button class="chip" aria-pressed="false">Events</button><button class="chip" aria-pressed="false">Finance</button><button class="chip" aria-pressed="false">Design</button></div>'
            f'<div class="gallery-body"><ul class="gallery-grid">{items}</ul></div></div>')


widgets = section('widgets', '3 · Home widgets', 'S = 1×1 · M = 2×1 · L = 2×2 on a 4-column grid (2 on phone)',
                  demo('Normal', normal) + demo('Edit mode · “Next event” lifted, its slot dashed, add tile last', '<div class="stack--tight" style="display:grid;gap:12px">' + edit_bar + editing + '</div>') +
                  demo('Widget gallery sheet', f'<div class="gallery gallery--inline" role="region" aria-labelledby="gallery-title">{gallery_body()}</div>'))

# ───────────── 4. Kanban + calendar ─────────────
def kcard(title, tag, hue, due, due_cls, people, link='', dragging=False):
    ppl = ''.join(avatar(i, h) for i, h in people) if people else '<span class="kanban-unassigned">Unassigned</span>'
    return (f'<li class="kanban-card{" is-dragging" if dragging else ""}" tabindex="0" aria-roledescription="draggable card"><p class="kanban-card-title"><a href="#board">{title}</a></p>'
            f'<div class="kanban-card-meta"><span class="kanban-tag" data-hue="{hue}">{tag}</span><span class="kanban-due {due_cls}">{due}</span></div>'
            + (f'<span class="kanban-link">{icon("link")} {link}</span>' if link else '') + f'<div class="kanban-card-foot"><span class="kanban-people">{ppl}</span><button class="icon-btn row-menu-btn" aria-label="Move or edit {title}">⋯</button></div></li>')


cols = [
    ('new', 'New', [kcard('Luma cover · Founder stories II', 'Event cover', 'amber', 'Due Nov 12', '', [], 'Founder stories II'), kcard('Instagram carousel · buildher recap', 'Social post', 'blue', 'Due Nov 20', '', [], 'buildher')]),
    ('accepted', 'Accepted', []),
    ('in-progress', 'In progress', [kcard('Venture night poster', 'Poster', 'violet', 'Due Nov 8', 'is-soon', [('LW', 'teal')], 'Fall venture night'), kcard('Saturdays sticker sheet', 'Merch', 'teal', 'Due Nov 15', '', [('LW', 'teal'), ('RS', 'coral')], '', True)]),
    ('in-review', 'In review', [kcard('Speaker card · Dana Ruiz', 'Social post', 'blue', 'Overdue · Oct 30', 'is-overdue', [('RS', 'coral')], 'Founder stories II')]),
    ('delivered', 'Delivered', [kcard('Fall recap reel', 'Video', 'green', 'Delivered Oct 27', '', [('LW', 'teal')])]),
]


def kcol(stage, name, cards):
    inner = ''.join(cards) if cards else '<li><p class="kanban-empty">Nothing here yet. Designers pick up requests from New.</p></li>'
    if stage == 'in-review':
        inner = '<li class="kanban-drop" aria-hidden="true"></li>' + inner
    return f'<section class="kanban-col" data-stage="{stage}" aria-label="{name}"><header class="kanban-col-head"><h3 class="kanban-col-name">{name}</h3><span class="kanban-count">{len(cards)}</span></header><ul class="kanban-list">{inner}</ul></section>'


board = ('<div class="toolbar"><div class="chips" role="group" aria-label="Filter"><button class="chip" aria-pressed="true">All <span class="chip-count">6</span></button><button class="chip" aria-pressed="false">Mine</button><button class="chip" aria-pressed="false">Unassigned <span class="chip-count">2</span></button></div>'
         + button(icon('plus') + ' New design request', True) + '</div><div class="kanban" role="region" aria-label="Design requests board" tabindex="0">' + ''.join(kcol(*c) for c in cols) + '</div>')
brand_empty = ('<div class="state state--brand"><div class="state-photos" aria-hidden="true"><figure class="polaroid" style="--tilt:-6deg"><img src="../img/community-tall.webp" alt=""></figure><figure class="polaroid" style="--tilt:5deg"><img src="../img/buildher-tall.webp" alt=""></figure></div>'
               '<p class="state-title">No design requests yet.</p><p class="state-body">Need a cover, a poster or a post? Ask the design team; they pick it up from here.</p><div class="state-action">' + button('New design request') + '</div></div>')

HUES = {'Community': 'blue', 'Entrepreneurship': 'amber', 'Saturdays': 'teal', 'Design': 'violet', 'Flagship': 'coral'}
ev = {4: [('6:00', 'Coffee & cofounders', 'Community', 0)], 6: [('6:30', 'Founder stories II', 'Entrepreneurship', 0)], 8: [('10:00', 'Saturdays', 'Saturdays', 0)],
      13: [('5:30', 'Design for people II', 'Design', 0), ('7:30', 'Speaker dinner', 'Entrepreneurship', 1), ('8:00', 'Officer social', 'Community', 0)],
      15: [('10:00', 'Saturdays', 'Saturdays', 0)], 20: [('6:00', 'Fall venture night', 'Flagship', 0)], 22: [('10:00', 'Saturdays', 'Saturdays', 0)],
      26: [('6:00', 'buildher social', 'Community', 1)], 29: [('10:00', 'Saturdays', 'Saturdays', 0)]}


def cal_event(t, title, team, draft):
    return f'<a class="cal-event{" is-draft" if draft else ""}" data-hue="{HUES[team]}" href="#calendar"><span class="cal-event-time">{t}</span><span class="cal-event-title">{title}{" (draft)" if draft else ""}</span></a>'


days = list(range(26, 32)) + list(range(1, 31)) + list(range(1, 7))
cells = ''
for i, d in enumerate(days):
    inside = 6 <= i < 36
    items = ev.get(d, []) if inside else []
    shown = items[:2]
    more = f'<button class="cal-more">+{len(items) - 2} more</button>' if len(items) > 2 else ''
    dots = '<span class="cal-dots" aria-hidden="true">' + ''.join(f'<span data-hue="{HUES[x[2]]}"></span>' for x in items) + '</span>' if items else ''
    label = f'{"November" if inside else ("October" if i < 6 else "December")} {d}' + (f', {len(items)} events' if items else '')
    cls = ' is-outside' if not inside else ''
    cls += ' is-today' if inside and d == 1 else ''
    cls += ' is-selected' if inside and d == 13 else ''
    cells += f'<div class="cal-day{cls}" role="gridcell" aria-label="{label}"><span class="cal-date">{d}</span>{"".join(cal_event(*x) for x in shown)}{more}{dots}</div>'
dows = ''.join(f'<div class="cal-dow" role="columnheader">{d}</div>' for d in ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'])
month = f'<div class="cal-month" role="grid" aria-label="November 2026">{dows}{cells}</div>'
agenda = ('<ul class="cal-agenda" aria-label="Events on Friday, November 13"><li class="cal-agenda-day">Fri, Nov 13</li>' + ''.join(f'<li>{cal_event(*x)}</li>' for x in ev[13]) + '</ul>')
cal_head = ('<div class="cal-head"><div class="cal-nav"><button class="icon-btn" aria-label="Previous month">' + icon('chevron-left') + '</button><h3 class="cal-title">November 2026</h3>'
            '<button class="icon-btn" aria-label="Next month">' + icon('chevron-right') + '</button>' + button('Today', False, 'class="btn btn--small"').replace('class="btn" class="btn btn--small"', 'class="btn btn--small"') + '</div>'
            '<div class="segmented" role="group" aria-label="Calendar view"><button class="segment" aria-pressed="true">Month</button><button class="segment" aria-pressed="false">Week</button><button class="segment" aria-pressed="false">List</button></div></div>')
wdays = [('Mon', 26), ('Tue', 27), ('Wed', 28), ('Thu', 29), ('Fri', 30), ('Sat', 31), ('Sun', 1)]
blocks = {28: [(18, 4, 'Founder stories', '6:00–8:00 pm', 'amber')], 29: [(7, 2, 'Officer sync', '12:30–1:30 pm', 'blue')], 31: [(2, 6, 'Saturdays', '10:00 am–1:00 pm', 'teal')], 1: [(12, 2, 'Design crit', '3:00–4:00 pm', 'violet')]}
gutter = '<div class="cal-gutter" style="--rows:24">' + ''.join(f'<span class="cal-hour" style="grid-row:{(h - 9) * 2 + 1}">{(h - 1) % 12 + 1} {"am" if h < 12 else "pm"}</span>' for h in range(9, 21)) + '</div>'
wcols = ''.join(f'<div class="cal-col" style="--rows:24" aria-label="{n} {d}">' + ''.join(f'<div class="cal-block" data-hue="{hu}" style="--start:{s};--len:{l}"><span class="cal-block-title">{t}</span><span class="cal-block-time">{tm}</span></div>' for s, l, t, tm, hu in blocks.get(d, [])) + ('<div class="cal-now" style="--now:13.1" aria-label="Now, 3:34 pm"></div>' if d == 1 else '') + '</div>' for n, d in wdays)
week = ('<div class="cal-week" aria-label="Week of October 26">' + '<div class="cal-week-corner"></div>' + ''.join(f'<div class="cal-week-day{" is-today" if d == 1 else ""}">{n}<strong>{d}</strong></div>' for n, d in wdays) + gutter + wcols + '</div>')
calendar = demo('Month · today Nov 1, Nov 13 selected; phone shows dots and the selected day’s agenda', f'<div class="cal">{cal_head}{month}{agenda}</div>') + demo('Week · 9 am–9 pm, 30-minute rows (phone uses the agenda)', week)
work = section('board', '4 · Kanban and calendar', 'Calm and dense: system type, hairlines, hue dots only',
               demo('Design requests · a card being dragged, its drop slot in “In review”', board) + demo('Brand empty state (first visit, no requests)', brand_empty)) + section('calendar', 'Calendar grid', 'Events by team colour; drafts are outlined', calendar)

# ───────────── 5. Onboarding shell ─────────────
def onb_top(step):
    segs = ''.join(f'<li class="onb-seg{" is-done" if i < step else ""}"{" aria-current=step" if i == step else ""}></li>' for i in range(1, 7))
    return (f'<header class="onb-top"><span class="wordmark">{LOGO}</span><ol class="onb-progress" aria-label="Setup progress, step {step} of 6">{segs}</ol>'
            f'<span class="onb-count" aria-hidden="true">0{step} / 06</span></header>')


hello = (f'<div class="onb onb--framed" role="region" aria-label="Onboarding, step 1">{onb_top(1)}<div class="onb-main"><div class="onb-step">'
         '<p class="brand-eyebrow">hello from cseed</p><h2 class="display display--xl onb-title" tabindex="-1">hey alex, <span class="mark">welcome in.</span></h2>'
         '<p class="brand-lede onb-lede">Jordan added you to the officer team. Six short steps: check your details, tell us how to reach you, and pick what shows up on your home.</p>'
         '<ul class="onb-checks"><li>' + icon('check') + ' About three minutes</li><li>' + icon('check') + ' You can change everything later in Settings</li></ul></div>'
         '<div class="onb-art" aria-hidden="true"><figure class="polaroid" style="--tilt:-4deg;left:4%;top:6%;width:52%;height:62%"><img src="../img/buildher-tall.webp" alt=""><figcaption>buildher, fall ’26</figcaption></figure>'
         '<figure class="polaroid" style="--tilt:3deg;right:2%;top:22%;width:46%;height:52%"><img src="../img/community-tall.webp" alt=""><figcaption>saturdays</figcaption></figure></div></div>'
         '<footer class="onb-foot"><a class="onb-skip" href="#onboarding">Finish later</a>' + button('Let’s go ' + icon('arrow-right'), True) + '</footer></div>')
confirm = (f'<div class="onb onb--framed" role="region" aria-label="Onboarding, step 2">{onb_top(2)}<div class="onb-main onb-main--single"><div class="onb-step">'
           '<p class="brand-eyebrow">02 · is this you?</p><h2 class="display display--l onb-title" tabindex="-1">check what <span class="mark">leadership</span> filled in.</h2>'
           '<dl class="onb-prefill"><div><dt>Name</dt><dd>Alex Chen</dd></div><div><dt>Subteam</dt><dd>Community</dd></div><div><dt>Discord</dt><dd class="mono">284019573311</dd></div></dl>'
           '<div class="onb-form">' + field('Preferred name <span class="field-optional">· optional</span>', '<input class="input" value="Alex">') +
           '<div class="grid-fields">' + field('Pronouns <span class="field-optional">· optional</span>', '<input class="input" placeholder="e.g. they/them">') + field('Title <span class="field-optional">· optional</span>', '<input class="input" placeholder="Events director">') + '</div>'
           '<p class="field-hint">Something wrong with your name or subteam? <a href="#onboarding">Tell Jordan</a>; leadership can change it.</p></div></div></div>'
           '<footer class="onb-foot">' + button('Back', False, 'class="btn btn--text"').replace('class="btn" class="btn btn--text"', 'class="btn btn--text"') + button('Looks right', True) + '</footer></div>')
motion_rows = [('Step forward', 'Old step fades and moves 16px left (240ms). New step’s headline, lede and form rise 8px and slide in from 16px right, staggered 120ms (420ms each, ease-soft).', 'Instant swap. Focus moves to the step heading.'),
               ('Step back', 'Mirror of forward: content moves right.', 'Instant swap. Focus moves to the step heading.'),
               ('Progress', 'The current segment fills left to right (320ms).', 'Segment is filled on arrival. “Step 2 of 6” is in the list’s accessible name.'),
               ('Polaroids (step 1, done)', 'Settle from 40px below at 0° to their tilt, 900ms, staggered 120ms after a 200ms wait. Hover lifts 4px.', 'Shown at rest at their tilt. No hover lift.'),
               ('Highlighter', 'Static. It never draws or animates.', 'Same.'),
               ('Done step', 'Checklist items rise in sequence; nothing loops, no confetti.', 'All items shown at once.')]
motion = '<div class="table-wrap"><table class="dt"><caption class="visually-hidden">Onboarding motion rules</caption><thead><tr><th scope="col">Moment</th><th scope="col">Motion</th><th scope="col">Reduced motion</th></tr></thead><tbody>' + ''.join(
    f'<tr><td data-label="Moment" class="dt-primary"><span class="cell-title">{a}</span></td><td data-label="Motion" class="small">{b}</td><td data-label="Reduced motion" class="small">{c}</td></tr>' for a, b, c in motion_rows) + '</tbody></table></div>'
onb = section('onboarding', '5 · Onboarding shell', 'Full screen on paper · six steps: hello → confirm → profile → calendar → widgets → done',
              demo('Step 1 · hello (collage hides below 1100px)', hello) + demo('Step 2 · confirm what leadership pre-filled', confirm) + demo('Motion rules', motion))

# ───────────── 6. Calendar-email picker ─────────────
EMAILS = [('alex.chen@gmail.com', 'personal'), ('achen@student.ubc.ca', 'school'), ('alex@cseed.co', 'club'), ('alex.chen.work@outlook.com', ''), ('a.chen@sauder.ubc.ca', '')]


def picker(n, open_=True, adding=False, selected=0):
    opts = ''.join(f'<li class="email-option{" is-active" if i == 1 and open_ and not adding else ""}" role="option" aria-selected="{str(i == selected).lower()}" id="em{n}-{i}">'
                   f'<svg class="icon email-option-check" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M20 6 9 17l-5-5"/></svg>'
                   f'<span class="email-option-text">{e}</span><span class="email-option-tag">{t}</span></li>' for i, (e, t) in enumerate(EMAILS[:n]))
    full = n >= 5
    add = (f'<li class="email-option email-option--add" role="option" aria-selected="false" aria-disabled="{str(full).lower()}"{" aria-describedby=em-full" if full else ""}>{icon("plus")} Add another…</li>'
           + ('<li role="presentation"><p class="email-picker-note" id="em-full">5 of 5 saved. Remove one in Settings to add another.</p></li>' if full else ''))
    if adding:
        add = ('<li role="presentation"><form class="email-picker-add" onsubmit="return false"><label class="visually-hidden" for="em-new">New calendar email</label>'
               '<input class="input" id="em-new" type="email" value="alex.c@" aria-describedby="em-new-hint">' + button('Save', True) + button('Cancel') + '</form>'
               '<p class="email-picker-note" id="em-new-hint" style="padding-left:10px">4 of 5 after saving.</p></li>')
    lst = f'<ul class="email-picker-list" role="listbox" aria-label="Calendar emails">{opts}{add}</ul>' if open_ else ''
    return (f'<div class="email-picker"><span class="field-label" id="em{n}-l">Send calendar invites to</span>'
            f'<button class="email-picker-btn" aria-haspopup="listbox" aria-expanded="{str(open_).lower()}" aria-labelledby="em{n}-l em{n}-v"><span class="email-picker-value" id="em{n}-v">{EMAILS[selected][0]}</span>{icon("chevron-down").replace("class=\"icon\"", "class=\"icon email-picker-chev\"")}</button>'
            f'{lst}<span class="email-picker-count">{n} of 5 saved</span></div>')


delivery = ('<fieldset class="fieldset"><legend>How invites reach you</legend><div class="choice-row choice-row--compact">'
            '<label class="choice"><input type="radio" name="cal-mode" checked><span class="choice-title">.ics email invites</span><span class="choice-sub">An invite email to the address above, for any calendar app</span></label>'
            '<label class="choice"><input type="radio" name="cal-mode"><span class="choice-title">Google Calendar sync</span><span class="choice-sub">Connect Google once; events appear and update by themselves</span></label></div></fieldset>')
pick = section('picker', '6 · Calendar-email picker', 'Up to five saved addresses · “Add another…” disabled at five',
               '<div class="demo-grid">' + demo('Closed, with delivery mode', '<div style="display:grid;gap:16px">' + picker(3, False) + delivery + '</div>') + demo('Open · 3 saved', picker(3)) +
               demo('Open · 5 saved, add disabled', picker(5, selected=2)) + demo('Adding another', picker(3, adding=True)) + '</div>')

# ───────────── 7. Invite dialog states ─────────────
def inv_dialog(title, body, actions):
    return f'<div class="demo-dialog" role="group" aria-label="{title}"><header class="dialog-head"><h2>Invite an officer</h2><button class="icon-btn" aria-label="Close">{icon("close")}</button></header><div class="dialog-body" style="display:grid;grid-template-columns:minmax(0,1fr);gap:16px">{body}<div class="form-actions">{actions}</div></div></div>'


summary = '<dl class="facts"><div><dt>Name</dt><dd>Taylor Brooks</dd></div><div><dt>Discord ID</dt><dd class="mono">771920384455</dd></div><div><dt>Subteam</dt><dd>Entrepreneurship</dd></div></dl>'
link = '<div class="invite-link"><code>dash.cseed.co/invite/k7Qm2…9xT</code>' + button(icon('copy') + ' Copy link') + '</div>'
form = ('<div class="form--stack" style="max-width:none">' + field('Name', '<input class="input" value="Taylor Brooks">') +
        '<div class="field"><label class="field-label" for="inv-did">Discord ID</label><input class="input mono" id="inv-did" value="771920384455" aria-describedby="inv-did-h"><p class="field-hint" id="inv-did-h">In Discord: right-click the person → Copy User ID.</p></div>' +
        field('Subteam', '<select class="select"><option>Entrepreneurship</option><option>Community</option><option>Design</option></select>') +
        '<fieldset class="fieldset"><legend>Send it by</legend><div class="invite-delivery"><label class="choice"><input type="checkbox" checked><span class="choice-title">Discord DM</span><span class="choice-sub">From the CSEED bot</span></label>'
        '<label class="choice"><input type="checkbox" checked><span class="choice-title">Copy link</span><span class="choice-sub">Share it yourself</span></label></div></fieldset></div>')
states = [
    ('Ready', form, button(icon('send') + ' Send invite', True) + button('Cancel')),
    ('Sending', summary + '<div class="invite-status" role="status"><span class="spinner" aria-hidden="true"></span><span class="invite-status-text">Sending DM to Taylor…</span></div>', button('Sending…', True, 'disabled') + button('Cancel')),
    ('DM sent', summary + '<div class="invite-status invite-status--sent" role="status">' + icon('check') + '<span class="invite-status-text">DM sent ✓ <time datetime="2026-11-01T16:12">4:12 PM</time></span></div>' + link + '<span class="invite-copied" role="status">Link copied ✓</span>', button('Done', True) + button('Invite another')),
    ('DM failed', summary + '<div class="invite-status invite-status--failed" role="alert"><span class="invite-status-text">DM not delivered: Taylor only accepts DMs from friends. Share the link instead, or retry after they change it.</span></div>' + link, button('Retry DM', True) + button('Close')),
]
invite = section('invite', '7 · Invite dialog states', 'Director+ only · Discord DM and/or copy link',
                 '<div class="demo-grid">' + ''.join(demo(t, inv_dialog(t, b, a)) for t, b, a in states) + '</div>')

DEMO_CSS = '''<style>
/* Mockup-only presentation (not part of the contract). */
.demo{display:grid;grid-template-columns:minmax(0,1fr);gap:10px;min-width:0}.demo-label{margin:0;font-size:12px;font-weight:600;color:var(--muted)}
.demo-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:24px;align-items:start}.demo-grid--3{grid-template-columns:repeat(3,minmax(0,1fr))}
.demo-paper{padding:32px;border-radius:var(--radius-card);background:var(--paper);color:var(--brand-ink)}.demo-work{padding:24px;border:1px solid var(--line);border-radius:var(--radius-card)}
.demo-swatches{list-style:none;margin:0;padding:0;display:grid;grid-template-columns:repeat(auto-fill,minmax(220px,1fr));gap:12px}.demo-swatches li{display:flex;gap:10px;align-items:center}
.demo-swatch{flex:none;width:36px;height:36px;border-radius:8px;box-shadow:inset 0 0 0 1px var(--line)}
.demo-dialog{border:1px solid var(--line);border-radius:var(--radius-overlay);background:var(--surface);box-shadow:var(--overlay-shadow)}
.gallery--inline{display:block;width:auto;max-height:none;box-shadow:none;border:1px solid var(--line)}
@media (max-width:1099px){.demo-grid,.demo-grid--3{grid-template-columns:minmax(0,1fr)}}
@media (max-width:639px){.demo .dt td::before{flex:0 0 6.5em}.demo .dt td{text-align:left}}
@media (max-width:767px){.demo-paper{padding:20px}.gallery--inline{width:auto;margin:0;border-radius:var(--radius-overlay)}}
</style>'''

intro = ('<p class="muted" style="max-width:72ch;margin:0">Proposed V0.3 components for the page design Panes (D1–D5). This page loads the three production stylesheets plus '
         '<code class="mono small">web/design/v03.css</code>; the contract is spec.md § V0.3 additions. Sample data only.</p>'
         '<nav aria-label="On this page" class="chips">' + ''.join(f'<a class="chip" href="#{a}">{b}</a>' for a, b in [('type', 'Typography'), ('roles', 'Roles'), ('widgets', 'Widgets'), ('board', 'Kanban'), ('calendar', 'Calendar'), ('onboarding', 'Onboarding'), ('picker', 'Email picker'), ('invite', 'Invite')]) + '</nav>')
body = intro + typo + roles + widgets + work + onb + pick + invite
dialogs = f'<dialog class="gallery" id="gallery" aria-labelledby="gallery-title-d">{gallery_body().replace("id=\"gallery-title\"", "id=\"gallery-title-d\"")}</dialog>'
html = shell('components-v03', 'V0.3 components', 'CSEED design system · V0.3 additions', body, dialogs)
html = html.replace('</head>', '<link rel="stylesheet" href="../v03.css">' + DEMO_CSS + '</head>', 1)
html = html.replace('<h1>V0.3 components</h1>', '<h1 class="display">V0.3 components</h1>', 1)
(root / 'mockups' / 'components-v03.html').write_text(html)
print('Generated components-v03.html')
