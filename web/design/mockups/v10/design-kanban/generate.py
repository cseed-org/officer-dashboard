"""Generate the three Design Kanban mockups (A, B, C) + comparison index into .pane/mockups/."""
import os, sys, html, re

OUT = sys.argv[1]

TOKENS = """
:root{color-scheme:light;--bg:#fff;--surface:#fff;--sunken:#f7f7f5;--text:#202320;--muted:#646963;--line:#e3e5e1;--control:#858b82;
--accent:#202320;--accent-ink:#fff;--positive:#326247;--negative:#a33732;--attention:#805b20;--hover:#eeefec;--brand:#f5d140;
--font:ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;--c-blue:#2f6fc4;--c-amber:#b47c0c;--c-teal:#0a948a;
--c-coral:#d9634a;--c-violet:#7a5bd0;--c-green:#3d8f3f;--tint:11%;--radius-card:12px;--radius-control:8px;--block-radius:16px;
--block-shadow:0 1px 2px rgb(20 20 20/.04),0 1px 1px rgb(20 20 20/.03);--overlay-shadow:0 18px 40px -18px rgb(0 0 0/.22),0 2px 6px rgb(0 0 0/.06);
--ease-out:cubic-bezier(.2,.8,.2,1)}
:root[data-theme=dark]{color-scheme:dark;--bg:#181a18;--surface:#1c1f1c;--sunken:#212421;--text:#eeefeb;--muted:#a8aea5;--line:#373d36;--control:#778271;
--accent:#eeefeb;--accent-ink:#181a18;--positive:#94c6a5;--negative:#f0a09a;--attention:#dbc38e;--hover:#2b302a;--c-blue:#4f8ad8;--c-amber:#b98428;
--c-teal:#1f9d92;--c-coral:#d96d52;--c-violet:#8f78e0;--c-green:#4fa156;--tint:18%;--block-shadow:0 1px 2px rgb(0 0 0/.4);
--overlay-shadow:0 18px 40px -16px rgb(0 0 0/.6),0 2px 6px rgb(0 0 0/.3)}
*{box-sizing:border-box}body{margin:0;background:var(--sunken);color:var(--text);font:14px/1.45 var(--font)}
"""

