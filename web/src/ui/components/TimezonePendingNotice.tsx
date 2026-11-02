import { useEffect, useState } from 'preact/hooks';
import { pendingTimezone } from '../../lib/timezone';

/**
 * Non-blocking notice for a zone change held back by unsaved edits. Rendered by the page
 * shell and again inside open drawers, since a modal dialog hides and inerts the page.
 */
export function TimezonePendingNotice() {
  const [pending, setPending] = useState<string | undefined>(pendingTimezone);
  useEffect(() => {
    const update = () => setPending(pendingTimezone());
    update();
    window.addEventListener('cseed-timezone-pending', update);
    return () => window.removeEventListener('cseed-timezone-pending', update);
  }, []);
  if (!pending) return null;
  return <p class="notice" role="status">Your time zone changed to {pending}; times will update after you save or close your form.</p>;
}
