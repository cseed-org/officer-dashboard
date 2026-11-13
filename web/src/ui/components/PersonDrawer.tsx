/**
 * PersonDrawer (V0.10): the one side drawer every people list opens: Board Members, Members,
 * External Contacts (and, later, Buildspace). It sits on Drawer (native modal <dialog>: focus
 * trapped, Esc and the backdrop close it, focus returns to the opener) and adds the profile
 * hero: a band in the person's colour, a large avatar, name, chips, quick actions and up to
 * three stats, then tabs (Overview / Activity / Manage, whichever the caller passes).
 *
 * The open person lives in the URL (`?person=<id>`, see usePersonParam), so a drawer can be
 * linked to, and Back / Forward close and reopen it.
 */
import type { ComponentChildren } from 'preact';
import { useEffect, useRef, useState } from 'preact/hooks';
import { personHue } from '../../lib/colors';
import { fmtDate } from '../../lib/format';
import { setParams, useSearch } from '../../lib/nav';
import type { PersonActivityItem } from '../../lib/contracts';
import { Drawer } from './Drawer';
import { Icon, type IconName } from './Icon';
import { Tabs } from './kit';
import { Avatar } from './people';
import { TimezonePendingNotice } from './TimezonePendingNotice';
import '../../styles/people.css';

// ─────────────────────────── URL state ───────────────────────────

const MARK = 'cseedPersonDrawer';

/**
 * The person open in this page's drawer, from `?person=<id>`. `open` pushes a history entry
 * (so Back closes the drawer); `close` steps back over that entry when this page pushed it, and
 * otherwise (a deep link) just drops the parameter. `legacy` names an older detail parameter
 * (`?id=`) that is rewritten to `?person=` on load, so old links still land on the drawer.
 */
export function usePersonParam(legacy?: string) {
  const search = useSearch();
  const old = legacy ? search.get(legacy) : null;
  useEffect(() => {
    if (old && !search.get('person')) setParams({ [legacy!]: null, person: old }, { replace: true });
  }, [old]);
  const personId = search.get('person') ?? (old || null);
  const open = (id: string) => {
    if (new URLSearchParams(location.search).get('person') === id) return;
    const replace = new URLSearchParams(location.search).has('person');
    setParams({ person: id }, { replace });
    if (!replace) history.replaceState({ ...(history.state ?? {}), [MARK]: true }, '', location.href);
  };
  const close = () => {
    if (!new URLSearchParams(location.search).has('person')) return; // Back already closed it
    if (history.state?.[MARK]) history.back();
    else setParams({ person: null }, { replace: true });
  };
  return { personId, open, close };
}

// ─────────────────────────── pieces ───────────────────────────

export interface PersonAction {
  label: string;
  icon?: IconName;
  href?: string;
  /** Opens in a new tab (LinkedIn, Discord). */
  external?: boolean;
  onClick?: () => void;
  primary?: boolean;
  /** Stable hook for tests. */
  data?: string;
}

export interface PersonStat {
  label: string;
  value: ComponentChildren;
}

export interface PersonTab<K extends string = string> {
  key: K;
  label: string;
  render: () => ComponentChildren;
}

/** Copies text and says so for a moment (Discord ids, emails). */
export function useCopied(): [string | null, (key: string, text: string) => void] {
  const [copied, setCopied] = useState<string | null>(null);
  useEffect(() => {
    if (!copied) return;
    const t = setTimeout(() => setCopied(null), 1500);
    return () => clearTimeout(t);
  }, [copied]);
  return [
    copied,
    (key, text) => {
      navigator.clipboard?.writeText(text).then(
        () => setCopied(key),
        () => {
          /* clipboard blocked: the value is still shown in Overview */
        },
      );
    },
  ];
}

/** A two-column definition list; empty values show a dash. */
export function PersonFacts({ facts }: { facts: [label: string, value: ComponentChildren, key?: string][] }) {
  return (
    <dl class="facts person-facts">
      {facts.map(([label, value, key]) => (
        <div key={key ?? label}>
          <dt>{label}</dt>
          <dd>{value === null || value === undefined || value === '' || value === false ? '—' : value}</dd>
        </div>
      ))}
    </dl>
  );
}

/** A titled block inside a tab. */
export function PersonSection({ title, children, data }: { title: string; children: ComponentChildren; data?: string }) {
  return (
    <section class="person-sec" data-person-section={data}>
      <h3>{title}</h3>
      {children}
    </section>
  );
}

const KIND_HUE = { event: 'green', design: 'violet', purchase: 'amber' } as const;
const label = (s: string) => s.replaceAll('-', ' ').replaceAll('_', ' ');
const itemText = (i: PersonActivityItem) =>
  i.kind === 'event'
    ? `${i.status === 'hosted' ? 'Hosted' : 'Hosting'} event`
    : i.kind === 'design'
      ? `${i.role === 'designing' ? 'Designing' : 'Requested design'} · ${label(i.status)}`
      : `Purchase request · ${label(i.status)}`;

