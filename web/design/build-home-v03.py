"""Build the D1 design fixture; no application behavior or API integration."""
from pathlib import Path
import runpy
p=Path(__file__).resolve().parent
base=runpy.run_path(str(p/'build-mockups.py'))
shell=base['shell']
body='''<div class="toolbar home-preview" role="group" aria-label="Prototype controls">
<label>Preview role <select class="select" id="role"><option>officer</option><option selected>director</option><option>leadership</option><option>owner</option></select></label>
<label>Data state <select class="select" id="state"><option value="disconnected">Not connected</option><option value="connected">Connected sample</option><option value="empty">Empty</option><option value="loading">Loading</option><option value="error">Error</option><option value="mixed">Mixed: one sync failed</option></select></label>
<span class="small muted">Design preview · sample data</span><p class="small home-role-note" id="role-note"></p></div>
<div class="toolbar"><div><h2>Your home</h2><p class="small muted" id="layout-note">Your layout follows you across devices.</p></div><div class="action-row"><button class="btn" id="gallery-open">Add widget</button><button class="btn btn--primary" id="edit">Edit home</button></div></div>
<div class="edit-bar" id="edit-bar" hidden><div class="edit-bar-text"><span class="edit-bar-title">Arrange your home</span><span class="edit-bar-hint">Drag a grip, or use arrow keys to move. Choose S, M or L.</span></div><div class="action-row"><button class="btn" id="edit-add">Add widget</button><button class="btn btn--primary" id="done">Done</button></div></div>
<ul class="widget-grid home-widgets" id="widgets" aria-label="Your widgets"></ul>
<div class="state state--brand" id="blank" hidden><h2 class="display display--m">a little room to grow.</h2><p>Add the things you want to see first.</p><button class="btn" id="blank-add">Explore widgets</button></div>
<p class="small muted" id="sync-line"></p>
<p class="small muted" id="feedback" role="status" aria-live="polite"></p>
<div class="toast" id="undo" hidden role="status">Widget removed. <button class="btn btn--small" id="undo-button">Undo</button></div>'''
gallery='''<dialog class="gallery" id="gallery" aria-labelledby="gallery-title"><div class="gallery-inner"><header class="gallery-head"><div><p class="brand-eyebrow">a home for your work</p><h2 class="display display--l" id="gallery-title">make home <span class="mark">yours.</span></h2></div><button class="icon-btn" id="gallery-close" aria-label="Close gallery">×</button><p class="brand-lede">A few useful windows into CSEED. Pick a size, then add. You can always change your mind.</p></header><div class="gallery-cats" role="group" aria-label="Widget categories"><button class="chip" aria-pressed="true" data-category="All">All</button><button class="chip" aria-pressed="false" data-category="Club">Club</button><button class="chip" aria-pressed="false" data-category="Cloudflare">Cloudflare</button><button class="chip" aria-pressed="false" data-category="Members">Members</button></div><div class="gallery-body"><ul class="gallery-grid" id="gallery-items"></ul></div></div></dialog>'''
html=shell('home','hey alex, welcome in.','Sunday, November 1 · Fall 2026 · CSEED officer workspace',body,gallery)
html=html.replace('<li><a class="nav-link" href="admin.html"', ''.join('<li><a class="nav-link" href="'+key+'.html">'+base['icon'](ico)+'<span class="nav-label">'+label+'</span></a></li>' for key,label,ico in [('officers','Officers','user'),('members','Members','user'),('speakers','Speakers & Mentors','message'),('design','Design','file')])+'<li><a class="nav-link" href="admin.html"')
html=html.replace('<h1>','<h1 class="display">',1).replace('</head>','<link rel="stylesheet" href="../v03.css"><link rel="stylesheet" href="../home-v03.css"></head>')
html=html.replace('</body>','<script src="../home-v03.js"></script></body>')
(p/'mockups/home-v03.html').write_text(html)
