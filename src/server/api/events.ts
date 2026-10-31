import { pacificDate } from '../../core/luma/time';
import { isRegistered } from '../../core/luma/status';
import { and, eq, sql } from 'drizzle-orm';
import * as s from '../db/schema';
import * as C from '../../core/contracts';
import {
  parseGuests,
  parseReflections,
  type GuestImport,
  type ReflectionImport,
} from '../../core/luma/csv';
import { eventMetrics, matchReflections } from '../../core/metrics';
import {
  type ApiContext,
  type BatchItem,
  found,
  fail,
  uuid,
  now,
  mutate,
  page,
} from './context';
import { catalog, metrics, eventItem, requestItem } from './read-model';
export async function events(
  c: ApiContext,
  name: string,
  id: string | undefined,
  body: unknown,
  query: unknown,
): Promise<unknown> {
  if (name === 'ingestLuma' || name === 'eventsImport')
    return importEvent(c, name, body);
  const cat = await catalog(c.db);
  if (name === 'eventsList') {
    const q = C.EventsListQuery.parse(query),
      today = now().toISOString();
    const rows = cat.events.filter(
      (e) =>
        (q.when === 'all' ||
          (q.when === 'past'
            ? Date.parse(e.startsAt) < Date.parse(today)
            : Date.parse(e.startsAt) >= Date.parse(today))) &&
        (!q.q || e.name.toLowerCase().includes(q.q.toLowerCase())) &&
        (!q.from || pacificDate(e.startsAt) >= q.from) &&
        (!q.to || pacificDate(e.startsAt) <= q.to),
    );
    return page(await Promise.all(rows.map((e) => eventItem(c.db, e, cat))), q);
  }
  if (name === 'eventCreate') {
    const b = C.EventUpsertRequest.parse(body),
      id = uuid();
    if (
      b.hostUserId &&
      !cat.users.some((u) => u.id === b.hostUserId && u.active)
    )
      fail(400, 'bad_request', 'Invalid host');
    if (b.endsAt && Date.parse(b.endsAt) < Date.parse(b.startsAt))
      fail(400, 'bad_request', 'Event ends before it starts');
    await mutate(
      c,
      sql`1`,
      [c.db.insert(s.events).values({ id, ...b })],
      'event.created',
      'event',
      id,
      null,
      b,
    );
    return {
      event: await eventItem(
        c.db,
        found(
          await c.db.select().from(s.events).where(eq(s.events.id, id)).get(),
        ),
      ),
    };
  }
  const e = found(cat.events.find((e) => e.id === id));
  if (name === 'eventUpdate') {
    const b = C.EventPatchRequest.parse(body);
    if (
      b.hostUserId &&
      !cat.users.some((u) => u.id === b.hostUserId && u.active)
    )
      fail(400, 'bad_request', 'Invalid host');
    const next = { ...e, ...b };
    if (next.endsAt && Date.parse(next.endsAt) < Date.parse(next.startsAt))
      fail(400, 'bad_request', 'Event ends before it starts');
    await mutate(
      c,
      sql`EXISTS (SELECT 1 FROM events WHERE id = ${e.id} AND updated_at = ${e.updatedAt.getTime()})`,
      [
        c.db
          .update(s.events)
          .set({ ...b, updatedAt: now() })
          .where(eq(s.events.id, e.id)),
      ],
      'event.updated',
      'event',
      e.id,
      e,
      b,
    );
    return {
      event: await eventItem(
        c.db,
        found(
          await c.db.select().from(s.events).where(eq(s.events.id, e.id)).get(),
        ),
      ),
    };
  }
  if (name === 'eventMetrics') {
    const [guests, reflections] = await Promise.all([
      c.db.select().from(s.eventGuests).where(eq(s.eventGuests.eventId, e.id)),
      c.db.select().from(s.reflections).where(eq(s.reflections.eventId, e.id)),
    ]);
    const registrations = new Map<string, number>(),
      responses = new Map<string, number>();
    for (const g of guests)
      if (
        g.registeredAt &&
        g.registeredAt.getTime() <= Date.parse(e.startsAt) &&
        isRegistered(g.approvalStatus, g.checkedInAt)
      ) {
        const day = pacificDate(g.registeredAt);
        registrations.set(day, (registrations.get(day) ?? 0) + 1);
      }
    for (const r of reflections) {
      const day = pacificDate(r.submittedAt);
      responses.set(day, (responses.get(day) ?? 0) + 1);
    }
    let cumulative = 0;
    return {
      eventId: e.id,
      totals: await metrics(c.db, e.id),
      registrationsByDay: [...registrations]
        .sort()
        .map(([date, count]) => ({ date, cumulative: (cumulative += count) })),
      reflectionsByDay: [...responses]
        .sort()
        .map(([date, count]) => ({ date, count })),
    };
  }
  const requests = (
    await c.db
      .select()
      .from(s.purchaseRequests)
      .where(eq(s.purchaseRequests.eventId, e.id))
  ).map((r) => requestItem(r, cat));
  return {
    event: {
      ...(await eventItem(c.db, e, cat)),
      endsAt: e.endsAt,
      url: e.url,
      updatedAt: e.updatedAt.toISOString(),
    },
    requests,
    spentCents: (
      await c.db
        .select()
        .from(s.purchaseRequests)
        .where(eq(s.purchaseRequests.eventId, e.id))
    ).reduce((sum, r) => sum + (r.actualTotalCents ?? 0), 0),
  };
}
async function importEvent(c: ApiContext, name: string, body: unknown) {
  let kind: 'luma_guests' | 'reflections' = 'luma_guests',
    dryRun = false,
    eventId: string | undefined;
  let eventValues: typeof s.events.$inferInsert | undefined;
  let guests: GuestImport[] = [],
    reflections: ReflectionImport[] = [],
    errors: { row: number; message: string }[] = [],
    total = 0;
  if (name === 'ingestLuma') {
    const b = C.LumaIngestRequest.parse(body);
    const existing = await c.db
      .select()
      .from(s.events)
      .where(eq(s.events.lumaId, b.event.lumaId))
      .get();
    eventId = existing?.id ?? uuid();
    eventValues = { ...b.event, id: eventId, updatedAt: now() };
    guests = b.guests.map((g) => ({
      clearFields: Object.keys(g).filter(
        (k) => g[k as keyof typeof g] === null,
      ),
      email: g.email.trim().toLowerCase(),
      name: g.name ?? null,
      lumaGuestId: g.lumaGuestId ?? null,
      approvalStatus: g.approvalStatus ?? null,
      registeredAt: g.registeredAt ?? null,
      checkedInAt: g.checkedInAt ?? null,
    }));
    total = guests.length;
  } else {
    const form = await c.request.formData(),
      f = C.CsvImportFields.parse(Object.fromEntries(form));
    kind = f.kind;
    dryRun = f.dryRun;
    const file = form.get('file');
    if (!(file instanceof File) || file.size > C.CSV_MAX_BYTES)
      fail(400, 'bad_request', 'CSV file required (maximum 5 MB)');
    let parsed:
      | ReturnType<typeof parseGuests>
      | ReturnType<typeof parseReflections>;
    try {
      parsed =
        kind === 'luma_guests'
          ? parseGuests(await file.text())
          : parseReflections(await file.text());
    } catch (e) {
      fail(
        422,
        'import_failed',
        e instanceof Error ? e.message : 'Invalid CSV',
      );
    }
    errors = parsed.errors;
    total = parsed.totalRows;
    if (kind === 'luma_guests') guests = parsed.rows as GuestImport[];
    else reflections = parsed.rows as ReflectionImport[];
    let existing = f.eventId
      ? await c.db
          .select()
          .from(s.events)
          .where(eq(s.events.id, f.eventId))
          .get()
      : f.lumaEventId
        ? await c.db
            .select()
            .from(s.events)
            .where(eq(s.events.lumaId, f.lumaEventId))
            .get()
        : undefined;
    if (f.eventId && !existing) fail(404, 'not_found', 'Event not found');
    if (!existing && (!f.lumaEventId || !f.eventName || !f.eventStartsAt))
      fail(
        400,
        'bad_request',
        'Supply eventId or lumaEventId, eventName and eventStartsAt',
      );
    eventId = existing?.id ?? uuid();
    if (!existing)
      eventValues = {
        id: eventId,
        lumaId: f.lumaEventId,
        name: f.eventName!,
        startsAt: f.eventStartsAt!,
      };
  }
  const eid = eventId!;
  const [oldGuests, oldReflections] = await Promise.all([
    c.db.select().from(s.eventGuests).where(eq(s.eventGuests.eventId, eid)),
    c.db.select().from(s.reflections).where(eq(s.reflections.eventId, eid)),
  ]);
  let created = 0,
    updated = 0;
  const writes: BatchItem[] = [];
  if (eventValues)
    writes.push(
      c.db
        .insert(s.events)
        .values(eventValues)
        .onConflictDoUpdate({ target: s.events.id, set: eventValues }),
    );
  const uniqueGuests = new Map(guests.map((g) => [g.email, g])),
    uniqueReflections = new Map(reflections.map((r) => [r.email, r]));
  const oldGuestMap = new Map(oldGuests.map((g) => [g.email, g]));
  const oldReflectionMap = new Map(oldReflections.map((r) => [r.email, r]));
  const guestValues = [...uniqueGuests.values()].map((g) => {
    const old = oldGuestMap.get(g.email);
    old ? updated++ : created++;
    return {
      ...g,
      id: old?.id ?? uuid(),
      registeredAt: g.registeredAt ? Date.parse(g.registeredAt) : null,
      checkedInAt: g.checkedInAt ? Date.parse(g.checkedInAt) : null,
    };
  });
  // json_each keeps large imports below D1's 100-bind-parameter statement limit.
  for (let i = 0; i < guestValues.length; i += 500)
    writes.push(
      c.db
        .insert(s.eventGuests)
        .select(
          sql`
   SELECT json_extract(value,'$.id'), ${eid}, json_extract(value,'$.email'), json_extract(value,'$.name'),
    json_extract(value,'$.lumaGuestId'),json_extract(value,'$.approvalStatus'),json_extract(value,'$.registeredAt'),json_extract(value,'$.checkedInAt'),${Date.now()}
   FROM json_each(${JSON.stringify(guestValues.slice(i, i + 500))}) WHERE true
  `,
        )
        .onConflictDoUpdate({
          target: [s.eventGuests.eventId, s.eventGuests.email],
          set: {
            name: sql`coalesce(excluded.name, ${s.eventGuests.name})`,
            lumaGuestId: sql`coalesce(excluded.luma_guest_id, ${s.eventGuests.lumaGuestId})`,
            approvalStatus: sql`coalesce(excluded.approval_status, ${s.eventGuests.approvalStatus})`,
            registeredAt: sql`coalesce(excluded.registered_at, ${s.eventGuests.registeredAt})`,
            checkedInAt: sql`coalesce(excluded.checked_in_at, ${s.eventGuests.checkedInAt})`,
          },
        }),
    );
  // Explicit JSON null clears a field; absent JSON/CSV fields preserve existing values.
  const cleared = Object.fromEntries(
    [...uniqueGuests.values()]
      .filter((g) => g.clearFields?.length)
      .map((g) => [
        g.email,
        Object.fromEntries(g.clearFields!.map((k) => [k, true])),
      ]),
  );
  for (const field of [
    'name',
    'lumaGuestId',
    'approvalStatus',
    'registeredAt',
    'checkedInAt',
  ] as const) {
    if (Object.values(cleared).some((flags) => flags[field]))
      writes.push(
        c.db
          .update(s.eventGuests)
          .set({ [field]: null })
          .where(
            and(
              eq(s.eventGuests.eventId, eid),
              sql`EXISTS (SELECT 1 FROM json_each(${JSON.stringify(cleared)}) flags WHERE flags.key = ${s.eventGuests.email} AND json_extract(flags.value, ${'$.' + field}) = 1)`,
            ),
          ),
      );
  }
  const reflectionValues = [...uniqueReflections.values()].map((r) => {
    const old = oldReflectionMap.get(r.email);
    old ? updated++ : created++;
    return {
      ...r,
      id: old?.id ?? uuid(),
      submittedAt: Date.parse(r.submittedAt),
    };
  });
  for (let i = 0; i < reflectionValues.length; i += 500)
    writes.push(
      c.db
        .insert(s.reflections)
        .select(
          sql`
   SELECT json_extract(value,'$.id'),${eid},json_extract(value,'$.email'),json_extract(value,'$.submittedAt'),NULL,${Date.now()}
   FROM json_each(${JSON.stringify(reflectionValues.slice(i, i + 500))}) WHERE true
  `,
        )
        .onConflictDoUpdate({
          target: [s.reflections.eventId, s.reflections.email],
          set: { submittedAt: sql`excluded.submitted_at` },
        }),
    );
  if (!dryRun) {
    if (c.tokenId)
      writes.push(
        c.db
          .update(s.serviceTokens)
          .set({ lastUsedAt: now() })
          .where(eq(s.serviceTokens.id, c.tokenId)),
      );
    await mutate(c, sql`1`, writes, 'event.imported', 'event', eid, null, {
      kind,
      created,
      updated,
      errors,
    });
  }
  const mergedGuests = new Map(
    oldGuests.map((g) => [
      g.email,
      { ...g, checkedInAt: g.checkedInAt?.toISOString() ?? null },
    ]),
  );
  for (const g of uniqueGuests.values()) {
    const old = mergedGuests.get(g.email);
    const merged = {
      ...old,
      ...Object.fromEntries(
        Object.entries(g).filter(
          ([k, v]) => v !== null || g.clearFields?.includes(k),
        ),
      ),
    };
    mergedGuests.set(g.email, merged as never);
  }
  const checkedInEmails = [...mergedGuests.values()]
    .filter((g) => g.checkedInAt)
    .map((g) => g.email);
  const reflectionEmails = new Set([
    ...oldReflections.map((r) => r.email),
    ...uniqueReflections.keys(),
  ]);
  const matched = matchReflections({ checkedInEmails, reflectionEmails });
  const m = dryRun
    ? eventMetrics({
        registered: [...mergedGuests.values()].filter((g) =>
          isRegistered(g.approvalStatus, g.checkedInAt),
        ).length,
        checkedIn: checkedInEmails.length,
        reflections: reflectionEmails.size,
        matchedReflections: matched.matched,
      })
    : await metrics(c.db, eid);
  const result = {
    eventId: eid,
    rows: total,
    created,
    updated,
    skipped: total - created - updated,
    errors,
    metrics: m,
  };
  return name === 'ingestLuma' ? result : { ...result, kind, dryRun };
}
