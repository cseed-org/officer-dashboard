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
    .mockResolvedValueOnce(Response.json({ files: [] }))
    .mockResolvedValueOnce(Response.json({ id: 'fy' }))
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
