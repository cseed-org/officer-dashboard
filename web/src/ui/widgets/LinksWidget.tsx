import { useApi } from '../../lib/api/useApi';
import { Async } from '../components/states';

export default function LinksWidget() {
  const state = useApi('clubSettingsGet');
  return <section class="tile" aria-label="Links"><header class="tile-head"><h2 class="tile-title">Links</h2></header><div class="tile-body"><Async state={state}>{s => <ul class="list">{[
    ['Club website', s.websiteUrl], ['Notion', s.notionUrl], ['Google Drive', s.driveUrl],
  ].map(([label, url]) => <li key={label}>{url ? <a href={url} target="_blank" rel="noopener noreferrer">{label} ↗</a> : <span class="muted">{label} · not set yet</span>}</li>)}</ul>}</Async><a class="small" href="/settings#club-links">Club link settings →</a></div></section>;
}
