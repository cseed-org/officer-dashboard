import type { ComponentChildren } from 'preact';
/** A titled widget tile in the Home block grid. */
export default function Block({ id, title, action, children }: { id: string; title: string; action?: ComponentChildren; children: ComponentChildren }) {
  return (
    <section class="tile" aria-labelledby={`${id}-h`} data-block={id}>
      <header class="tile-head">
        <h2 class="tile-title" id={`${id}-h`}>{title}</h2>
        {action && <div class="tile-link">{action}</div>}
      </header>
      <div class="tile-body">{children}</div>
    </section>
  );
}

