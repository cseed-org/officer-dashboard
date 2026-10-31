import type { FileStore, StoredFile } from '../../core/ports';
import {
  ATTACHMENT_MAX_BYTES,
  ATTACHMENT_MIME_TYPES,
} from '../../core/contracts';
interface ServiceAccount {
  client_email: string;
  private_key: string;
  token_uri?: string;
}
const base64url = (value: Uint8Array) =>
  btoa(Array.from(value, (b) => String.fromCharCode(b)).join(''))
    .replaceAll('+', '-')
    .replaceAll('/', '_')
    .replace(/=+$/, '');
const encode = (value: unknown) =>
  base64url(new TextEncoder().encode(JSON.stringify(value)));
export function validateUpload(mimeType: string, size: number) {
  if (size > ATTACHMENT_MAX_BYTES || size === 0)
    throw new Error('File must be 1 byte to 10 MB');
  if (!(ATTACHMENT_MIME_TYPES as readonly string[]).includes(mimeType))
    throw new Error('Unsupported receipt MIME type');
}
export async function signServiceAccountJwt(
  account: ServiceAccount,
  now = Math.floor(Date.now() / 1000),
) {
  const encoded = account.private_key
    .replace(/-----[^-]+-----/g, '')
    .replace(/\s/g, '');
  const key = await crypto.subtle.importKey(
    'pkcs8',
    Uint8Array.from(atob(encoded), (c) => c.charCodeAt(0)),
    { name: 'RSASSA-PKCS1-v1_5', hash: 'SHA-256' },
    false,
    ['sign'],
  );
  const content = `${encode({ alg: 'RS256', typ: 'JWT' })}.${encode({ iss: account.client_email, scope: 'https://www.googleapis.com/auth/drive', aud: 'https://oauth2.googleapis.com/token', iat: now, exp: now + 3600 })}`;
  const signature = await crypto.subtle.sign(
    'RSASSA-PKCS1-v1_5',
    key,
    new TextEncoder().encode(content),
  );
  return `${content}.${base64url(new Uint8Array(signature))}`;
}
export class GoogleDriveFileStore implements FileStore {
  private token?: { value: string; expires: number };
  private account: ServiceAccount;
  constructor(
    json: string,
    private rootId: string,
    private fetcher: typeof fetch = fetch,
  ) {
    this.account = JSON.parse(json);
  }
  private async accessToken() {
    if (this.token && this.token.expires > Date.now() + 60000)
      return this.token.value;
    const response = await this.fetcher('https://oauth2.googleapis.com/token', {
      method: 'POST',
      body: new URLSearchParams({
        grant_type: 'urn:ietf:params:oauth:grant-type:jwt-bearer',
        assertion: await signServiceAccountJwt(this.account),
      }),
    });
    if (!response.ok)
      throw new Error(`Drive token failed (${response.status})`);
    const data = (await response.json()) as {
      access_token: string;
      expires_in: number;
    };
    this.token = {
      value: data.access_token,
      expires: Date.now() + data.expires_in * 1000,
    };
    return data.access_token;
  }
  private async call(path: string, init: RequestInit = {}) {
    const headers = new Headers(init.headers);
    headers.set('Authorization', `Bearer ${await this.accessToken()}`);
    const response = await this.fetcher(`https://www.googleapis.com${path}`, {
      ...init,
      headers,
    });
    if (!response.ok)
      throw new Error(`Drive operation failed (${response.status})`);
    return response;
  }
  private corpus?: Promise<Record<string, string>>;
  /**
   * List queries must search the shared drive that holds the root folder: the
   * default `user` corpus can miss shared-drive children (N4). Looked up once.
   */
  private listCorpus() {
    return (this.corpus ??= this.call(
      `/drive/v3/files/${encodeURIComponent(this.rootId)}?fields=id,driveId&supportsAllDrives=true`,
    )
      .then((r) => r.json() as Promise<{ driveId?: string }>)
      .then(
        (f): Record<string, string> =>
          f.driveId
            ? { corpora: 'drive', driveId: f.driveId }
            : { corpora: 'allDrives' },
      )
      .catch((e) => {
        this.corpus = undefined;
        throw e;
      }));
  }
  private async findFolders(name: string, parent: string) {
    const escape = (s: string) =>
      s.replaceAll('\\', '\\\\').replaceAll("'", "\\'");
    const query = new URLSearchParams({
      q: `'${escape(parent)}' in parents and name = '${escape(name)}' and mimeType = 'application/vnd.google-apps.folder' and trashed = false`,
      fields: 'files(id)',
      orderBy: 'createdTime',
      supportsAllDrives: 'true',
      includeItemsFromAllDrives: 'true',
      ...(await this.listCorpus()),
    });
    const data = (await (
      await this.call(`/drive/v3/files?${query}`)
    ).json()) as { files: { id: string }[] };
    return data.files.map((f) => f.id);
  }
  /**
   * Find or create a folder. Concurrent first uploads can both create it, so
   * after creating we re-list and every uploader keeps the oldest; a losing
   * duplicate (ours, still empty) is trashed.
   */
  private async folder(name: string, parent: string) {
    const existing = await this.findFolders(name, parent);
    if (existing[0]) return existing[0];
    const created = (await (
      await this.call('/drive/v3/files?supportsAllDrives=true&fields=id', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name,
          mimeType: 'application/vnd.google-apps.folder',
          parents: [parent],
        }),
      })
    ).json()) as { id: string };
    const oldest = (await this.findFolders(name, parent))[0] ?? created.id;
    if (oldest !== created.id)
      await this.call(
        `/drive/v3/files/${encodeURIComponent(created.id)}?supportsAllDrives=true`,
        {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ trashed: true }),
        },
      ).catch(() => undefined);
    return oldest;
  }
  async put(input: Parameters<FileStore['put']>[0]): Promise<StoredFile> {
    validateUpload(input.mimeType, input.body.byteLength);
    let parent = this.rootId;
    for (const part of input.folder) parent = await this.folder(part, parent);
    const boundary = `cseed_${crypto.randomUUID()}`;
    const body = new Blob([
      `--${boundary}\r\nContent-Type: application/json; charset=UTF-8\r\n\r\n${JSON.stringify({ name: input.filename, parents: [parent] })}\r\n--${boundary}\r\nContent-Type: ${input.mimeType}\r\n\r\n`,
      input.body,
      `\r\n--${boundary}--`,
    ]);
    const file = (await (
      await this.call(
        '/upload/drive/v3/files?uploadType=multipart&supportsAllDrives=true&fields=id,webViewLink,size',
        {
          method: 'POST',
          headers: {
            'Content-Type': `multipart/related; boundary=${boundary}`,
          },
          body,
        },
      )
    ).json()) as { id: string; webViewLink?: string };
    return {
      storage: 'drive',
      key: file.id,
      webViewLink:
        file.webViewLink ?? `https://drive.google.com/file/d/${file.id}/view`,
      sizeBytes: input.body.byteLength,
    };
  }
  async get(key: string) {
    const response = await this.call(
      `/drive/v3/files/${encodeURIComponent(key)}?alt=media&supportsAllDrives=true`,
    );
    return {
      body: await response.arrayBuffer(),
      mimeType:
        response.headers.get('Content-Type') ?? 'application/octet-stream',
    };
  }
}
