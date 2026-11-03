import type { CurrentUser } from '../../lib/contracts';
import { useApi } from '../../lib/api/useApi';
import { Async } from '../components/states';

export default function LinksWidget({ me }: { me: CurrentUser }) {
  const state = useApi('clubSettingsGet');
  const leadership = me.role === 'leadership' || me.isOwner;
  return <section class="tile tile--links" aria-label="Links">
    <header class="tile-head"><h2 class="tile-title">Links</h2></header>
    <div class="tile-body"><Async state={state}>{s => <ul class="club-links">{[
      ['Club website', s.websiteUrl, '↗'], ['Notion', s.notionUrl, 'N'], ['Google Drive', s.driveUrl, '△'],
    ].map(([label, url, icon]) => <li key={label}>
      {url || leadership ? <a class={`club-link${url ? '' : ' is-unset'}`} href={url || '/settings#club-links'} target="_blank" rel="noopener noreferrer">
        <span class="club-link-icon" aria-hidden="true">{icon}</span><span>{label}</span><span class="club-link-action">{url ? '↗' : 'Add link'}</span>
      </a> : <div class="club-link is-unset"><span class="club-link-icon" aria-hidden="true">{icon}</span><span>{label}</span><span class="club-link-action">Not set</span></div>}
    </li>)}</ul>}</Async></div>
  </section>;
}