BASE = """
.doc{max-width:1520px;margin:0 auto;padding:24px 16px 64px}
.doc h1{font-size:22px;margin:0 0 4px}.doc .why{max-width:900px;color:var(--muted);margin:0 0 20px}
.doc h2{font-size:13px;text-transform:uppercase;letter-spacing:.06em;color:var(--muted);margin:28px 0 10px}
.toggle{position:fixed;top:12px;right:12px;z-index:50}
.frame{background:var(--bg);border:1px solid var(--line);border-radius:14px;overflow:hidden;box-shadow:var(--overlay-shadow)}
.desk{width:1440px;height:900px;display:grid;grid-template-columns:232px 1fr;position:relative}
.scroll{overflow-x:auto;padding-bottom:8px}
.side{background:var(--sunken);border-right:1px solid var(--line);padding:20px 12px;display:grid;align-content:start;gap:4px;font-size:14px}
.side b{font-size:18px;margin:0 8px 18px}.side span{padding:8px 10px;border-radius:8px;color:var(--text)}.side .on{background:color-mix(in srgb,var(--c-blue) 10%,var(--surface));font-weight:600}
.main{padding:32px 40px;overflow:hidden;position:relative;min-width:0}
.main h3.pt{font-size:28px;margin:0 0 20px;letter-spacing:-.01em}
.stats{display:grid;grid-template-columns:repeat(4,1fr);gap:16px;margin-bottom:18px}
.tile{background:var(--surface);border:1px solid var(--line);border-radius:var(--block-radius);box-shadow:var(--block-shadow);padding:14px 16px;display:grid;gap:4px}
.tile small{font-size:13px;color:var(--muted);font-weight:500;display:flex;align-items:center;gap:8px}
.tile .chip{width:22px;height:22px;border-radius:7px;display:inline-grid;place-items:center;color:var(--hue);background:color-mix(in srgb,var(--hue) var(--tint),var(--surface))}
.tile strong{font-size:1.75rem;font-weight:500;letter-spacing:-.04em;line-height:1.05}.tile em{font-style:normal;font-size:12px;color:var(--muted)}
.tile.alert strong{color:var(--negative)}
.bar{display:flex;align-items:center;gap:8px;flex-wrap:wrap;margin-bottom:14px}
.btn{display:inline-flex;align-items:center;gap:6px;height:34px;padding:0 14px;border-radius:var(--radius-control);border:1px solid var(--line);background:var(--surface);color:var(--text);font:500 13px var(--font)}
.btn--primary{background:var(--accent);color:var(--accent-ink);border-color:var(--accent)}
.fchip{display:inline-flex;align-items:center;gap:6px;height:30px;padding:0 12px;border-radius:999px;border:1px solid var(--line);background:var(--surface);font-size:13px;color:var(--text)}
.fchip[aria-pressed=true]{background:var(--accent);color:var(--accent-ink);border-color:var(--accent)}
.fchip .n{font-variant-numeric:tabular-nums;color:var(--muted)}.fchip[aria-pressed=true] .n{color:inherit;opacity:.7}
.grow{flex:1}
.seg{display:inline-flex;border:1px solid var(--line);border-radius:10px;padding:2px;background:var(--sunken)}.seg span{padding:5px 12px;border-radius:8px;font-size:13px;color:var(--muted)}.seg .on{background:var(--surface);color:var(--text);font-weight:600;box-shadow:var(--block-shadow)}
/* card */
.card{position:relative;background:var(--surface);border:1px solid var(--line);border-radius:var(--radius-card);box-shadow:var(--block-shadow);padding:12px;display:grid;gap:8px;transition:transform .18s var(--ease-out),box-shadow .18s var(--ease-out),border-color .12s}
.card:hover{border-color:color-mix(in srgb,var(--control) 50%,var(--line));transform:translateY(-1px);box-shadow:var(--overlay-shadow)}
.card.lift{transform:rotate(-2deg) scale(1.03);box-shadow:0 24px 48px -16px rgb(0 0 0/.35);border-color:var(--control);z-index:5}
.card.sel{outline:2px solid var(--c-blue);outline-offset:1px}
.ctop{display:flex;gap:8px;align-items:flex-start}
.ticon{flex:none;width:26px;height:26px;border-radius:8px;display:grid;place-items:center;color:var(--hue);background:color-mix(in srgb,var(--hue) var(--tint),var(--surface))}
.ctitle{flex:1;min-width:0;font-weight:600;font-size:13.5px;line-height:1.3}
.cmeta{display:flex;flex-wrap:wrap;gap:6px;align-items:center;font-size:12px;color:var(--muted)}
.pill{display:inline-flex;align-items:center;gap:4px;height:22px;padding:0 8px;border-radius:999px;font-size:12px;border:1px solid var(--line);color:var(--muted);white-space:nowrap}
.pill.over{color:var(--negative);border-color:color-mix(in srgb,var(--negative) 40%,var(--line));background:color-mix(in srgb,var(--negative) 7%,var(--surface));font-weight:600}
.pill.soon{color:var(--attention);border-color:color-mix(in srgb,var(--c-amber) 45%,var(--line));background:color-mix(in srgb,var(--c-amber) 8%,var(--surface))}
.pill.ok{color:var(--positive)}
.pill.appr{color:var(--c-violet);border-color:color-mix(in srgb,var(--c-violet) 40%,var(--line));background:color-mix(in srgb,var(--c-violet) 8%,var(--surface));font-weight:600}
.pill.hi{color:var(--negative);border:0;padding:0;font-weight:700;font-size:11px;letter-spacing:.03em}
.cfoot{display:flex;align-items:center;gap:8px}
.av{flex:none;width:24px;height:24px;border-radius:50%;display:inline-grid;place-items:center;font-size:10px;font-weight:700;color:var(--hue);background:color-mix(in srgb,var(--hue) 16%,var(--surface));box-shadow:0 0 0 2px var(--surface)}
.av.empty{color:var(--muted);background:var(--sunken);border:1px dashed var(--control)}
.req{font-size:12px;color:var(--muted);flex:1;min-width:0;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
/* design links */
.dl{display:inline-flex;gap:4px}
.dlb{position:relative;display:inline-grid;place-items:center;width:28px;height:28px;border-radius:8px;border:1px solid var(--line);background:var(--surface);color:var(--muted);transition:background .12s,color .12s,transform .12s,border-color .12s}
.dlb.off{border-style:dashed;opacity:.75;cursor:not-allowed}
.dlb.on{color:var(--text)}
.dlb.on.figma{--hue:var(--c-violet)}.dlb.on.canva{--hue:var(--c-teal)}.dlb.on.folder{--hue:var(--c-amber)}
.dlb.on{color:var(--hue);background:color-mix(in srgb,var(--hue) var(--tint),var(--surface));border-color:color-mix(in srgb,var(--hue) 30%,var(--line))}
.dlb.hover{transform:translateY(-1px);box-shadow:var(--block-shadow)}
.dlb.press{transform:scale(.94)}
.dlb.focus{outline:2px solid var(--c-blue);outline-offset:2px}
.tip{position:absolute;bottom:calc(100% + 6px);left:50%;transform:translateX(-50%);white-space:nowrap;background:var(--accent);color:var(--accent-ink);font-size:11.5px;padding:4px 8px;border-radius:6px;font-weight:500;pointer-events:none}
.dlw{display:inline-flex;align-items:center;gap:8px;height:36px;padding:0 12px;border-radius:10px;border:1px solid var(--line);background:var(--surface);font:500 13px var(--font);color:var(--text)}
.dlw small{color:var(--muted);font-weight:400}.dlw.off{border-style:dashed;color:var(--muted)}
.dlw.on{--hue:var(--c-violet);border-color:color-mix(in srgb,var(--hue) 35%,var(--line));background:color-mix(in srgb,var(--hue) 7%,var(--surface))}
/* columns */
.cols{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:14px;align-items:stretch}
.col{background:var(--sunken);border:1px solid var(--line);border-radius:var(--block-radius);padding:10px;display:grid;align-content:start;gap:10px;min-height:420px;transition:opacity .15s,border-color .15s,background .15s}
.col.target{border:2px dashed color-mix(in srgb,var(--hue) 60%,var(--line));background:color-mix(in srgb,var(--hue) 6%,var(--sunken))}
.col.nope{opacity:.45}
.chead{display:grid;gap:6px;padding:2px 4px}
.chead .row{display:flex;align-items:center;gap:8px}
.chead .dot{width:10px;height:10px;border-radius:50%;background:var(--hue)}
.chead b{font-size:13.5px}.chead .n{margin-left:auto;font-size:12px;color:var(--muted);font-variant-numeric:tabular-nums}
.prog{height:4px;border-radius:4px;background:color-mix(in srgb,var(--hue) 15%,var(--surface));overflow:hidden}.prog i{display:block;height:100%;background:var(--hue);border-radius:4px}
.drop{height:60px;border-radius:12px;border:2px dashed color-mix(in srgb,var(--hue) 60%,var(--line));background:color-mix(in srgb,var(--hue) 8%,transparent)}
.nohint{font-size:12px;color:var(--muted);text-align:center;padding:6px;border:1px dashed var(--line);border-radius:10px}
.addq{font-size:12.5px;color:var(--muted);padding:6px 8px;border-radius:8px}
.todo{--hue:var(--control)}.doing{--hue:var(--c-amber)}.review{--hue:var(--c-violet)}.done{--hue:var(--c-green)}
/* peek panel */
.peek{position:absolute;top:0;right:0;bottom:0;width:440px;z-index:10;background:var(--surface);border-left:1px solid var(--line);box-shadow:-24px 0 48px -32px rgb(0 0 0/.35);padding:20px 22px;display:grid;align-content:start;gap:14px;overflow:hidden}
.peek .eb{font-size:11px;text-transform:uppercase;letter-spacing:.08em;color:var(--muted);display:flex;justify-content:space-between}
.peek h4{font-size:20px;margin:0;line-height:1.25}
.steps{display:grid;grid-template-columns:repeat(4,1fr);gap:4px}.steps span{height:6px;border-radius:4px;background:var(--line)}.steps span.on{background:var(--hue)}
.steplab{display:grid;grid-template-columns:repeat(4,1fr);font-size:11px;color:var(--muted)}.steplab b{color:var(--text)}
.kv{display:grid;grid-template-columns:96px 1fr;gap:8px 12px;font-size:13px}.kv dt{color:var(--muted)}.kv dd{margin:0;display:flex;align-items:center;gap:6px}
.sec{border-top:1px solid var(--line);padding-top:12px;display:grid;gap:8px}.sec h5{margin:0;font-size:12px;color:var(--muted);font-weight:600}
.brief{font-size:13px;line-height:1.5;margin:0}
.tl{display:grid;gap:8px;font-size:12.5px}.tl div{display:flex;gap:8px;align-items:flex-start}.tl p{margin:0}.tl small{color:var(--muted)}
.acts{display:flex;gap:8px;flex-wrap:wrap}
/* toast / celebrate */
.celebrate{position:absolute;display:grid;place-items:center;pointer-events:none}
.conf i{position:absolute;width:6px;height:10px;border-radius:2px}
.toast{position:absolute;left:50%;bottom:20px;transform:translateX(-50%);background:var(--accent);color:var(--accent-ink);padding:10px 16px;border-radius:10px;font-size:13px;display:flex;gap:10px;align-items:center;box-shadow:var(--overlay-shadow)}
.toast u{opacity:.75}
/* phone */
.phones{display:flex;gap:24px;flex-wrap:wrap}
.phone{width:390px;height:844px;border-radius:36px;border:10px solid #111;background:var(--bg);overflow:hidden;position:relative;display:flex;flex-direction:column}
.ptop{display:flex;align-items:center;justify-content:space-between;padding:14px 16px;border-bottom:1px solid var(--line);font-weight:600}
.pbody{padding:14px 16px;display:grid;gap:12px;align-content:start;overflow:hidden;flex:1}
.pstats{display:grid;grid-template-columns:1fr 1fr;gap:10px}.pstats .tile{padding:10px 12px}.pstats .tile strong{font-size:1.35rem}
.tabs4{display:grid;grid-template-columns:repeat(4,1fr);gap:4px;background:var(--sunken);border:1px solid var(--line);border-radius:12px;padding:3px}
.tabs4 span{text-align:center;font-size:12.5px;padding:7px 0;border-radius:9px;color:var(--muted)}.tabs4 span b{display:block;font-size:15px;color:var(--text)}
.tabs4 .on{background:var(--surface);box-shadow:var(--block-shadow);color:var(--text);font-weight:600}
.sheet{position:absolute;left:0;right:0;bottom:0;top:120px;background:var(--surface);border-radius:20px 20px 0 0;box-shadow:0 -20px 40px -20px rgb(0 0 0/.4);padding:10px 18px 18px;display:grid;align-content:start;gap:12px}
.sheet .grab{width:40px;height:5px;border-radius:3px;background:var(--line);margin:0 auto 4px}
.scrim{position:absolute;inset:0;background:rgb(0 0 0/.28)}
.states{display:flex;gap:28px;flex-wrap:wrap;align-items:flex-end;background:var(--surface);border:1px solid var(--line);border-radius:14px;padding:28px 20px 16px}
.states figure{margin:0;display:grid;gap:8px;justify-items:center}.states figcaption{font-size:11.5px;color:var(--muted)}
@media (prefers-reduced-motion: reduce){*{transition:none!important;animation:none!important}}
"""

