import { expect, it, vi } from 'vitest';
import {
  GoogleDriveFileStore,
  signServiceAccountJwt,
  validateUpload,
} from '../../src/server/files/drive';
it('signs a verifiable RS256 service JWT and uploads through shared-drive folders', async () => {
  const keys = await crypto.subtle.generateKey(
    {
      name: 'RSASSA-PKCS1-v1_5',
      modulusLength: 2048,
      publicExponent: new Uint8Array([1, 0, 1]),
      hash: 'SHA-256',
    },
    true,
    ['sign', 'verify'],
  );
  const exported = await crypto.subtle.exportKey('pkcs8', keys.privateKey);
  const private_key = `-----BEGIN PRIVATE KEY-----\n${btoa(String.fromCharCode(...new Uint8Array(exported)))}\n-----END PRIVATE KEY-----`;
  const account = {
    client_email: 'test@example.iam.gserviceaccount.com',
    private_key,
  };
  const jwt = await signServiceAccountJwt(account, 1000);
  const [header, payload, signature] = jwt.split('.');
  const decode = (s: string) =>
    Uint8Array.from(atob(s.replaceAll('-', '+').replaceAll('_', '/')), (c) =>
      c.charCodeAt(0),
    );
  expect(JSON.parse(new TextDecoder().decode(decode(payload!)))).toMatchObject({
    iss: account.client_email,
    iat: 1000,
    exp: 4600,
    aud: 'https://oauth2.googleapis.com/token',
  });
  expect(
    await crypto.subtle.verify(
      'RSASSA-PKCS1-v1_5',
      keys.publicKey,
      decode(signature!),
      new TextEncoder().encode(`${header}.${payload}`),
    ),
  ).toBe(true);
  const fetcher = vi
    .fn<typeof fetch>()
    .mockResolvedValueOnce(
      Response.json({ access_token: 'access', expires_in: 3600 }),
    )
    .mockResolvedValueOnce(Response.json({ id: 'root', driveId: 'drive' }))
    .mockResolvedValueOnce(Response.json({ files: [] }))
    .mockResolvedValueOnce(Response.json({ id: 'fy' }))
    .mockResolvedValueOnce(Response.json({ files: [{ id: 'fy' }] }))
    .mockResolvedValueOnce(Response.json({ files: [{ id: 'request' }] }))
    .mockResolvedValueOnce(
      Response.json({
        id: 'file',
        webViewLink: 'https://drive.google.com/file/d/file/view',
      }),
    );
  const store = new GoogleDriveFileStore(
    JSON.stringify(account),
    'root',
    fetcher,
  );
  const result = await store.put({
    folder: ['FY2027', 'req'],
    filename: 'receipt.pdf',
    mimeType: 'application/pdf',
    body: new TextEncoder().encode('PDF').buffer,
  });
  expect(result).toMatchObject({ key: 'file', storage: 'drive', sizeBytes: 3 });
  for (const [url] of fetcher.mock.calls.slice(1))
    expect(String(url)).toContain('supportsAllDrives=true');
  const last = fetcher.mock.calls.at(-1)!;
  expect(last[1]?.headers).toBeDefined();
  expect(await (last[1]!.body as Blob).text()).toContain('receipt.pdf');
  expect(() => validateUpload('text/html', 10)).toThrow(/MIME/);
  expect(() => validateUpload('image/png', 10 * 1024 * 1024 + 1)).toThrow(
    /10 MB/,
  );
});

