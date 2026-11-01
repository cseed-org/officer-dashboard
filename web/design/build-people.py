"""Build the D3 Officers and Members design fixtures (mockups only, no app or service code).

Run from anywhere: python3 web/design/build-people.py
Both pages reuse the shared shell and helpers from build-mockups.py and load v03.css.
Directory rows are rendered by people-preview.js from the JSON sample data embedded
below, so the directory, profile drawer, filters and confirm steps share one record set.
"""
import json
from pathlib import Path

here = Path(__file__).resolve().parent
ns = {'__file__': str(here / 'build-mockups.py')}
exec((here / 'build-mockups.py').read_text().split('eventrows=')[0], ns)
ns['nav'] = [
    ('home', 'Home', 'home'), ('events', 'Events', 'calendar'),
    ('officers-v03', 'Officers', 'user'), ('members-v03', 'Members', 'ledger'),
    ('finance', 'Requests & Finance', 'wallet'), ('admin', 'Admin', 'shield'),
]
button, field, status, icon = ns['button'], ns['field'], ns['status'], ns['icon']


def btn(text, variant='', attrs=''):
    """A .btn with an extra modifier class (the shared button() helper takes only primary)."""
    return f'<button class="btn {variant}" {attrs}>{text}</button>'


def select(label, options, attrs=''):
    return field(label, f'<select class="select" {attrs}>' + ''.join(f'<option>{o}</option>' for o in options) + '</select>')


def facts(items):
    """items: (label, dd id, initial html)."""
    return '<dl class="facts">' + ''.join(f'<div><dt>{k}</dt><dd id="{i}">{v}</dd></div>' for k, i, v in items) + '</dl>'


def drawer(id, heading, body):
    return (f'<dialog class="drawer" id="{id}" aria-labelledby="{id}-title"><div class="drawer-inner">'
            f'<header class="drawer-head"><h2 id="{id}-title">{heading}</h2>'
            f'<button class="icon-btn" aria-label="Close" data-close>{icon("close")}</button></header>'
            f'<div class="drawer-body stack">{body}<p class="notice" data-result role="status" hidden></p></div></div></dialog>')


def modal(id, heading, body, actions):
    """Centred .dialog with a normal action row: used for invites and every confirm step."""
    return (f'<dialog class="dialog" id="{id}" aria-labelledby="{id}-title"><div class="dialog-inner">'
            f'<header class="dialog-head"><h2 id="{id}-title">{heading}</h2>'
            f'<button class="icon-btn" aria-label="Close" data-close>{icon("close")}</button></header>'
            f'<div class="dialog-body stack">{body}<div class="action-row">{actions}</div></div></div></dialog>')


def prototype_controls(states, note):
    return ('<details class="block prototype"><summary class="block-head">Prototype controls · sample data only</summary>'
            '<div class="block-body grid-fields">'
            + select('Viewing as', ['Director', 'Officer', 'Leadership', 'Owner'], 'id="viewer"')
            + select('Page state', states, 'id="state-choice"')
            + f'</div><p class="block-body small muted">{note} Production enforces every permission on the server.</p></details>')


def fallback():
    return ('<section id="fallback" class="state" hidden><span class="spinner" id="state-spinner" hidden></span>'
            '<h2 class="state-title" id="state-title"></h2><p class="state-body" id="state-body"></p>'
            + button('Try again', False, 'id="retry"') + '</section>')


def data_script(name, value):
    return f'<script type="application/json" id="{name}">{json.dumps(value, ensure_ascii=False)}</script>'


announce = '<p class="notice" id="announce" role="status" hidden></p>'