ICON = {
 'poster': '<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="5" y="3" width="14" height="18" rx="2"/><path d="M8 15l3-3 2 2 3-3"/><circle cx="10" cy="8" r="1.4"/></svg>',
 'social': '<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="4" width="16" height="16" rx="4"/><circle cx="12" cy="12" r="3.5"/><circle cx="16.5" cy="7.5" r=".8" fill="currentColor"/></svg>',
 'slides': '<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="12" rx="2"/><path d="M12 16v4M8 20h8"/></svg>',
 'merch': '<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8 4l-5 3 2 4 2-1v10h10V10l2 1 2-4-5-3a4 4 0 0 1-8 0z"/></svg>',
 'video': '<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="6" width="13" height="12" rx="2"/><path d="M16 10l5-3v10l-5-3"/></svg>',
 'figma': '<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 19l7-7 3 3-7 7-3-3z"/><path d="M18 13l-1.5-7.5L2 2l3.5 14.5L13 18l5-5z"/><path d="M2 2l7.6 7.6"/><circle cx="11" cy="11" r="2"/></svg>',
 'canva': '<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="13.5" cy="6.5" r="1.2"/><circle cx="17.5" cy="10.5" r="1.2"/><circle cx="8.5" cy="7.5" r="1.2"/><circle cx="6.5" cy="12.5" r="1.2"/><path d="M12 2a10 10 0 0 0 0 20c1 0 1.7-.8 1.7-1.7 0-.5-.2-.9-.5-1.2-.3-.3-.5-.7-.5-1.2 0-.9.8-1.7 1.7-1.7H16a6 6 0 0 0 6-6C22 6 17.5 2 12 2z"/></svg>',
 'folder': '<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/></svg>',
 'cal': '<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="3" y="5" width="18" height="16" rx="2"/><path d="M3 10h18M8 3v4M16 3v4"/></svg>',
 'check': '<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12l5 5 9-10"/></svg>',
 'grip': '<svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor"><circle cx="9" cy="6" r="1.6"/><circle cx="15" cy="6" r="1.6"/><circle cx="9" cy="12" r="1.6"/><circle cx="15" cy="12" r="1.6"/><circle cx="9" cy="18" r="1.6"/><circle cx="15" cy="18" r="1.6"/></svg>',
 'inbox': '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 12h-6l-2 3h-4l-2-3H2"/><path d="M5.5 5h13L22 12v6a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2v-6z"/></svg>',
 'clock': '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></svg>',
 'user': '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="8" r="4"/><path d="M4 21a8 8 0 0 1 16 0"/></svg>',
 'spark': '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 3v4M12 17v4M3 12h4M17 12h4M6 6l2.5 2.5M15.5 15.5L18 18M6 18l2.5-2.5M15.5 8.5L18 6"/></svg>',
}
HUE = {'poster': 'var(--c-blue)', 'social': 'var(--c-violet)', 'slides': 'var(--c-amber)', 'merch': 'var(--c-coral)', 'video': 'var(--c-teal)'}
PEOPLE = {'RP': ('Riley Park', 'var(--c-teal)'), 'AR': ('Ava Romero', 'var(--c-coral)'), 'PS': ('Priya Shah', 'var(--c-blue)'), 'SR': ('Sam Rivera', 'var(--c-violet)'), 'JL': ('Jordan Lee', 'var(--c-green)')}

