ALTER TABLE buildspace_projects RENAME TO buildspace_launchpad_projects;
--> statement-breakpoint
ALTER TABLE buildspace_people RENAME TO buildspace_launchpad_people;
--> statement-breakpoint
ALTER TABLE buildspace_mentors RENAME TO buildspace_launchpad_mentors;
--> statement-breakpoint
ALTER TABLE buildspace_activity RENAME TO buildspace_launchpad_activity;
--> statement-breakpoint
ALTER TABLE buildspace_week_rules RENAME TO buildspace_launchpad_week_rules;
--> statement-breakpoint
DROP INDEX buildspace_activity_project;
--> statement-breakpoint
CREATE INDEX buildspace_launchpad_activity_project ON buildspace_launchpad_activity(project_id,date);
--> statement-breakpoint
DROP INDEX buildspace_activity_person;
--> statement-breakpoint
CREATE INDEX buildspace_launchpad_activity_person ON buildspace_launchpad_activity(person_id,date);
--> statement-breakpoint
DROP INDEX buildspace_activity_calendar;
--> statement-breakpoint
CREATE INDEX buildspace_launchpad_activity_calendar ON buildspace_launchpad_activity(kind,date);
