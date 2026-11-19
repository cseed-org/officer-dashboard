package api

import (
	"context"
	"officerdashboard/internal/store"
)

// membersList holds a read transaction: the generation and map are from the same
// snapshot. Cached maps are immutable. A newer/older snapshot simply replaces the
// cache; neither can cause another transaction to use a mismatched generation.
func (c *Ctx) cachedMemberEmails(ctx context.Context, q *store.Queries) (map[string]string, error) {
	generation, err := q.MemberGeneration(ctx)
	if err != nil {
		return nil, err
	}
	c.S.memberMu.Lock()
	defer c.S.memberMu.Unlock()
	if c.S.memberEmails != nil && c.S.memberGeneration == generation {
		return c.S.memberEmails, nil
	}
	emails, err := q.MemberIDsByEmail(ctx)
	if err != nil {
		return nil, err
	}
	c.S.memberEmails, c.S.memberGeneration = emails, generation
	return emails, nil
}
