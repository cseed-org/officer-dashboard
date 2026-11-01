"""Measures the V0.3 token pairs listed in contrast.md. Run: python3 web/design/contrast-v03.py"""
def h(c):c=c.lstrip('#');return [int(c[i:i+2],16)/255 for i in (0,2,4)]
def lin(v):return v/12.92 if v<=0.03928 else ((v+0.055)/1.055)**2.4
def L(c):r,g,b=map(lin,h(c));return .2126*r+.7152*g+.0722*b
def ratio(a,b):x,y=sorted([L(a),L(b)],reverse=True);return (x+.05)/(y+.05)
def mix(a,b,p):A,B=h(a),h(b);return '#'+''.join(f'{round((x*p+y*(1-p))*255):02x}' for x,y in zip(A,B))
pairs=[
('Light brand ink on paper','#141414','#e4e3e1'),
('Light brand ink-soft on paper','#3d3d3d','#e4e3e1'),
('Light brand control on paper','#76756f','#e4e3e1'),
('Light brand ink on paper-raised','#141414','#f4f4f4'),
('Light ink on yellow (leadership badge, highlighter)','#202320','#f5d140'),
('Light muted on sunken (Kanban column, calendar outside-month)','#646963','#f7f7f5'),
('Light text on amber event tint (worst tint)','#202320',mix('#b47c0c','#ffffff',.11)),
('Light text on coral event tint','#202320',mix('#d9634a','#ffffff',.11)),
('Light muted on amber event tint','#646963',mix('#b47c0c','#ffffff',.11)),
('Light positive on sunken (DM sent)','#326247','#f7f7f5'),
('Light caption on polaroid','#3d3d3d','#ffffff'),
('Light today marker (ink disc) date','#ffffff','#202320'),
('Light negative on white (overdue)','#a33732','#ffffff'),
('Dark brand ink on paper','#f1efe8','#1a1916'),
('Dark brand ink-soft on paper','#c4c1b8','#1a1916'),
('Dark brand control on paper','#87847c','#1a1916'),
('Dark brand ink on paper-raised','#f1efe8','#24231f'),
('Dark ink on yellow (leadership badge)','#202320','#f5d140'),
('Dark yellow segment on paper (non-text)','#f5d140','#1a1916'),
('Dark muted on sunken (Kanban column)','#a8aea5','#212421'),
('Dark text on amber event tint (worst tint)','#eeefeb',mix('#b98428','#1c1f1c',.18)),
('Dark text on blue event tint','#eeefeb',mix('#4f8ad8','#1c1f1c',.18)),
('Dark muted on amber event tint','#a8aea5',mix('#b98428','#1c1f1c',.18)),
('Dark positive on sunken (DM sent)','#94c6a5','#212421'),
('Dark caption on polaroid frame','#3d3d3d','#e8e6df'),
('Dark today marker (ink disc) date','#181a18','#eeefeb'),
('Yellow on light paper (decorative only)','#f5d140','#e4e3e1'),
]
for n,a,b in pairs:print(f'| {n} | `{a}` / `{b}` | {ratio(a,b):.2f}:1 |')
