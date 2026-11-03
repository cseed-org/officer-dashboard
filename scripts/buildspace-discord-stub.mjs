/**
 * Adapter stub for the existing Discord bot. This module opens no Discord connection.
 * Call from a weekly-check-in slash command or a dedicated-channel message handler.
 * Store DASHBOARD_URL and BUILDSPACE_INGEST_TOKEN in the bot's server environment.
 * `date` is the check-in date in the club time zone. Resolves to `{ok, week: {start, due}}`,
 * the check-in week the dashboard assigned under its configurable due day.
 */
export async function postWeeklyCheckIn(
  { messageId, discordId, body, date, needsHelp = false },
  {
    origin = process.env.DASHBOARD_URL,
    token = process.env.BUILDSPACE_INGEST_TOKEN,
  } = {},
) {
  if (!origin || !token)
    throw new Error("Dashboard URL and Buildspace ingest token are required");
  const res = await fetch(new URL("/api/ingest/buildspace", origin), {
    method: "POST",
    headers: {
      "content-type": "application/json",
      authorization: `Bearer ${token}`,
    },
    body: JSON.stringify({
      externalId: messageId,
      discordId,
      kind: "weekly",
      date,
      body,
      status: needsHelp ? "needs-help" : "on-track",
    }),
  });
  if (!res.ok) throw new Error(`Check-in rejected (${res.status})`);
  return res.json();
}
