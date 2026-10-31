import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import type { FileStore } from '../../core/ports';
import { validateUpload } from './drive';
/** Persistent development adapter; use Drive for production Workers. */
export class LocalFileStore implements FileStore {
  constructor(private root = '.local/receipts', private backing?: FileStore) {}
  async put(input: Parameters<FileStore['put']>[0]) {
    validateUpload(input.mimeType, input.body.byteLength);
    if (this.backing) return this.backing.put(input);
    const key = crypto.randomUUID();
    await mkdir(this.root, { recursive: true });
    await writeFile(resolve(this.root, key), new Uint8Array(input.body));
    await writeFile(
      resolve(this.root, `${key}.json`),
      JSON.stringify({ mimeType: input.mimeType }),
    );
    return {
      storage: 'local' as const,
      key,
      webViewLink: null,
      sizeBytes: input.body.byteLength,
    };
  }
  async get(key: string) {
    if (this.backing) return this.backing.get(key);
    if (!/^[a-f0-9-]{36}$/.test(key)) return null;
    try {
      const bytes = await readFile(resolve(this.root, key));
      const meta = JSON.parse(
        await readFile(resolve(this.root, `${key}.json`), 'utf8'),
      );
      return {
        body: bytes.buffer.slice(
          bytes.byteOffset,
          bytes.byteOffset + bytes.byteLength,
        ) as ArrayBuffer,
        mimeType: String(meta.mimeType),
      };
    } catch (e) {
      if ((e as NodeJS.ErrnoException).code === 'ENOENT') return null;
      throw e;
    }
  }
}
