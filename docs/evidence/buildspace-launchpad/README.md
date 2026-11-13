# Buildspace + Launchpad rename

The page title, navigation label and rocket icon now identify the same builder register as Buildspace + Launchpad. No fields were added.

| Width | Before (historical V0.6 repository evidence) | After (this branch) |
| --- | --- | --- |
| Desktop 1440 | [Before](before-1440.png) | [After](after-1440.png) |
| Phone 390 | [Before](before-390.png) | [After](after-390.png) |

Before captures predate this branch and use different sample dates; compare naming and navigation rather than counts. The page-chrome and shared PersonDrawer work are owned by other Panes.

Migration 0023 renames all five tables and three indexes, preserving existing data and foreign keys. The old page returns 301 with query parameters preserved; old API paths remain aliases with unchanged authorization. Calendar source/item IDs and token scopes stay stable.

Go tests, contract check (241 responses, zero schema errors), Astro check (zero errors/warnings), and production build passed. Browser checks run serially at 1440, 820 and 390 with one worker, after checking load is at most 20.
