// npm run test:scripts (node --test "scripts/*.test.mjs")
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { test } from "node:test";
import { accessHeaders, postWeeklyCheckIn } from "./buildspace-discord-stub.mjs";

const checkIn = { messageId: "m1", discordId: "123", body: "shipped", date: "2026-11-16" };

/** A fake dashboard; `answer` decides each response from the request it saw. */
async function server(answer) {
  const seen = [];
  const s = createServer((req, res) => {
    let body = "";
    req.on("data", (c) => (body += c));
    req.on("end", () => {
      seen.push({ method: req.method, url: req.url, headers: req.headers, body });
      answer(req, res);
    });
  });
  await new Promise((r) => s.listen(0, "127.0.0.1", r));
  return { origin: `http://127.0.0.1:${s.address().port}`, seen, close: () => s.close() };
}

const ok = (_req, res) => {
  res.writeHead(200, { "content-type": "application/json" });
  res.end('{"ok":true,"week":{"start":"2026-11-16","due":"2026-11-20"}}');
};

test("without Access credentials no Access headers are sent", async () => {
  const s = await server(ok);
  try {
    const out = await postWeeklyCheckIn(checkIn, { origin: s.origin, token: "tok", accessClientId: "", accessClientSecret: "" });
    assert.equal(out.week.start, "2026-11-16");
    const [req] = s.seen;
    assert.equal(req.url, "/api/ingest/buildspace-launchpad");
    assert.equal(req.headers.authorization, "Bearer tok");
    assert.equal(req.headers["cf-access-client-id"], undefined);
    assert.equal(req.headers["cf-access-client-secret"], undefined);
    assert.equal(JSON.parse(req.body).externalId, "m1");
  } finally {
    s.close();
  }
});

test("with CF_ACCESS_CLIENT_ID and CF_ACCESS_CLIENT_SECRET both headers are sent", async () => {
  const s = await server(ok);
  const saved = { id: process.env.CF_ACCESS_CLIENT_ID, secret: process.env.CF_ACCESS_CLIENT_SECRET };
  process.env.CF_ACCESS_CLIENT_ID = "abc.access";
  process.env.CF_ACCESS_CLIENT_SECRET = "s3cret";
  try {
    await postWeeklyCheckIn(checkIn, { origin: s.origin, token: "tok" });
    const [req] = s.seen;
    assert.equal(req.headers["cf-access-client-id"], "abc.access");
    assert.equal(req.headers["cf-access-client-secret"], "s3cret");
    assert.equal(req.headers.authorization, "Bearer tok", "the dashboard token is still sent");
  } finally {
    for (const [k, v] of [["CF_ACCESS_CLIENT_ID", saved.id], ["CF_ACCESS_CLIENT_SECRET", saved.secret]])
      if (v === undefined) delete process.env[k];
      else process.env[k] = v;
    s.close();
  }
});

test("an Access sign-in redirect is reported, not followed", async () => {
  const s = await server((_req, res) => {
    res.writeHead(302, { location: "https://cseed.cloudflareaccess.com/cdn-cgi/access/login/officers.example" });
    res.end();
  });
  try {
    await assert.rejects(
      postWeeklyCheckIn(checkIn, { origin: s.origin, token: "tok", accessClientId: "", accessClientSecret: "" }),
      /stopped at Cloudflare Access: set CF_ACCESS_CLIENT_ID/,
    );
    assert.equal(s.seen.length, 1);
  } finally {
    s.close();
  }
});

test("half a service token is refused before any request", async () => {
  assert.deepEqual(accessHeaders("", ""), {});
  assert.throws(() => accessHeaders("abc.access", ""), /both CF_ACCESS_CLIENT_ID and CF_ACCESS_CLIENT_SECRET/);
  assert.throws(() => accessHeaders(undefined, "s3cret"), /both/);
  assert.deepEqual(accessHeaders(" abc.access ", "s3cret\n"), { "CF-Access-Client-Id": "abc.access", "CF-Access-Client-Secret": "s3cret" });
});
