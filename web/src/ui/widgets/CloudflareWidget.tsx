import { useApi } from "../../lib/api/useApi";
import type { ResponseOf } from "../../lib/api/client";
import { widgetRegistry } from "./registry";
import { Icon } from "../components/Icon";
import "../../styles/cloudflare-widgets.css";
export const cloudflareIds = [
  "web-analytics",
  "traffic-security",
  "performance",
  "workers-d1",
  "member-signups",
];
type CloudflareState = ReturnType<typeof useApi<"cloudflareWidgets">>;
export default function CloudflareWidget({
  id,
  size,
  state,
}: {
  id: string;
  size: "S" | "M" | "L";
  state: CloudflareState;
}) {
  const title = widgetRegistry.find((w) => w.id === id)?.title ?? id;
  const widget =
    state.status === "ready"
      ? state.data.widgets.find((w) => w.id === id)
      : undefined;
  const metrics = widget ? extractMetrics(widget) : [];
  const stale =
    widget?.lastSync &&
    (Date.now() - widget.lastSync > 36 * 60 * 60 * 1000 || widget.error);
  return (
    <section
      class={`cf-widget widget widget--${size.toLowerCase()}`}
      aria-label={title}
    >
      <header class="widget-head">
        <span class="widget-icon">
          <Icon name={id === "member-signups" ? "user" : "activity"} />
        </span>
        <h2 class="widget-title">{title}</h2>
        {stale && <span class="widget-stale">Stale</span>}
        {id === "member-signups" && (
          <a class="widget-link" href="/members">
            Members →
          </a>
        )}
      </header>
      <div class="widget-body">
        {state.status === "loading" ? (
          <p role="status">Loading analytics…</p>
        ) : state.status === "error" ? (
          <div class="widget-state">
            <h3 class="widget-state-title">Could not load analytics</h3>
            <button class="btn" onClick={state.reload}>
              Retry
            </button>
          </div>
        ) : !widget?.connected ? (
          <div class="widget-state">
            <h3 class="widget-state-title">Not connected</h3>
            <p class="widget-state-body">
              Cloudflare credentials haven’t been added. Ask a director to
              connect this source.
            </p>
            {size !== "S" && (
              <details>
                <summary>Connection details</summary>
                <p class="widget-state-body">
                  A director configures Cloudflare on the server. Daily sync
                  keeps this widget up to date; no credentials are stored in
                  your browser.
                </p>
              </details>
            )}
            {size === "L" && (
              <ul class="widget-ghost" aria-label="Available after connection">
                {(id === "web-analytics"
                  ? [
                      "Visits and page views",
                      "Top pages",
                      "Referrers and countries",
                    ]
                  : ["Daily metrics", "Source breakdown"]
                ).map((label) => (
                  <li key={label}>
                    {label}
                    <span>—</span>
                  </li>
                ))}
              </ul>
            )}
          </div>
        ) : !widget.data ? (
          <div class="widget-state">
            <h3 class="widget-state-title">
              {widget.error ? "Sync unavailable" : "Waiting for first sync"}
            </h3>
            <p class="widget-state-body">
              {widget.error
                ? "Check Cloudflare permissions, then sync again."
                : "Your data will appear after the daily sync."}
            </p>
          </div>
        ) : metrics.length === 0 ? (
          <div class="widget-state">
            <h3 class="widget-state-title">No data for this period</h3>
            <p class="widget-state-body">
              No measurements were returned for this period.
            </p>
          </div>
        ) : (
          <>
            <p class="widget-sub">
              {id === "member-signups"
                ? "Membership directory"
                : id === "workers-d1"
                  ? "Workers: last 24 hours · D1: previous UTC day"
                  : id === "web-analytics"
                    ? "Last 7 days · account analytics"
                    : id === "traffic-security"
                      ? "Last 24 hours · configured zone"
                      : "Last 24 hours · account analytics"}
            </p>
            {size === "S" ? (
              <>
                <p class="widget-figure">{metrics[0]?.[1]}</p>
                <p class="widget-sub">{metrics[0]?.[0]}</p>
                {id === "member-signups" && (
                  <p class="widget-sub">+{metrics[1]?.[1]} joined in 7 days</p>
                )}
              </>
            ) : (
              <ul class="widget-list">
                {metrics
                  .filter(
                    ([label]) =>
                      size !== "M" ||
                      id !== "workers-d1" ||
                      [
                        "Worker requests",
                        "CPU time · p50",
                        "D1 rows read",
                        "D1 rows written",
                      ].includes(label),
                  )
                  .slice(
                    0,
                    size === "M"
                      ? id === "traffic-security" || id === "workers-d1"
                        ? 4
                        : 3
                      : metrics.length,
                  )
                  .map(([label, value]) => (
                    <li key={label}>
                      <span>{label}</span>
                      <strong>{value}</strong>
                    </li>
                  ))}
              </ul>
            )}
            {size === "L" && <Breakdowns data={widget.data} />}
            {widget.error && (
              <p class="widget-sub" role="status">
                Latest sync failed · showing saved data
              </p>
            )}
          </>
        )}
      </div>
      <footer class="widget-foot">
        Synced daily, last{" "}
        {widget?.lastSync
          ? new Date(widget.lastSync).toLocaleString()
          : "never"}
      </footer>
    </section>
  );
}
type Widget = ResponseOf<"cloudflareWidgets">["widgets"][number];
// Only expected numeric aggregates are displayed. Raw response objects never enter markup.
function extractMetrics(w: Widget): [string, string][] {
  const data = w.data as Record<string, unknown> | null;
  if (!data) return [];
  if (w.id === "member-signups")
    return [
      ["Total members", Number(data.members ?? 0).toLocaleString()],
      ["Joined in 7 days", Number(data.last7Days ?? 0).toLocaleString()],
      ["Joined today", Number(data.today ?? 0).toLocaleString()],
      ["Joined in 30 days", Number(data.last30Days ?? 0).toLocaleString()],
    ];
  const wanted: Record<string, string> =
    w.id === "web-analytics"
      ? { count: "Page views", visits: "Visits" }
      : w.id === "traffic-security"
        ? { count: "Requests", edgeResponseBytes: "Bandwidth" }
        : w.id === "performance"
          ? {
              largestContentfulPaintP75: "LCP · p75",
              interactionToNextPaintP75: "INP · p75",
              cumulativeLayoutShiftP75: "CLS · p75",
            }
          : {
              requests: "Worker requests",
              errors: "Worker errors",
              cpuTimeP50: "CPU time · p50",
              readQueries: "D1 read queries",
              writeQueries: "D1 write queries",
              rowsRead: "D1 rows read",
              rowsWritten: "D1 rows written",
              databaseSizeBytes: "Largest D1 database",
            };
  const values = new Map<string, number>();
  function walk(v: unknown, path = "") {
    if (!v || typeof v !== "object") return;
    for (const [key, n] of Object.entries(v)) {
      if (
        typeof n === "number" &&
        Number.isFinite(n) &&
        n >= 0 &&
        wanted[key]
      ) {
        const label =
          w.id === "traffic-security" &&
          key === "count" &&
          path.includes("firewall")
            ? "Security events"
            : wanted[key]!;
        values.set(
          label,
          label === "Security events" ? (values.get(label) ?? 0) + n : n,
        );
      } else walk(n, path + "." + key);
    }
  }
  const root = data as {
    viewer?: {
      accounts?: Record<string, unknown>[];
      zones?: Record<string, unknown>[];
    };
  };
  const scoped = root.viewer?.accounts?.[0] ?? root.viewer?.zones?.[0] ?? {};
  walk(
    Object.fromEntries(
      Object.entries(scoped).filter(
        ([key]) =>
          !["topPages", "referrers", "countries", "cached"].includes(key),
      ),
    ),
  );
  const requests = values.get("Requests");
  const cached = (scoped.cached as { count?: number }[] | undefined)?.[0]
    ?.count;
  if (requests && typeof cached === "number")
    values.set("Cache hit", (cached / requests) * 100);
  return [...values].map(([label, n]) => {
    if (label === "Bandwidth" || label === "Largest D1 database")
      return [label, formatBytes(n)];
    if (["LCP · p75", "INP · p75", "CPU time · p50"].includes(label))
      return [
        label,
        (n / 1000).toLocaleString(undefined, { maximumFractionDigits: 1 }) +
          " ms",
      ];
    if (label === "Cache hit")
      return [
        label,
        n.toLocaleString(undefined, { maximumFractionDigits: 1 }) + "%",
      ];
    return [label, n.toLocaleString(undefined, { maximumFractionDigits: 3 })];
  });
}