/** Recent activity as a dotted timeline of links. */
export function ActivityList({ items, empty = 'No recent activity.' }: { items: PersonActivityItem[]; empty?: string }) {
  if (!items.length) return <p class="muted">{empty}</p>;
  return (
    <ul class="person-timeline" data-person-activity>
      {items.map((i) => (
        <li key={`${i.kind}-${i.id}`} data-hue={KIND_HUE[i.kind]}>
          <span class="person-timeline-dot" aria-hidden="true" />
          <span class="person-timeline-text">
            <a href={i.href}>{i.title}</a>
            <span class="muted small">{itemText(i)}</span>
          </span>
          <time class="muted small" dateTime={i.at}>
            {fmtDate(i.at)}
          </time>
        </li>
      ))}
    </ul>
  );
}

// ─────────────────────────── drawer ───────────────────────────

export function PersonDrawer<K extends string>({
  open,
  onClose,
  id = 'person',
  personKey,
  name,
  avatarUrl,
  eyebrow,
  subtitle,
  chips,
  actions = [],
  stats = [],
  tabs,
  status,
  dismissible = true,
}: {
  open: boolean;
  onClose: () => void;
  id?: string;
  /** Stable key for the person (their id): picks the colour and resets the tab on change. */
  personKey: string;
  name: string;
  avatarUrl?: string | null;
  /** Small label above the name: "Officer", "Member", "External contact". */
  eyebrow?: string;
  subtitle?: ComponentChildren;
  chips?: ComponentChildren;
  actions?: PersonAction[];
  stats?: PersonStat[];
  tabs: PersonTab<K>[];
  /** Replaces the tabs while the person loads or fails to (Loading / ErrorState). */
  status?: ComponentChildren;
  dismissible?: boolean;
}) {
  const [tab, setTab] = useState<K | null>(null);
  const body = useRef<HTMLDivElement>(null);
  useEffect(() => setTab(null), [personKey]);
  const active = tabs.find((t) => t.key === tab) ?? tabs[0];
  const change = (k: K) => {
    setTab(k);
    body.current?.scrollTo?.({ top: 0 });
  };
  const close = (e: Event) => (e.currentTarget as HTMLElement).closest('dialog')?.close();

  return (
    <Drawer open={open} onClose={onClose} id={id} title={name} bare className="person-drawer" dismissible={dismissible}>
      <div class="person-scroll" ref={body} data-person={personKey}>
        <div class="person-band" data-hue={personHue(personKey)} aria-hidden="true" />
        <button type="button" class="icon-btn person-close" aria-label="Close" disabled={!dismissible} onClick={close}>
          <Icon name="close" />
        </button>
        <header class="person-hero">
          <Avatar name={name} id={personKey} src={avatarUrl} size="xl" />
          <div class="person-id">
            {eyebrow && <span class="eyebrow">{eyebrow}</span>}
            <h2 id={`${id}-title`} tabIndex={-1}>
              {name}
            </h2>
            {subtitle && <p class="muted person-sub">{subtitle}</p>}
          </div>
          {chips && <div class="person-chips">{chips}</div>}
          {actions.length > 0 && (
            <div class="person-actions">
              {actions.map((a) =>
                a.href ? (
                  <a
                    key={a.label}
                    class={`btn${a.primary ? ' btn--primary' : ''}`}
                    href={a.href}
                    data-person-action={a.data}
                    {...(a.external ? { target: '_blank', rel: 'noopener noreferrer' } : {})}
                  >
                    {a.icon && <Icon name={a.icon} />}
                    {a.label}
                  </a>
                ) : (
                  <button key={a.label} type="button" class={`btn${a.primary ? ' btn--primary' : ''}`} data-person-action={a.data} onClick={a.onClick}>
                    {a.icon && <Icon name={a.icon} />}
                    {a.label}
                  </button>
                ),
              )}
            </div>
          )}
        </header>
        <TimezonePendingNotice />
        {status ?? (
          <>
            {tabs.length > 1 && active && (
              <div class="person-tabs">
                <Tabs label={`${name}: sections`} idPrefix={`${id}-t`} active={active.key} tabs={tabs.map((t) => ({ key: t.key, label: t.label }))} onChange={change} />
              </div>
            )}
            <div class="person-body" {...(tabs.length > 1 ? { id: `${id}-t-panel`, role: 'tabpanel', 'aria-labelledby': `${id}-t-tab-${active?.key}` } : {})}>
              {stats.length > 0 && active === tabs[0] && (
                <dl class="person-stats" data-person-stats>
                  {stats.map((s) => (
                    <div key={s.label}>
                      <dt>{s.label}</dt>
                      <dd>{s.value}</dd>
                    </div>
                  ))}
                </dl>
              )}
              {active?.render()}
            </div>
          </>
        )}
      </div>
    </Drawer>
  );
}
