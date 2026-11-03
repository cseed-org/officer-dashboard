import { useEffect, useState } from 'preact/hooks';
import type { ClubSettings, CurrentUser } from '../../lib/contracts';
import { api } from '../../lib/api/client';
import type { Loadable } from '../../lib/api/useApi';
import { timezoneSnapshot } from '../../lib/timezone';
import { Async, Loading } from '../components/states';

export default function LinksWidget({ me }: { me: CurrentUser }) {
  // The timezone gate already read the club settings for this page (and rereads them on focus);
  // fetch here only when that snapshot is missing.
  const cached = timezoneSnapshot().club;
  const [visible, setVisible] = useState<Loadable<ClubSettings>>(cached ? { status: 'ready', data: cached } : { status: 'loading' });
  useEffect(() => {
    if (cached) return;
    let live = true;
    api('clubSettingsGet').then((data) => live && setVisible({ status: 'ready', data }), (error) => live && setVisible({ status: 'error', error }));
    return () => { live = false; };
  }, []);
  const leadership = me.role === 'leadership' || me.isOwner;
  return <section class="tile tile--links" aria-label="Links">
    <header class="tile-head"><h2 class="tile-title">Links</h2></header>
    <div class="tile-body"><Async state={visible} loading={<Loading rows={3} label="Loading links" />}>{(s: ClubSettings) => <ul class="club-links motion-list">{[
      ['Club website', s.websiteUrl, '↗'], ['Notion', s.notionUrl, 'N'], ['Google Drive', s.driveUrl, '△'],
    ].map(([label, url, icon]) => <li key={label}>
      {url || leadership ? <a class={`club-link${url ? '' : ' is-unset'}`} href={url || '/settings#club-links'} target="_blank" rel="noopener noreferrer">
        <span class="club-link-icon" aria-hidden="true">{icon}</span><span>{label}</span><span class="club-link-action">{url ? '↗' : 'Add link'}</span>
      </a> : <div class="club-link is-unset"><span class="club-link-icon" aria-hidden="true">{icon}</span><span>{label}</span><span class="club-link-action">Not set</span></div>}
    </li>)}</ul>}</Async></div>
  </section>;
}
