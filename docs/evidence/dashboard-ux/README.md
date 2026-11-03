# Dashboard arrangement draft

Refreshed after PR #44 review, November 3, 2026, using a disposable seeded backend and a leadership account. The club URLs remain unset so the leadership Add link rows are visible.

| State | Desktop light | Desktop dark | Phone light | Phone dark |
| --- | --- | --- | --- | --- |
| Home | [1440](home-1440-light.png) | [1440](home-1440-dark.png) | [390](home-390-light.png) | [390](home-390-dark.png) |
| Edit | [1440](edit-1440-light.png) | [1440](edit-1440-dark.png) | [390](edit-390-light.png) | [390](edit-390-dark.png) |
| Resize coachmark | [1440](tutorial-resize-1440-light.png) | [1440](tutorial-resize-1440-dark.png) | [390](tutorial-resize-390-light.png) | [390](tutorial-resize-390-dark.png) |

The resize coachmark captures step 4 with a transparent cutout around the actual resize handle and temporary edit mode. The capture script verified cutout coordinates against that handle and verified that reduced motion disables the tour transitions.

The Playwright home-grid spec covers pointer and keyboard movement/resizing, target size labels, save/cancel, legacy migration, compact Links row geometry, leadership link actions, every coachmark target, tutorial replay, and restoration of the original layout after temporary preview. Real touch-device ergonomics and sustained 60fps profiling remain unverified.