function formatBytes(n: number) {
  for (const unit of ["B", "KB", "MB", "GB", "TB"]) {
    if (n < 1000 || unit === "TB")
      return (
        n.toLocaleString(undefined, { maximumFractionDigits: 1 }) + " " + unit
      );
    n /= 1000;
  }
  return "";
}
function Breakdowns({ data }: { data: unknown }) {
  const account = (
    data as { viewer?: { accounts?: Record<string, unknown>[] } }
  )?.viewer?.accounts?.[0];
  if (!account) return null;
  return (
    <div class="home-details">
      {[
        ["topPages", "Top pages", "requestPath"],
        ["referrers", "Referrers", "refererHost"],
        ["countries", "Countries", "countryName"],
      ].map(([key, label, dim]) => {
        const rows = account[key!] as
          { count?: number; dimensions?: Record<string, string> }[] | undefined;
        return rows?.length ? (
          <section key={key}>
            <h3 class="widget-sub">{label}</h3>
            <ul class="widget-list">
              {rows.map((row, i) => (
                <li key={i}>
                  <span>
                    {row.dimensions?.[dim!] ||
                      (key === "referrers" ? "Direct" : "Unknown")}
                  </span>
                  <strong>{row.count?.toLocaleString() ?? "—"}</strong>
                </li>
              ))}
            </ul>
          </section>
        ) : null;
      })}
    </div>
  );
}
