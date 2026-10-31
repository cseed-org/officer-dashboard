/**
 * Ports: the small interfaces core logic depends on. Adapters live in src/server/*
 * (Google Drive / Discord webhook in Phase 1; local stubs now), so core stays portable to
 * the future Node / Raspberry Pi deployment.
 */
import type { PurchaseStatus, StorageBackend } from './domain';

export interface StoredFile {
  storage: StorageBackend;
  /** Drive fileId, or the local stub key. */
  key: string;
  /** Link a signed-in officer can open (Drive webViewLink, or /api/attachments/:id). */
  webViewLink: string | null;
  sizeBytes: number;
}

export interface FileStore {
  /**
   * Store a file under a logical folder path, e.g. ["FY2027", "2027-0007"]
   * → Drive: <shared drive folder>/FY2027/2027-0007/<filename>.
   */
  put(input: { folder: string[]; filename: string; mimeType: string; body: ArrayBuffer }): Promise<StoredFile>;
  get(key: string): Promise<{ body: ArrayBuffer; mimeType: string } | null>;
}

export type NotificationEvent =
  | { type: 'request.submitted'; requestId: string; number: string; title: string; totalCents: number; requester: string }
  | { type: 'request.approved' | 'request.denied'; requestId: string; number: string; title: string; director: string; comment?: string }
  | { type: 'request.status'; requestId: string; number: string; title: string; status: PurchaseStatus };

export interface Notifier {
  /** Best effort: implementations must not throw for delivery failures (log instead). */
  notify(event: NotificationEvent, link: string): Promise<void>;
}
