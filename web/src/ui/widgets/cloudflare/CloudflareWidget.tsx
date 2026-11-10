import type { ComponentChildren } from "preact";
import { fmtTime } from "../../../lib/format";
import { useApi } from "../../../lib/api/useApi";
import type { ResponseOf } from "../../../lib/api/client";
import type { CurrentUser } from "../../../lib/contracts";
import { widgetDef, type BlockSize } from "../registry";
import { CountUp } from "../../components/kit";
import WidgetCard, { WidgetEmpty, WidgetState } from "../shell/WidgetCard";

export const cloudflareIds = [
  "web-analytics",
  "traffic-security",
  "performance",
  "workers-d1",
  "member-signups",
];
type CloudflareState = ReturnType<typeof useApi<"cloudflareWidgets">>;
/** Detail level by footprint: 1×1 summary, 2×2 principal metrics, 4×2 adds breakdowns. */
export const detailLevel = (size: BlockSize): "S" | "M" | "L" =>
  size === "1x1" ? "S" : size.startsWith("4") ? "L" : "M";

const ASIDE: Record<string, string> = {
  "web-analytics": "Last 7 days",
  "traffic-security": "Last 24 hours",
  performance: "p75 · last 24 hours",
  "workers-d1": "Last 24 hours",
  "member-signups": "Directory",
};
/** Core Web Vitals "good" thresholds (ms, ms, unitless). */
const VITALS: Record<string, { name: string; good: number; poor: number; limit: string }> = {
  "LCP · p75": { name: "Largest paint (LCP)", good: 2_500_000, poor: 4_000_000, limit: "≤ 2.5 s" },
  "INP · p75": { name: "Interaction (INP)", good: 200_000, poor: 500_000, limit: "≤ 200 ms" },
  "CLS · p75": { name: "Layout shift (CLS)", good: 0.1, poor: 0.25, limit: "≤ 0.1" },
};
const vitalChip = (label: string, n: number) => {
  const v = VITALS[label];
  if (!v) return null;
  const [text, hue] = n <= v.good ? ["Good", "green"] : n <= v.poor ? ["Needs work", "amber"] : ["Poor", "coral"];
  return <span class="st-dot" data-hue={hue}>{text}</span>;
};

