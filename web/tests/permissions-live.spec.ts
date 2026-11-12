import { expect, test, type Page } from '@playwright/test';
import { devSignIn, settle } from './helpers';

/**
 * Permission changes reach an open tab without a reload: leadership changes Priya's role in
 * one browser, and Priya's already-open Officers page follows on her next request or when the
 * tab regains focus.
 */
test.describe('permission changes reach open tabs', () => {
  test.beforeEach(({}, info) => test.skip(info.project.name !== 'desktop', 'one viewport is enough: this is about state, not layout'));

  const setRole = async (lead: Page, role: 'director' | 'officer') => {
    const res = await lead.request.patch('/api/admin/users/user-priya', { data: { role }, headers: { origin: new URL(lead.url()).origin } });
    expect(res.status(), await res.text()).toBe(200);
  };

  test('demotion and promotion apply in place', async ({ browser, baseURL }) => {
    const leadCtx = await browser.newContext({ baseURL });
    const priyaCtx = await browser.newContext({ baseURL });
    const lead = await leadCtx.newPage();
    const priya = await priyaCtx.newPage();
    try {
      await devSignIn(lead, { as: 'leadership' });
      await setRole(lead, 'director');

      await devSignIn(priya, { discord: '100000000000000002' }, '/officers?tab=access');
      await settle(priya);
      const tab = (name: string) => priya.getByRole('tab', { name, exact: true });
      await expect(tab('Access')).toBeVisible();
      await expect(priya.locator('html')).toHaveAttribute('data-role', 'director');

      // Demoted mid-session: her next request (opening the director-only Audit log, which the
      // server now refuses) updates the page in place instead of showing a broken error state.
      await setRole(lead, 'officer');
      await tab('Audit log').click();
      await expect(tab('Access')).toHaveCount(0);
      await expect(tab('Audit log')).toHaveCount(0);
      await expect(priya.locator('[data-directory]')).toBeVisible();
      await expect(priya.locator('html')).toHaveAttribute('data-role', 'officer');
      await expect(priya.locator('[data-user-chip]')).toHaveAttribute('data-user-chip', 'officer');
      await expect(priya.locator('[data-permissions-changed]')).toContainText('Your permissions changed');
      await expect(priya.getByText('Requires the director role')).toHaveCount(0);

      // Promoted while the tab sits in the background: coming back to it is enough
      // (the focus check runs at most every 10 seconds).
      await setRole(lead, 'director');
      await expect(async () => {
        await priya.evaluate(() => window.dispatchEvent(new Event('focus')));
        await expect(tab('Access')).toBeVisible({ timeout: 1_000 });
      }).toPass({ timeout: 20_000 });
      await expect(priya.locator('html')).toHaveAttribute('data-role', 'director');
    } finally {
      await setRole(lead, 'officer').catch(() => {});
      await leadCtx.close();
      await priyaCtx.close();
    }
  });
});
