import { useEffect } from 'preact/hooks';
import type { CurrentUser } from '../../lib/contracts';
import { useApi } from '../../lib/api/useApi';
import { Gate } from '../shell/Gate';
import { Section } from '../components/kit';
import { ErrorState, Loading } from '../components/states';
import { DrivePanel } from '../views/DrivePanel';
import { OrgGooglePanel } from '../views/OrgGooglePanel';

function OwnerSetup({ me }: { me: CurrentUser }) {
  useEffect(() => { if (!me.isOwner) location.replace('/'); }, [me.isOwner]);
  return me.isOwner ? <SetupChecklist /> : null;
}

function SetupChecklist() {
  const status = useApi('setupStatus');
  useEffect(() => {
    if (status.status === 'ready' && location.hash) document.getElementById(location.hash.slice(1))?.scrollIntoView({ block: 'start' });
  }, [status.status]);
  return <div class="stack settings">
    <p>Connect the club's Google accounts and check integration configuration. Credentials are managed in host secret files.</p>
    <div><button type="button" class="btn" onClick={status.reload}>Refresh status</button></div>
    {status.status === 'loading' ? <Loading rows={5} label="Loading integration status" /> : status.status === 'error' ?
      <ErrorState error={status.error} onRetry={status.reload} /> : status.data.integrations.map((item) => (
        <Section key={item.key} title={item.label} id={item.key}>
          <div class="stack stack--tight">
            <div><span class={`badge ${item.state === 'connected' || item.state === 'configured' ? 'badge--positive' : item.state === 'error' ? 'badge--negative' : 'badge--attention'}`} role="status">{item.state}</span></div>
            <p style={{ overflowWrap: 'anywhere' }}>{item.detail}</p>
            {item.key === 'drive' && <DrivePanel />}
            {item.key === 'org-calendar' && <OrgGooglePanel />}
            <div><a href={item.docs} target="_blank" rel="noopener noreferrer">Configuration guide</a></div>
          </div>
        </Section>
      ))}
  </div>;
}

export default function SetupPage() {
  return <Gate label="setup">{(me) => <OwnerSetup me={me} />}</Gate>;
}