# title, type, lane, needsApproval, event, eventDate, due, dueState, assignee, requester, high, link
CARDS = [
 ('Hack Night poster', 'poster', 'todo', True, 'Hack Night', 'Fri Nov 13', 'Nov 6', 'over', None, 'AR', True, None),
 ('BuildHer workshop tees', 'merch', 'todo', False, 'BuildHer Workshop', 'Sat Dec 5', 'Nov 24', 'ok', 'JL', 'PS', False, None),
 ('Founders panel slides', 'slides', 'todo', True, 'Founders Panel', 'Thu Dec 3', 'Nov 26', 'ok', None, 'SR', False, None),
 ('Mentor coffee social', 'social', 'doing', False, 'Mentor Coffee', 'Tue Nov 17', 'Nov 15', 'soon', 'RP', 'PS', False, 'figma'),
 ('Demo Day teaser', 'video', 'doing', False, 'Demo Day', 'Fri Dec 11', 'Dec 1', 'ok', 'JL', 'SR', False, 'canva'),
 ('Fall Kickoff recap slides', 'slides', 'review', False, 'Fall Kickoff', 'Sep 18', 'Nov 4', 'over', 'RP', 'AR', False, 'figma'),
 ('Welcome week banner', 'poster', 'done', False, 'Welcome Week', 'Sep 2', 'Delivered', 'done', 'RP', 'SR', False, 'figma'),
 ('Speaker series carousel', 'social', 'done', False, 'Speaker Series', 'Oct 22', 'Delivered', 'done', 'JL', 'PS', False, 'canva'),
]
LANES = [('todo', 'To do'), ('doing', 'Doing'), ('review', 'Review'), ('done', 'Done')]

def av(code, size=24):
    if not code:
        return f'<span class="av empty" style="width:{size}px;height:{size}px" title="Unassigned">?</span>'
    name, hue = PEOPLE[code]
    return f'<span class="av" style="--hue:{hue};width:{size}px;height:{size}px" title="{name}">{code}</span>'

def links(kind, states=None):
    """kind: None (not linked) | 'figma' | 'canva'. Folder is never linked in the mockup."""
    if kind:
        label = 'Figma' if kind == 'figma' else 'Canva'
        a = f'<span class="dlb on {kind}" title="Open in {label}" aria-label="Open in {label}">{ICON[kind]}</span>'
    else:
        a = f'<span class="dlb off" title="Design file: Not linked yet" aria-label="Design file, not linked yet" aria-disabled="true">{ICON["figma"]}</span>'
    b = f'<span class="dlb off" title="Folder: Not linked yet" aria-label="Folder, not linked yet" aria-disabled="true">{ICON["folder"]}</span>'
    return f'<span class="dl">{a}{b}</span>'

def due(c):
    s, d = c[7], c[6]
    if s == 'done':
        return f'<span class="pill ok">{ICON["check"]} Delivered</span>'
    cls = {'over': 'over', 'soon': 'soon'}.get(s, '')
    word = {'over': ' · Overdue', 'soon': ' · Due soon'}.get(s, '')
    return f'<span class="pill {cls}">{ICON["clock"]} {d}{word}</span>'

def card(c, extra='', compact=False):
    t, ty, lane, appr, ev, evd, d, ds, a, r, hi, lk = c
    chips = []
    if appr:
        chips.append('<span class="pill appr">Needs approval</span>')
    chips.append(due(c))
    if hi and lane != 'done':
        chips.append('<span class="pill hi">▲ HIGH</span>')
    evline = f'<div class="cmeta">{ICON["cal"]} <span style="color:var(--text)">{ev}</span> · {evd}</div>' if not compact else ''
    return f'''<article class="card {extra}" style="--hue:{HUE[ty]}">
  <div class="ctop"><span class="ticon">{ICON[ty]}</span><div class="ctitle">{t}</div></div>
  {evline}
  <div class="cmeta">{''.join(chips)}</div>
  <div class="cfoot">{av(a)}<span class="req">from {PEOPLE[r][0].split()[0]}</span>{links(lk)}</div>
</article>'''

def stats():
    t = [('Open', '6', 'not delivered yet', 'var(--c-blue)', ICON['inbox'], ''), ('In progress', '2', 'being designed now', 'var(--c-amber)', ICON['spark'], ''),
         ('Waiting on me', '2', 'assigned or to approve', 'var(--c-violet)', ICON['user'], ''), ('Due soon', '3', '2 overdue', 'var(--c-coral)', ICON['clock'], 'alert')]
    return '<div class="stats">' + ''.join(f'<div class="tile {al}"><small><span class="chip" style="--hue:{h}">{i}</span>{l}</small><strong>{v}</strong><em>{s}</em></div>' for l, v, s, h, i, al in t) + '</div>'

def sidebar():
    items = ['Home', 'Buildspace', 'Events', 'Requests &amp; Finance', 'Design', 'People']
    return '<nav class="side"><b>cseed<span style="color:#7cb342">❦</span></b>' + ''.join(f'<span class="{"on" if i == "Design" else ""}">{i}</span>' for i in items) + '</nav>'

def peek(c, style=''):
    t, ty, lane, appr, ev, evd, d, ds, a, r, hi, lk = c
    order = ['todo', 'doing', 'review', 'done']
    k = order.index(lane)
    steps = ''.join(f'<span class="{"on" if i <= k else ""}"></span>' for i in range(4))
    labs = ''.join(f'<span>{"<b>" if i == k else ""}{n}{"</b>" if i == k else ""}</span>' for i, (_, n) in enumerate(LANES))
    lk_label = 'Figma' if lk == 'figma' else 'Canva'
    file_btn = (f'<span class="dlw on">{ICON[lk]} Open in {lk_label} <small>↗</small></span>' if lk else
                f'<span class="dlw off" title="Not linked yet">{ICON["figma"]} Design file <small>Not linked yet</small></span>')
    return f'''<aside class="peek" style="--hue:var(--c-amber);{style}">
  <div class="eb"><span>Design request · Social</span><span>⤢ &nbsp; ✕</span></div>
  <h4>{t}</h4>
  <div class="steps">{steps}</div><div class="steplab">{labs}</div>
  <div class="acts">{file_btn}<span class="dlw off" title="Not linked yet">{ICON["folder"]} Folder <small>Not linked yet</small></span></div>
  <dl class="kv"><dt>Event</dt><dd>{ICON["cal"]} {ev} · {evd}</dd><dt>Due</dt><dd>{due(c)}</dd>
  <dt>Designer</dt><dd>{av(a)} {PEOPLE[a][0] if a else "Unassigned"}</dd><dt>Requested by</dt><dd>{av(r)} {PEOPLE[r][0]}</dd>
  <dt>Formats</dt><dd>1080 × 1350 PNG, story 1080 × 1920</dd></dl>
  <div class="sec"><h5>Brief</h5><p class="brief">Invite mentors and students to coffee on Tue Nov 17, 4 PM at the Atrium café. Friendly, warm; include the RSVP link and our logo.</p></div>
  <div class="sec"><h5>Activity</h5><div class="tl">
   <div>{av("RP", 20)}<p><b>Riley</b> moved it to <b>Doing</b><br><small>2 h ago</small></p></div>
   <div>{av("PS", 20)}<p><b>Priya</b> “Can we try the yellow brand colour?”<br><small>Yesterday</small></p></div></div></div>
  <div class="acts"><span class="btn btn--primary">Move to Review →</span><span class="btn">Comment</span></div>
</aside>'''

