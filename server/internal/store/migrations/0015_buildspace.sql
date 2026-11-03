CREATE TABLE buildspace_projects (
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL,
 description TEXT NOT NULL DEFAULT '',
 track TEXT NOT NULL CHECK(track IN ('Software','Hardware','Creative','Extended Virtual Reality (XR)')),
 stage TEXT NOT NULL CHECK(stage IN ('Ideating','Building','Demo-ready','Shipped','At risk')),
 links TEXT NOT NULL DEFAULT '[]',
 updated_at INTEGER NOT NULL
);
--> statement-breakpoint
CREATE TABLE buildspace_people (
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL,
 member_id TEXT REFERENCES members_mirror(person_id) ON DELETE SET NULL,
 discord_id TEXT UNIQUE,
 project_id TEXT NOT NULL REFERENCES buildspace_projects(id),
 joined_on TEXT NOT NULL
);
--> statement-breakpoint
CREATE TABLE buildspace_mentors (
 project_id TEXT NOT NULL REFERENCES buildspace_projects(id) ON DELETE CASCADE,
 contact_id TEXT NOT NULL REFERENCES contacts(id),
 PRIMARY KEY(project_id,contact_id)
);
--> statement-breakpoint
CREATE TABLE buildspace_activity (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES buildspace_projects(id),
 person_id TEXT REFERENCES buildspace_people(id),
 mentor_id TEXT REFERENCES contacts(id),
 kind TEXT NOT NULL CHECK(kind IN ('weekly','mentor','request','purchase','absence')),
 date TEXT NOT NULL,
 body TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('open','resolved','on-track','needs-help')),
 purchase_id TEXT REFERENCES purchase_requests(id),
 source TEXT NOT NULL CHECK(source IN ('manual','discord')),
 external_id TEXT UNIQUE,
 created_at INTEGER NOT NULL,
 CHECK(kind != 'weekly' OR person_id IS NOT NULL),
 CHECK(kind != 'mentor' OR mentor_id IS NOT NULL)
);
--> statement-breakpoint
CREATE INDEX buildspace_activity_project ON buildspace_activity(project_id,date);
--> statement-breakpoint
CREATE INDEX buildspace_activity_person ON buildspace_activity(person_id,date);
