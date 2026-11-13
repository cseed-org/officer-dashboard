/**
 * V0.10 people tables: one rounded card per page, groups with a heading and a collapse toggle,
 * a sticky column header per group, rows that open the PersonDrawer. Phones show each row as
 * avatar + name + chip (people.css). Shared by Board Members, Members and External Contacts.
 */
import type { ComponentChildren } from 'preact';
import { useState } from 'preact/hooks';
import type { Hue } from '../../lib/colors';
import { Icon } from './Icon';
import { Avatar } from './people';
import '../../styles/people.css';

export type SortDir = 'asc' | 'desc';
export interface Sort<K extends string> {
  key: K;
  dir: SortDir;
}
export interface Column<K extends string = string> {
  label: string;
  /** Sortable columns pass their key. */
  sort?: K;
  width?: string;
}

/** Toggle a column's sort: off → ascending → descending → off (back to the server order). */
export function nextSort<K extends string>(cur: Sort<K> | null, key: K): Sort<K> | null {
  if (!cur || cur.key !== key) return { key, dir: 'asc' };
  return cur.dir === 'asc' ? { key, dir: 'desc' } : null;
}

export function sortBy<T, K extends string>(rows: T[], sort: Sort<K> | null, value: (row: T, key: K) => string | number | null | undefined): T[] {
  if (!sort) return rows;
  const mul = sort.dir === 'asc' ? 1 : -1;
  return [...rows].sort((a, b) => {
    const x = value(a, sort.key);
    const y = value(b, sort.key);
    if (x === y) return 0;
    if (x === null || x === undefined || x === '') return 1; // blanks last either way
    if (y === null || y === undefined || y === '') return -1;
    return (typeof x === 'number' && typeof y === 'number' ? x - y : String(x).localeCompare(String(y), undefined, { sensitivity: 'base', numeric: true })) * mul;
  });
}

/** A rounded card holding a page's toolbar, groups and footer. */
export function PeopleCard({ children, label, busy, data }: { children: ComponentChildren; label: string; busy?: boolean; data?: string }) {
  return (
    <section class="people-card" aria-label={label} aria-busy={busy || undefined} data-people-card={data}>
      {children}
    </section>
  );
}

/** Search input with a leading icon, for the card toolbar. */
export function PeopleSearch({ label, value, placeholder, onInput, data }: { label: string; value: string; placeholder: string; onInput: (v: string) => void; data?: Record<string, string> }) {
  return (
    <label class="field people-search">
      <span class="field-label">{label}</span>
      <input class="input" type="search" placeholder={placeholder} value={value} onInput={(e) => onInput(e.currentTarget.value)} {...data} />
      <Icon name="search" size={16} />
    </label>
  );
}

/** Single-choice filter pills (subteams). */
export function PeoplePills<K extends string>({ label, value, options, onChange }: { label: string; value: K; options: { key: K; label: string }[]; onChange: (k: K) => void }) {
  return (
    <div class="people-pills" role="group" aria-label={label}>
      {options.map((o) => (
        <button key={o.key} type="button" class="people-pill" aria-pressed={value === o.key} onClick={() => onChange(o.key)}>
          {o.label}
        </button>
      ))}
    </div>
  );
}

/** A tinted chip with a dot (role, contact type, status). */
export function PeopleChip({ hue, children, ghost, data }: { hue?: Hue | null; children: ComponentChildren; ghost?: boolean; data?: Record<string, string> }) {
  return (
    <span class={`people-chip${ghost ? ' people-chip--ghost' : ''}`} data-hue={hue ?? undefined} {...data}>
      {children}
    </span>
  );
}

/** Avatar, a name button that opens the drawer, and one muted line under it. */
export function PersonCell({ id, name, avatarUrl, sub, onOpen, href }: { id: string; name: string; avatarUrl?: string | null; sub?: ComponentChildren; onOpen?: () => void; href?: string }) {
  return (
    <span class="person-cell">
      <Avatar name={name} id={id} src={avatarUrl} />
      <span class="person-cell-text">
        {href ? (
          <a class="person-name" href={href}>
            {name}
          </a>
        ) : (
          <button type="button" class="person-name" onClick={onOpen}>
            {name}
          </button>
        )}
        {sub && <span class="person-sub">{sub}</span>}
      </span>
    </span>
  );
}

