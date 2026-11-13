-- Club support contacts: external contacts the club leans on (advisors, department staff).
-- They show under Board Members and are managed from the Contacts page.
ALTER TABLE contacts ADD COLUMN club_support INTEGER NOT NULL DEFAULT 0 CHECK(club_support IN (0,1));
