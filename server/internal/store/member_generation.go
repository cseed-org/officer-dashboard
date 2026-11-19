package store

import "context"

func (q *Queries) MemberGeneration(ctx context.Context) (int64, error) {
	var generation int64
	err := q.q.QueryRowContext(ctx, `SELECT generation FROM members_generation WHERE id=1`).Scan(&generation)
	return generation, err
}