def confetti(x, y):
    cols = ['var(--c-green)', 'var(--brand)', 'var(--c-blue)', 'var(--c-coral)', 'var(--c-violet)']
    import math
    bits = ''
    for i in range(14):
        ang = i / 14 * 2 * math.pi
        dx, dy = math.cos(ang) * (26 + (i % 3) * 10), math.sin(ang) * (22 + (i % 2) * 12)
        bits += f'<i style="left:{dx:.0f}px;top:{dy:.0f}px;background:{cols[i % 5]};transform:rotate({i * 37}deg)"></i>'
    return f'<div class="celebrate conf" style="left:{x}px;top:{y}px">{bits}</div>'

def link_states():
    figs = [
        ('Not linked (rest)', '<span class="dlb off">' + ICON['figma'] + '</span>'),
        ('Not linked (hover: tooltip)', '<span class="dlb off hover">' + ICON['folder'] + '<span class="tip">Folder · Not linked yet</span></span>'),
        ('Keyboard focus', '<span class="dlb off focus">' + ICON['figma'] + '</span>'),
        ('Linked Figma', '<span class="dlb on figma">' + ICON['figma'] + '</span>'),
        ('Linked Canva (hover)', '<span class="dlb on canva hover">' + ICON['canva'] + '<span class="tip">Open in Canva ↗</span></span>'),
        ('Linked (pressed)', '<span class="dlb on folder press">' + ICON['folder'] + '</span>'),
        ('Detail: not linked', '<span class="dlw off">' + ICON['figma'] + ' Design file <small>Not linked yet</small></span>'),
        ('Detail: linked', '<span class="dlw on">' + ICON['figma'] + ' Open in Figma <small>↗</small></span>'),
    ]
    return '<div class="states">' + ''.join(f'<figure>{h}<figcaption>{c}</figcaption></figure>' for c, h in figs) + '</div>'

def page(title, why, desk, phones, extra_css=''):
    desk_drag = re.sub(r'<aside class="peek".*?</aside>', '', desk, flags=re.S)
    desk_peek = re.sub(r' (lift|target|nope)(?=[" ])', ' ', desk).replace('<div class="drop"></div>', '')
    desk_peek = desk_peek.replace('<div class="nohint">Only the requester approves into Done</div>', '')
    desk_peek = re.sub(r'<div class="celebrate.*?</div>|<div class="toast">.*?</div>', '', desk_peek, flags=re.S)
    desk_drag = re.sub(r' sel(?=[" ])', ' ', desk_drag)
    return f'''<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{html.escape(title)}</title><style>{TOKENS}{BASE}{extra_css}</style></head><body>
<button class="btn toggle" onclick="var d=document.documentElement;d.dataset.theme=d.dataset.theme==='dark'?'light':'dark'">Light / dark</button>
<div class="doc"><h1>{title}</h1><p class="why">{why}</p>
<h2>Desktop · 1440 · dragging a card (allowed lane dashed, disallowed faded)</h2><div class="scroll">{desk_drag}</div>
<h2>Desktop · 1440 · card opened: detail “peek” panel over the board</h2><div class="scroll">{desk_peek}</div>
<h2>Phone · 390</h2><div class="scroll"><div class="phones">{phones}</div></div>
<h2>Design-file buttons · states (DesignLinks)</h2>{link_states()}
<p class="why" style="margin-top:16px">Illustrative only: names, events and the confetti pattern are sample content. Figma/Canva use generic pen/palette glyphs (no brand logos), chosen from the URL.</p>
</div></body></html>'''

def by_lane(lane):
    return [c for c in CARDS if c[2] == lane]

def phone_tabs(active='todo', counts=(3, 2, 1, 2)):
    return '<div class="tabs4">' + ''.join(f'<span class="{"on" if k == active else ""}"><b>{n}</b>{lab}</span>' for (k, lab), n in zip(LANES, counts)) + '</div>'

# ───────────────────────────── A: Studio board (Linear/Trello) ─────────────────────────────
def option_a():
    cols = ''
    pct = {'todo': 0, 'doing': 40, 'review': 70, 'done': 100}
    for k, lab in LANES:
        cs = by_lane(k)
        cls = {'review': 'target', 'done': 'nope'}.get(k, '')
        body = ''.join(card(c, 'lift' if c[0] == 'Demo Day teaser' else ('sel' if c[0] == 'Mentor coffee social' else '')) for c in cs)
        if k == 'review':
            body += '<div class="drop"></div>'
        if k == 'done':
            body = '<div class="nohint">Only the requester approves into Done</div>' + body
        foot = '<div class="addq">+ Request design</div>' if k == 'todo' else ''
        cols += f'''<section class="col {k} {cls}"><div class="chead"><div class="row"><span class="dot"></span><b>{lab}</b><span class="n">{len(cs)}</span></div>
        <div class="prog"><i style="width:{max(len(cs)*16,6)}%"></i></div></div>{body}{foot}</section>'''
    desk = f'''<div class="frame"><div class="desk">{sidebar()}<div class="main">
<h3 class="pt">Design</h3>{stats()}
<div class="bar"><span class="btn btn--primary">+ New request</span><span style="width:8px"></span>
<span class="fchip" aria-pressed="true">All <span class="n">8</span></span><span class="fchip">Mine <span class="n">2</span></span><span class="fchip">Due soon <span class="n">3</span></span><span class="fchip">Unassigned <span class="n">2</span></span>
<span class="fchip" style="--hue:var(--c-blue)">{ICON["poster"]}</span><span class="fchip">{ICON["social"]}</span><span class="fchip">{ICON["slides"]}</span><span class="fchip">{ICON["video"]}</span>
<span class="grow"></span><span class="seg"><span class="on">Board</span><span>List</span></span></div>
<div class="cols">{cols}</div>
{peek(by_lane("doing")[0])}
</div></div></div>'''
    phone1 = f'''<div class="phone"><div class="ptop"><span>☰</span>Design<span>＋</span></div><div class="pbody">
<div class="pstats"><div class="tile"><small>Open</small><strong>6</strong></div><div class="tile alert"><small>Due soon</small><strong>3</strong></div></div>
{phone_tabs()}
<div style="display:flex;gap:6px;overflow:hidden"><span class="fchip" aria-pressed="true">All</span><span class="fchip">Mine</span><span class="fchip">Due soon</span></div>
{''.join(card(c) for c in by_lane("todo"))}</div></div>'''
    phone2 = f'''<div class="phone"><div class="ptop"><span>☰</span>Design<span>＋</span></div><div class="pbody">{phone_tabs("doing")}{card(by_lane("doing")[0])}</div>
<div class="scrim"></div><div class="sheet" style="--hue:var(--c-amber)"><div class="grab"></div>
<div class="eb" style="font-size:11px;color:var(--muted);text-transform:uppercase;letter-spacing:.08em">Design request · Social</div>
<h4 style="margin:0;font-size:19px">Mentor coffee social</h4><div class="steps"><span class="on"></span><span class="on"></span><span></span><span></span></div>
<div class="acts"><span class="dlw on">{ICON["figma"]} Figma <small>↗</small></span><span class="dlw off">{ICON["folder"]} Folder <small>Not linked</small></span></div>
<dl class="kv"><dt>Due</dt><dd>{due(by_lane("doing")[0])}</dd><dt>Designer</dt><dd>{av("RP")} Riley Park</dd><dt>Event</dt><dd>Mentor Coffee · Tue Nov 17</dd></dl>
<div class="acts"><span class="btn btn--primary" style="flex:1;justify-content:center">Move to Review</span><span class="btn">Move…</span></div></div></div>'''
    return page('Option A · Studio board',
        'A calm, Linear-style board with Trello-size drop lanes. Four full-height columns (To do / Doing / Review / Done), each with a count and a thin progress bar. '
        'Rich cards: type icon in its colour, event and date, a due chip that turns amber and red, assignee avatar, requester, and the two design-file buttons. Quick filters are chips with counts; type filters are icon chips. '
        'Clicking a card opens a <b>peek panel</b> over the right of the board (deep-linkable), so you keep your place. While dragging, allowed columns get a dashed outline, and columns you can\'t drop into fade with the reason. '
        'On phones, a 4-tab stage switcher shows one column at a time, and the detail opens as a bottom sheet with a big “Move to …” button. This is the most familiar option and the cheapest to build.',
        desk, phone1 + phone2)

