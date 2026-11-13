import { test, expect } from "@playwright/test";
import { devLogin, useTheme, overflow, THEMES } from "./helpers";
import { defaultWidgetLayout } from "../src/ui/widgets/registry";

const makeMember = (n: number) => ({
  person_id: `person-${n}`,
  first_name: `Member ${String(n).padStart(4, "0")}`,
  last_name: "Seed",
  preferred_name: null,
  uw_email: `member${n}@uw.edu`,
  cs_email: null,
  personal_email: null,
  major: n % 2 ? "Computer Science" : "Design",
  grad_year: n % 2 ? "2028" : "2029",
  campus: n % 2 ? "Seattle" : "Bothell",
  pronouns: null,
  demographics: null,
  join_date: "2026-10-31",
  agreed_to_membership_agreement: 1,
  has_been_cseed_officer: n % 5 === 0 ? 1 : 0,
});
const members = Array.from({ length: 1001 }, (_, n) => makeMember(n));

for (const theme of THEMES) {
  test(`Members directory, pagination, filters and read-only detail · ${theme}`, async ({
    page,
  }) => {
    await useTheme(page, theme);
    await devLogin(page, "officer");
    await page.route("**/api/members?*", async (route) => {
      const q = new URL(route.request().url()).searchParams;
      const items = members.filter(
        (m) =>
          (!q.get("q") ||
            `${m.first_name} ${m.last_name} ${m.uw_email}`
              .toLowerCase()
              .includes(q.get("q")!.toLowerCase())) &&
          (!q.get("campus") || m.campus === q.get("campus")) &&
          (!q.get("gradYear") || m.grad_year === q.get("gradYear")) &&
          (!q.get("officer") ||
            Boolean(m.has_been_cseed_officer) === (q.get("officer") === "yes")),
      );
      const offset = Number(q.get("offset") || 0),
        limit = Number(q.get("limit") || 25);
      await route.fulfill({
        json: {
          officers: [],
          officerRows: [],
          items: items.slice(offset, offset + limit),
          total: items.length,
          offset,
          limit,
          connected: true,
          lastSync: Date.now(),
          syncError: "",
          facets: {
            campus: ["Bothell", "Seattle"],
            grad_year: ["2028", "2029"],
          },
        },
      });
    });
    await page.route("**/api/members/person-*", (route) =>
      route.fulfill({
        json: {
          member: members.find((m) =>
            route.request().url().endsWith(m.person_id),
          ),
        },
      }),
    );
    await page.goto("/members");
    await expect(
      page.getByRole("heading", { name: "1,001 members", exact: true }),
    ).toBeVisible();
    await expect(page.locator("tbody tr")).toHaveCount(25);
    const visibleName = await page
      .locator(".member-name")
      .first()
      .boundingBox();
    expect(visibleName).not.toBeNull();
    expect(visibleName!.x).toBeGreaterThanOrEqual(0);
    expect(visibleName!.x + visibleName!.width).toBeLessThanOrEqual(
      page.viewportSize()!.width,
    );
    await expect(page.locator(".members-table")).toHaveJSProperty(
      "scrollLeft",
      0,
    );
    await expect(page.getByRole("link", { name: "Export CSV" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Sync now" })).toHaveCount(0);
    const next = page.getByRole("button", { name: "Next", exact: true });
    await next.focus();
    await next.click();
    await expect(
      page.getByRole("status").filter({ hasText: "26–50" }),
    ).toBeVisible();
    await expect(next).toBeFocused();
    await page.getByRole("button", { name: "Previous", exact: true }).click();
    await page.getByLabel("Search members").fill("member1000@uw.edu");
    await expect(page.locator("tbody tr")).toHaveCount(1);
    await page.getByRole("button", { name: "Member 1000 Seed" }).click();
    const drawer = page.locator("#member-detail[open]");
    await expect(drawer).toContainText("2026-10-31 · Read-only");
    await expect(drawer).toContainText("Yes · Read-only");
    await expect(drawer.getByLabel("Audit note")).toHaveCount(0);
    await page.keyboard.press("Escape");
    await page.getByRole("button", { name: "Clear filters" }).click();
    await page
      .getByRole("combobox", { name: "Campus", exact: true })
      .selectOption("Seattle");
    await expect(
      page.getByRole("heading", { name: "500 members", exact: true }),
    ).toBeVisible();
    expect(await overflow(page)).toEqual([]);
  });
  test(`Director review keeps edits after D1 failure · ${theme}`, async ({
    page,
  }) => {
    await useTheme(page, theme);
    await devLogin(page, "director");
    const member = makeMember(1);
    let writes = 0;
    await page.route("**/api/members?*", (route) =>
      route.fulfill({
        json: {
          officers: [],
          officerRows: [],
          items: [member],
          total: 1,
          limit: 50,
          offset: 0,
          connected: true,
          lastSync: Date.now(),
          syncError: "",
          facets: { campus: ["Seattle"], grad_year: ["2028"] },
        },
      }),
    );
    await page.route("**/api/members/person-1", async (route) => {
      if (route.request().method() === "PATCH") {
        writes++;
        const body = route.request().postDataJSON();
        expect(body.auditNote).toBe("Correcting major");
        expect(body.major).toBe("Mathematics");
        expect(body.join_date).toBeUndefined();
        await route.fulfill({
          status: 502,
          json: {
            error: {
              code: "internal",
              message:
                "D1 did not confirm the update. Your edits are kept. Sync before retrying.",
            },
          },
        });
      } else await route.fulfill({ json: { member } });
    });
    await page.goto("/members");
    await page.getByRole("button", { name: "Member 0001 Seed" }).click();
    const drawer = page.locator("#member-detail[open]");
    await expect(drawer.getByLabel("Join date", { exact: true })).toHaveCount(
      0,
    );
    await drawer.getByLabel("Major", { exact: true }).fill("Mathematics");
    await drawer.getByLabel("Audit note").fill("Correcting major");
    await drawer.getByRole("button", { name: "Review changes" }).click();
    await drawer
      .getByRole("button", { name: "Confirm and write to D1" })
      .click();
    await expect(drawer.getByRole("alert")).toContainText(
      "Your edits are kept",
    );
    await drawer.getByRole("button", { name: "Back to edit" }).click();
    await expect(drawer.getByLabel("Major", { exact: true })).toHaveValue(
      "Mathematics",
    );
    await expect(drawer.getByLabel("Audit note")).toHaveValue(
      "Correcting major",
    );
    expect(writes).toBe(1);
    expect(await overflow(page)).toEqual([]);
  });
  test(`Officers are listed with members, deduped by email · ${theme}`, async ({ page }) => {
    await useTheme(page, theme);
    await devLogin(page, "officer");
    const matched = makeMember(1);
    const other = makeMember(2);
    await page.route("**/api/members?*", (route) => {
      const filtered = !!new URL(route.request().url()).searchParams.get("campus");
      return route.fulfill({
        json: {
          officers: [
            { id: "u-1", name: "Priya Shah", title: "Treasurer", subteam: "Finance", email: "priya@uw.edu", memberId: matched.person_id },
            { id: "u-2", name: "Sam Rivera", title: null, subteam: null, email: "sam@uw.edu", memberId: null },
          ],
          officerRows: filtered ? [] : [{ id: "u-2", name: "Sam Rivera", title: null, subteam: null, email: "sam@uw.edu", memberId: null }],
          items: [matched, other],
          total: filtered ? 2 : 3,
          limit: 25,
          offset: 0,
          connected: true,
          lastSync: Date.now(),
          syncError: "",
          facets: { campus: ["Seattle"], grad_year: ["2028"] },
        },
      });
    });
    await page.goto("/members");
    // 2 member rows + 1 officer without a member row; the matched officer is not repeated.
    await expect(page.locator("tbody tr")).toHaveCount(3);
    await expect(page.getByRole("heading", { name: "3 members", exact: true })).toBeVisible();
    await expect(page.locator("[data-officer-row]")).toHaveCount(1);
    await expect(page.locator("[data-officer-row]")).toContainText("Sam Rivera");
    await expect(page.locator("[data-officer-row] [data-officer-badge]")).toHaveText("Officer");
    // The member row that shares the officer's email carries the badge instead.
    await expect(page.locator("tbody tr", { hasText: "Member 0001" }).locator("[data-officer-badge]")).toHaveText("Officer");
    await expect(page.locator("tbody tr", { hasText: "Member 0002" }).locator("[data-officer-badge]")).toHaveCount(0);
    // Narrowing by a member-only field drops the officer-only line.
    await page.getByRole("combobox", { name: "Campus", exact: true }).selectOption("Seattle");
    await expect(page.locator("[data-officer-row]")).toHaveCount(0);
    expect(await overflow(page)).toEqual([]);
  });
  // Layout persistence moved to home-grid.spec.ts, which resets the layout before each test.
  test(`Disconnected Cloudflare widget · ${theme}`, async ({ page }) => {
    await useTheme(page, theme);
    await devLogin(page, "officer");
    const res = await page.request.put("/api/me/prefs", {
      data: { widgetLayout: [{ id: "web-analytics", size: "2x2" }] },
      timeout: 15_000,
    });
    expect(res.ok()).toBe(true);
    // Restore the shared officer's layout even when an assertion fails, so later specs
    // (purchase-deadline's Home check) never inherit a one-widget layout.
    try {
      await page.goto("/");
      const w = page.locator('[data-widget="web-analytics"]');
      // Disconnected: the empty state and the connection help, no sync footer.
      await expect(w).toContainText("Not connected yet");
      await expect(w.locator("summary")).toHaveText("Connection details");
      await expect(w).not.toContainText("Synced daily");
      expect(await overflow(page)).toEqual([]);
    } finally {
      await page.request.put("/api/me/prefs", { data: { widgetLayout: defaultWidgetLayout }, timeout: 15_000 });
    }
  });
}
