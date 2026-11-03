# Dashboard arrangement draft

Refreshed for merge prep after PRs #42 and #43, November 3, 2026. Captured from the built app served by Go with its production CSP, using a disposable seeded database and leadership account. Website, Notion and Drive use the user-supplied club defaults; leadership can override them in Settings or clear a field to restore the default.

| State | Desktop light | Desktop dark | Phone light | Phone dark |
| --- | --- | --- | --- | --- |
| Home | [1440](home-1440-light.png) | [1440](home-1440-dark.png) | [390](home-390-light.png) | [390](home-390-dark.png) |
| Edit | [1440](edit-1440-light.png) | [1440](edit-1440-dark.png) | [390](edit-390-light.png) | [390](edit-390-dark.png) |
| Resize coachmark | [1440](tutorial-resize-1440-light.png) | [1440](tutorial-resize-1440-dark.png) | [390](tutorial-resize-390-light.png) | [390](tutorial-resize-390-dark.png) |

Defaults fill twenty logical blocks: a complete 4×5 rectangle on desktop, with full-width rows and paired stats on phones. Actions and activity use compact 2×1 cards with two preview items. Standard recent events shows three items; a wide card shows five.

The resize coachmark captures step 4 with a transparent cutout around the actual resize handle and temporary edit mode. Capture verifies production CSP headers, no CSP console errors, cutout coordinates, and disabled transitions under reduced motion.

Playwright covers pointer/keyboard move and resize, target size labels, save/cancel, legacy migration, row geometry, club default overrides, external new-tab navigation, rectangular packing, coachmark targets and restoration, Settings replay, and once-only tutorial behaviour after both onboarding Done and Back. Real touch-device ergonomics and sustained 60fps profiling remain unverified.
