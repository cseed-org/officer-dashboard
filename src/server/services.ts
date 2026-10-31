import type { FileStore, Notifier } from '../core/ports';
import { MemoryFileStore } from './files/memory';
import { ConsoleNotifier } from './notify/console';

/**
 * Adapter selection. Phase 1 swaps in GoogleDriveFileStore when GOOGLE_SERVICE_ACCOUNT_JSON
 * is set and DiscordWebhookNotifier when DISCORD_WEBHOOK_URL is set.
 */
let fileStore: FileStore | undefined;
export function getFileStore(): FileStore {
  return (fileStore ??= new MemoryFileStore());
}

let notifier: Notifier | undefined;
export function getNotifier(): Notifier {
  return (notifier ??= new ConsoleNotifier());
}