# ---------------------------------------------------------------- Officers
viewers = {
    'Officer': 'jamie', 'Director': 'samira', 'Leadership': 'noor', 'Owner': 'alex',
}
officers = [
    dict(id='alex', name='Alex Chen', pref='Alex', pronouns='he/him', team='Operations', role='leadership', status='Active', handle='alexchen', discord='512903385174016001', phone='+1 206 555 0102', birthday='March 3', title='President', personal='alex.chen@example.com', school='alexc@uw.edu', grad='June 2027', linkedin='linkedin.com/in/alexchen-cseed', since='Fall 2024'),
    dict(id='noor', name='Noor Hassan', pref='Noor', pronouns='she/her', team='Operations', role='leadership', status='Active', handle='noorh', discord='688120455907131392', phone='+1 425 555 0177', birthday='July 22', title='Vice president', personal='noor.hassan@example.com', school='nhassan@uw.edu', grad='June 2027', linkedin='linkedin.com/in/noor-hassan', since='Fall 2024'),
    dict(id='samira', name='Samira Ahmed', pref='Samira', pronouns='she/her', team='Operations', role='director', status='Active', handle='samira.a', discord='701442918263390208', phone='+1 206 555 0131', birthday='November 9', title='Operations director', personal='samira.ahmed@example.com', school='samira@uw.edu', grad='June 2028', linkedin='linkedin.com/in/samira-ahmed', since='Spring 2025'),
    dict(id='maya', name='Maya Patel', pref='Maya', pronouns='she/her', team='Community', role='director', status='Active', handle='mayap', discord='742800619335114752', phone='+1 206 555 0148', birthday='April 18', title='Community director', personal='maya.patel@example.com', school='mpatel@uw.edu', grad='June 2028', linkedin='linkedin.com/in/maya-patel-cseed', since='Fall 2025'),
    dict(id='jamie', name='Jamie Wong', pref='Jamie', pronouns='they/them', team='Community', role='officer', status='Active', handle='jamiew', discord='790011246518550528', phone='+1 253 555 0119', birthday='January 30', title='Events coordinator', personal='jamie.wong@example.com', school='jwong@uw.edu', grad='June 2029', linkedin='', since='Fall 2026'),
    dict(id='chris', name='Chris Okafor', pref='Chris', pronouns='he/him', team='Community', role='officer', status='Deactivated', handle='chriso', discord='655390114283765760', phone='+1 206 555 0163', birthday='August 14', title='Outreach coordinator', personal='chris.okafor@example.com', school='cokafor@uw.edu', grad='June 2026', linkedin='linkedin.com/in/chris-okafor', since='Fall 2024'),
    dict(id='lena', name='Lena Fischer', pref='Lena', pronouns='she/her', team='Entrepreneurship', role='director', status='Active', handle='lenaf', discord='722615309911605248', phone='+1 206 555 0185', birthday='May 6', title='Entrepreneurship director', personal='lena.fischer@example.com', school='lfisch@uw.edu', grad='June 2028', linkedin='linkedin.com/in/lena-fischer', since='Spring 2025'),
    dict(id='diego', name='Diego Morales', pref='Diego', pronouns='he/him', team='Entrepreneurship', role='officer', status='Active', handle='diegom', discord='815204771960897536', phone='+1 425 555 0124', birthday='October 2', title='Founder relations', personal='diego.morales@example.com', school='dmorales@uw.edu', grad='June 2029', linkedin='', since='Fall 2026'),
    dict(id='jordan', name='Jordan Lee', pref='Jordan', pronouns='he/him', team='Design', role='officer', status='Active', handle='jordanl', discord='760018233467617280', phone='+1 206 555 0156', birthday='February 11', title='Visual designer', personal='jordan.lee@example.com', school='jlee24@uw.edu', grad='June 2028', linkedin='linkedin.com/in/jordanlee-design', since='Spring 2025'),
    dict(id='riley', name='Riley Park', pref='Riley', pronouns='she/her', team='Design', role='officer', status='Active', handle='rileypark', discord='798432005512937472', phone='+1 206 555 0190', birthday='December 1', title='UX designer', personal='riley.park@example.com', school='rpark@uw.edu', grad='June 2029', linkedin='', since='Fall 2026'),
]
officer_data = {'officers': officers, 'owners': ['alex'], 'viewers': viewers,
                'teams': ['Operations', 'Community', 'Entrepreneurship', 'Design']}

directory_filters = (
    field('Find an officer', '<input id="query" class="input" type="search" placeholder="Name or Discord handle">')
    + select('Subteam', ['All subteams'] + officer_data['teams'], 'id="team-filter"')
    + select('Role', ['All roles', 'Officer', 'Director', 'Leadership', 'Owner'], 'id="role-filter"')
    + select('Status', ['All statuses', 'Active', 'Deactivated'], 'id="status-filter"'))


