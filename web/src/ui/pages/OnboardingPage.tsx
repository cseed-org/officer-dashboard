import { useEffect, useRef, useState } from 'preact/hooks';
import { api, ApiCallError } from '../../lib/api/client';
import type { CalendarSettings, CurrentUser, OnboardingState, ProfileResponse, WidgetPlacement } from '../../lib/contracts';
import { clearCachedRole } from '../../lib/session';
import { choiceOf, layoutForChoices, onboardingChoices } from '../widgets/registry';
import { Icon } from '../components/Icon';
import { ProfileForm, type ProfileFormHandle } from '../views/ProfileForm';
import { CalendarPanel } from '../views/CalendarPanel';

const NAMES = ['Hello', 'Confirm', 'Profile', 'Calendar', 'Widgets', 'Done'];
const WIDGET_SUB: Record<string, string> = {
  kpis: 'Events, check-ins and spend at a glance',
  'my-action': 'Requests waiting on you',
  'recent-events': 'What’s coming up and what just happened',
  budget: 'Budget against actual spend',
  activity: 'Recent changes across the club',
};

type Loaded = { user: CurrentUser; onboarding: OnboardingState };

const firstName = (s: string) => s.trim().split(/\s+/)[0]!.toLowerCase();
const roleLabel = (u: CurrentUser) => (u.isOwner ? 'Leadership · Owner' : u.role[0]!.toUpperCase() + u.role.slice(1));
const ROLE_NOTE: Record<string, string> = {
  officer: 'Officers work on events, contacts and design requests.',
  director: 'Directors can also invite officers and update member records.',
  leadership: 'Leadership manages all access, including leadership grants.',
};

function Unavailable() {
  return (
    <div class="onb">
      <header class="onb-top">
        <span class="wordmark">CSEED</span>
      </header>
      <div class="onb-main onb-main--single">
        <section class="onb-step">
          <p class="brand-eyebrow">invitation unavailable</p>
          <h1 class="display display--l onb-title" id="step-title" tabIndex={-1}>
            Let’s get you a fresh start.
          </h1>
          <p class="brand-lede">This invitation has expired or isn’t valid. Ask the person who invited you for a new link.</p>
          <p>Invitations only work with the matching Discord account. If you have more than one, sign out of Discord and try the other.</p>
          <div class="action-row">
            <a class="btn" href="/login">
              Return to sign in
            </a>
          </div>
          <p class="field-hint">No profile information has been submitted.</p>
        </section>
      </div>
    </div>
  );
}