export default function CloudflareWidget({
  id,
  size: blockSize,
  state,
  me,
}: {
  id: string;
  size: BlockSize;
  state: CloudflareState;
  me: CurrentUser;
}) {
  const size = detailLevel(blockSize);
  const icon = widgetDef(id)?.icon ?? "activity";
  const widget =
    state.status === "ready"
      ? state.data.widgets.find((w) => w.id === id)
      : undefined;
  const metrics = widget ? extractMetrics(widget) : [];
  const stale = !!(
    widget?.lastSync &&
    (Date.now() - widget.lastSync > 36 * 60 * 60 * 1000 || widget.error)
  );
  const synced = widget?.lastSync ? fmtTime(new Date(widget.lastSync).toISOString()) : null;
  const ready = !!widget?.connected && !!widget.data && metrics.length > 0;
  const foot = ready && synced ? (size === "S" ? synced : `Synced daily, last ${synced}`) : null;
  const aside = stale ? <span class="cf-stale">Stale</span> : ready && size !== "S" ? ASIDE[id] : null;
  const empty = (line: ComponentChildren, detail?: ComponentChildren) => (
    <>
      <WidgetEmpty icon={icon} size={blockSize}>{line}</WidgetEmpty>
      {detail && size !== "S" && detail}
    </>
  );
  let body: ComponentChildren;
  if (state.status !== "ready") body = <WidgetState state={state} label="analytics" />;
  else if (!widget?.connected)
    body = empty(
      id === "member-signups" ? "No members yet" : "Not connected yet",
      <details class="cf-details">
        <summary>Connection details</summary>
        <p>
          A director configures Cloudflare on the server. Daily sync keeps this
          widget up to date; no credentials are stored in your browser.
        </p>
      </details>,
    );
  else if (!widget.data)
    body = empty(widget.error ? "Sync unavailable · check Cloudflare permissions" : "Waiting for first sync");
  else if (!metrics.length) body = empty("No data for this period");
  else {
    const [first] = metrics;
    const value = (v: string) => <CountUp value={v} />;
    if (size === "S") {
      const sub =
        id === "member-signups"
          ? `members · +${metrics[1]?.[1] ?? 0} in 7 days`
          : id === "traffic-security"
            ? `requests${metrics.find(([l]) => l === "Security events") ? ` · ${metrics.find(([l]) => l === "Security events")![1]} security events` : ""}`
            : id === "web-analytics"
              ? `${first![0].toLowerCase()} · last 7 days`
              : id === "workers-d1"
                ? "worker requests · 24 h"
                : <>LCP p75 · {vitalChip(first![0], first![2])}</>;
      body = (
        <div class="stat">
          <span class="kpi-value stat-value">{value(first![1])}</span>
          <span class="stat-sub">{sub}</span>
        </div>
      );
    } else if (id === "performance") {
      body = (
        <div class="vitals">
          {metrics.map(([label, text, n]) => (
            <div class="vital" key={label}>
              <b>{VITALS[label]?.name ?? label}</b>
              <span>{text}</span>
              <span class="vital-chip">{vitalChip(label, n)}</span>
              <small>{VITALS[label]?.limit ?? ""}</small>
            </div>
          ))}
        </div>
      );
    } else {
      const shown = metrics
        .filter(
          ([label]) =>
            id !== "workers-d1" || size === "L" ||
            ["Worker requests", "CPU time · p50", "D1 rows read", "D1 rows written"].includes(label),
        )
        .slice(0, size === "M" ? 4 : 8);
      body = (
        <>
          <div class={`metrics${size === "L" && shown.length > 2 ? " metrics--4" : ""}`}>
            {shown.map(([label, text]) => (
              <div class="metric" key={label}>
                <span class="metric-label">{label}</span>
                <span class="metric-value">{value(text)}</span>
              </div>
            ))}
          </div>
          {id === "web-analytics" && <Breakdowns data={widget.data} only={size === "M" ? 1 : 3} />}
          {widget.error && <p class="stat-sub" role="status">Latest sync failed · showing saved data</p>}
        </>
      );
    }
  }
  return (
    <WidgetCard id={id} me={me} aside={aside} foot={foot} class={`cf-widget widget--${size.toLowerCase()}`}>
      {body}
    </WidgetCard>
  );
}
type Widget = ResponseOf<"cloudflareWidgets">["widgets"][number];
// Only expected numeric aggregates are displayed. Raw response objects never enter markup.
function extractMetrics(w: Widget): [string, string, number][] {
  const data = w.data as Record<string, unknown> | null;
  if (!data) return [];
  if (w.id === "member-signups")
    return ([
      ["Total members", data.members],
      ["Joined in 7 days", data.last7Days],
      ["Joined today", data.today],
      ["Joined in 30 days", data.last30Days],
    ] as const).map(([label, v]): [string, string, number] => [label, Number(v ?? 0).toLocaleString("en-US"), Number(v ?? 0)]);
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
  return [...values].map(([label, n]): [string, string, number] => [label, formatMetric(label, n), n]);
}

const compact = new Intl.NumberFormat("en-US", { notation: "compact", maximumFractionDigits: 1 });
function formatMetric(label: string, n: number): string {
    if (label === "Bandwidth" || label === "Largest D1 database")
      return formatBytes(n);
    if (label === "LCP · p75" && n >= 1_000_000)
      return (n / 1_000_000).toLocaleString("en-US", { maximumFractionDigits: 1 }) + " s";
    if (["LCP · p75", "INP · p75", "CPU time · p50"].includes(label))
      return (n / 1000).toLocaleString("en-US", { maximumFractionDigits: 1 }) + " ms";
    if (label === "Cache hit")
      return n.toLocaleString("en-US", { maximumFractionDigits: 1 }) + "%";
    if (label === "CLS · p75") return n.toLocaleString("en-US", { maximumFractionDigits: 3 });
    return n >= 10_000 ? compact.format(n).toLowerCase() : n.toLocaleString("en-US", { maximumFractionDigits: 3 });
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
function Breakdowns({ data, only }: { data: unknown; only: number }) {
  const account = (
    data as { viewer?: { accounts?: Record<string, unknown>[] } }
  )?.viewer?.accounts?.[0];
  if (!account) return null;
  const sections = [
    ["topPages", "Top pages", "requestPath"],
    ["referrers", "Referrers", "refererHost"],
    ["countries", "Countries", "countryName"],
  ].slice(0, only);
  return (
    <div class={`breakdowns${only === 1 ? " breakdowns--1" : ""}`}>
      {sections.map(([key, label, dim]) => {
        const rows = account[key!] as
          | { count?: number; dimensions?: Record<string, string> }[]
          | undefined;
        return rows?.length ? (
          <section key={key}>
            {only > 1 && <h3>{label}</h3>}
            <ul class="kv" aria-label={label}>
              {rows.map((row, i) => (
                <li key={i}>
                  <span>
                    {row.dimensions?.[dim!] ||
                      (key === "referrers" ? "Direct" : "Unknown")}
                  </span>
                  <span>{row.count?.toLocaleString("en-US") ?? "—"}</span>
                </li>
              ))}
            </ul>
          </section>
        ) : null;
      })}
    </div>
  );
}
