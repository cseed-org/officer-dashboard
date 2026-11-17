package api

import (
	"errors"

	"officerdashboard/internal/store"
)

// ConnectedAgentDTO is the ConnectedAgent of src/core/contracts.ts: one MCP agent an
// officer connected (docs/mcp.md).
type ConnectedAgentDTO struct {
	ID          string  `json:"id"`
	ClientName  string  `json:"clientName"`
	ConnectedAt string  `json:"connectedAt"`
	LastUsedAt  *string `json:"lastUsedAt"`
}

// agentsList is GET /api/me/agents: the caller's connected agents.
func (c *Ctx) agentsList() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	gs, err := c.S.db.Read().UserGrants(c.Context(), u.ID, c.S.nowMs())
	if err != nil {
		return err
	}
	out := make([]ConnectedAgentDTO, len(gs))
	for i, g := range gs {
		out[i] = ConnectedAgentDTO{ID: g.ID, ClientName: g.ClientName, ConnectedAt: isoMs(g.CreatedAt), LastUsedAt: isoPtr(g.LastUsedAt)}
	}
	writeJSON(c.W, 200, map[string]any{"agents": out})
	return nil
}

// agentDisconnect is DELETE /api/me/agents/:id: revoke one of the caller's agents at once.
func (c *Ctx) agentDisconnect() error {
	u, err := c.officer()
	if err != nil {
		return err
	}
	ctx := c.Context()
	err = c.S.db.Write(ctx, func(w *store.Queries) error {
		g, err := w.GrantByID(ctx, c.R.PathValue("id"))
		if errors.Is(err, store.ErrNotFound) || (err == nil && (g.UserID != u.ID || g.RevokedAt != nil)) {
			return notFoundErr("Agent not found")
		}
		if err != nil {
			return err
		}
		if err := w.RevokeGrant(ctx, g.ID, c.S.nowMs()); err != nil {
			return err
		}
		return c.audit(ctx, w, "agent.disconnected", "agent", g.ID, map[string]any{"client": g.ClientName}, map[string]any{"revoked": true})
	})
	if err != nil {
		return err
	}
	writeJSON(c.W, 200, map[string]any{"ok": true})
	return nil
}
