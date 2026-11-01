import { useState } from 'preact/hooks';
import type { CurrentUser } from '../../lib/contracts';
import { type Density, getDensity, getTheme, setDensity, setTheme, type ThemePref } from '../../lib/prefs';
import { clearCachedRole, initials } from '../../lib/session';
import { Section } from '../components/kit';
import { safeImageUrl } from '../../lib/url';

const THEMES: { key: ThemePref; label: string; sub: string }[] = [
  { key: 'light', label: 'Light', sub: 'the default' },
  { key: 'dark', label: 'Dark', sub: 'easier at night' },
  { key: 'system', label: 'System', sub: 'follow this device' },
];
const DENSITIES: { key: Density; label: string; sub: string }[] = [
  { key: 'comfortable', label: 'Comfortable', sub: 'more breathing room' },
  { key: 'compact', label: 'Compact', sub: 'more rows on screen' },
];

export default function SettingsView({ me }: { me: CurrentUser }) {
  const [theme, setThemeState] = useState<ThemePref>(getTheme);
  const [density, setDensityState] = useState<Density>(getDensity);

  return (
    <div class="stack settings">
      <Section title="appearance" id="appearance">
        <fieldset class="fieldset">
          <legend>theme</legend>
          <div class="choice-row" data-setting="theme">
            {THEMES.map((t) => (
              <label class="choice" key={t.key}>
                <input
                  type="radio"
                  name="theme"
                  value={t.key}
                  checked={theme === t.key}
                  onChange={() => {
                    setTheme(t.key);
                    setThemeState(t.key);
                  }}
                />
                <span class="choice-title">{t.label}</span>
                <span class="choice-sub">{t.sub}</span>
              </label>
            ))}
          </div>
        </fieldset>
        <fieldset class="fieldset">
          <legend>density</legend>
          <div class="choice-row" data-setting="density">
            {DENSITIES.map((d) => (
              <label class="choice" key={d.key}>
                <input
                  type="radio"
                  name="density"
                  value={d.key}
                  checked={density === d.key}
                  onChange={() => {
                    setDensity(d.key);
                    setDensityState(d.key);
                  }}
                />
                <span class="choice-title">{d.label}</span>
                <span class="choice-sub">{d.sub}</span>
              </label>
            ))}
          </div>
        </fieldset>
        <p class="muted small">Saved in this browser only.</p>
      </Section>

      <Section title="your profile" id="profile">
        <div class="profile">
          {safeImageUrl(me.avatarUrl) ? <img class="avatar avatar--lg" src={safeImageUrl(me.avatarUrl)!} alt="" width={56} height={56} /> : <span class="avatar avatar--lg" aria-hidden="true">{initials(me.name)}</span>}
          <dl class="facts">
            <div>
              <dt>name</dt>
              <dd>{me.name}</dd>
            </div>
            <div>
              <dt>role</dt>
              <dd data-profile-role>{me.role}</dd>
            </div>
            <div>
              <dt>title</dt>
              <dd>{me.title ?? '—'}</dd>
            </div>
            <div>
              <dt>Discord id</dt>
              <dd class="mono">{me.discordId}</dd>
            </div>
          </dl>
        </div>
        <p class="muted small">Your name and picture come from Discord. A director can change your role or title.</p>
      </Section>

      <Section title="session" id="session">
        <form
          method="post"
          action="/auth/logout"
          onSubmit={() => {
            // The next person on this browser must not inherit a cached director role.
            clearCachedRole();
          }}
        >
          <button type="submit" class="btn btn--danger" data-sign-out>
            sign out
          </button>
        </form>
      </Section>
    </div>
  );
}