# ───────────────────────────── B: Event swimlanes (GitHub Projects / Jira) ─────────────────────────────
def option_b():
    lanes = [('Hack Night', 'Fri Nov 13', 'Today', 'var(--negative)', ['Hack Night poster']),
             ('Mentor Coffee', 'Tue Nov 17', 'in 4 days', 'var(--attention)', ['Mentor coffee social']),
             ('Founders Panel', 'Thu Dec 3', 'in 20 days', 'var(--muted)', ['Founders panel slides']),
             ('Demo Day', 'Fri Dec 11', 'in 4 weeks', 'var(--muted)', ['Demo Day teaser']),
             ('BuildHer Workshop', 'Sat Dec 5', 'in 22 days', 'var(--muted)', ['BuildHer workshop tees']),
             ('Past events', '', '', 'var(--muted)', ['Fall Kickoff recap slides', 'Welcome week banner', 'Speaker series carousel'])]
    head = '<div class="lhead"><span></span>' + ''.join(f'<div class="chead {k}"><div class="row"><span class="dot"></span><b>{lab}</b><span class="n">{len(by_lane(k))}</span></div></div>' for k, lab in LANES) + '</div>'
    rows = ''
    for i, (ev, date, rel, tone, titles) in enumerate(lanes):
        cs = [c for c in CARDS if c[0] in titles]
        done = sum(1 for c in cs if c[2] == 'done')
        mon, day = (date.split()[1], date.split()[2]) if date else ('', '')
        cells = ''
        for k, _ in LANES:
            inner = ''.join(card(c, 'lift sel' if c[0] == 'Mentor coffee social' else '', compact=True) for c in cs if c[2] == k)
            target = ' target' if (ev == 'Mentor Coffee' and k in ('review', 'todo')) else ''
            nope = ' nope' if (ev == 'Mentor Coffee' and k == 'done') else ''
            if target and k == 'review':
                inner += '<div class="drop"></div>'
            cells += f'<div class="cell {k}{target}{nope}">{inner}</div>'
        date_block = (f'<div class="evdate"><small>{mon}</small><b>{day}</b></div>' if date else '<div class="evdate"><small>—</small></div>')
        rows += f'''<div class="lane"><div class="lmeta">{date_block}<div><b>{ev}</b><br><small style="color:{tone}">{rel}</small>
          <div class="lprog"><i style="width:{(done/len(cs)*100) if cs else 0:.0f}%"></i></div><small>{done}/{len(cs)} delivered</small></div></div>{cells}</div>'''
    needs = '''<div class="needs"><b>Needs design</b><span class="pill soon">Robotics Social · Dec 9</span><span class="pill soon">Alumni Night · Dec 14</span><span class="addq">+ Request for an event</span></div>'''
    desk = f'''<div class="frame"><div class="desk">{sidebar()}<div class="main">
<h3 class="pt">Design</h3>{stats()}
<div class="bar"><span class="btn btn--primary">+ New request</span><span style="width:8px"></span>
<span class="fchip" aria-pressed="true">All</span><span class="fchip">Mine <span class="n">2</span></span><span class="fchip">Due soon <span class="n">3</span></span>
<span class="grow"></span><span class="seg"><span class="on">By event</span><span>By designer</span><span>Flat</span></span></div>
{needs}
<div class="sw">{head}{rows}</div>
{peek(by_lane("doing")[0])}
</div></div></div>'''
    css = '''.sw{display:grid;gap:8px}.lhead,.lane{display:grid;grid-template-columns:180px repeat(4,minmax(0,1fr));gap:8px}
.lane{background:var(--surface);border:1px solid var(--line);border-radius:var(--block-radius);padding:8px;box-shadow:var(--block-shadow)}
.lmeta{display:flex;gap:10px;align-items:flex-start;font-size:13px;padding:4px}.lmeta small{font-size:11.5px;color:var(--muted)}
.evdate{flex:none;width:40px;border-radius:10px;background:var(--sunken);border:1px solid var(--line);text-align:center;padding:4px 0;display:grid}
.evdate small{font-size:10px;text-transform:uppercase;color:var(--muted)}.evdate b{font-size:17px;line-height:1}
.lprog{height:4px;border-radius:4px;background:color-mix(in srgb,var(--c-green) 15%,var(--surface));margin:6px 0 2px;width:110px}.lprog i{display:block;height:100%;border-radius:4px;background:var(--c-green)}
.cell{background:var(--sunken);border-radius:12px;padding:6px;display:grid;gap:6px;align-content:start;min-height:64px;border:1px solid transparent}
.cell.target{border:2px dashed color-mix(in srgb,var(--hue) 60%,var(--line));background:color-mix(in srgb,var(--hue) 6%,var(--sunken))}.cell.nope{opacity:.45}
.cell .card{padding:9px;gap:6px}.cell .ctitle{font-size:13px}
.needs{display:flex;gap:8px;align-items:center;flex-wrap:wrap;margin:0 0 12px 0;padding:10px 12px;border:1px dashed color-mix(in srgb,var(--c-amber) 50%,var(--line));border-radius:12px;font-size:13px;background:color-mix(in srgb,var(--c-amber) 5%,var(--surface))}
.acc{border:1px solid var(--line);border-radius:14px;background:var(--surface);padding:10px;display:grid;gap:8px}
.acc .lmeta{padding:0}.minis{display:flex;gap:4px}.minis span{flex:1;height:6px;border-radius:3px;background:var(--line)}'''
    lane_phone = ''
    for ev, date, rel, tone, titles in lanes[:3]:
        cs = [c for c in CARDS if c[0] in titles]
        mon, day = date.split()[1], date.split()[2]
        open_ = ev == 'Mentor Coffee'
        lane_phone += f'''<div class="acc"><div class="lmeta"><div class="evdate"><small>{mon}</small><b>{day}</b></div><div style="flex:1"><b>{ev}</b><br><small style="color:{tone}">{rel} · {len(cs)} request</small></div><span>{"▾" if open_ else "▸"}</span></div>
        {''.join(card(c) for c in cs) if open_ else '<div class="cmeta">' + ''.join(f'<span class="pill">{[n for k,n in LANES if k==c[2]][0]}</span>' for c in cs) + '</div>'}</div>'''
    phone1 = f'''<div class="phone"><div class="ptop"><span>☰</span>Design<span>＋</span></div><div class="pbody">
<div class="pstats"><div class="tile"><small>Open</small><strong>6</strong></div><div class="tile alert"><small>Due soon</small><strong>3</strong></div></div>
<div style="display:flex;gap:6px"><span class="fchip" aria-pressed="true">By event</span><span class="fchip">Mine</span><span class="fchip">Due soon</span></div>
{lane_phone}</div></div>'''
    phone2 = f'''<div class="phone"><div class="ptop"><span>☰</span>Design<span>＋</span></div><div class="pbody">{card(by_lane("doing")[0], "lift")}
<div class="nohint" style="text-align:left">Move “Mentor coffee social” to…</div>
<div style="display:grid;gap:8px">
<span class="btn" style="justify-content:space-between;height:44px"><span>To do</span><small style="color:var(--muted)">←</small></span>
<span class="btn" style="justify-content:space-between;height:44px;border:2px solid var(--c-amber)"><span>Doing · current</span></span>
<span class="btn btn--primary" style="justify-content:space-between;height:44px"><span>Review</span><span>→</span></span>
<span class="btn" style="justify-content:space-between;height:44px;opacity:.45"><span>Done</span><small>Requester approves</small></span></div></div></div>'''
    return page('Option B · Event swimlanes',
        'Design work exists for events, so the board is grouped by event (GitHub Projects “group by”, Jira swimlanes). Each upcoming event is a row: a date tile, a countdown that turns red as it nears, and an x/y delivered progress bar. '
        'The four stages are columns across all rows. The separate “Upcoming events” rail becomes a single <b>Needs design</b> strip of events that have no request yet. A segmented switch regroups by designer or shows a flat board. '
        'Dragging stays inside the row, and disallowed cells fade. The detail is the same peek panel. On phones, events become collapsible cards, and moving a card uses a big-target “Move to…” sheet that greys out stages you can\'t move to. '
        'This answers “are we ready for this event?” at a glance. It is denser, and cards with no event go in a “No event / past” row.',
        desk, phone1 + phone2, css)

