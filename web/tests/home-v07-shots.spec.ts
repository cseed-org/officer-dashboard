import { test } from '@playwright/test';
import { devLogin, settle, useTheme } from './helpers';

/** V0.7 Home captures for design review (web/design/captures/v07). Run with SHOTS=1. */
const SHOTS = !!process.env.SHOTS;
const dir = 'design/captures/v07';
const LAYOUTS = {
  default: null,
  large: [
    { id: 'calendar', size: '2x2' }, { id: 'my-action', size: '2x2' },
    { id: 'kpi-upcoming', size: '2x1' }, { id: 'kpi-checkin', size: '2x1' },
    { id: 'kpi-budget', size: '2x1' }, { id: 'kpi-pending', size: '2x1' },
    { id: 'activity', size: '4x2' }, { id: 'budget', size: '4x2' }, { id: 'recent-events', size: '4x2' },
    { id: 'links', size: '2x2' }, { id: 'web-analytics', size: '2x2' }, { id: 'kpi-reflection', size: '2x1' }, { id: 'calendar-month', size: '4x2' },
  ],
} as const;

for (const theme of ['light', 'dark'] as const) {
  for (const [name, layout] of Object.entries(LAYOUTS)) {
    test(`home v07 capture · ${name} · ${theme}`, async ({ page }, info) => {
      test.skip(!SHOTS || info.project.name === 'tablet', 'captures only with SHOTS=1');
      await page.emulateMedia({ reducedMotion: 'reduce' });
      await useTheme(page, theme);
      await devLogin(page, 'director');
      const widgetLayout = layout ? layout.filter((w) => w.id !== 'calendar-month') : undefined;
      await page.request.put('/api/me/prefs', { data: { dashboardTutorialSeen: true, ...(widgetLayout ? { widgetLayout } : { widgetLayout: (await import('../src/ui/widgets/registry')).defaultWidgetLayout }) } });
      await page.goto('/');
      await settle(page);
      await page.waitForLoadState('networkidle');
      await page.screenshot({ path: `${dir}/home-${name}-${info.project.name}-${theme}.png`, fullPage: true });
    });
  }
}
