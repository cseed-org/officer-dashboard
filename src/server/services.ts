import type { FileStore, Notifier } from '../core/ports';
import { LocalFileStore } from './files/local';
import { D1LocalFileStore } from './files/d1-local';
import { GoogleDriveFileStore } from './files/drive';
import { ConsoleNotifier } from './notify/console';
import { DiscordWebhookNotifier } from './notify/discord';
import { getEnv } from './env';
export class ReceiptStorageUnavailable extends Error {
  readonly status = 503;
}
export function createFileStore(env: { DB: D1Database; GOOGLE_SERVICE_ACCOUNT_JSON?: string; DRIVE_ROOT_FOLDER_ID?: string; GOOGLE_DRIVE_FOLDER_ID?: string }, dev: boolean): FileStore {
  const root = env.DRIVE_ROOT_FOLDER_ID || env.GOOGLE_DRIVE_FOLDER_ID;
  if (env.GOOGLE_SERVICE_ACCOUNT_JSON && root) return new GoogleDriveFileStore(env.GOOGLE_SERVICE_ACCOUNT_JSON, root);
  if (!dev || env.GOOGLE_SERVICE_ACCOUNT_JSON) throw new ReceiptStorageUnavailable('Receipt storage requires Google Drive credentials and DRIVE_ROOT_FOLDER_ID');
  // Workerd's virtual filesystem is request-scoped. Keep the local adapter interface,
  // but persist Worker-dev bytes in the local SQLite file; Node/Pi can use its filesystem directly.
  return new LocalFileStore('.local/receipts', new D1LocalFileStore(env.DB));
}
let fileStore: FileStore | undefined;
export function getFileStore(): FileStore {
  return fileStore ??= createFileStore(getEnv(), import.meta.env.DEV);
}
let notifier: Notifier | undefined;
export function getNotifier(): Notifier {
  return (notifier ??= getEnv().DISCORD_WEBHOOK_URL
    ? new DiscordWebhookNotifier(getEnv().DISCORD_WEBHOOK_URL!)
    : new ConsoleNotifier());
}
