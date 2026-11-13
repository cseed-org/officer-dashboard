/**
 * The officer team / committee choices on the intake form (profile.team). There is no team
 * table to read them from (subteams are assigned by invite, not chosen), so the list lives
 * here, in one place. A value saved earlier that is not in the list still shows as chosen.
 */
export const OFFICER_TEAMS = ['Executive board', 'Events', 'Finance', 'Marketing and outreach', 'Design', 'Build', 'Operations', 'Membership and community'] as const;