def invite_row(name, sub, sent, delivery, state, actions):
    return f'<tr>' + ''.join(f'<td data-label="{h}" class="{"dt-primary" if i == 0 else ""}"><span class="people-cell">{v}</span></td>' for i, (h, v) in enumerate([
        ('Invitee', f'<span class="cell-title">{name}</span><span class="cell-sub">{sub}</span>'),
        ('Sent', sent), ('Delivery', delivery), ('Status', state),
        ('Actions', '<span class="action-row">' + ''.join(btn(a, 'btn--small', f'data-invite-action="{a}" data-invitee="{name}"') for a in actions) + '</span>' if actions else '—'),
    ])) + '</tr>'


pending_rows = ''.join([
    invite_row('Taylor Brooks', 'Entrepreneurship · @taylorb', 'Nov 1, 4:12 PM<span class="cell-sub">Expires Nov 8</span>', '<span class="badge badge--positive">DM sent ✓ Nov 1, 4:12 PM</span>', status('Awaiting sign-in', 'attention'), ['Resend', 'Revoke']),
    invite_row('Hana Sato', 'Community · @hanasato', 'Oct 31, 9:40 AM<span class="cell-sub">Expires Nov 7</span>', '<span class="badge badge--negative">DM failed · DMs closed</span><span class="cell-sub">Link copied as fallback</span>', status('DM failed', 'negative'), ['Retry DM', 'Copy link', 'Revoke']),
    invite_row('Evan Liu', 'Design · @evanliu', 'Oct 30, 2:05 PM<span class="cell-sub">Expires Nov 6</span>', 'Link copied', status('Awaiting sign-in', 'attention'), ['Copy link', 'Revoke']),
    invite_row('Marcus Bell', 'Operations · @marcusb', 'Oct 20, 11:18 AM<span class="cell-sub">Expired Oct 27</span>', 'DM sent ✓ Oct 20', status('Expired', 'neutral'), ['Resend', 'Remove']),
    invite_row('Diego Morales', 'Entrepreneurship · @diegom', 'Oct 27, 6:30 PM<span class="cell-sub">Accepted Oct 29</span>', 'DM sent ✓ Oct 27', status('Accepted', 'positive'), []),
])
pending = ('<section class="stack" id="invites" data-director><div class="section-head"><h2>Invites</h2><span class="small muted">3 pending · 1 expired · 1 accepted this week</span></div>'
           '<div class="table-wrap"><table class="dt"><caption class="visually-hidden">Officer invites</caption><thead><tr>'
           + ''.join(f'<th scope="col">{h}</th>' for h in ['Invitee', 'Sent', 'Delivery', 'Status', 'Actions'])
           + f'</tr></thead><tbody>{pending_rows}</tbody></table></div>'
           '<p class="small muted">Invites expire after 7 days. Only the matching Discord account can accept, then completes onboarding after sign-in.</p></section>')

officers_body = (
    prototype_controls(['Ready', 'No officers yet', 'Loading', 'Error'],
                       'Every name opens that officer’s profile. “Viewing as” swaps the signed-in sample user: Jamie (officer), Samira (director), Noor (leadership) or Alex (owner).')
    + announce
    + '<div class="toolbar"><span class="small muted" id="officer-count"></span><div class="toolbar-actions">'
    + button('My profile', False, 'data-open-own') + button('Invite officer', True, 'data-open="invite" data-director') + '</div></div>'
    + f'<div class="toolbar"><div class="toolbar-main">{directory_filters}</div></div>'
    + '<div id="directory" class="stack"></div>' + fallback() + pending)

