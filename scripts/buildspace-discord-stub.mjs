/**
 * Adapter stub for the existing Discord bot. This module opens no Discord connection.
 * Call from a weekly-check-in slash command or a dedicated-channel message handler.
 * Store DASHBOARD_URL and BUILDSPACE_INGEST_TOKEN in the bot's server environment.
 * Behind Cloudflare Access (docs/deployment.md, step 5), also set CF_ACCESS_CLIENT_ID and
 * CF_ACCESS_CLIENT_SECRET (the Access service token): each request then carries them as
 * CF-Access-Client-Id / CF-Access-Client-Secret. Unset, no Access headers are sent.
 * `date` is the check-in date in the club time zone. Resolves to `{ok, week: {start, due}}`,
 * the check-in week the dashboard assigned under its configurable due day.
 */
export async function postWeeklyCheckIn(
  { messageId, discordId, body, date, needsHelp = false },
  {
    origin = process.env.DASHBOARD_URL,
    token = process.env.BUILDSPACE_INGEST_TOKEN,
    accessClientId = process.env.CF_ACCESS_CLIENT_ID,
    accessClientSecret = process.env.CF_ACCESS_CLIENT_SECRET,
  } = {},
) {
  if (!origin || !token)
    throw new Error("Dashboard URL and Buildspace + Launchpad ingest token are required");
  const res = await fetch(new URL("/api/ingest/buildspace-launchpad", origin), {
    method: "POST",
    headers: {
      "content-type": "application/json",
      authorization: `Bearer ${token}`,
      ...accessHeaders(accessClientId, accessClientSecret),
    },
    // Access answers a missing or wrong service token with a redirect to its sign-in page;
    // following it would turn the refusal into a confusing HTML 200 or a second POST.
    redirect: "manual",
    body: JSON.stringify({
      externalId: messageId,
      discordId,
      kind: "weekly",
      date,
      body,
      status: needsHelp ? "needs-help" : "on-track",
    }),
  });
  if (res.type === "opaqueredirect" || (res.status >= 300 && res.status < 400))
    throw new Error(
      "Check-in stopped at Cloudflare Access: set CF_ACCESS_CLIENT_ID and CF_ACCESS_CLIENT_SECRET to the dashboard's Access service token",
    );
  if (!res.ok) throw new Error(`Check-in rejected (${res.status})`);
  return res.json();
}

/**
 * The Cloudflare Access service-token headers, or none when neither half is set.
 * Exported for other callers of the dashboard's service-token APIs.
 */
export function accessHeaders(
  id = process.env.CF_ACCESS_CLIENT_ID,
  secret = process.env.CF_ACCESS_CLIENT_SECRET,
) {
  id = id?.trim();
  secret = secret?.trim();
  if (!id && !secret) return {};
  if (!id || !secret)
    throw new Error("Set both CF_ACCESS_CLIENT_ID and CF_ACCESS_CLIENT_SECRET, or neither");
  return { "CF-Access-Client-Id": id, "CF-Access-Client-Secret": secret };
}
