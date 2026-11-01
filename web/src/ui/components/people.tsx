import type { ComponentChildren } from 'preact';
import { useEffect, useId, useRef, useState } from 'preact/hooks';
import { personHue, type Hue } from '../../lib/colors';
import { initials } from '../../lib/session';
import { safeImageUrl } from '../../lib/url';

/** Initials (or the Discord picture) on a stable per-person tint. */
export function Avatar({ name, id, src, size = 'md' }: { name: string; id?: string | null; src?: string | null; size?: 'sm' | 'md' }) {
  const img = safeImageUrl(src);
  return (
    <span class={`avatar avatar--${size}`} data-hue={personHue(id ?? name)} aria-hidden="true">
      {img ? <img src={img} alt="" /> : initials(name)}
    </span>
  );
}

/** A coloured dot plus a label (role, scope): text stays in text colour. */
export function DotLabel({ hue, children }: { hue: Hue; children: ComponentChildren }) {
  return (
    <span class="dot-label" data-hue={hue}>
      {children}
    </span>
  );
}

/** A secondary identifier (Discord id, token prefix) that copies itself on click. */
export function CopyText({ text, label }: { text: string; label: string }) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const t = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(t);
  }, [copied]);
  return (
    <button
      type="button"
      class="copy-text"
      title={`Copy ${label}`}
      aria-label={`${label} ${text}, copy`}
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(text);
          setCopied(true);
        } catch {
          /* clipboard blocked: the id is still selectable */
        }
      }}
    >
      <span class="mono">{text}</span>
      <span class="copy-text-state" aria-live="polite">
        {copied ? 'copied' : ''}
      </span>
    </button>
  );
}

export interface MenuItem {
  label: string;
  onSelect: () => void;
  danger?: boolean;
  disabled?: boolean;
}

/**
 * Row "⋯" menu: a button that opens a small menu (role=menu). Arrow keys move, Enter or
 * Space picks, Escape or a click outside closes and returns focus to the button.
 */
export function RowMenu({ label, items }: { label: string; items: MenuItem[] }) {
  const [open, setOpen] = useState(false);
  const btn = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const id = useId();
  const enabled = items.filter((i) => !i.disabled);

  useEffect(() => {
    if (!open) return;
    const first = menu.current?.querySelector<HTMLElement>('[role="menuitem"]:not([aria-disabled="true"])');
    first?.focus();
    const onDoc = (e: MouseEvent) => {
      if (!menu.current?.contains(e.target as Node) && !btn.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', onDoc);
    return () => document.removeEventListener('mousedown', onDoc);
  }, [open]);

  if (!enabled.length) return null;
  const close = (refocus = true) => {
    setOpen(false);
    if (refocus) btn.current?.focus();
  };
  const onKey = (e: KeyboardEvent) => {
    const els = [...(menu.current?.querySelectorAll<HTMLElement>('[role="menuitem"]:not([aria-disabled="true"])') ?? [])];
    const i = els.indexOf(document.activeElement as HTMLElement);
    if (e.key === 'Escape' || e.key === 'Tab') {
      if (e.key === 'Escape') e.preventDefault();
      close(e.key === 'Escape');
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      els[(i + 1) % els.length]?.focus();
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      els[(i - 1 + els.length) % els.length]?.focus();
    }
  };

  return (
    <span class="row-menu">
      <button
        ref={btn}
        type="button"
        class="icon-btn row-menu-btn"
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={() => setOpen((o) => !o)}
        onKeyDown={(e) => {
          if (e.key === 'ArrowDown' && !open) {
            e.preventDefault();
            setOpen(true);
          }
        }}
      >
        <span aria-hidden="true">⋯</span>
      </button>
      {open && (
        <div class="row-menu-list" role="menu" id={id} aria-label={label} ref={menu} onKeyDown={onKey}>
          {items.map((it) => (
            <button
              type="button"
              role="menuitem"
              key={it.label}
              class={`row-menu-item${it.danger ? ' is-danger' : ''}`}
              aria-disabled={it.disabled ? 'true' : undefined}
              tabIndex={-1}
              onClick={() => {
                if (it.disabled) return;
                close();
                it.onSelect();
              }}
            >
              {it.label}
            </button>
          ))}
        </div>
      )}
    </span>
  );
}

/** Small two-way toggle (officer / director) with a dot per option. */
export function RoleToggle<K extends string>({
  label,
  value,
  options,
  disabled,
  onChange,
}: {
  label: string;
  value: K;
  options: { key: K; label: string; hue: Hue }[];
  disabled?: boolean;
  onChange: (k: K) => void;
}) {
  return (
    <span class="role-toggle" role="group" aria-label={label}>
      {options.map((o) => (
        <button type="button" key={o.key} class="role-toggle-opt" data-hue={o.hue} aria-pressed={value === o.key} disabled={disabled} onClick={() => value !== o.key && onChange(o.key)}>
          {o.label}
        </button>
      ))}
    </span>
  );
}
