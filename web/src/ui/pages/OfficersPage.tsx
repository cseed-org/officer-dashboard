import OfficersView from '../views/OfficersView';
import AdminUsersView from '../views/AdminUsersView';
import AdminTokensView from '../views/AdminTokensView';
import AuditLog from '../views/AuditLog';
import { Gate } from '../shell/Gate';
import { Tabs } from '../components/kit';
import { setParams, useSearch } from '../../lib/nav';
import type { CurrentUser } from '../../lib/contracts';

function People({ me }: { me: CurrentUser }) {
  const search = useSearch();
  const tabs = [
    { key: 'directory', label: 'Directory' },
    ...(me.role === 'officer' ? [] : [
      { key: 'invites', label: 'Invites' },
      { key: 'access', label: 'Access' },
      { key: 'tokens', label: 'Service tokens' },
      { key: 'audit', label: 'Audit log' },
    ]),
  ];
  const tab = tabs.find((t) => t.key === search.get('tab'))?.key ?? 'directory';
  const creating = search.get('new') === '1';
  const closeCreate = () => setParams({ new: null });
  const action = tab === 'access' ? 'add officer' : tab === 'tokens' ? 'create token' : null;

  return (
    <div class="stack officers-page">
      <div class="toolbar">
        <Tabs
          label="Officers" idPrefix="people" active={tab} tabs={tabs}
          onChange={(key) => setParams({ tab: key === 'directory' ? null : key, new: null }, { replace: true })}
        />
        {action && (
          <div class="toolbar-actions">
            <button type="button" class="btn btn--primary" data-admin-create onClick={() => setParams({ new: 1 })}>{action}</button>
          </div>
        )}
      </div>
      <div key={tab} id="people-panel" role="tabpanel" aria-labelledby={`people-tab-${tab}`} class="tab-panel">
        {tab === 'access' ? (
          <AdminUsersView meId={me.id} isOwner={me.isOwner} canChangeRoles={me.role === 'leadership'} adding={creating} onCloseAdd={closeCreate} />
        ) : tab === 'tokens' ? (
          <AdminTokensView creating={creating} onCloseCreate={closeCreate} />
        ) : tab === 'audit' ? (
          <AuditLog />
        ) : (
          <OfficersView me={me} section={tab === 'invites' ? 'invites' : 'directory'} />
        )}
      </div>
    </div>
  );
}

export default function OfficersPage() {
  return <Gate label="officers">{(me) => <People me={me} />}</Gate>;
}