profile = (
    '<div class="action-row"><span id="pf-role"></span><span id="pf-status"></span></div>'
    '<div><h3 id="pf-name"></h3><p class="muted" id="pf-sub"></p></div>'
    + facts([
        ('Discord', 'pf-discord', ''),
        ('Phone', 'pf-phone-cell', '<span data-private id="pf-phone"></span><span data-officer-only class="muted">Visible to directors and leadership</span>'),
        ('Birthday', 'pf-birthday', ''), ('Preferred name', 'pf-pref', ''), ('Title', 'pf-title', ''),
        ('Personal email', 'pf-personal', ''), ('School email', 'pf-school', ''), ('Graduation', 'pf-grad', ''),
        ('LinkedIn', 'pf-linkedin', ''), ('Officer since', 'pf-since', ''),
    ])
    + '<section class="stack" id="pf-access" data-leadership><h3>Workspace access</h3>'
    + select('Role', ['Officer', 'Director', 'Leadership'], 'id="new-role" aria-describedby="role-hint"')
    + '<p class="field-hint" id="role-hint">Choose a different role to review the change.</p>'
    + '<div class="action-row">' + button('Review role change', False, 'id="review-role" disabled') + btn('Deactivate officer', 'btn--danger', 'id="deactivate"') + '</div></section>'
    + '<p class="notice" id="pf-access-note" hidden></p>'
    + '<section class="stack" id="pf-owner" data-owner><h3>Owner controls</h3><p class="small muted">Ownership is separate from roles. Owners can add co-owners and transfer ownership. At least one owner must remain.</p>'
    + '<div class="action-row">' + button('Make co-owner', False, 'id="make-owner"') + button('Transfer ownership', False, 'id="transfer"') + btn('Remove owner status', 'btn--danger', 'id="remove-owner" aria-describedby="owner-hint"') + '</div>'
    + '<p id="owner-hint" class="field-hint"></p></section>')

invite = (
    '<div class="grid-fields">'
    + field('Name', '<input class="input" id="invite-name" value="Taylor Brooks" required autocomplete="off">')
    + select('Subteam', ['Entrepreneurship', 'Community', 'Design', 'Operations'], 'id="invite-team"') + '</div>'
    + field('Discord ID', '<input class="input mono" id="invite-discord" value="834610592744103936" required inputmode="numeric"><span class="field-hint">Discord → Settings → Advanced → Developer mode, then right-click the person and Copy User ID.</span>')
    + '<fieldset class="fieldset"><legend>Send invitation by</legend><div class="invite-delivery">'
      '<label class="choice"><input type="checkbox" id="dm" checked><span class="choice-title">Discord DM</span><span class="choice-sub">The CSEED bot messages them a sign-in link</span></label>'
      '<label class="choice"><input type="checkbox" id="link" checked><span class="choice-title">Copy link</span><span class="choice-sub">Share it yourself, in any channel</span></label>'
      '</div><p class="field-hint" id="delivery-hint">Choose one or both.</p></fieldset>'
    + '<dl class="facts" id="invite-summary" hidden></dl>'
    + '<div class="invite-status" id="invite-status" role="status"><span class="spinner" hidden></span><span class="invite-status-text">Ready to invite Taylor to Entrepreneurship.</span></div>'
    + '<div class="invite-status invite-status--failed" id="invite-error" role="alert" hidden><span class="invite-status-text"></span></div>'
    + '<div class="invite-link" id="invite-link" hidden><code>https://officers.cseed.co/invite/7Kq2-taylor</code>' + button('Copy link', False, 'id="copy-invite"') + '<span class="invite-copied" id="invite-copied" hidden>Link copied ✓</span></div>'
    + '<label class="field-hint prototype-toggle"><input type="checkbox" id="sim-dm-fail"> Prototype only: make the Discord DM fail</label>')
invite_actions = button('Send invitation', True, 'id="send-invite"') + button('Done', False, 'id="invite-done" data-close hidden') + button('Cancel', False, 'id="invite-cancel" data-close')

calendar = (
    '<p class="muted" id="cal-owner"></p>'
    '<div class="email-picker"><label class="field-label" for="email-select">Calendar email</label>'
    '<select class="select" id="email-select"></select><p class="email-picker-note" id="email-note"></p></div>'
    '<section class="stack stack--tight"><h3>Saved emails</h3><ul class="list" id="saved-emails"></ul></section>'
    + select('Delivery mode', ['.ics email invites', 'Google Calendar sync (OAuth)'], 'id="delivery"')
    + '<p id="delivery-note" class="notice">Event invitations arrive as .ics email attachments at the address above.</p>'
    + '<div class="action-row">' + button('Save calendar preferences', True, 'id="save-calendar"') + '</div>')

