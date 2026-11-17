import { useState } from 'preact/hooks';
import { api } from '../../lib/api/client';
import { useApi } from '../../lib/api/useApi';
import { fmtDate, relative } from '../../lib/format';
import { Section } from '../components/kit';
import { ErrorState, Loading } from '../components/states';

/** Settings → Connected agents: connect your own AI agent to /mcp, see and disconnect agents (docs/mcp.md). */
export function AgentsPanel() {
  const agents = useApi('agentsList');
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const [copied, setCopied] = useState('');
  const url = `${typeof location === 'undefined' ? '' : location.origin}/mcp`;
  const command = `claude mcp add --transport http cseed ${url}`;

  async function copy(text: string, which: string) {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(which);
    } catch {
      setCopied('');
    }
  }

  async function disconnect(id: string, name: string) {
    if (!confirm(`Disconnect ${name}? It stops working at once; connect it again any time.`)) return;
    setBusy(id);
    setError('');
    try {
      await api('agentDisconnect', { params: { id } });
      agents.reload();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy('');
    }
  }

  return (
    <Section title="connected agents" id="agents">
      <p class="small muted flush">
        Connect your own AI agent (Claude Code, Claude, Cursor, ChatGPT…) to work in the dashboard for you. It signs in as you and can do exactly what you can, nothing more; everything it changes shows in the audit log as you, via the agent.
      </p>
      <div class="stack" data-agents-connect>
        <div class="field">
          <span>MCP server URL</span>
          <span class="secret">
            <code data-mcp-url>{url}</code>
            <button type="button" class="btn" onClick={() => copy(url, 'url')}>
              {copied === 'url' ? 'copied ✓' : 'copy'}
            </button>
          </span>
        </div>
        <div class="field">
          <span>Claude Code</span>
          <span class="secret">
            <code>{command}</code>
            <button type="button" class="btn" onClick={() => copy(command, 'cmd')}>
              {copied === 'cmd' ? 'copied ✓' : 'copy'}
            </button>
          </span>
        </div>
        <p class="small muted flush">
          In Claude, Cursor or ChatGPT, add the URL as a custom connector / MCP server. Your browser opens this dashboard: sign in if asked, then choose <strong>Allow</strong>.
        </p>
      </div>
      {error && <p role="alert">{error}</p>}
      {agents.status === 'loading' ? (
        <Loading rows={2} label="Loading connected agents" />
      ) : agents.status === 'error' ? (
        <ErrorState error={agents.error} onRetry={agents.reload} />
      ) : agents.data.agents.length === 0 ? (
        <p class="small muted" data-agents-empty>No agents connected yet.</p>
      ) : (
        <ul class="list agents-list" data-agents-list>
          {agents.data.agents.map((a) => (
            <li class="list-row" key={a.id}>
              <span class="list-main">
                <strong>{a.clientName}</strong>
                <span class="small muted">
                  connected {fmtDate(a.connectedAt)} · {a.lastUsedAt ? `last used ${relative(a.lastUsedAt)}` : 'not used yet'}
                </span>
              </span>
              <span class="list-side">
                <button type="button" class="btn btn--danger" disabled={busy === a.id} onClick={() => disconnect(a.id, a.clientName)}>
                  {busy === a.id ? 'disconnecting…' : 'disconnect'}
                </button>
              </span>
            </li>
          ))}
        </ul>
      )}
    </Section>
  );
}