/** A routing fake for the Drive REST API: one shared drive, folders created by "someone else" can be injected. */
function fakeDrive(opts: { driveId?: string; raceWith?: string } = {}) {
  const folders: { id: string; name: string; parent: string; created: number }[] = [];
  let clock = 0;
  let nextId = 0;
  const calls: { method: string; url: URL; body?: unknown }[] = [];
  const fetcher = vi.fn<typeof fetch>(async (input, init) => {
    const url = new URL(String(input));
    const method = init?.method ?? 'GET';
    const body = typeof init?.body === 'string' ? JSON.parse(init.body) : undefined;
    calls.push({ method, url, body });
    if (url.host === 'oauth2.googleapis.com')
      return Response.json({ access_token: 'access', expires_in: 3600 });
    if (method === 'GET' && url.pathname === '/drive/v3/files/root')
      return Response.json(opts.driveId ? { id: 'root', driveId: opts.driveId } : { id: 'root' });
    if (method === 'GET' && url.pathname === '/drive/v3/files') {
      const q = url.searchParams.get('q')!;
      const [, parent, name] = /^'(.+?)' in parents and name = '(.+?)'/.exec(q)!;
      return Response.json({
        files: folders
          .filter((f) => f.parent === parent && f.name === name)
          .sort((a, b) => a.created - b.created)
          .map(({ id }) => ({ id })),
      });
    }
    if (method === 'POST' && url.pathname === '/drive/v3/files') {
      const b = body as { name: string; parents: string[] };
      // A concurrent first upload created the same folder just before ours.
      if (opts.raceWith && b.name === opts.raceWith && !folders.some((f) => f.name === b.name))
        folders.push({ id: `theirs-${b.name}`, name: b.name, parent: b.parents[0]!, created: clock++ });
      const id = `mine-${nextId++}`;
      folders.push({ id, name: b.name, parent: b.parents[0]!, created: clock++ });
      return Response.json({ id });
    }
    if (method === 'PATCH') return Response.json({ id: url.pathname.split('/').at(-1) });
    if (url.pathname.startsWith('/upload/'))
      return Response.json({ id: 'file', webViewLink: 'https://drive.google.com/file/d/file/view' });
    return new Response('unexpected', { status: 500 });
  });
  return { fetcher, calls, folders };
}
async function serviceAccount() {
  const keys = await crypto.subtle.generateKey(
    { name: 'RSASSA-PKCS1-v1_5', modulusLength: 2048, publicExponent: new Uint8Array([1, 0, 1]), hash: 'SHA-256' },
    true,
    ['sign', 'verify'],
  );
  const exported = await crypto.subtle.exportKey('pkcs8', keys.privateKey);
  return JSON.stringify({
    client_email: 'test@example.iam.gserviceaccount.com',
    private_key: `-----BEGIN PRIVATE KEY-----\n${btoa(String.fromCharCode(...new Uint8Array(exported)))}\n-----END PRIVATE KEY-----`,
  });
}
const upload = (store: GoogleDriveFileStore, req = 'req') =>
  store.put({ folder: ['FY2027', req], filename: 'r.pdf', mimeType: 'application/pdf', body: new TextEncoder().encode('PDF').buffer });

it('lists folders in the shared drive corpus that holds the root folder, and reuses existing folders', async () => {
  const drive = fakeDrive({ driveId: 'shared-1' });
  const store = new GoogleDriveFileStore(await serviceAccount(), 'root', drive.fetcher);
  await upload(store, 'req-a');
  await upload(store, 'req-b');
  const lists = drive.calls.filter((c) => c.method === 'GET' && c.url.pathname === '/drive/v3/files');
  expect(lists.length).toBeGreaterThan(0);
  for (const l of lists) {
    expect(l.url.searchParams.get('corpora')).toBe('drive');
    expect(l.url.searchParams.get('driveId')).toBe('shared-1');
    expect(l.url.searchParams.get('includeItemsFromAllDrives')).toBe('true');
  }
  // The FY folder is created once and reused by the second upload.
  expect(drive.folders.filter((f) => f.name === 'FY2027')).toHaveLength(1);
  // The root's drive id is looked up once per store.
  expect(drive.calls.filter((c) => c.url.pathname === '/drive/v3/files/root')).toHaveLength(1);
});

it('falls back to corpora=allDrives when the root folder is not in a shared drive', async () => {
  const drive = fakeDrive();
  await upload(new GoogleDriveFileStore(await serviceAccount(), 'root', drive.fetcher));
  const lists = drive.calls.filter((c) => c.method === 'GET' && c.url.pathname === '/drive/v3/files');
  for (const l of lists) {
    expect(l.url.searchParams.get('corpora')).toBe('allDrives');
    expect(l.url.searchParams.has('driveId')).toBe(false);
  }
});

it('converges on the oldest folder when a concurrent first upload created the same folder', async () => {
  const drive = fakeDrive({ driveId: 'shared-1', raceWith: 'FY2027' });
  await upload(new GoogleDriveFileStore(await serviceAccount(), 'root', drive.fetcher));
  // The request folder is created under the other uploader's (older) FY folder.
  const req = drive.folders.find((f) => f.name === 'req')!;
  expect(req.parent).toBe('theirs-FY2027');
  // Our duplicate is trashed, not left behind.
  const trashed = drive.calls.filter((c) => c.method === 'PATCH');
  expect(trashed).toHaveLength(1);
  expect(trashed[0]!.url.pathname).toMatch(/mine-0$/);
  expect(trashed[0]!.body).toEqual({ trashed: true });
  // The list after create asks for oldest first.
  const relist = drive.calls.filter((c) => c.method === 'GET' && c.url.pathname === '/drive/v3/files')[1]!;
  expect(relist.url.searchParams.get('orderBy')).toBe('createdTime');
});