# ───────────────────────────── C: Pipeline + focus (Height / Asana) ─────────────────────────────
def option_c():
    pipe = ''
    counts = {k: len(by_lane(k)) for k, _ in LANES}
    for k, lab in LANES:
        pipe += f'<div class="pseg {k}" style="flex:{max(counts[k],1)}"><b>{counts[k]}</b> {lab}</div>'
    focus = ''.join(f'<div class="fcard" style="--hue:{HUE[c[1]]}"><span class="ticon">{ICON[c[1]]}</span><div><b>{c[0]}</b><br>{due(c)}</div>{av(c[8])}</div>'
                    for c in [CARDS[0], CARDS[5], CARDS[3]])
    cols = ''
    for k, lab in LANES:
        cs = by_lane(k)
        cls = {'review': 'target', 'done': 'nope'}.get(k, '')
        if k == 'done':
            body = '<div class="shipped">' + ''.join(f'<div class="ship" style="--hue:{HUE[c[1]]}"><span class="thumb">{ICON[c[1]]}</span><b>{c[0]}</b><small>{c[4]} · {links(c[11])}</small></div>' for c in cs) + '</div>'
            body += '<div class="nohint">Only the requester approves into Done</div>'
        else:
            # group by assignee inside the column
            groups = {}
            for c in cs:
                groups.setdefault(c[8], []).append(c)
            body = ''
            for a, gc in groups.items():
                body += f'<div class="ghead">{av(a, 18)} {PEOPLE[a][0] if a else "Unassigned"} <span>{len(gc)}</span></div>' + ''.join(card(c, 'lift' if c[0] == 'Demo Day teaser' else ('sel' if c[0] == 'Mentor coffee social' else '')) for c in gc)
            if k == 'review':
                body += '<div class="drop"></div>'
        cols += f'<section class="col {k} {cls}"><div class="chead"><div class="row"><span class="dot"></span><b>{lab}</b><span class="n">{len(cs)}</span></div></div>{body}</section>'
    desk = f'''<div class="frame"><div class="desk">{sidebar()}<div class="main">
<h3 class="pt">Design</h3>{stats()}
<div class="pipe">{pipe}</div>
<div class="focusrow"><div class="flabel">{ICON["clock"]} <b>This week</b><small>3 need attention</small></div>{focus}</div>
<div class="bar"><span class="btn btn--primary">+ New request</span><span class="fchip" aria-pressed="true">All</span><span class="fchip">Mine</span><span class="fchip">Due soon</span><span class="grow"></span><span class="seg"><span class="on">Board</span><span>List</span></span></div>
<div class="cols">{cols}</div>
{peek(by_lane("doing")[0])}
{confetti(1010, 560)}
<div class="toast">{ICON["check"]} “Speaker series carousel” delivered <u>Undo</u></div>
</div></div></div>'''
    css = '''.pipe{display:flex;gap:4px;margin:0 0 12px;height:34px}.pseg{display:flex;align-items:center;gap:6px;padding:0 12px;border-radius:10px;font-size:12.5px;color:var(--text);background:color-mix(in srgb,var(--hue) 14%,var(--surface));border:1px solid color-mix(in srgb,var(--hue) 35%,var(--line));min-width:96px}
.pseg b{font-size:15px}.pipe--sm .pseg{min-width:0;padding:0 8px}
.focusrow{display:grid;grid-template-columns:150px repeat(3,1fr);gap:10px;margin:0 0 14px;align-items:stretch}
.flabel{display:grid;align-content:center;gap:2px;font-size:13px}.flabel small{color:var(--muted);font-size:12px}
.fcard{display:flex;gap:10px;align-items:center;padding:10px 12px;border-radius:14px;background:var(--surface);border:1px solid var(--line);box-shadow:var(--block-shadow);font-size:13px}.fcard>div{flex:1;min-width:0}
.ghead{display:flex;align-items:center;gap:6px;font-size:12px;color:var(--muted);padding:2px 4px}.ghead span{margin-left:auto}
.shipped{display:grid;grid-template-columns:1fr 1fr;gap:8px}.ship{background:var(--surface);border:1px solid var(--line);border-radius:12px;padding:8px;display:grid;gap:4px;font-size:12px}
.ship .thumb{height:64px;border-radius:8px;display:grid;place-items:center;color:var(--hue);background:linear-gradient(135deg,color-mix(in srgb,var(--hue) 22%,var(--surface)),color-mix(in srgb,var(--brand) 25%,var(--surface)))}
.ship .thumb svg{width:26px;height:26px}.ship small{color:var(--muted);display:flex;align-items:center;justify-content:space-between;gap:4px}.ship .dlb{width:22px;height:22px}
.stack h6{position:sticky;top:0;margin:4px 0 0;font-size:12px;display:flex;align-items:center;gap:6px;color:var(--muted);text-transform:uppercase;letter-spacing:.06em}.stack h6 i{width:8px;height:8px;border-radius:50%;background:var(--hue)}'''
    phone1 = f'''<div class="phone"><div class="ptop"><span>☰</span>Design<span>＋</span></div><div class="pbody">
<div class="pipe pipe--sm" style="height:30px">{pipe.replace(" To do","").replace(" Doing","").replace(" Review","").replace(" Done","")}</div>
<div class="fcard" style="--hue:var(--c-blue)"><span class="ticon">{ICON["poster"]}</span><div><b>Hack Night poster</b><br>{due(CARDS[0])}</div></div>
<div class="stack"><h6 class="todo"><i></i>To do · 3</h6></div>{card(CARDS[1])}
<div class="stack"><h6 class="doing"><i></i>Doing · 2</h6></div>{card(CARDS[3])}</div></div>'''
    phone2 = f'''<div class="phone"><div class="ptop"><span>☰</span>Design<span>＋</span></div><div class="pbody"><div class="stack"><h6 class="review"><i></i>Review · 1</h6></div>{card(CARDS[5])}
<div class="stack"><h6 class="done"><i></i>Done · 2</h6></div><div class="shipped">{''.join(f'<div class="ship" style="--hue:{HUE[c[1]]}"><span class="thumb">{ICON[c[1]]}</span><b>{c[0]}</b></div>' for c in by_lane("done"))}</div></div>
<div class="toast" style="bottom:28px">{ICON["check"]} Delivered <u>Undo</u></div></div>'''
    return page('Option C · Pipeline + focus',
        'Built to feel alive (Height and Asana energy). Under the stat tiles, a <b>pipeline bar</b> shows how the work splits across To do / Doing / Review / Done, sized by count. Below it, a <b>This week</b> strip pins the 3 requests that need attention (overdue, due soon, waiting on approval). '
        'The board groups cards inside each column by designer, so you can see who is busy. <b>Done</b> is a small gallery of delivered work with a colour thumbnail per type. '
        'Landing a card in Done plays a short confetti burst and shows a toast with Undo, all off under reduced motion. The detail is the peek panel. On phones, it becomes one scrolling stack with sticky stage headers, the focus card on top and the Done gallery at the bottom. '
        'It is the most distinctive and has the most to build. The pipeline and focus strip partly repeat the stat tiles.',
        desk, phone1 + phone2, css)

