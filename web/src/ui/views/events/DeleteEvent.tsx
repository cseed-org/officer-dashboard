import { useState } from 'preact/hooks';
import { api } from '../../../lib/api/client';
import { useApi } from '../../../lib/api/useApi';
import { errorMessage, ErrorState, Loading } from '../../components/states';

export function DeleteEvent({ eventId, onDeleted, onCancel }: { eventId: string; onDeleted: (name: string) => void; onCancel: () => void }) {
  const detail = useApi('eventDetail', { params: { id: eventId } });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  if (detail.status === 'loading') return <Loading rows={2} label="Loading the event" />;
  if (detail.status === 'error') return <ErrorState error={detail.error} onRetry={detail.reload} />;
  const e = detail.data.event;
  const draft = e.status === 'draft';
  const remove = async () => {
    setBusy(true);
    setError(null);
    try {
      await api('eventDelete', { params: { id: e.id } });
      onDeleted(e.name);
    } catch (err) {
      setError(errorMessage(err));
      setBusy(false);
    }
  };
  return (
    <div class="stack" data-delete-dialog>
      <p>
        Delete <strong>{e.name}</strong> from the dashboard? Its guest list and reflections go with it. The published Luma event must be removed separately.
      </p>
      {error && (
        <p class="notice notice--negative" role="alert">
          {error}
        </p>
      )}
      <div class="action-row">
        <button class="btn" type="button" onClick={onCancel}>
          Cancel
        </button>
        <button class="btn btn--danger" type="button" onClick={remove} disabled={busy} data-confirm-delete>
          {busy ? 'Deleting…' : draft ? 'Delete draft' : 'Delete event'}
        </button>
      </div>
    </div>
  );
}