confirm_body = '<p id="confirm-body"></p><p class="field-hint">This change is recorded in the workspace audit log with your name and the time.</p>'
confirm_actions = button('Confirm', True, 'id="confirm-go"') + button('Cancel', False, 'data-close')

officers_dialogs = (
    drawer('profile', 'Officer profile', profile)
    + modal('invite', 'Invite an officer', invite, invite_actions)
    + drawer('own-profile', 'My profile · Calendar', calendar)
    + modal('confirm', 'Confirm change', confirm_body, confirm_actions)
    + data_script('officer-data', officer_data))

# ---------------------------------------------------------------- Members
members = [
    dict(id='person_1086', name='Avery Nguyen', pref='Avery', uw='averyn@uw.edu', cs='', major='Computer Science', grad='2028', campus='Seattle', joined='2026-10-31', officer='No'),
    dict(id='person_1085', name='Sophia Kim', pref='Sophie', uw='sophiak@uw.edu', cs='sophiak@cs.washington.edu', major='Business Administration', grad='2027', campus='Seattle', joined='2026-10-30', officer='Yes'),
    dict(id='person_1084', name='Daniel Garcia', pref='Danny', uw='dgarcia@uw.edu', cs='', major='Informatics', grad='2029', campus='Bothell', joined='2026-10-29', officer='No'),
    dict(id='person_1083', name='Priya Shah', pref='Priya', uw='pshah@uw.edu', cs='', major='Human Centered Design & Engineering', grad='2028', campus='Tacoma', joined='2026-10-28', officer='No'),
    dict(id='person_1082', name='Marcus Johnson', pref='Marcus', uw='marcusj@uw.edu', cs='marcusj@cs.washington.edu', major='Computer Engineering', grad='2027', campus='Seattle', joined='2026-10-27', officer='No'),
    dict(id='person_1081', name='Hannah Lee', pref='Hannah', uw='hlee22@uw.edu', cs='', major='Economics', grad='2029', campus='Bothell', joined='2026-10-25', officer='No'),
    dict(id='person_1080', name='Omar Ali', pref='Omar', uw='omarali@uw.edu', cs='', major='Mechanical Engineering', grad='2028', campus='Seattle', joined='2026-10-24', officer='Yes'),
    dict(id='person_1079', name='Grace Liu', pref='Gracie', uw='gliu@uw.edu', cs='', major='Marketing', grad='2027', campus='Tacoma', joined='2026-10-22', officer='No'),
]
member_data = {'members': members, 'total': 1086, 'pageSize': 25,
               'viewers': {'Officer': 'Jamie Wong', 'Director': 'Samira Ahmed', 'Leadership': 'Noor Hassan', 'Owner': 'Alex Chen'}}

member_filters = (
    field('Search members', '<input id="query" class="input" type="search" placeholder="Name, preferred name or email">')
    + select('Campus', ['All campuses', 'Seattle', 'Bothell', 'Tacoma'], 'id="campus-filter"')
    + select('Graduation', ['All years', '2027', '2028', '2029'], 'id="grad-filter"')
    + select('Officer history', ['Any history', 'Has been officer', 'Never officer'], 'id="history-filter"'))

members_body = (
    prototype_controls(['Ready', 'Empty', 'Loading', 'Page error', 'Sync failed', 'Not connected'],
                       'Every name opens that member’s record. The sample draws 8 of 1,086 records.')
    + announce
    + '<div class="notice notice--info" id="sync-status"></div>'
    + '<div class="toolbar"><span class="small muted">Website signups · cseed-membership / People</span><div class="toolbar-actions" data-director>'
    + button('Export CSV', False, 'id="export"') + '</div></div>'
    + f'<div class="toolbar"><div class="toolbar-main">{member_filters}</div></div>'
    + '<p class="small muted">Search and filters run on the server against the local mirror. 25 results per page; only the current page is loaded.</p>'
    + '<div id="directory" class="stack"><div id="member-table"></div><div class="toolbar"><span id="page-label"></span><div class="toolbar-actions">'
    + button('Previous', False, 'id="previous"') + button('Next', False, 'id="next"') + '</div></div></div>' + fallback())

