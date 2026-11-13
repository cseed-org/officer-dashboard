import { markGreetingSignedOut } from '../views/home/browserGreeting';
import { useEffect, useRef, useState } from 'preact/hooks';
import { api, ApiCallError } from '../../lib/api/client';
import type { CalendarSettings, OnboardingState, ProfileResponse, WidgetPlacement, CurrentUser } from '../../lib/contracts';
import { clearCachedRole } from '../../lib/session';
import { choiceOf, layoutForChoices, onboardingChoices } from '../widgets/registry';
import { ProfileForm, type ProfileFormHandle, type ProfileSection } from '../views/ProfileForm';
import { CalendarPanel } from '../views/CalendarPanel';
import { WelcomeIntro } from '../onboarding/WelcomeIntro';
import { ShellLogo } from '../onboarding/LogoMark';

/**
 * First sign-in (and, once, for accounts from before V0.9): the welcome intro, then the intake
 * in five stages. Stages 1–3 are the profile (basics, about you, your role), 4 and 5 are the
 * optional calendar and Home widgets. Each stage saves when you leave it and the place is
 * saved too, so the flow resumes where it stopped. The intro ends on the header logo, which
 * stays put while the stages slide in.
 */
const NAMES = ['Basics', 'About you', 'Your role', 'Calendar', 'Widgets'];
const TOTAL = NAMES.length;
const SECTIONS: Record<number, ProfileSection> = { 1: 'basics', 2: 'about', 3: 'role' };
const WIDGET_SUB: Record<string, string> = {
  kpis: 'Events, check-ins and spend at a glance',
  'my-action': 'Requests waiting on you',
  'recent-events': 'What’s coming up and what just happened',
  budget: 'Budget against actual spend',
  activity: 'Recent changes across the club',
};

type Loaded = { user: CurrentUser; onboarding: OnboardingState };
/** An inline, persistent error with one action (Retry, Leave without saving). */
type StepError = { text: string; action?: { label: string; run: () => void } };

/** First word of the name as written: "McKenzie" stays "McKenzie". */
const firstName = (s: string) => s.trim().split(/\s+/)[0] ?? '';
const reducedMotion = () => matchMedia('(prefers-reduced-motion: reduce)').matches;

/** Moves focus to the screen's heading once, so screen readers start there. */
function useFocusOnMount() {
  const ref = useRef<HTMLHeadingElement>(null);
  useEffect(() => ref.current?.focus(), []);
  return ref;
}

function LoadError({ error, onRetry }: { error: unknown; onRetry: () => void }) {
  const ref = useFocusOnMount();
  return (
    <div class="onb">
      <div class="onb-main onb-main--single">
        <section class="onb-step">
          <h1 class="display display--l onb-title" id="step-title" tabIndex={-1} ref={ref}>
            We couldn’t load your setup.
          </h1>
          <p class="notice notice--negative" role="alert">
            {error instanceof Error ? error.message : 'Check your connection and try again.'}
          </p>
          <div class="action-row">
            <button class="btn" onClick={onRetry}>
              Try again
            </button>
          </div>
        </section>
      </div>
    </div>
  );
}

