package store

import "context"

// OnboardingProgress is where a user stands in the intro + intake flow.
func (q *Queries) OnboardingProgress(ctx context.Context, userID string) (version, step int, err error) {
	err = q.q.QueryRowContext(ctx, `SELECT onboarding_version,onboarding_step FROM users WHERE id=?`, userID).Scan(&version, &step)
	return version, step, notFound(err)
}

// SetUserOnboardingStep records the step to resume at.
func (q *Queries) SetUserOnboardingStep(ctx context.Context, userID string, step int) error {
	res, err := q.q.ExecContext(ctx, `UPDATE users SET onboarding_step=? WHERE id=?`, step, userID)
	return oneRow(res, err)
}

// SetOnboardingVersion marks the flow of that version as finished (version 0 replays it).
func (q *Queries) SetOnboardingVersion(ctx context.Context, userID string, version int) error {
	res, err := q.q.ExecContext(ctx, `UPDATE users SET onboarding_version=?,onboarding_step=1 WHERE id=?`, version, userID)
	return oneRow(res, err)
}

// SetStartTerm is the admin edit of an officer's start term.
func (q *Queries) SetStartTerm(ctx context.Context, userID string, term *string) error {
	res, err := q.q.ExecContext(ctx, `UPDATE officer_profiles SET start_term=? WHERE user_id=?`, term, userID)
	return oneRow(res, err)
}
