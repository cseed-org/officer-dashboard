import type { ComponentChildren } from 'preact';
import { useEffect, useRef } from 'preact/hooks';
import { Icon } from './Icon';

/**
 * Slide-over panel on a native modal <dialog>: focus is trapped while open, Escape and the
 * backdrop close it, the heading takes focus on open and focus returns to the opener.
 */
export function Drawer({
  open,
  onClose,
  title,
  eyebrow,
  id,
  size = 'md',
  kind = 'drawer',
  dismissible = true,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title: ComponentChildren;
  eyebrow?: ComponentChildren;
  id: string;
  size?: 'md' | 'lg';
  /** `dialog` centres it (CSV import); `drawer` slides in from the right. */
  kind?: 'drawer' | 'dialog';
  dismissible?: boolean;
  children: ComponentChildren;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const opener = useRef<HTMLElement | null>(null);
  const closeRef = useRef(onClose);
  closeRef.current = onClose;

  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) {
      opener.current = document.activeElement instanceof HTMLElement && document.activeElement !== document.body ? document.activeElement : null;
      d.showModal();
      document.documentElement.classList.add('has-modal');
      requestAnimationFrame(() => d.querySelector<HTMLElement>(`#${id}-title`)?.focus());
    } else if (!open && d.open) {
      d.close();
    }
  }, [open, id]);

  useEffect(() => () => document.documentElement.classList.remove('has-modal'), []);

  return (
    <dialog
      ref={ref}
      id={id}
      class={`${kind} ${kind}--${size}`}
      aria-labelledby={`${id}-title`}
      onClose={() => {
        if (!document.querySelector('dialog[open]')) document.documentElement.classList.remove('has-modal');
        if (opener.current?.isConnected) opener.current.focus();
        opener.current = null;
        closeRef.current();
      }}
      onCancel={(e) => { if (!dismissible) e.preventDefault(); }}
      onClick={(e) => {
        if (dismissible && e.target === ref.current) ref.current?.close();
      }}
    >
      <div class={`${kind}-inner`}>
        <header class={`${kind}-head`}>
          <div class={`${kind}-heading`}>
            {eyebrow && <span class="eyebrow">{eyebrow}</span>}
            <h2 id={`${id}-title`} tabIndex={-1}>
              {title}
            </h2>
          </div>
          <button type="button" class="icon-btn" aria-label="Close" disabled={!dismissible} onClick={() => ref.current?.close()}>
            <Icon name="close" />
          </button>
        </header>
        <div class={`${kind}-body`}>{open ? children : null}</div>
      </div>
    </dialog>
  );
}

