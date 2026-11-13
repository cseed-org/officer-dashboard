import { useEffect, useState } from 'preact/hooks';
import type { ClubSettings } from '../../../lib/contracts';
import { api } from '../../../lib/api/client';
import type { Loadable } from '../../../lib/api/useApi';
import { timezoneSnapshot } from '../../../lib/timezone';
import WidgetCard, { WidgetEmpty, WidgetState } from '../shell/WidgetCard';
import type { WidgetProps } from '../registry';

const domain = (url: string) => { try { const u = new URL(url); return (u.hostname.replace(/^www\./, '') + u.pathname).replace(/\/$/, ''); } catch { return url; } };

export default function LinksWidget({ me, size }: WidgetProps) {
  // The timezone gate already read the club settings for this page (and rereads them on focus);
  // fetch here only when that snapshot is missing.
  const cached = timezoneSnapshot().club;
  const [state, setState] = useState<Loadable<ClubSettings>>(cached ? { status: 'ready', data: cached } : { status: 'loading' });
  useEffect(() => {
    if (cached) return;
    let live = true;
    api('clubSettingsGet').then((data) => live && setState({ status: 'ready', data }), (error) => live && setState({ status: 'error', error }));
    return () => { live = false; };
  }, []);
  const leadership = me.role === 'leadership' || me.isOwner;
  const layout = size === '2x1' ? 'row' : size === '2x2' ? 'grid' : 'list';
  const body = () => {
    if (state.status !== 'ready') return <WidgetState state={state} label="links" />;
    const s = state.data;
    const links = ([['C', 'Club website', s.websiteUrl], ['N', 'Notion', s.notionUrl], ['D', 'Google Drive', s.driveUrl]] as const)
      .filter(([, , url]) => url || leadership);
    if (!links.length) return <WidgetEmpty icon="link" size={size}>No club links yet</WidgetEmpty>;
    return <ul class={`links links--${layout} club-links`}>
      {links.map(([chip, label, url]) => <li key={label}>
        <a class={`link-row${url ? '' : ' link-unset'}`} href={url || '/settings#club-links'} {...(url ? { target: '_blank', rel: 'noopener noreferrer' } : {})}>
          <span class="link-chip" aria-hidden="true">{chip}</span>
          <span class="row-main"><span class="row-title">{label}{url && <span class="visually-hidden"> (opens in a new tab)</span>}</span>
            {layout !== 'row' && <span class="row-meta">{url ? domain(url) : layout === 'grid' ? 'Not set · add in Settings' : 'Add link'}</span>}</span>
        </a>
      </li>)}
      {layout === 'grid' && leadership && <li><a class="link-row link-unset" href="/settings#club-links"><span class="link-chip" aria-hidden="true">+</span><span class="row-main"><span class="row-title">Add a link</span><span class="row-meta">Leadership only</span></span></a></li>}
    </ul>;
  };
  return <WidgetCard id="links" me={me}>{body()}</WidgetCard>;
}
