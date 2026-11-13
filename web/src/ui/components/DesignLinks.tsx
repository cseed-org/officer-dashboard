import { useEffect, useId, useState } from 'preact/hooks';
import { designTool, DESIGN_TOOL_LABEL, type DesignLinkSet } from '../../lib/design';
import { safeUrl } from '../../lib/url';
import { Icon, type IconName } from './Icon';

const TOOL_ICON = { figma: 'pen', canva: 'palette', file: 'pen' } as const satisfies Record<string, IconName>;

/**
 * The two design-file buttons on a request: its working file (Figma or Canva, picked from the
 * URL) and its shared folder. Nothing stores these yet, so today every request shows the
 * disabled "Not linked yet" state; pass `links` once an agent fills them in.
 *
 * `compact` is the icon pair on board cards; `full` is the labelled pair in the request detail.
 */
export function DesignLinks({ links, variant = 'compact', title }: { links: DesignLinkSet; variant?: 'compact' | 'full'; title?: string }) {
  const tool = designTool(links.design);
  const of = title ? ` for ${title}` : '';
  return (
    <span class={`dlinks dlinks--${variant}`} data-design-links>
      <LinkButton
        kind={tool}
        url={links.design}
        icon={TOOL_ICON[tool]}
        label={DESIGN_TOOL_LABEL[tool]}
        openLabel={`Open in ${DESIGN_TOOL_LABEL[tool]}`}
        a11y={links.design ? `Open ${tool === 'file' ? 'the design file' : `in ${DESIGN_TOOL_LABEL[tool]}`}${of}` : `Design file${of}: not linked yet`}
        variant={variant}
      />
      <LinkButton kind="folder" url={links.folder} icon="folder" label="Folder" openLabel="Open folder" a11y={links.folder ? `Open the folder${of}` : `Folder${of}: not linked yet`} variant={variant} />
    </span>
  );
}

function LinkButton({ kind, url, icon, label, openLabel, a11y, variant }: { kind: string; url?: string | null; icon: IconName; label: string; openLabel: string; a11y: string; variant: 'compact' | 'full' }) {
  const id = useId();
  const href = url ? safeUrl(url) : null;
  // Touch has no hover: a tap on a disabled button shows its tooltip for a moment.
  const [shown, setShown] = useState(false);
  useEffect(() => {
    if (!shown) return;
    const t = setTimeout(() => setShown(false), 1800);
    return () => clearTimeout(t);
  }, [shown]);
  const tip = href ? `${openLabel} ↗` : `${label} · Not linked yet`;
  const body =
    variant === 'full' ? (
      <>
        <Icon name={icon} size={16} />
        <span class="dlink-label">{href ? openLabel : label}</span>
        <span class="dlink-sub" aria-hidden="true">{href ? '↗' : 'Not linked yet'}</span>
      </>
    ) : (
      <Icon name={icon} size={15} />
    );
  return (
    <span class="dlink-wrap">
      {href ? (
        <a class={`dlink is-linked`} data-kind={kind} href={href} target="_blank" rel="noopener noreferrer" aria-label={a11y} aria-describedby={variant === 'compact' ? id : undefined} draggable={false}>
          {body}
        </a>
      ) : (
        <button
          type="button"
          class="dlink is-off"
          data-kind={kind}
          aria-disabled="true"
          aria-label={a11y}
          aria-describedby={variant === 'compact' ? id : undefined}
          onClick={(e) => {
            e.preventDefault();
            e.stopPropagation();
            setShown(true);
          }}
        >
          {body}
        </button>
      )}
      {variant === 'compact' && (
        <span class={`dlink-tip${shown ? ' is-shown' : ''}`} role="tooltip" id={id}>
          {tip}
        </span>
      )}
    </span>
  );
}