files = {'A-studio-board.html': option_a(), 'B-event-swimlanes.html': option_b(), 'C-pipeline-focus.html': option_c()}
for name, body in files.items():
    open(os.path.join(OUT, name), 'w').write(body)

index = f'''<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Design Kanban mockups</title>
<style>{TOKENS}
.wrap{{max-width:1500px;margin:0 auto;padding:24px 16px}}h1{{font-size:22px;margin:0 0 6px}}p{{color:var(--muted);max-width:900px}}
.opt{{background:var(--surface);border:1px solid var(--line);border-radius:16px;padding:16px;margin:16px 0;box-shadow:var(--block-shadow)}}
.opt h2{{margin:0 0 8px;font-size:17px;display:flex;gap:10px;align-items:center}}.opt h2 a{{font-size:13px;font-weight:500}}
.vp{{width:100%;height:760px;overflow:hidden;border-radius:12px;border:1px solid var(--line);position:relative}}
.vp iframe{{width:1560px;height:1400px;border:0;transform:scale(.62);transform-origin:0 0}}
@media (max-width:900px){{.vp{{height:420px}} .vp iframe{{transform:scale(.24)}}}}
</style></head><body><div class="wrap"><h1>Design page · Kanban mockups (V0.10)</h1>
<p>Three directions for the Design request board. All three use the real CSEED tokens and the Home tile look. Each one shows the 4 columns (To do / Doing / Review / Done), a rich card, the detail panel, the Figma/Canva and Folder buttons, desktop at 1440 and phone at 390. Open an option on its own for full size and a light/dark toggle.
Step 0 (the drag fix) is already committed separately. Research notes: research.md.</p>
''' + ''.join(f'''<section class="opt"><h2>{t} <a href="{f}" target="_blank">Open full size ↗</a></h2><div class="vp"><iframe src="{f}" loading="lazy" title="{t}"></iframe></div></section>'''
              for f, t in [('A-studio-board.html', 'A · Studio board'), ('B-event-swimlanes.html', 'B · Event swimlanes'), ('C-pipeline-focus.html', 'C · Pipeline + focus')]) + '</div></body></html>'
open(os.path.join(OUT, 'index.html'), 'w').write(index)
print('ok')
