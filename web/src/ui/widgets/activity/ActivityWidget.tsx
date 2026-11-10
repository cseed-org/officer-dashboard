import type { AuditLogEntry } from '../../../lib/contracts';
import { useApi } from '../../../lib/api/useApi';
import { relative } from '../../../lib/format';
import { href } from '../../../lib/routes';
import { Async, Empty, Loading } from '../../components/states';

import Block from '../shell/WidgetCard';
import type { WidgetProps } from '../registry';
import { RequestList } from '../my-action/MyActionWidget';
export default function ActivityWidget({ me, size }: WidgetProps) {
 const director = me.role !== 'officer';
  return (
              <Block id="activity" title={director ? 'recent activity' : 'latest requests'} action={director ? <a href={href.admin('audit')}>audit log →</a> : <a href={href.requests({ scope: 'all' })}>all requests →</a>}>
                {director ? <AuditFeed limit={size === '2x1' ? 2 : size === '1x2' ? 5 : 8} /> : <RequestFeed limit={size === '2x1' ? 2 : size === '1x2' ? 4 : 6} />}
              </Block>
            );
}
function RequestFeed({ limit }: { limit: number }) {
  const state = useApi('requestsList', { query: { scope: 'all', limit } });
  return (
    <Async state={state} loading={<Loading rows={4} label="Loading requests" />}>
      {(d) => (d.items.length ? <RequestList items={d.items} showRequester /> : <Empty title="No requests yet">Purchase requests from every officer show here.</Empty>)}
    </Async>
  );
}

/** "request.approve" → "approve"; "purchase_request" → "purchase request". */
const words = (s: string) => s.replace(/[._]/g, ' ').trim();
export function auditHref(e: Pick<AuditLogEntry, 'entityType' | 'entityId'>): string | null {
  if (!e.entityId) return null;
  if (/request/.test(e.entityType)) return href.request(e.entityId);
  if (/event/.test(e.entityType)) return href.event(e.entityId);
  if (/user/.test(e.entityType)) return href.admin('users');
  if (/token/.test(e.entityType)) return href.admin('tokens');
  return null;
}
export function auditSummary(e: AuditLogEntry) {
  const who = e.actor?.name ?? (e.actorTokenName ? `token “${e.actorTokenName}”` : 'system');
  const verb = words(e.action.split('.').pop() ?? e.action);
  return { who, what: `${verb} ${words(e.entityType)}` };
}

function AuditFeed({ limit }: { limit: number }) {
  const state = useApi('adminAudit', { query: { limit } });
  return (
    <Async state={state} loading={<Loading rows={4} label="Loading activity" />}>
      {(d) =>
        d.items.length === 0 ? (
          <Empty title="No activity yet">Approvals, imports and admin changes show up here.</Empty>
        ) : (
          <ol class="feed">
            {d.items.map((e) => {
              const s = auditSummary(e);
              const link = auditHref(e);
              return (
                <li key={e.id} class="feed-item">
                  <span class="feed-text">
                    <strong>{s.who}</strong> {s.what}
                    {link && (
                      <>
                        {' · '}
                        <a href={link}>open</a>
                      </>
                    )}
                  </span>
                  <time class="feed-time" dateTime={e.createdAt}>
                    {relative(e.createdAt)}
                  </time>
                </li>
              );
            })}
          </ol>
        )
      }
    </Async>
  );
}
