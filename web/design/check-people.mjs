// Run from the repository root: node web/design/check-people.mjs
// Drives the D3 Officers and Members prototypes at 1440 and 390 in both themes, asserts the
// role gating, states and interaction contract, and captures the overlay screenshots.
import { chromium } from '../node_modules/@playwright/test/index.mjs';
import { pathToFileURL } from 'node:url';
import { writeFile } from 'node:fs/promises';

const browser = await chromium.launch({executablePath: process.env.CHROMIUM_PATH || '/usr/bin/chromium', args: ['--no-sandbox', '--disable-gpu']});
const results = [];
const assert = (ok, msg) => { if (!ok) throw new Error(msg); };
let shots = 0;
try {
  for (const width of [1440, 390]) for (const theme of ['light', 'dark']) {
    const page = await browser.newPage({viewport: {width, height: 1000}, reducedMotion: 'reduce', colorScheme: theme});
    const errors = [];
    page.on('pageerror', e => errors.push(e.message));
    const tag = `${width} ${theme}`;
    const open = async name => {
      await page.goto(pathToFileURL(`${process.cwd()}/web/design/mockups/${name}-v03.html`).href + '?theme=' + theme);
      await page.locator('details.prototype summary').click();
    };
    const noOverflow = async name => assert(!(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth
      || [...document.querySelectorAll('dialog[open]')].some(d => d.scrollWidth > d.clientWidth + 1))), `${name} overflows at ${tag}`);
    const shot = async name => { await noOverflow(name); await page.screenshot({path: `web/design/shots/${name}-${width}-${theme}.png`, fullPage: true}); shots++; };
    const visible = sel => page.locator(sel).isVisible();
    const focusedText = () => page.evaluate(() => document.activeElement?.textContent?.trim());
    const rows = () => page.locator('#directory tbody tr').count();

    // ---------------- Officers
    await open('officers');
    assert(await rows() === 10, 'Officers directory should list 10 sample officers');
    assert(await page.locator('[data-person]').count() === 10, 'Every officer name should open a profile');

    await page.locator('#viewer').selectOption('Officer');
    assert(!(await visible('#invites')) && !(await visible('[data-open="invite"]')), `Officer sees invites (${tag})`);
    await page.locator('[data-person="maya"]').click();
    assert(!(await visible('#pf-phone')), `Officer phone leak (${tag})`);
    assert(!(await visible('#pf-access')) && !(await visible('#pf-owner')), 'Officer sees access controls');
    await page.keyboard.press('Escape');
    assert(await focusedText() === 'Maya Patel', 'Focus should return to the row that opened the profile');
    assert(!(await page.evaluate(() => document.querySelector('dialog[open]'))), 'Escape should close the drawer');

    await page.locator('#viewer').selectOption('Director');
    await page.locator('[data-person="maya"]').click();
    assert(await visible('#pf-phone'), 'Director should see phone');
    assert(!(await visible('#pf-access')) && await visible('#pf-access-note'), 'Director should see the leadership note, not role controls');
    await page.keyboard.press('Escape');

    await page.locator('#viewer').selectOption('Owner');
    await page.locator('[data-person="maya"]').click();
    assert(await page.locator('#new-role').inputValue() === 'Director', 'Role picker should preselect the current role');
    assert(await page.locator('#review-role').isDisabled(), 'Review should stay disabled until the role changes');
    assert(await visible('#pf-owner') && await visible('#make-owner'), 'Owner controls missing');
    assert(await page.evaluate(() => getComputedStyle(document.body).overflow) === 'hidden', 'Background should not scroll behind a dialog');
    await shot('officers-v03-profile-owner');
    await page.locator('#new-role').selectOption('Leadership');
    await page.locator('#review-role').click();
    assert((await page.locator('#confirm-body').textContent()).includes('from Director to Leadership'), 'Role confirm should describe the change');
    await shot('officers-v03-role-confirm');
    await page.locator('#confirm-go').click();
    assert((await page.locator('#profile [data-result]').textContent()).includes('is now Leadership'), 'Role change result should show in the profile');
    assert(await page.locator('#announce').isHidden(), 'Result should be announced once, inside the drawer');
    await page.keyboard.press('Escape');

    await page.locator('[data-person="alex"]').click();
    assert(await page.locator('#remove-owner').isDisabled(), 'The last owner cannot be removed');
    assert(!(await visible('#pf-access')), 'You cannot change your own role');
    await page.keyboard.press('Escape');

    await page.locator('[data-open="invite"]').click();
    await page.locator('#send-invite').click();
    await page.locator('#invite-status').filter({hasText: 'DM sent ✓'}).waitFor();
    assert(await visible('#invite-link') && await visible('#invite-done'), 'Sent state should show the link and Done');
    await shot('officers-v03-invite-sent');
    await page.locator('#send-invite').click();
    await page.locator('#invite-name').fill('Hana Sato');
    await page.locator('#invite-discord').fill('901234567812345678');
    await page.locator('#sim-dm-fail').check();
    await page.locator('#send-invite').click();
    await page.locator('#invite-error').waitFor();
    assert(await visible('#invite-link'), 'DM failure should keep the link as a fallback');
    await shot('officers-v03-invite-error');
    await page.keyboard.press('Escape');

    await page.locator('[data-open-own]').click();
    assert(await page.locator('#email-select option').last().isDisabled(), 'Sixth calendar email should be disabled');
    await shot('officers-v03-calendar');
    await page.locator('[data-remove-email]').last().click();
    assert(!(await page.locator('#email-select option').last().isDisabled()), 'Removing an email should re-enable Add another');
    await page.keyboard.press('Escape');

    await page.locator('#team-filter').selectOption('Design');
    assert(await page.locator('.people-group').count() === 1, 'Subteam filter failed');
    await page.locator('#team-filter').selectOption('All subteams');
    await page.locator('#status-filter').selectOption('Deactivated');
    assert(await rows() === 1, 'Status filter should show the one deactivated officer');
    await page.locator('#query').fill('zzz');
    assert(await visible('#fallback'), 'Filtered-to-nothing should show the empty state');
    for (const state of ['No officers yet', 'Loading', 'Error']) { await page.locator('#state-choice').selectOption(state); await noOverflow('officers ' + state); }

    // ---------------- Members
    await open('members');
    assert(await page.locator('[data-member]').count() === 8, 'Every member name should open a record');
    await page.locator('#campus-filter').selectOption('Bothell');
    await page.locator('#query').fill('a');
    assert((await page.locator('#directory tbody tr td:nth-child(3)').allTextContents()).every(t => t.includes('Bothell')), 'Search must keep the campus filter');
    await page.locator('#query').fill(''); await page.locator('#campus-filter').selectOption('All campuses');

    await page.locator('#viewer').selectOption('Officer');
    assert(!(await visible('#export')), 'Officers should not see CSV export');
    await page.locator('#viewer').selectOption('Director');

    await page.locator('[data-member="person_1086"]').click();
    assert(await page.locator('#review-changes').isDisabled(), 'Review needs a change');
    await shot('members-v03-detail');
    await page.locator('[data-field="pref"]').first().fill('Ave');
    assert(await page.locator('#review-changes').isDisabled(), 'Review needs an audit note');
    await page.locator('#audit-note').fill('Member asked to use “Ave” at Venture Night.');
    await page.locator('#review-changes').click();
    assert(await page.locator('#d1-diff tr').count() === 1, 'Confirm should list only the changed field');
    assert((await page.locator('#d1-diff').textContent()).includes('Preferred name'), 'Confirm diff should name the changed field');
    await shot('members-v03-confirm');
    await page.locator('#sim-d1-fail').check();
    await page.locator('#write-d1').click();
    assert(await visible('#d1-error'), 'Write failure should show an alert and keep the dialog');
    await page.locator('#sim-d1-fail').uncheck();
    await page.locator('#write-d1').click();
    assert((await page.locator('#member [data-result]').textContent()).includes('Changed Preferred name'), 'Missing write result');
    assert(await page.locator('#md-pref').textContent() === 'Ave', 'Saved value should show in the record');
    await page.keyboard.press('Escape');

    await page.locator('#next').click();
    assert((await page.locator('#page-label').textContent()).includes('Page 2'), 'Pagination failed');

    await page.locator('#state-choice').selectOption('Sync failed');
    assert(await rows() > 0, 'A failed sync must keep the last mirror visible');
    assert(await page.locator('#sync-status.notice--attention').count() === 1, 'Failed sync banner should use the attention tone');
    await shot('members-v03-sync-failed');
    for (const state of ['Empty', 'Loading', 'Page error', 'Not connected']) {
      await page.locator('#state-choice').selectOption(state);
      await noOverflow('members ' + state);
      if (state !== 'Empty') assert(await page.locator('#directory').isHidden(), `${state} should hide the table`);
    }

    assert(!errors.length, errors.join('\n'));
    results.push({width, theme, checks: ['role gating (officer, director, leadership, owner)', 'profile focus return and Escape', 'background scroll lock', 'role confirm with preselected role', 'last-owner protection', 'invite sent and DM failure', 'calendar limit and inline remove', 'subteam, status and empty filters', 'members search keeps filters', 'CSV export director+', 'D1 diff lists only changed fields, write failure keeps edits', 'pagination', 'sync failed keeps mirror', 'state and overlay overflow', 'no script errors']});
    await page.close();
  }
  await writeFile('web/design/shots/validation-people-interactions.json', JSON.stringify(results, null, 2) + '\n');
  console.log(`People interactions passed; ${shots} supplemental screenshots captured.`);
} finally { await browser.close(); }