edit_fields = [
    ('name', 'Name', 'text'), ('pref', 'Preferred name', 'text'), ('uw', 'UW email', 'email'), ('cs', 'CS email', 'email'),
    ('major', 'Major', 'text'), ('grad', 'Graduation year', ['2026', '2027', '2028', '2029', '2030']),
    ('campus', 'Campus', ['Seattle', 'Bothell', 'Tacoma']), ('joined', 'Join date', 'date'), ('officer', 'Has been officer', ['No', 'Yes']),
]


def edit_control(key, label, kind):
    if isinstance(kind, list):
        return select(label, kind, f'data-field="{key}"')
    extra = ' placeholder="Not provided"' if key == 'cs' else ''
    return field(label, f'<input class="input" type="{kind}" data-field="{key}"{extra}>')


member = (
    '<div><h3 id="md-name"></h3><p class="muted" id="md-sub"></p></div>'
    + facts([('Preferred name', 'md-pref', ''), ('UW email', 'md-uw', ''), ('CS email', 'md-cs', ''), ('Major', 'md-major', ''),
             ('Graduation year', 'md-grad', ''), ('Campus', 'md-campus', ''), ('Joined', 'md-joined', ''),
             ('Has been officer', 'md-officer', ''), ('Source', 'md-source', '')])
    + '<p class="small muted" id="md-mirror"></p>'
    + '<p class="notice" data-officer-only>Read-only. Directors and leadership can edit member records.</p>'
    + '<section data-director class="stack" id="edit-section"><h3>Edit website record</h3>'
    + '<p class="notice notice--attention" id="edit-warning">Saving writes to Cloudflare D1, the source for the public membership site.</p>'
    + '<div class="grid-fields">' + ''.join(edit_control(*f) for f in edit_fields) + '</div>'
    + field('Audit note', '<textarea class="textarea" id="audit-note" placeholder="Why is this record changing?" required></textarea><span class="field-hint">Required. Saved to the audit log with the changed fields.</span>')
    + '<div class="action-row">' + button('Review changes', True, 'id="review-changes" disabled aria-describedby="review-hint"') + '</div>'
    + '<p class="field-hint" id="review-hint">Change a field and add an audit note to review.</p></section>')

d1_confirm = (
    '<p id="d1-target"></p>'
    '<div class="table-wrap"><table class="dt"><caption class="visually-hidden">Fields that will change</caption><thead><tr><th scope="col">Field</th><th scope="col">Current</th><th scope="col">New</th></tr></thead><tbody id="d1-diff"></tbody></table></div>'
    '<p id="d1-audit"></p>'
    '<p class="field-hint" id="d1-actor"></p>'
    '<div class="invite-status invite-status--failed" id="d1-error" role="alert" hidden><span class="invite-status-text">Cloudflare D1 didn’t respond. Nothing was saved and your edits are kept. Try again in a moment.</span></div>'
    '<label class="field-hint prototype-toggle"><input type="checkbox" id="sim-d1-fail"> Prototype only: make the D1 write fail</label>')
d1_actions = button('Confirm and write to D1', True, 'id="write-d1"') + button('Back to edit', False, 'data-close')

members_dialogs = (
    drawer('member', 'Member record', member)
    + modal('d1-confirm', 'Confirm D1 update', d1_confirm, d1_actions)
    + data_script('member-data', member_data))

# ---------------------------------------------------------------- Write
pages = [
    ('officers-v03', 'Officers', 'The people making CSEED happen.', officers_body, officers_dialogs),
    ('members-v03', 'Members', 'A growing community, one signup at a time.', members_body, members_dialogs),
]
for key, heading, sub, body, dialogs in pages:
    html = (ns['shell'](key, heading, sub, body, dialogs)
            .replace('</head>', '<link rel="stylesheet" href="../v03.css"></head>')
            .replace('</body>', '<script src="../people-preview.js"></script></body>'))
    (here / 'mockups' / f'{key}.html').write_text(html)
print('Built Officers and Members design fixtures')
