import type { FileStore, StoredFile } from '../../core/ports';

/**
 * Local stub FileStore for development: keeps files in memory for the life of the
 * isolate (Workers have no writable filesystem). Replaced by the Google Drive adapter
 * in Phase 1; a filesystem adapter can follow for the Node / Pi deployment.
 */
export class MemoryFileStore implements FileStore {
  private files = new Map<string, { body: ArrayBuffer; mimeType: string }>();

  async put(input: { folder: string[]; filename: string; mimeType: string; body: ArrayBuffer }): Promise<StoredFile> {
    const key = [...input.folder, `${crypto.randomUUID()}-${input.filename}`].join('/');
    this.files.set(key, { body: input.body, mimeType: input.mimeType });
    return { storage: 'local', key, webViewLink: null, sizeBytes: input.body.byteLength };
  }

  async get(key: string) {
    return this.files.get(key) ?? null;
  }
}
