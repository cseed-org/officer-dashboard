import type { FileStore } from '../../core/ports';
import { validateUpload } from './drive';
/** Worker dev cannot access the host filesystem. Local D1 persists bytes in its SQLite file. */
export class D1LocalFileStore implements FileStore {
  constructor(private db: D1Database) {}
  async put(input: Parameters<FileStore['put']>[0]) {
    validateUpload(input.mimeType, input.body.byteLength);
    const key = crypto.randomUUID();
    await this.db
      .prepare(
        'INSERT INTO local_receipt_files (id, mime_type, body) VALUES (?, ?, ?)',
      )
      .bind(key, input.mimeType, input.body)
      .run();
    return {
      storage: 'local' as const,
      key,
      webViewLink: null,
      sizeBytes: input.body.byteLength,
    };
  }
  async get(key: string) {
    const row = await this.db
      .prepare('SELECT mime_type, body FROM local_receipt_files WHERE id = ?')
      .bind(key)
      .first<{ mime_type: string; body: number[] }>();
    return row
      ? { body: new Uint8Array(row.body).buffer, mimeType: row.mime_type }
      : null;
  }
}
