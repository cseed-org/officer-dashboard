import { useImperativeHandle, useRef, useState } from 'preact/hooks';
import type { Ref } from 'preact';
import { api, ApiCallError, fieldErrors } from '../../lib/api/client';
import type { MyProfile, ProfileResponse, ProfileUpsertRequest } from '../../lib/contracts';
import { isGradMonth, isLinkedIn, PROFILE_MAX } from '../../lib/profileRules';
import { OFFICER_TEAMS } from '../../lib/officerTeams';

const MONTHS = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'];
const DAYS_IN = [31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
const pad = (n: number) => String(n).padStart(2, '0');

type Values = Record<
  'phone' | 'month' | 'day' | 'preferredName' | 'pronouns' | 'personalEmail' | 'schoolEmail' | 'gradDate' | 'title' | 'team' | 'linkedin' | 'subteamId',
  string
>;

function initial(p: MyProfile | null): Values {
  const [m, d] = (p?.birthday ?? '').split('-');
  return {
    phone: p?.phone ?? '',
    month: m ?? '',
    day: d ?? '',
    preferredName: p?.preferredName ?? '',
    pronouns: p?.pronouns ?? '',
    personalEmail: p?.personalEmail ?? '',
    schoolEmail: p?.schoolEmail ?? '',
    gradDate: p?.gradDate ?? '',
    title: p?.title ?? '',
    team: p?.team ?? '',
    linkedin: p?.linkedin ?? '',
    subteamId: p?.subteam?.id ?? '',
  };
}

const OPTIONAL = ['preferredName', 'pronouns', 'personalEmail', 'schoolEmail', 'gradDate', 'title', 'team', 'linkedin'] as const;

/** The intake form shows the profile in three stages; Settings shows all of it (no section). */
export type ProfileSection = 'basics' | 'about' | 'role';
const SECTION_FIELDS: Record<ProfileSection, string[]> = {
  basics: ['phone', 'birthday', 'subteamId'],
  about: ['preferredName', 'pronouns', 'personalEmail', 'schoolEmail'],
  role: ['team', 'title', 'gradDate', 'linkedin'],
};

const MAX: Partial<Record<keyof Values, number>> = PROFILE_MAX;

/** Client checks with the same copy as the server, so errors read the same either way. */
function check(v: Values, needsSubteam: boolean, section?: ProfileSection): Record<string, string> {
  const e: Record<string, string> = {};
  const digits = v.phone.replace(/\D/g, '').length;
  if (!v.phone.trim()) e.phone = 'Enter your phone number';
  else if (!/^[+0-9 ().-]+$/.test(v.phone.trim()) || digits < 7 || digits > 15) e.phone = 'Enter a phone number with at least 7 digits';
  if (!v.month || !v.day) e.birthday = 'Choose the month and day of your birthday';
  else if (+v.day > DAYS_IN[+v.month - 1]!) e.birthday = `${MONTHS[+v.month - 1]} has only ${DAYS_IN[+v.month - 1]} days`;
  const email = /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/;
  if (v.personalEmail && !email.test(v.personalEmail.trim())) e.personalEmail = 'Enter a valid email, like you@example.org';
  if (v.schoolEmail && !email.test(v.schoolEmail.trim())) e.schoolEmail = 'Enter a valid email, like you@school.edu';
  if (v.gradDate.trim() && !isGradMonth(v.gradDate.trim())) e.gradDate = 'Enter a month, like 2028-05';
  if (v.linkedin.trim() && !isLinkedIn(v.linkedin.trim())) e.linkedin = 'Use your https://linkedin.com/in/… address';
  if (needsSubteam && !v.subteamId) e.subteamId = 'Choose your subteam';
  if (!section) return e;
  // A stage only reports its own fields; earlier stages were saved when they were left.
  return Object.fromEntries(Object.entries(e).filter(([k]) => SECTION_FIELDS[section].includes(k)));
}

export interface ProfileFormHandle {
  /** Validates and saves. Resolves true on success. */
  submit: () => Promise<boolean>;
  /** True when the entries differ from what was last saved. */
  dirty: () => boolean;
}

/**
 * Required: phone and birthday (month and day only). Optional fields live in a disclosure
 * that opens itself when one of them is invalid. Errors are inline under each field.
 */
export function ProfileForm({
  data,
  onSaved,
  brand = false,
  handle,
  showSave = true,
  section,
}: {
  data: ProfileResponse;
  onSaved?: (r: ProfileResponse) => void;
  brand?: boolean;
  handle?: Ref<ProfileFormHandle>;
  showSave?: boolean;
  /** Intake only: show one stage of the form. The other stages stay mounted, so nothing typed is lost. */
  section?: ProfileSection;
}) {
  const [v, setV] = useState<Values>(() => initial(data.profile));
  const savedValues = useRef<Values>(initial(data.profile));
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const [moreOpen, setMoreOpen] = useState(() => OPTIONAL.some((k) => !!initial(data.profile)[k]));
  const form = useRef<HTMLFormElement>(null);
  const needsSubteam = !data.subteamLocked;
  const subteamName = data.profile?.subteam?.name ?? data.defaultSubteam?.name ?? null;

  const set = (k: keyof Values) => (e: Event) => {
    const val = (e.target as HTMLInputElement).value;
    setV((o) => ({ ...o, [k]: val }));
    setSaved(false);
    const ek = k === 'month' || k === 'day' ? 'birthday' : k;
    if (errors[ek]) setErrors(({ [ek]: _, ...rest }) => rest);
  };

  const focusFirst = (errs: Record<string, string>) => {
    const order = ['phone', 'birthday', 'subteamId', ...OPTIONAL];
    const first = order.find((k) => errs[k]);
    if (!first) return;
    if ((OPTIONAL as readonly string[]).includes(first)) setMoreOpen(true);
    requestAnimationFrame(() => form.current?.querySelector<HTMLElement>(`#pf-${first === 'birthday' ? 'month' : first}`)?.focus());
  };

  const submit = async (): Promise<boolean> => {
    const errs = check(v, needsSubteam, section);
    setErrors(errs);
    setFormError(null);
    if (Object.keys(errs).length) {
      focusFirst(errs);
      return false;
    }
    const opt = (s: string) => (s.trim() ? s.trim() : null);
    const body: ProfileUpsertRequest = {
      phone: v.phone.trim(),
      birthday: `${pad(+v.month)}-${pad(+v.day)}`,
      preferredName: opt(v.preferredName),
      pronouns: opt(v.pronouns),
      personalEmail: opt(v.personalEmail),
      schoolEmail: opt(v.schoolEmail),
      gradDate: opt(v.gradDate),
      title: opt(v.title),
      team: opt(v.team),
      linkedin: opt(v.linkedin),
      ...(needsSubteam ? { subteamId: v.subteamId } : {}),
    };
    setBusy(true);
    try {
      const r = await api('profilePut', { body });
      savedValues.current = v;
      setSaved(true);
      onSaved?.(r);
      return true;
    } catch (e) {
      const fe = fieldErrors(e);
      if (Object.keys(fe).length) {
        setErrors(fe);
        focusFirst(fe);
      } else setFormError(e instanceof ApiCallError ? e.message : 'We couldn’t save your profile. Your entries are still here. Try again.');
      return false;
    } finally {
      setBusy(false);
    }
  };
  const dirty = () => (Object.keys(v) as (keyof Values)[]).some((k) => v[k].trim() !== savedValues.current[k].trim());
  useImperativeHandle(handle as Ref<ProfileFormHandle>, () => ({ submit, dirty }), [v, needsSubteam, section]);

  const desc = (id: string, hint = false) => {
    const ids = [hint && `pf-${id}-hint`, errors[id] && `pf-${id}-error`].filter(Boolean).join(' ');
    return { 'aria-invalid': errors[id] ? true : undefined, 'aria-describedby': ids || undefined };
  };
  const err = (id: string) =>
    errors[id] ? (
      <p class="field-error" id={`pf-${id}-error`}>
        {errors[id]}
      </p>
    ) : null;
  const text = (id: keyof Values, label: string, type = 'text', auto = 'off') => (
    <div class="field">
      <label class="field-label" for={`pf-${id}`}>
        {label}
      </label>
      <input class="input" id={`pf-${id}`} type={type as "text"} autocomplete={auto} maxLength={MAX[id]} value={v[id]} onInput={set(id)} {...desc(id)} />
      {err(id)}
    </div>
  );
  const errorCount = Object.keys(errors).length;
  const maxDay = v.month ? DAYS_IN[+v.month - 1]! : 31;

  const aboutFields = (
    <>
      {text('preferredName', 'Preferred name', 'text', 'nickname')}
      {text('pronouns', 'Pronouns')}
      {text('personalEmail', 'Personal email', 'email', 'email')}
      {text('schoolEmail', 'School email', 'email', 'email')}
    </>
  );
  const teams: string[] = [...OFFICER_TEAMS];
  if (v.team && !teams.includes(v.team)) teams.unshift(v.team);
  const roleFields = (
    <>
      <div class="field">
        <label class="field-label" for="pf-team">
          Team or committee
        </label>
        <select class="select" id="pf-team" value={v.team} onChange={set('team')} {...desc('team')}>
          <option value="">Choose one</option>
          {teams.map((t) => (
            <option key={t} value={t}>
              {t}
            </option>
          ))}
        </select>
        {err('team')}
      </div>
      {text('title', 'Title', 'text', 'organization-title')}
      <div class="field">
        <label class="field-label" for="pf-gradDate">
          Graduation date
        </label>
        <input class="input" id="pf-gradDate" type="month" placeholder="2028-05" maxLength={7} value={v.gradDate} onInput={set('gradDate')} {...desc('gradDate', true)} />
        <p class="field-hint" id="pf-gradDate-hint">
          Month and year, like 2028-05.
        </p>
        {err('gradDate')}
      </div>
      {text('linkedin', 'LinkedIn', 'url', 'url')}
    </>
  );

  return (
    <form
      ref={form}
      class={brand ? 'onb-form' : 'stack'}
      noValidate
      data-profile-form
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
    >
      {errorCount >= 2 && (
        <p class="notice notice--negative" role="alert">
          {errorCount} fields need attention.
        </p>
      )}
      <div class="grid-fields" hidden={!!section && section !== 'basics'}>
        <div class="field">
          <label class="field-label" for="pf-phone">
            Phone · required
          </label>
          <input class="input" id="pf-phone" type="tel" autocomplete="tel" maxLength={MAX.phone} value={v.phone} onInput={set('phone')} required {...desc('phone', true)} />
          <p class="field-hint" id="pf-phone-hint">
            Visible to directors and leadership only (including owners).
          </p>
          {err('phone')}
        </div>
        <fieldset class="field" style="border:0;padding:0;margin:0;min-width:0">
          <legend class="field-label">Birthday · required</legend>
          <div style="display:grid;grid-template-columns:minmax(0,1.4fr) minmax(0,1fr);gap:8px">
            <label class="visually-hidden" for="pf-month">
              Birthday month
            </label>
            <select class="select" id="pf-month" autocomplete="bday-month" value={v.month} onChange={set('month')} {...desc('birthday', true)}>
              <option value="">Month</option>
              {MONTHS.map((m, i) => (
                <option key={m} value={pad(i + 1)}>
                  {m}
                </option>
              ))}
            </select>
            <label class="visually-hidden" for="pf-day">
              Birthday day
            </label>
            <select class="select" id="pf-day" autocomplete="bday-day" value={v.day} onChange={set('day')} {...desc('birthday', true)}>
              <option value="">Day</option>
              {Array.from({ length: maxDay }, (_, i) => (
                <option key={i} value={pad(i + 1)}>
                  {i + 1}
                </option>
              ))}
            </select>
          </div>
          <p class="field-hint" id="pf-birthday-hint">
            Visible to all officers. Year not collected.
          </p>
          {err('birthday')}
        </fieldset>
        {needsSubteam && (
          <div class="field">
            <label class="field-label" for="pf-subteamId">
              Subteam · required
            </label>
            <select class="select" id="pf-subteamId" value={v.subteamId} onChange={set('subteamId')} {...desc('subteamId')}>
              <option value="">Choose a subteam</option>
              {data.subteams.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </select>
            {err('subteamId')}
          </div>
        )}
      </div>
      {section ? (
        <>
          <div class="grid-fields" hidden={section !== 'about'}>
            {aboutFields}
          </div>
          <div class="grid-fields" hidden={section !== 'role'}>
            {roleFields}
          </div>
        </>
      ) : (
        <details open={moreOpen} onToggle={(e) => setMoreOpen((e.target as HTMLDetailsElement).open)}>
          <summary>More about you · optional</summary>
          <div class="grid-fields" style="margin-top:12px">
            {aboutFields}
            {roleFields}
          </div>
          {data.profile?.startTerm && (
            <p class="field-hint" data-start-term>
              Start term: {data.profile.startTerm}. Set automatically when you joined; a director can change it.
            </p>
          )}
        </details>
      )}
      <p class="field-hint" hidden={!!section && section !== 'basics'}>
        {subteamName ? `Name, Discord and subteam (${subteamName}) come from your invitation or a director.` : 'Name and Discord come from your account.'}
      </p>
      {formError && (
        <p class="notice notice--negative" role="alert">
          {formError}
        </p>
      )}
      {showSave && (
        <div class="action-row">
          <button class="btn btn--primary" type="submit" disabled={busy}>
            {busy ? 'Saving…' : 'Save profile'}
          </button>
          <span class="small muted" role="status">
            {saved ? 'Saved ✓' : ''}
          </span>
        </div>
      )}
    </form>
  );
}
