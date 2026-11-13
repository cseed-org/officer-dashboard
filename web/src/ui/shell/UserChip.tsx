import { initials, useMe } from '../../lib/session';
import { watchSystemTheme } from '../../lib/prefs';
import { useEffect } from 'preact/hooks';
import { safeImageUrl } from '../../lib/url';

/** Sidebar footer: who is signed in, linking to Settings. */
export default function UserChip() {
  const me = useMe();
  useEffect(() => watchSystemTheme(), []);
  if (me.status !== 'ready')
    return (
      <span class="user-chip user-chip--loading" aria-hidden="true">
        <span class="avatar" />
      </span>
    );
  const u = me.user;
  return (
    <a class="user-chip" href="/settings" data-user-chip={u.role}>
      {safeImageUrl(u.avatarUrl) ? <img class="avatar" src={safeImageUrl(u.avatarUrl)!} alt="" width={28} height={28} /> : <span class="avatar" aria-hidden="true">{initials(u.name)}</span>}
      <span class="user-chip-text">
        <span class="user-chip-name">{u.name}</span>
        <span class="user-chip-role">{u.role}</span>
      </span>
    </a>
  );
}
