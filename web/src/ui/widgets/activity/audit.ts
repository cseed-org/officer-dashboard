import type { AuditLogEntry } from '../../../lib/contracts';
import { href } from '../../../lib/routes';

/** "request.approve" → "approve"; "purchase_request" → "purchase request". */
export const words = (s: string) => s.replace(/[._]/g, ' ').trim();
export function auditHref(e: Pick<AuditLogEntry, 'entityType' | 'entityId'>): string | null {
  if (!e.entityId) return null;
  if (/request/.test(e.entityType)) return href.request(e.entityId);
  if (/event/.test(e.entityType)) return href.event(e.entityId);
  if (/user/.test(e.entityType)) return href.admin('users');
  if (/token/.test(e.entityType)) return href.admin('tokens');
  return null;
}
export function auditSummary(e: AuditLogEntry) {
  const base = e.actor?.name ?? (e.actorTokenName ? `token “${e.actorTokenName}”` : 'system');
  // An officer's MCP agent acts as them; say which agent did it.
  const who = e.actorAgentName ? `${base} via ${e.actorAgentName}` : base;
  const verb = words(e.action.split('.').pop() ?? e.action);
  // `actor` is the person (or token) alone: filters group an officer with their agents.
  return { who, actor: base, what: `${verb} ${words(e.entityType)}` };
}