function Unavailable() {
  const ref = useFocusOnMount();
  return (
    <div class="onb">
      <header class="onb-top">
        <span class="wordmark">CSEED</span>
      </header>
      <div class="onb-main onb-main--single">
        <section class="onb-step">
          <p class="brand-eyebrow">invitation unavailable</p>
          <h1 class="display display--l onb-title" id="step-title" tabIndex={-1} ref={ref}>
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

function OnboardingFlow({ intro, onIntroStart }: { intro: boolean; onIntroStart: (on: boolean) => void }) {
  const [unavailable, setUnavailable] = useState(false);
  const [data, setData] = useState<Loaded | null>(null);
  const [loadError, setLoadError] = useState<unknown>(null);
  const [step, setStep] = useState(1);
  const [dir, setDir] = useState<1 | -1>(1);
  const [busy, setBusy] = useState(false);
  const [stepError, setStepError] = useState<StepError | null>(null);
  const [profile, setProfile] = useState<ProfileResponse | null>(null);
  const [cal, setCal] = useState<CalendarSettings | null>(null);
  const [picked, setPicked] = useState<Set<string> | null>(null);
  const profileRef = useRef<ProfileFormHandle>(null);
  const profileWrap = useRef<HTMLDivElement>(null);
  const titleRef = useRef<HTMLHeadingElement>(null);
  const focusNext = useRef(false);
  const pushed = useRef(0); // history entries this page added, so Back never leaves setup
  // The popstate listener is installed once, so it reads the live values through refs.
  const stepRef = useRef(step);
  stepRef.current = step;
  const hasProfileRef = useRef(false);
  const done = useRef(false);
  const saves = useRef<Promise<unknown>>(Promise.resolve());
  const shown = useRef(0);

  /** Saves the step to resume at. Saves run one after another, in the order asked. */
  const saveStep = (s: number) => {
    const p = saves.current.catch(() => {}).then(() => api('onboardingSave', { body: { step: Math.min(Math.max(s, 1), TOTAL) } }));
    saves.current = p;
    return p;
  };

  const show = (s: number, from: number) => {
    setStepError(null);
    setDir(s >= from ? 1 : -1);
    focusNext.current = true;
    setStep(s);
  };

  const markProfileSaved = () => {
    hasProfileRef.current = true;
    setData((d) => (d ? { ...d, onboarding: { ...d.onboarding, hasProfile: true } } : d));
  };

  /** Saves the profile stage being left. Resolves false when the entries don't validate. */
  const saveProfileStage = async (): Promise<boolean> => {
    if (!SECTIONS[stepRef.current] || !profileRef.current) return true;
    setBusy(true);
    const ok = await profileRef.current.submit();
    setBusy(false);
    if (ok) markProfileSaved();
    return ok;
  };

  const load = () => {
    setLoadError(null);
    Promise.all([api('me'), api('profileGet'), api('prefsGet')]).then(
      ([me, p, prefs]) => {
        if (!me.onboarding) {
          location.replace('/');
          return;
        }
        setData({ user: me.user, onboarding: me.onboarding });
        hasProfileRef.current = me.onboarding.hasProfile;
        setProfile(p);
        setPicked(new Set(prefs.widgetLayout.map((w) => choiceOf(w.id)).filter((id): id is string => !!id)));
        // Resume at the saved step, never past the basics until they're saved. Google consent
        // returns to the calendar stage.
        const back = new URLSearchParams(location.search).get('google');
        const cap = me.onboarding.hasProfile ? TOTAL : 1;
        const s = Math.min(back ? 4 : me.onboarding.step, cap);
        setStep(s);
        history.replaceState({ step: s }, '', back && s === 4 ? location.pathname + location.search : location.pathname);
        // The intro is the first thing a person sees, not something to sit through again on resume.
        onIntroStart(s === 1 && !back);
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
      // After setup is done, Back leaves for Home instead of replaying the steps.
      if (done.current) {
        location.replace('/?tutorial=onboarding');
        return;
      }
      const raw = (e.state as { step?: number } | null)?.step;
      if (!raw || raw < 1 || raw > TOTAL) return;
      const from = stepRef.current;
      // Entries typed so far stay in the form, so moving between stages loses nothing.
      const to = hasProfileRef.current ? raw : 1;
      if (raw < from) pushed.current = Math.max(0, pushed.current - 1);
      else if (raw > from) pushed.current++;
      if (to !== raw) history.replaceState({ step: to }, '', location.pathname);
      if (to !== from) show(to, from);
      saveStep(to).catch(() =>
        setStepError({ text: 'We couldn’t save your place. If you leave now, setup resumes at an earlier step.', action: { label: 'Try again', run: () => void saveStep(to).then(() => setStepError(null), () => {}) } }),
      );
    };
    addEventListener('popstate', onPop);
    return () => removeEventListener('popstate', onPop);
  }, []);

  // Once the intro (and its inert background) is gone, focus lands on the stage heading.
  const wasIntro = useRef(false);
  useEffect(() => {
    if (intro) wasIntro.current = true;
    else if (wasIntro.current) {
      wasIntro.current = false;
      requestAnimationFrame(() => document.getElementById('step-title')?.focus({ preventScroll: true }));
    }
  }, [intro]);
  useEffect(() => {
    if (focusNext.current && !intro) {
      focusNext.current = false;
      requestAnimationFrame(() => titleRef.current?.focus());
    }
  }, [step, intro]);

  // The profile stages share one mounted form, so slide it in by hand when the stage changes.
  useEffect(() => {
    if (shown.current++ === 0 || reducedMotion()) return;
    profileWrap.current?.animate(
      [
        { opacity: 0, transform: `translateX(${dir * 28}px)` },
        { opacity: 1, transform: 'none' },
      ],
      { duration: 360, easing: 'cubic-bezier(.2, .8, .2, 1)' },
    );
  }, [step]);

  if (unavailable) return <Unavailable />;
  if (loadError) return <LoadError error={loadError} onRetry={load} />;
  if (!data || !profile || !picked)
    return (
      <div class="onb" aria-busy="true">
        <div class="onb-main onb-main--single">
          <section class="onb-step">
            <p class="brand-eyebrow">one moment</p>
            <h1 class="display display--l onb-title">Getting your space ready.</h1>
            <p role="status">Loading your setup…</p>
            <div class="skel skel--row" aria-hidden="true" />
          </section>
        </div>
      </div>
    );

  const { onboarding: ob } = data;
  const returning = ob.returning;

  /** Saves the new step, then shows it. A failed save keeps you here with a Retry. */
  const goTo = async (s: number) => {
    const from = stepRef.current;
    setStepError(null);
    setBusy(true);
    try {
      await saveStep(s);
    } catch (e) {
      setStepError({ text: e instanceof ApiCallError ? e.message : 'We couldn’t save your progress. Check your connection and try again.', action: { label: 'Try again', run: () => void goTo(s) } });
      return;
    } finally {
      setBusy(false);
    }
    show(s, from);
    if (s > from) {
      history.pushState({ step: s }, '', location.pathname);
      pushed.current++;
    } else history.replaceState({ step: s }, '', location.pathname);
  };

  const back = () => {
    if (pushed.current > 0) history.back(); // popstate shows the stage and saves the place
    else void goTo(step - 1);
  };

  /** Saves the place (and any profile edits) before signing out; a failed save keeps you signed in. */
  const finishLater = async (discardProfile = false) => {
    setStepError(null);
    if (!discardProfile && SECTIONS[step] && profileRef.current?.dirty() && !(await saveProfileStage())) {
      setStepError({ text: 'Your entries aren’t saved yet. Fix the highlighted fields to keep them, or leave without them.', action: { label: 'Leave without saving', run: () => void finishLater(true) } });
      return;
    }
    setBusy(true);
    try {
      await saveStep(hasProfileRef.current ? step : 1);
    } catch {
      setBusy(false);
      setStepError({ text: 'We couldn’t save your place, so you’re still signed in. Check your connection and try again.', action: { label: 'Try again', run: () => void finishLater(discardProfile) } });
      return;
    }
    markGreetingSignedOut(data.user.id);
    clearCachedRole();
    const f = document.createElement('form');
    f.method = 'post';
    f.action = '/auth/logout';
    document.body.append(f);
    f.submit();
  };

  const next = async () => {
    setStepError(null);
    if (SECTIONS[step]) {
      if (!(await saveProfileStage())) return;
      return goTo(step + 1);
    }
    if (step === TOTAL) {
      setBusy(true);
      try {
        const layout: WidgetPlacement[] = layoutForChoices(picked);
        await api('prefsPut', { body: { widgetLayout: layout, dashboardTutorialSeen: false } }); // the tour follows, once
        await api('onboardingComplete');
        done.current = true;
        // Setup is finished: the steps' history entries now lead Home (see onPop).
        history.replaceState({ done: true }, '', location.pathname);
        location.replace('/?tutorial=onboarding');
      } catch (e) {
        setStepError({ text: e instanceof ApiCallError ? e.message : 'We couldn’t save this step. Your choices are still here. Try again.', action: { label: 'Try again', run: () => void next() } });
      } finally {
        setBusy(false);
      }
      return;
    }
    return goTo(step + 1);
  };

  const googlePending = step === 4 && cal?.mode === 'google' && !cal.google.connected;
  const nextLabel = step === TOTAL ? 'Go to dashboard' : step === 4 && (googlePending || !cal?.activeEmail) ? 'Set up later' : 'Continue';

  const titles: preact.ComponentChildren[] = [
    returning ? (
      <>
        hey {firstName(ob.name)},<br />
        quick <span class="mark">refresh.</span>
      </>
    ) : (
      <>
        hey {firstName(ob.name)},<br />
        welcome <span class="mark">in.</span>
      </>
    ),
    <>a little about you.</>,
    <>your place on the team.</>,
    <>make room for CSEED.</>,
    <>your home, your way.</>,
  ];
  const ledes = [
    returning ? 'We’ve added a few things since you joined. Check what we have and fill in the gaps. It takes a minute.' : 'Start with how your team can reach you. Phone and birthday are required; everything else can wait.',
    'All optional. It helps officers get to know each other.',
    'Pick your team or committee, and add your title and graduation date if you like.',
    'Keep club events alongside everything else in your life. You can change this in Settings.',
    'Start with what’s useful. Add, remove and resize widgets on Home whenever you like.',
  ];

  let body: preact.ComponentChildren = null;
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

  const section = SECTIONS[step];
  const eyebrow = `${String(step).padStart(2, '0')} · ${NAMES[step - 1]!.toLowerCase()}`;
  return (
    <div class={`onb${intro ? ' onb--intro' : ''}`} data-step={step} data-intro={intro ? 'on' : 'off'}>
      <a class="skip-link" href="#step-title">
        Skip to setup
      </a>
      <header class="onb-top">
        <span class="onb-logo-slot">
          <ShellLogo />
        </span>
        {!intro && (
          <>
            <ol class="onb-progress" style={`--steps:${TOTAL}`} aria-label={`Setup progress, step ${step} of ${TOTAL}`}>
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
              0{step} / 0{TOTAL}
            </span>
          </>
        )}
      </header>
      {!intro && (
        <div class="onb-main onb-main--single">
          <section class={`onb-step is-entering${dir === -1 ? ' is-back' : ''}`} key={section ? 'profile' : step} aria-busy={busy || undefined}>
            <p class="brand-eyebrow" key={`e${step}`}>
              {eyebrow}
            </p>
            <h1 class="display display--xl onb-title" id="step-title" tabIndex={-1} ref={titleRef} key={`t${step}`}>
              {titles[step - 1]}
            </h1>
            <p class="brand-lede onb-lede" key={`l${step}`}>
              {ledes[step - 1]}
            </p>
            {section ? (
              <div ref={profileWrap} data-onb-profile>
                <ProfileForm data={profile} brand showSave={false} handle={profileRef} onSaved={setProfile} section={section} />
                {step === 3 && (
                  <p class="field-hint" data-start-term>
                    Start term: {ob.startTerm}. It’s set automatically when you join; a director can change it later.
                  </p>
                )}
              </div>
            ) : (
              body
            )}
            {stepError && (
              <div class="notice notice--negative" role="alert" data-step-error>
                {stepError.text}{' '}
                {stepError.action && (
                  <button class="btn btn--small" type="button" onClick={stepError.action.run} disabled={busy}>
                    {stepError.action.label}
                  </button>
                )}
              </div>
            )}
          </section>
        </div>
      )}
      {!intro && (
        <footer class="onb-foot">
          <button class="btn btn--text" type="button" onClick={back} disabled={step === 1 || busy} data-onb-back>
            ← Back
          </button>
          <button class="btn btn--text onb-skip" type="button" onClick={() => void finishLater()} disabled={busy} data-finish-later>
            Finish later
          </button>
          <button class="btn btn--primary" type="button" onClick={next} disabled={busy} data-onb-next>
            {busy ? 'Saving…' : `${nextLabel} →`}
          </button>
        </footer>
      )}
    </div>
  );
}

/** The welcome intro is the first step of the flow; it hands over to the intake without a cut. */
export default function OnboardingPage() {
  const [intro, setIntro] = useState(false);
  return (
    <>
      <OnboardingFlow intro={intro} onIntroStart={setIntro} />
      {intro && <WelcomeIntro onDone={() => setIntro(false)} />}
    </>
  );
}