/** Email / Discord / LinkedIn as small icon links; each is named by what it opens. */
export function ContactLinks({ email, discordId, linkedin }: { email?: string | null; discordId?: string | null; linkedin?: string | null }) {
  if (!email && !discordId && !linkedin) return <span class="muted">—</span>;
  return (
    <span class="people-contact">
      {email && (
        <a href={`mailto:${email}`} aria-label={email} title={email}>
          <Icon name="mail" size={16} />
        </a>
      )}
      {discordId && (
        <a href={`https://discord.com/users/${encodeURIComponent(discordId)}`} target="_blank" rel="noopener noreferrer" aria-label="Discord profile" title="Discord profile">
          <Icon name="message" size={16} />
        </a>
      )}
      {linkedin && (
        <a href={linkedin} target="_blank" rel="noopener noreferrer" aria-label="LinkedIn profile" title="LinkedIn profile">
          <Icon name="link" size={16} />
        </a>
      )}
    </span>
  );
}

/**
 * A row that opens its person on click anywhere outside its own links and buttons (those keep
 * working, and the name button is the keyboard path).
 */
export function PersonRow({ onOpen, children, data }: { onOpen?: () => void; children: ComponentChildren; data?: Record<string, string> }) {
  return (
    <tr
      {...data}
      onClick={(e) => {
        if (!onOpen || (e.target as Element).closest('a, button, input, select, label')) return;
        onOpen();
      }}
    >
      {children}
    </tr>
  );
}

/** One group: heading with count and collapse toggle, then a table with a sticky header. */
export function PeopleGroup<K extends string>({
  section,
  title,
  count,
  extra,
  caption,
  columns,
  sort,
  onSort,
  empty,
  children,
  collapsible = true,
}: {
  section: string;
  title: string;
  count?: number;
  extra?: ComponentChildren;
  caption?: string;
  columns: Column<K>[];
  sort?: Sort<K> | null;
  onSort?: (s: Sort<K> | null) => void;
  /** Shown instead of the table when there are no rows. */
  empty?: ComponentChildren;
  children: ComponentChildren;
  collapsible?: boolean;
}) {
  const [open, setOpen] = useState(true);
  const headId = `people-${section}`;
  return (
    <section class="people-group" data-section={section} aria-labelledby={headId}>
      <div class="people-group-head">
        {collapsible && (
          <button type="button" class="people-collapse" aria-expanded={open} aria-controls={`${headId}-rows`} aria-label={`${open ? 'Collapse' : 'Expand'} ${title}`} onClick={() => setOpen(!open)}>
            <Icon name="chevron-down" size={14} />
          </button>
        )}
        <h2 id={headId}>{title}</h2>
        {count !== undefined && <span class="people-count">{count}</span>}
        {extra && <span class="people-group-extra">{extra}</span>}
      </div>
      <div id={`${headId}-rows`} hidden={!open}>
        {empty ?? (
          <table class="people-table">
            <caption class="visually-hidden">{caption ?? title}</caption>
            <colgroup>
              {columns.map((c) => (
                <col key={c.label} style={c.width ? { width: c.width } : undefined} />
              ))}
            </colgroup>
            <thead>
              <tr>
                {columns.map((c) => {
                  const dir = c.sort && sort?.key === c.sort ? sort.dir : undefined;
                  return (
                    <th key={c.label} scope="col" aria-sort={c.sort ? (dir === 'asc' ? 'ascending' : dir === 'desc' ? 'descending' : 'none') : undefined}>
                      {c.sort && onSort ? (
                        <button type="button" class="people-sort" data-dir={dir} onClick={() => onSort(nextSort(sort ?? null, c.sort!))}>
                          {c.label}
                          <Icon name={dir === 'asc' ? 'chevron-down' : dir === 'desc' ? 'chevron-down' : 'chevrons-up-down'} size={12} />
                        </button>
                      ) : c.label ? (
                        c.label
                      ) : (
                        <span class="visually-hidden">Actions</span>
                      )}
                    </th>
                  );
                })}
              </tr>
            </thead>
            <tbody>{children}</tbody>
          </table>
        )}
      </div>
    </section>
  );
}