export default function OnboardingPage() {
  const [unavailable, setUnavailable] = useState(false);
  const [data, setData] = useState<Loaded | null>(null);
  const [loadError, setLoadError] = useState<unknown>(null);
  const [step, setStep] = useState(1);
  const [dir, setDir] = useState<1 | -1>(1);
  const [busy, setBusy] = useState(false);
  const [stepError, setStepError] = useState<string | null>(null);
  const [profile, setProfile] = useState<ProfileResponse | null>(null);
  const [cal, setCal] = useState<CalendarSettings | null>(null);
  const [picked, setPicked] = useState<Set<string> | null>(null);
  const [correcting, setCorrecting] = useState(false);
  const [correction, setCorrection] = useState('');
  const [correctionResult, setCorrectionResult] = useState<{ ok: boolean; text: string } | null>(null);
  const profileRef = useRef<ProfileFormHandle>(null);
  const titleRef = useRef<HTMLHeadingElement>(null);
  const focusNext = useRef(false);
  const pushed = useRef(0); // history entries this page added, so Back never leaves setup

  const load = () => {
    setLoadError(null);
    Promise.all([api('me'), api('profileGet'), api('prefsGet')]).then(
      ([me, p, prefs]) => {
        if (!me.onboarding) {
          location.replace('/');
          return;
        }
        setData({ user: me.user, onboarding: me.onboarding });
        setProfile(p);
        setPicked(new Set(prefs.widgetLayout.map((w) => choiceOf(w.id)).filter((id): id is string => !!id)));
        // Resume at the saved step; Google consent returns to the calendar step.
        const back = new URLSearchParams(location.search).get('google');
        const s = back ? 4 : Math.min(me.onboarding.step, me.onboarding.hasProfile ? 6 : 3);
        setStep(s === 6 ? 5 : s);
        history.replaceState({ step: s === 6 ? 5 : s }, '', back ? location.pathname + location.search : location.pathname);
      },
      (e) => setLoadError(e),
    );
  };
  useEffect(() => {
    // Read the URL after hydration: the static HTML is the loading state.
    if (new URLSearchParams(location.search).has('unavailable')) {
      setUnavailable(true);
      return;
    }
    load();
    const onPop = (e: PopStateEvent) => {
      const s = (e.state as { step?: number } | null)?.step;
      pushed.current = Math.max(0, pushed.current - 1);
      if (s && s >= 1 && s <= 5) {
        setDir(-1);
        focusNext.current = true;
        setStep(s);
      }
    };
    addEventListener('popstate', onPop);
    return () => removeEventListener('popstate', onPop);
  }, []);

  useEffect(() => {
    if (focusNext.current) {
      focusNext.current = false;
      requestAnimationFrame(() => titleRef.current?.focus());
    }
  }, [step]);

  if (unavailable) return <Unavailable />;
  if (loadError)
    return (
      <div class="onb">
        <div class="onb-main onb-main--single">
          <section class="onb-step">
            <h1 class="display display--l onb-title">We couldn’t load your setup.</h1>
            <p class="notice notice--negative" role="alert">
              {loadError instanceof Error ? loadError.message : 'Check your connection and try again.'}
            </p>
            <div class="action-row">
              <button class="btn" onClick={load}>
                Try again
              </button>
            </div>
          </section>
        </div>
      </div>
    );
  if (!data || !profile || !picked)
    return (
      <div class="onb" aria-busy="true">
        <div class="onb-main onb-main--single">
          <section class="onb-step">
            <p class="brand-eyebrow">one moment</p>
            <h1 class="display display--l onb-title">Getting your space ready.</h1>
            <p role="status">Loading your invitation and saved setup…</p>
            <div class="skel skel--row" aria-hidden="true" />
          </section>
        </div>
      </div>
    );

  const { user, onboarding: ob } = data;
  const inviter = ob.invitedBy?.name ?? 'your inviter';
  const team = ob.subteam?.name ?? 'the officer team';

  const goTo = (s: number) => {
    setStepError(null);
    setDir(s > step ? 1 : -1);
    focusNext.current = true;
    setStep(s);
    if (s > step) {
      history.pushState({ step: s }, '', location.pathname);
      pushed.current++;
    } else history.replaceState({ step: s }, '', location.pathname);
    if (s <= 5) api('onboardingSave', { body: { step: s } }).catch(() => {});
  };

  const finishLater = async () => {
    setBusy(true);
    try {
      await api('onboardingSave', { body: { step } });
    } catch {
      /* best effort: sign-in resumes at the last saved step */
    }
    clearCachedRole();
    const f = document.createElement('form');
    f.method = 'post';
    f.action = '/auth/logout';
    document.body.append(f);
    f.submit();
  };

  const next = async () => {
    setStepError(null);
    if (step === 3) {
      setBusy(true);
      const ok = await profileRef.current?.submit();
      setBusy(false);
      if (!ok) return;
      setData({ user, onboarding: { ...ob, hasProfile: true } });
      return goTo(4);
    }
    if (step === 5) {
      setBusy(true);
      try {
        const layout: WidgetPlacement[] = layoutForChoices(picked);
        await api('prefsPut', { body: { widgetLayout: layout } });
        await api('onboardingComplete');
        setDir(1);
        focusNext.current = true;
        setStep(6);
      } catch (e) {
        setStepError(e instanceof ApiCallError ? e.message : 'We couldn’t save this step. Your choices are still here. Try again.');
      } finally {
        setBusy(false);
      }
      return;
    }
    if (step === 6) {
      location.assign('/');
      return;
    }
    goTo(step + 1);
  };

  const sendCorrection = async (e: Event) => {
    e.preventDefault();
    if (!correction.trim()) return;
    setBusy(true);
    try {
      const r = await api('onboardingCorrection', { body: { message: correction.trim() } });
      if (r.dm.status === 'sent') setCorrectionResult({ ok: true, text: `Sent to ${r.inviter.name} on Discord. You can keep going; they’ll fix it from the Officers page.` });
      else setCorrectionResult({ ok: false, text: r.dm.message ?? `Discord DMs aren’t set up here. Message ${r.inviter.name} on Discord directly.` });
      if (r.dm.status === 'sent') setCorrecting(false);
    } catch (err) {
      setCorrectionResult({ ok: false, text: err instanceof Error ? err.message : 'Couldn’t send that.' });
    } finally {
      setBusy(false);
    }
  };

  const googlePending = step === 4 && cal?.mode === 'google' && !cal.google.connected;
  const nextLabel = step === 1 ? 'Let’s begin' : step === 6 ? 'Go to dashboard' : step === 4 && (googlePending || !cal?.activeEmail) ? 'Set up later' : 'Continue';

  const titles: preact.ComponentChildren[] = [
    <>
      hey {firstName(ob.name)},<br />
      welcome <span class="mark">in.</span>
    </>,
    <>a familiar face.</>,
    <>a little about you.</>,
    <>make room for CSEED.</>,
    <>your home, your way.</>,
    <>you’re part of it.</>,
  ];
  const ledes = [
    'A community of people who make things happen. Now, with you in it. Let’s get your CSEED space ready.',
    `${inviter} invited you to ${team}. Check that we’ve got the right person before we get started.`,
    'A few essentials to help your team stay in touch. The rest is up to you.',
    'Keep club events alongside everything else in your life. You can change this in Settings.',
    'Start with what’s useful. Add, remove and resize widgets on Home whenever you like.',
    'Your profile, calendar preferences and Home layout are ready. Let’s make something happen.',
  ];

  let body: preact.ComponentChildren = null;
  if (step === 1)
    body = (
      <>
        <ul class="onb-checks">
          <li>
            <Icon name="check" /> About four minutes to make this space yours
          </li>
          <li>
            <Icon name="check" /> You can change everything later in Settings
          </li>
        </ul>
        <p class="brand-eyebrow">invited by {inviter.toLowerCase()}</p>
      </>
    );
  if (step === 2)
    body = (
      <>
        <dl class="onb-prefill" data-prefill>
          <dt>Name</dt>
          <dd>{ob.name}</dd>
          <dt>Discord</dt>
          <dd class="mono">{user.discordId}</dd>
          <dt>Subteam</dt>
          <dd>{team}</dd>
          <dt>Access</dt>
          <dd>
            <span class="role-badge" data-role={user.isOwner ? 'leadership' : user.role}>
              {roleLabel(user)}
            </span>
          </dd>
        </dl>
        <p class="field-hint">
          Pre-filled by {inviter} when they invited you. {ROLE_NOTE[user.role] ?? ''} Access is assigned by leadership, never chosen here.
        </p>
        {correcting ? (
          <form class="onb-form" onSubmit={sendCorrection} data-correction>
            <div class="field">
              <label class="field-label" for="correction">
                What should {inviter} fix?
              </label>
              <textarea
                class="textarea input"
                id="correction"
                rows={3}
                maxLength={500}
                value={correction}
                onInput={(e) => setCorrection((e.target as HTMLTextAreaElement).value)}
                aria-describedby="correction-hint"
              />
              <p class="field-hint" id="correction-hint">
                {ob.correctionsByDm ? `Sent to ${inviter} as a Discord DM from the CSEED bot.` : `We’ll note it; then message ${inviter} on Discord.`}
              </p>
            </div>
            <div class="action-row">
              <button class="btn" type="submit" disabled={busy || !correction.trim()}>
                Send to {inviter}
              </button>
              <button class="btn btn--text" type="button" onClick={() => setCorrecting(false)}>
                Cancel
              </button>
            </div>
          </form>
        ) : (
          <p class="field-hint">
            Something off with your name or subteam?{' '}
            <button type="button" class="linkish" onClick={() => (setCorrecting(true), setCorrectionResult(null))} data-correction-open>
              Tell {inviter}
            </button>
            .
          </p>
        )}
        {correctionResult && (
          <p class={`notice${correctionResult.ok ? ' notice--positive' : ''}`} role="status">
            {correctionResult.text}
            {!correctionResult.ok && ob.inviterDiscordId && (
              <>
                {' '}
                <a href={`https://discord.com/users/${ob.inviterDiscordId}`} target="_blank" rel="noopener noreferrer">
                  Open {inviter} on Discord
                </a>
              </>
            )}
          </p>
        )}
      </>
    );
  if (step === 3) body = <ProfileForm data={profile} brand showSave={false} handle={profileRef} onSaved={setProfile} />;
  if (step === 4)
    body = (
      <div class="onb-form">
        <CalendarPanel next="/onboarding" onState={setCal} />
        <p class="field-hint">Calendar setup is optional. Continue without connecting to finish it later in Settings.</p>
      </div>
    );
  if (step === 5)
    body = (
      <div class="onb-form">
        <fieldset class="fieldset">
          <legend>Widgets for your Home</legend>
          <div class="choice-row">
            {onboardingChoices.map((w) => (
              <label class="choice" key={w.id}>
                <input
                  type="checkbox"
                  value={w.id}
                  checked={picked.has(w.id)}
                  onChange={(e) => {
                    const s = new Set(picked);
                    (e.target as HTMLInputElement).checked ? s.add(w.id) : s.delete(w.id);
                    setPicked(s);
                  }}
                />
                <span class="choice-title">{w.title}</span>
                <span class="choice-sub">{WIDGET_SUB[w.id] ?? 'Available on Home'}</span>
              </label>
            ))}
          </div>
        </fieldset>
        <p class="field-hint" aria-live="polite" data-widget-count>
          {picked.size
            ? `${picked.size} widget${picked.size === 1 ? '' : 's'} selected. Your layout follows you across devices.`
            : 'No widgets selected. Start with a quiet Home and add widgets later.'}
        </p>
      </div>
    );
  if (step === 6) {
    const ok = (t: string) => (
      <li>
        <Icon name="check" />
        <span class="visually-hidden">Done: </span>
        {t}
      </li>
    );
    const later = (t: string) => (
      <li>
        <Icon name="arrow-right" />
        <span class="visually-hidden">Deferred: </span>
        {t}
      </li>
    );
    body = (
      <ul class="onb-checks" data-summary>
        {ok(`${ob.name} · ${team}`)}
        {ok('Profile complete')}
        {cal?.mode === 'google'
          ? cal.google.connected
            ? ok(`Google Calendar connected${cal.google.accountEmail ? ` as ${cal.google.accountEmail}` : ''}`)
            : later('Calendar setup deferred · Google not connected')
          : cal?.activeEmail
            ? ok(`.ics invites to ${cal.activeEmail}${cal.ics.configured ? '' : ' (email not configured yet)'}`)
            : later('Calendar setup deferred · no email selected')}
        {ok(`${picked.size} Home widget${picked.size === 1 ? '' : 's'} selected`)}
      </ul>
    );
  }

  const eyebrow = step === 1 ? 'your cseed starts here' : `${String(step).padStart(2, '0')} · ${NAMES[step - 1]!.toLowerCase()}`;
  return (
    <div class="onb" data-step={step}>
      <a class="skip-link" href="#step-title">
        Skip to setup
      </a>
      <header class="onb-top">
        <span class="wordmark">CSEED</span>
        <ol class="onb-progress" aria-label={`Setup progress, step ${step} of 6`}>
          {NAMES.map((n, i) => (
            <li key={n} class={`onb-seg${i + 1 < step ? ' is-done' : ''}`} aria-current={i + 1 === step ? 'step' : undefined}>
              <span class="visually-hidden">
                {n}
                {i + 1 < step ? ', completed' : ''}
              </span>
            </li>
          ))}
        </ol>
        <span class="onb-count" aria-hidden="true">
          0{step} / 06
        </span>
      </header>
      <div class={`onb-main${step === 3 || step === 4 || step === 5 ? ' onb-main--single' : ''}`}>
        <section class={`onb-step is-entering${dir === -1 ? ' is-back' : ''}`} key={step} aria-busy={busy || undefined}>
          <p class="brand-eyebrow">{eyebrow}</p>
          <h1 class="display display--xl onb-title" id="step-title" tabIndex={-1} ref={titleRef}>
            {titles[step - 1]}
          </h1>
          <p class="brand-lede onb-lede">{ledes[step - 1]}</p>
          {body}
          {stepError && (
            <div class="notice notice--negative" role="alert">
              {stepError}{' '}
              <button class="btn btn--small" onClick={next}>
                Try again
              </button>
            </div>
          )}
        </section>
        {!(step === 3 || step === 4 || step === 5) && (
          <aside class="onb-art" aria-hidden="true">
            <figure class="polaroid" style="--tilt:-6deg">
              <img src="/images/brand/community-tall.webp" alt="" />
              <figcaption>good people. big ideas.</figcaption>
            </figure>
            <figure class="polaroid" style="--tilt:7deg;--n:1">
              <img src="/images/brand/buildher-wide.webp" alt="" />
              <figcaption>make something that matters.</figcaption>
            </figure>
          </aside>
        )}
      </div>
      <footer class="onb-foot">
        <button class="btn btn--text" type="button" onClick={() => (pushed.current > 0 ? history.back() : goTo(step - 1))} disabled={step === 1 || step === 6 || busy} data-onb-back>
          ← Back
        </button>
        {step < 6 && (
          <button class="btn btn--text onb-skip" type="button" onClick={finishLater} disabled={busy} data-finish-later>
            Finish later
          </button>
        )}
        <button class="btn btn--primary" type="button" onClick={next} disabled={busy} data-onb-next>
          {busy ? 'Saving…' : `${nextLabel} →`}
        </button>
      </footer>
    </div>
  );
}
